// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// S3AnonymousBinding is one row of the s3_anonymous_bindings table — a
// bucket exposed for unauthenticated S3 reads through the proxy. Mode is
// presently always "ReadOnly" (only safe default); per_source_ip_rps is
// enforced by the proxy's IPLimiter.
type S3AnonymousBinding struct {
	BackendID      string
	Bucket         string
	Mode           string
	PerSourceIPRPS int
	CreatedAt      time.Time
	CreatedBy      sql.NullString
}

var ErrS3AnonymousBindingNotFound = errors.New("s3 anonymous binding not found")

const s3AnonCols = `backend_id, bucket, mode, per_source_ip_rps, created_at, created_by`

func scanS3AnonymousBinding(row interface{ Scan(...any) error }) (*S3AnonymousBinding, error) {
	var b S3AnonymousBinding
	if err := row.Scan(
		&b.BackendID, &b.Bucket, &b.Mode, &b.PerSourceIPRPS, &b.CreatedAt, &b.CreatedBy,
	); err != nil {
		return nil, err
	}
	return &b, nil
}

// UpsertS3AnonymousBinding inserts or replaces the binding for
// (backend_id, bucket). Replacing preserves the original CreatedAt/CreatedBy
// — the binding is conceptually the same row even if its mode or RPS
// changed.
func (s *Store) UpsertS3AnonymousBinding(ctx context.Context, b *S3AnonymousBinding) error {
	_, err := s.DB.ExecContext(ctx, `
INSERT INTO s3_anonymous_bindings (`+s3AnonCols+`)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(backend_id, bucket) DO UPDATE SET
  mode              = excluded.mode,
  per_source_ip_rps = excluded.per_source_ip_rps`,
		b.BackendID, b.Bucket, b.Mode, b.PerSourceIPRPS, b.CreatedAt, b.CreatedBy,
	)
	if err != nil {
		return fmt.Errorf("s3_anonymous_bindings upsert: %w", err)
	}
	return nil
}

func (s *Store) GetS3AnonymousBinding(ctx context.Context, backendID, bucket string) (*S3AnonymousBinding, error) {
	row := s.R.QueryRowContext(ctx,
		`SELECT `+s3AnonCols+` FROM s3_anonymous_bindings WHERE backend_id = ? AND bucket = ?`,
		backendID, bucket)
	b, err := scanS3AnonymousBinding(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrS3AnonymousBindingNotFound
	}
	return b, err
}

// ListS3AnonymousBindings returns every row, ordered by (backend_id, bucket).
func (s *Store) ListS3AnonymousBindings(ctx context.Context) ([]*S3AnonymousBinding, error) {
	rows, err := s.R.QueryContext(ctx,
		`SELECT `+s3AnonCols+` FROM s3_anonymous_bindings ORDER BY backend_id, bucket`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*S3AnonymousBinding
	for rows.Next() {
		b, err := scanS3AnonymousBinding(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *Store) DeleteS3AnonymousBinding(ctx context.Context, backendID, bucket string) error {
	res, err := s.DB.ExecContext(ctx,
		`DELETE FROM s3_anonymous_bindings WHERE backend_id = ? AND bucket = ?`,
		backendID, bucket)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrS3AnonymousBindingNotFound
	}
	return nil
}
