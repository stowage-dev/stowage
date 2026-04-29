// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// BucketQuota maps to one row of bucket_quotas. SoftBytes and HardBytes
// are both stored as INTEGER; 0 means "unset" for that level. A row's
// existence implies at least one of the two is non-zero (enforced by the
// upsert path, not the schema, so admins can clear by deleting the row).
type BucketQuota struct {
	BackendID string
	Bucket    string
	SoftBytes int64
	HardBytes int64
	UpdatedAt time.Time
	UpdatedBy string
}

var ErrQuotaNotFound = errors.New("bucket quota not configured")

func (s *Store) GetQuota(ctx context.Context, backendID, bucket string) (*BucketQuota, error) {
	row := s.R.QueryRowContext(ctx, `
SELECT backend_id, bucket, soft_bytes, hard_bytes, updated_at, updated_by
FROM bucket_quotas WHERE backend_id = ? AND bucket = ?`, backendID, bucket)
	var q BucketQuota
	if err := row.Scan(&q.BackendID, &q.Bucket, &q.SoftBytes, &q.HardBytes, &q.UpdatedAt, &q.UpdatedBy); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrQuotaNotFound
		}
		return nil, err
	}
	return &q, nil
}

// UpsertQuota writes or replaces the row.
func (s *Store) UpsertQuota(ctx context.Context, q *BucketQuota) error {
	_, err := s.DB.ExecContext(ctx, `
INSERT INTO bucket_quotas (backend_id, bucket, soft_bytes, hard_bytes, updated_at, updated_by)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT (backend_id, bucket) DO UPDATE SET
  soft_bytes = excluded.soft_bytes,
  hard_bytes = excluded.hard_bytes,
  updated_at = excluded.updated_at,
  updated_by = excluded.updated_by`,
		q.BackendID, q.Bucket, q.SoftBytes, q.HardBytes, q.UpdatedAt, q.UpdatedBy)
	return err
}

func (s *Store) DeleteQuota(ctx context.Context, backendID, bucket string) error {
	_, err := s.DB.ExecContext(ctx,
		`DELETE FROM bucket_quotas WHERE backend_id = ? AND bucket = ?`, backendID, bucket)
	return err
}

// ListAllQuotas returns every configured row — used by the scanner so it
// only spends API calls on buckets the admin actually cares about.
func (s *Store) ListAllQuotas(ctx context.Context) ([]*BucketQuota, error) {
	rows, err := s.R.QueryContext(ctx, `
SELECT backend_id, bucket, soft_bytes, hard_bytes, updated_at, updated_by
FROM bucket_quotas ORDER BY backend_id, bucket`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*BucketQuota
	for rows.Next() {
		var q BucketQuota
		if err := rows.Scan(&q.BackendID, &q.Bucket, &q.SoftBytes, &q.HardBytes, &q.UpdatedAt, &q.UpdatedBy); err != nil {
			return nil, err
		}
		out = append(out, &q)
	}
	return out, rows.Err()
}
