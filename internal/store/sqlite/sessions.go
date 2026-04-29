// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type SessionFlags int

const (
	FlagMustChangePW SessionFlags = 1 << 0
)

type Session struct {
	ID             string
	UserID         string
	IdentitySource string
	CSRFToken      string
	IP             string
	UserAgent      string
	Flags          SessionFlags
	CreatedAt      time.Time
	LastSeenAt     time.Time
	ExpiresAt      time.Time
}

var ErrSessionNotFound = errors.New("session not found")

func (s *Store) CreateSession(ctx context.Context, sess *Session) error {
	_, err := s.DB.ExecContext(ctx, `
INSERT INTO sessions (id, user_id, identity_source, csrf_token, ip, user_agent, flags, created_at, last_seen_at, expires_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sess.ID, sess.UserID, sess.IdentitySource, sess.CSRFToken,
		sess.IP, sess.UserAgent, int(sess.Flags),
		sess.CreatedAt, sess.LastSeenAt, sess.ExpiresAt,
	)
	return err
}

func (s *Store) GetSession(ctx context.Context, id string) (*Session, error) {
	row := s.R.QueryRowContext(ctx, `
SELECT id, user_id, identity_source, csrf_token, ip, user_agent, flags, created_at, last_seen_at, expires_at
FROM sessions WHERE id = ?`, id)

	var sess Session
	var flags int
	if err := row.Scan(
		&sess.ID, &sess.UserID, &sess.IdentitySource, &sess.CSRFToken,
		&sess.IP, &sess.UserAgent, &flags,
		&sess.CreatedAt, &sess.LastSeenAt, &sess.ExpiresAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrSessionNotFound
		}
		return nil, err
	}
	sess.Flags = SessionFlags(flags)
	return &sess, nil
}

// TouchSession updates last_seen_at and extends expires_at by lifetime if the
// session is still valid. Returns the updated session.
func (s *Store) TouchSession(ctx context.Context, id string, now, expiresAt time.Time) error {
	_, err := s.DB.ExecContext(ctx,
		`UPDATE sessions SET last_seen_at = ?, expires_at = ? WHERE id = ?`,
		now, expiresAt, id)
	return err
}

func (s *Store) ClearSessionFlags(ctx context.Context, id string, mask SessionFlags) error {
	_, err := s.DB.ExecContext(ctx,
		`UPDATE sessions SET flags = flags & ? WHERE id = ?`, int(^mask), id)
	return err
}

func (s *Store) DeleteSession(ctx context.Context, id string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id)
	return err
}

// DeleteUserSessions removes every session for a given user. Optionally
// preserves one session id (e.g. the caller's own when changing password).
func (s *Store) DeleteUserSessions(ctx context.Context, userID, keepID string) error {
	if keepID == "" {
		_, err := s.DB.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, userID)
		return err
	}
	_, err := s.DB.ExecContext(ctx,
		`DELETE FROM sessions WHERE user_id = ? AND id <> ?`, userID, keepID)
	return err
}

func (s *Store) PurgeExpiredSessions(ctx context.Context, now time.Time) (int64, error) {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at < ?`, now)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
