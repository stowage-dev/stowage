// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package sqlite

import (
	"context"
	"time"
)

// BucketPin is one row of bucket_pins. Pins are per-user favorites
// surfaced in the sidebar; insertion is idempotent so re-pinning is a no-op.
type BucketPin struct {
	UserID    string
	BackendID string
	Bucket    string
	CreatedAt time.Time
}

func (s *Store) ListPinsByUser(ctx context.Context, userID string) ([]*BucketPin, error) {
	rows, err := s.R.QueryContext(ctx, `
SELECT user_id, backend_id, bucket, created_at
FROM bucket_pins
WHERE user_id = ?
ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*BucketPin
	for rows.Next() {
		var p BucketPin
		if err := rows.Scan(&p.UserID, &p.BackendID, &p.Bucket, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &p)
	}
	return out, rows.Err()
}

// InsertPin is idempotent — re-pinning the same (user, backend, bucket)
// silently leaves the original timestamp in place so the sidebar order
// doesn't churn.
func (s *Store) InsertPin(ctx context.Context, p *BucketPin) error {
	_, err := s.DB.ExecContext(ctx, `
INSERT INTO bucket_pins (user_id, backend_id, bucket, created_at)
VALUES (?, ?, ?, ?)
ON CONFLICT (user_id, backend_id, bucket) DO NOTHING`,
		p.UserID, p.BackendID, p.Bucket, p.CreatedAt)
	return err
}

func (s *Store) DeletePin(ctx context.Context, userID, backendID, bucket string) error {
	_, err := s.DB.ExecContext(ctx,
		`DELETE FROM bucket_pins WHERE user_id = ? AND backend_id = ? AND bucket = ?`,
		userID, backendID, bucket)
	return err
}
