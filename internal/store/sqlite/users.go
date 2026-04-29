// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

type User struct {
	ID             string
	Username       string
	Email          sql.NullString
	PasswordHash   string
	Role           string
	IdentitySource string
	Enabled        bool
	MustChangePW   bool
	FailedAttempts int
	LockedUntil    sql.NullTime
	CreatedAt      time.Time
	CreatedBy      sql.NullString
	LastLoginAt    sql.NullTime
	PWChangedAt    time.Time
	// OIDCSubject is the immutable 'sub' claim from the issuer. NULL for
	// local/static users and for OIDC rows created before migration v6.
	OIDCSubject sql.NullString
}

var ErrUserNotFound = errors.New("user not found")
var ErrUsernameTaken = errors.New("username already taken")
var ErrEmailTaken = errors.New("email already taken")
var ErrOIDCSubjectTaken = errors.New("oidc subject already mapped to another user")

const userCols = `id, username, email, password_hash, role, identity_source,
 enabled, must_change_pw, failed_attempts, locked_until,
 created_at, created_by, last_login_at, pw_changed_at, oidc_subject`

func scanUser(row interface {
	Scan(dest ...any) error
}) (*User, error) {
	var u User
	var enabled, mustChange int
	if err := row.Scan(
		&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.Role, &u.IdentitySource,
		&enabled, &mustChange, &u.FailedAttempts, &u.LockedUntil,
		&u.CreatedAt, &u.CreatedBy, &u.LastLoginAt, &u.PWChangedAt, &u.OIDCSubject,
	); err != nil {
		return nil, err
	}
	u.Enabled = enabled != 0
	u.MustChangePW = mustChange != 0
	return &u, nil
}

func (s *Store) CreateUser(ctx context.Context, u *User) error {
	_, err := s.DB.ExecContext(ctx, `
INSERT INTO users (`+userCols+`)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0, NULL, ?, ?, NULL, ?, ?)`,
		u.ID, u.Username, u.Email, u.PasswordHash, u.Role, u.IdentitySource,
		boolToInt(u.Enabled), boolToInt(u.MustChangePW),
		u.CreatedAt, u.CreatedBy, u.PWChangedAt, u.OIDCSubject,
	)
	if err != nil {
		return translateUserErr(err)
	}
	return nil
}

func (s *Store) GetUserByID(ctx context.Context, id string) (*User, error) {
	row := s.R.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE id = ?`, id)
	u, err := scanUser(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	return u, err
}

func (s *Store) GetUserByUsername(ctx context.Context, username string) (*User, error) {
	row := s.R.QueryRowContext(ctx,
		`SELECT `+userCols+` FROM users WHERE username = ? COLLATE NOCASE`, username)
	u, err := scanUser(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	return u, err
}

// GetUserByOIDCSubject looks up a user by the immutable (identity_source, sub)
// pair the IdP issues. Used during OIDC callback to bind a session to the
// stable identifier rather than the mutable preferred_username.
func (s *Store) GetUserByOIDCSubject(ctx context.Context, identitySource, sub string) (*User, error) {
	row := s.R.QueryRowContext(ctx,
		`SELECT `+userCols+` FROM users
		 WHERE identity_source = ? AND oidc_subject = ?`,
		identitySource, sub)
	u, err := scanUser(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	return u, err
}

// RenameUser changes the username on an existing row. Used when an OIDC IdP
// renames a user upstream — the row is keyed by (source, sub) so the rename
// is safe. Returns ErrUsernameTaken on collision; callers may ignore.
func (s *Store) RenameUser(ctx context.Context, id, newUsername string) error {
	res, err := s.DB.ExecContext(ctx,
		`UPDATE users SET username = ? WHERE id = ?`,
		newUsername, id)
	if err != nil {
		return translateUserErr(err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrUserNotFound
	}
	return nil
}

// SetOIDCSubject records the IdP-issued sub on an existing user row. Used to
// upgrade a pre-migration-v6 OIDC user the first time they sign in after the
// upgrade — we trust the username-based lookup once and pin the sub from
// then on.
func (s *Store) SetOIDCSubject(ctx context.Context, id, sub string) error {
	res, err := s.DB.ExecContext(ctx,
		`UPDATE users SET oidc_subject = ? WHERE id = ?`,
		sql.NullString{String: sub, Valid: sub != ""}, id)
	if err != nil {
		return translateUserErr(err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrUserNotFound
	}
	return nil
}

type UserListFilter struct {
	Query   string // substring match on username/email
	Role    string
	Enabled *bool
	Source  string
	Limit   int
	Offset  int
}

func (s *Store) ListUsers(ctx context.Context, f UserListFilter) ([]*User, error) {
	var clauses []string
	var args []any
	if f.Query != "" {
		clauses = append(clauses, "(username LIKE ? OR COALESCE(email,'') LIKE ?)")
		like := "%" + f.Query + "%"
		args = append(args, like, like)
	}
	if f.Role != "" {
		clauses = append(clauses, "role = ?")
		args = append(args, f.Role)
	}
	if f.Enabled != nil {
		clauses = append(clauses, "enabled = ?")
		args = append(args, boolToInt(*f.Enabled))
	}
	if f.Source != "" {
		clauses = append(clauses, "identity_source = ?")
		args = append(args, f.Source)
	}
	where := ""
	if len(clauses) > 0 {
		where = " WHERE " + strings.Join(clauses, " AND ")
	}
	limit := f.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	args = append(args, limit, f.Offset)
	q := `SELECT ` + userCols + ` FROM users` + where + ` ORDER BY username LIMIT ? OFFSET ?`

	rows, err := s.R.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

type UserPatch struct {
	Role         *string
	Enabled      *bool
	Email        *sql.NullString
	Unlock       bool
	MustChangePW *bool
}

func (s *Store) UpdateUser(ctx context.Context, id string, p UserPatch) error {
	var sets []string
	var args []any
	if p.Role != nil {
		sets = append(sets, "role = ?")
		args = append(args, *p.Role)
	}
	if p.Enabled != nil {
		sets = append(sets, "enabled = ?")
		args = append(args, boolToInt(*p.Enabled))
	}
	if p.Email != nil {
		sets = append(sets, "email = ?")
		args = append(args, *p.Email)
	}
	if p.Unlock {
		sets = append(sets, "locked_until = NULL", "failed_attempts = 0")
	}
	if p.MustChangePW != nil {
		sets = append(sets, "must_change_pw = ?")
		args = append(args, boolToInt(*p.MustChangePW))
	}
	if len(sets) == 0 {
		return nil
	}
	args = append(args, id)
	res, err := s.DB.ExecContext(ctx,
		`UPDATE users SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...)
	if err != nil {
		return translateUserErr(err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrUserNotFound
	}
	return nil
}

