// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// BucketSizeTracking is one row of bucket_size_tracking. The table only
// records explicit choices — absence of a row means "tracked" (the
// proxy-side default). Disabling writes Enabled=false; re-enabling
// writes Enabled=true rather than deleting, so the audit trail
// (UpdatedAt/UpdatedBy) stays intact.
type BucketSizeTracking struct {
	BackendID string
	Bucket    string
	Enabled   bool
	UpdatedAt time.Time
	UpdatedBy string
}

// IsBucketSizeTracked returns whether size tracking is on for this bucket.
// The default is true — only an explicit Enabled=false row turns it off.
func (s *Store) IsBucketSizeTracked(ctx context.Context, backendID, bucket string) (bool, error) {
	var enabled int
	err := s.R.QueryRowContext(ctx,
		`SELECT enabled FROM bucket_size_tracking WHERE backend_id = ? AND bucket = ?`,
		backendID, bucket,
	).Scan(&enabled)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return true, nil
		}
		return true, err
	}
	return enabled != 0, nil
}

// SetBucketSizeTracking upserts the row. Writing enabled=true is preferred
// over deleting the row so the audit metadata is retained.
func (s *Store) SetBucketSizeTracking(ctx context.Context, t *BucketSizeTracking) error {
	enabled := 0
	if t.Enabled {
		enabled = 1
	}
	_, err := s.DB.ExecContext(ctx, `
INSERT INTO bucket_size_tracking (backend_id, bucket, enabled, updated_at, updated_by)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT (backend_id, bucket) DO UPDATE SET
  enabled    = excluded.enabled,
  updated_at = excluded.updated_at,
  updated_by = excluded.updated_by`,
		t.BackendID, t.Bucket, enabled, t.UpdatedAt, t.UpdatedBy)
	return err
}

// ListDisabledSizeTracking returns every bucket with an explicit
// Enabled=false row. The scanner uses this to filter out opted-out
// buckets without having to query per-bucket.
func (s *Store) ListDisabledSizeTracking(ctx context.Context) (map[string]struct{}, error) {
	rows, err := s.R.QueryContext(ctx,
		`SELECT backend_id, bucket FROM bucket_size_tracking WHERE enabled = 0`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]struct{})
	for rows.Next() {
		var bid, bucket string
		if err := rows.Scan(&bid, &bucket); err != nil {
			return nil, err
		}
		out[bid+"/"+bucket] = struct{}{}
	}
	return out, rows.Err()
}
