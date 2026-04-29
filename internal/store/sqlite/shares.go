// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Share is one row of the shares table. See migration v2.
//
// Nullable-shaped fields: ExpiresAt, MaxDownloads, RevokedAt, LastAccessedAt.
// PasswordHash is modelled as an empty string rather than NULL because the
// hash format ("$argon2id$…") never starts empty — simpler querying.
type Share struct {
	ID             string
	Code           string
	BackendID      string
	Bucket         string
	Key            string
	CreatedBy      string
	CreatedAt      time.Time
	ExpiresAt      sql.NullTime
	PasswordHash   string
	MaxDownloads   sql.NullInt64
	DownloadCount  int64
	Revoked        bool
	RevokedAt      sql.NullTime
	LastAccessedAt sql.NullTime
	Disposition    string
}

var (
	ErrShareNotFound  = errors.New("share not found")
	ErrShareCodeTaken = errors.New("share code already exists")
)

const shareCols = `id, code, backend_id, bucket, object_key, created_by,
 created_at, expires_at, password_hash, max_downloads, download_count,
 revoked, revoked_at, last_accessed_at, disposition`

func scanShare(row interface{ Scan(dest ...any) error }) (*Share, error) {
	var s Share
	var revoked int
	if err := row.Scan(
		&s.ID, &s.Code, &s.BackendID, &s.Bucket, &s.Key, &s.CreatedBy,
		&s.CreatedAt, &s.ExpiresAt, &s.PasswordHash, &s.MaxDownloads, &s.DownloadCount,
		&revoked, &s.RevokedAt, &s.LastAccessedAt, &s.Disposition,
	); err != nil {
		return nil, err
	}
	s.Revoked = revoked != 0
	return &s, nil
}

// InsertShare persists a new share row. Callers must populate ID, Code,
// CreatedAt. If Code already exists returns ErrShareCodeTaken — callers are
// expected to retry with a freshly-minted code on collision.
func (s *Store) InsertShare(ctx context.Context, sh *Share) error {
	_, err := s.DB.ExecContext(ctx, `
INSERT INTO shares (`+shareCols+`)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, 0, NULL, NULL, ?)`,
		sh.ID, sh.Code, sh.BackendID, sh.Bucket, sh.Key, sh.CreatedBy,
		sh.CreatedAt, sh.ExpiresAt, sh.PasswordHash, sh.MaxDownloads,
		sh.Disposition,
	)
	if err != nil && isUniqueViolation(err) {
		return ErrShareCodeTaken
	}
	return err
}

func (s *Store) GetShareByCode(ctx context.Context, code string) (*Share, error) {
	row := s.R.QueryRowContext(ctx,
		`SELECT `+shareCols+` FROM shares WHERE code = ?`, code)
	sh, err := scanShare(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrShareNotFound
	}
	return sh, err
}

func (s *Store) GetShareByID(ctx context.Context, id string) (*Share, error) {
	row := s.R.QueryRowContext(ctx,
		`SELECT `+shareCols+` FROM shares WHERE id = ?`, id)
	sh, err := scanShare(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrShareNotFound
	}
	return sh, err
}

func (s *Store) ListSharesByUser(ctx context.Context, userID string) ([]*Share, error) {
	rows, err := s.R.QueryContext(ctx,
		`SELECT `+shareCols+` FROM shares WHERE created_by = ? ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	return collectShares(rows)
}

func (s *Store) ListAllShares(ctx context.Context) ([]*Share, error) {
	rows, err := s.R.QueryContext(ctx,
		`SELECT `+shareCols+` FROM shares ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	return collectShares(rows)
}

func collectShares(rows *sql.Rows) ([]*Share, error) {
	defer rows.Close()
	var out []*Share
	for rows.Next() {
		sh, err := scanShare(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sh)
	}
	return out, rows.Err()
}

// RevokeShare flips the revoked flag. Idempotent — revoking an already-
// revoked share is a no-op but still returns nil.
func (s *Store) RevokeShare(ctx context.Context, id string, revokedAt time.Time) error {
	res, err := s.DB.ExecContext(ctx,
		`UPDATE shares SET revoked = 1, revoked_at = ? WHERE id = ?`,
		revokedAt, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrShareNotFound
	}
	return nil
}

// IncrementShareAccess atomically bumps the download counter and stamps
// last_accessed_at. The UPDATE also enforces max_downloads at the DB level
// so concurrent resolvers can't race past the cap.
//
// Returns ErrShareNotFound if the row is missing OR the cap is already met —
// callers should treat both as "410 Gone".
func (s *Store) IncrementShareAccess(ctx context.Context, id string, at time.Time) error {
	res, err := s.DB.ExecContext(ctx, `
UPDATE shares
SET download_count = download_count + 1,
    last_accessed_at = ?
WHERE id = ?
  AND revoked = 0
  AND (max_downloads IS NULL OR download_count < max_downloads)
  AND (expires_at IS NULL OR expires_at > ?)`,
		at, id, at)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrShareNotFound
	}
	return nil
}

// isUniqueViolation detects SQLite's UNIQUE constraint error via message
// matching — modernc.org/sqlite doesn't expose a typed error here.
func isUniqueViolation(err error) bool {
	return err != nil && containsAll(err.Error(), "UNIQUE", "constraint")
}

func containsAll(s string, subs ...string) bool {
	for _, x := range subs {
		found := false
		for i := 0; i+len(x) <= len(s); i++ {
			if s[i:i+len(x)] == x {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
