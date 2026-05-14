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

// S3BucketCORS is one row of the s3_bucket_cors table — the CORS rules
// the embedded proxy enforces for (backend_id, bucket). Rules is the raw
// JSON array; the proxy decodes it once during Reload and serves
// preflights from the in-memory cache.
type S3BucketCORS struct {
	BackendID string
	Bucket    string
	Rules     string // JSON array — opaque to the store layer
	CreatedAt time.Time
	UpdatedAt time.Time
}

var ErrS3BucketCORSNotFound = errors.New("s3 bucket CORS not found")

const s3BucketCORSCols = `backend_id, bucket, rules, created_at, updated_at`

func scanS3BucketCORS(row interface{ Scan(...any) error }) (*S3BucketCORS, error) {
	var c S3BucketCORS
	if err := row.Scan(
		&c.BackendID, &c.Bucket, &c.Rules, &c.CreatedAt, &c.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return &c, nil
}

// UpsertS3BucketCORS inserts or updates the rules for (backend_id, bucket).
// CreatedAt is preserved on update; UpdatedAt is always overwritten.
func (s *Store) UpsertS3BucketCORS(ctx context.Context, c *S3BucketCORS) error {
	_, err := s.DB.ExecContext(ctx, `
INSERT INTO s3_bucket_cors (`+s3BucketCORSCols+`)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(backend_id, bucket) DO UPDATE SET
  rules      = excluded.rules,
  updated_at = excluded.updated_at`,
		c.BackendID, c.Bucket, c.Rules, c.CreatedAt, c.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("s3_bucket_cors upsert: %w", err)
	}
	return nil
}

func (s *Store) GetS3BucketCORS(ctx context.Context, backendID, bucket string) (*S3BucketCORS, error) {
	row := s.R.QueryRowContext(ctx,
		`SELECT `+s3BucketCORSCols+` FROM s3_bucket_cors WHERE backend_id = ? AND bucket = ?`,
		backendID, bucket)
	c, err := scanS3BucketCORS(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrS3BucketCORSNotFound
	}
	return c, err
}

// ListS3BucketCORS returns every row, ordered by (backend_id, bucket). Used
// by the proxy's SQLiteSource to prime its bucket-keyed CORS cache.
func (s *Store) ListS3BucketCORS(ctx context.Context) ([]*S3BucketCORS, error) {
	rows, err := s.R.QueryContext(ctx,
		`SELECT `+s3BucketCORSCols+` FROM s3_bucket_cors ORDER BY backend_id, bucket`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*S3BucketCORS
	for rows.Next() {
		c, err := scanS3BucketCORS(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) DeleteS3BucketCORS(ctx context.Context, backendID, bucket string) error {
	res, err := s.DB.ExecContext(ctx,
		`DELETE FROM s3_bucket_cors WHERE backend_id = ? AND bucket = ?`,
		backendID, bucket)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrS3BucketCORSNotFound
	}
	return nil
}