func (s *Store) SetPasswordHash(ctx context.Context, id, hash string, mustChange bool) error {
	res, err := s.DB.ExecContext(ctx, `
UPDATE users
SET password_hash = ?, must_change_pw = ?, pw_changed_at = ?, failed_attempts = 0, locked_until = NULL
WHERE id = ?`,
		hash, boolToInt(mustChange), time.Now().UTC(), id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrUserNotFound
	}
	return nil
}

func (s *Store) RecordLoginSuccess(ctx context.Context, id string) error {
	_, err := s.DB.ExecContext(ctx,
		`UPDATE users SET last_login_at = ?, failed_attempts = 0, locked_until = NULL WHERE id = ?`,
		time.Now().UTC(), id)
	return err
}

// RecordLoginFailure increments failed_attempts. If it reaches maxAttempts,
// locked_until is set to now + window. Returns the new attempt count and
// whether the account is now locked.
func (s *Store) RecordLoginFailure(ctx context.Context, id string, maxAttempts int, window time.Duration) (int, bool, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, false, err
	}
	defer tx.Rollback() //nolint:errcheck

	var attempts int
	if err := tx.QueryRowContext(ctx,
		`SELECT failed_attempts FROM users WHERE id = ?`, id).Scan(&attempts); err != nil {
		return 0, false, err
	}
	attempts++

	var lockedUntil sql.NullTime
	locked := false
	if attempts >= maxAttempts {
		lockedUntil = sql.NullTime{Time: time.Now().UTC().Add(window), Valid: true}
		locked = true
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE users SET failed_attempts = ?, locked_until = ? WHERE id = ?`,
		attempts, lockedUntil, id); err != nil {
		return 0, false, err
	}
	return attempts, locked, tx.Commit()
}

func (s *Store) DeleteUser(ctx context.Context, id string) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrUserNotFound
	}
	return nil
}

// CountEnabledAdmins returns the number of users with role='admin' and
// enabled=1. Used by the API layer to refuse a patch/delete that would leave
// the system without any active admin.
func (s *Store) CountEnabledAdmins(ctx context.Context) (int, error) {
	var n int
	err := s.R.QueryRowContext(ctx,
		`SELECT COUNT(1) FROM users WHERE role = 'admin' AND enabled = 1`,
	).Scan(&n)
	return n, err
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func translateUserErr(err error) error {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "users_username_unique"):
		return ErrUsernameTaken
	case strings.Contains(msg, "users_email_unique"):
		return ErrEmailTaken
	case strings.Contains(msg, "users_oidc_subject_unique"):
		return ErrOIDCSubjectTaken
	default:
		return fmt.Errorf("users: %w", err)
	}
}
