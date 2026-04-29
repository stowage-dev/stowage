// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package sqlite

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

// AuditEvent is one row of audit_events. ID is monotonic; the audit trail
// is append-only.
type AuditEvent struct {
	ID         int64
	Timestamp  time.Time
	UserID     sql.NullString
	Action     string
	BackendID  sql.NullString
	Bucket     sql.NullString
	ObjectKey  sql.NullString
	RequestID  sql.NullString
	IP         sql.NullString
	UserAgent  sql.NullString
	Status     string
	DetailJSON sql.NullString
}

// AuditFilter narrows ListAuditEvents. Empty fields are ignored. Limit is
// capped at 1000 by the query path so a wide-open filter can't blow up
// memory.
type AuditFilter struct {
	UserID    string
	Action    string
	BackendID string
	Bucket    string
	Status    string
	Since     time.Time
	Until     time.Time
	Limit     int
	Offset    int
}

const auditCols = `id, ts, user_id, action, backend_id, bucket, object_key,
 request_id, ip, user_agent, status, detail`

// InsertAuditEvent appends an event. Caller sets Timestamp + Action;
// everything else is optional.
func (s *Store) InsertAuditEvent(ctx context.Context, e *AuditEvent) error {
	_, err := s.DB.ExecContext(ctx, auditInsertSQL,
		e.Timestamp, e.UserID, e.Action, e.BackendID, e.Bucket, e.ObjectKey,
		e.RequestID, e.IP, e.UserAgent, e.Status, e.DetailJSON)
	return err
}

// InsertAuditEvents appends a slice of events in a single transaction.
// Empty input is a no-op. Used by the async batch drainer; equivalent to
// N InsertAuditEvent calls but folds them into one fsync at commit.
func (s *Store) InsertAuditEvents(ctx context.Context, events []*AuditEvent) error {
	if len(events) == 0 {
		return nil
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, auditInsertSQL)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	for _, e := range events {
		if _, err := stmt.ExecContext(ctx,
			e.Timestamp, e.UserID, e.Action, e.BackendID, e.Bucket, e.ObjectKey,
			e.RequestID, e.IP, e.UserAgent, e.Status, e.DetailJSON,
		); err != nil {
			_ = stmt.Close()
			_ = tx.Rollback()
			return err
		}
	}
	if err := stmt.Close(); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

const auditInsertSQL = `
INSERT INTO audit_events (ts, user_id, action, backend_id, bucket, object_key,
  request_id, ip, user_agent, status, detail)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

// ListAuditEvents returns events newest-first matching the filter.
func (s *Store) ListAuditEvents(ctx context.Context, f AuditFilter) ([]*AuditEvent, error) {
	q, args := buildAuditQuery(f, false)
	rows, err := s.R.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*AuditEvent
	for rows.Next() {
		var e AuditEvent
		if err := rows.Scan(
			&e.ID, &e.Timestamp, &e.UserID, &e.Action, &e.BackendID, &e.Bucket, &e.ObjectKey,
			&e.RequestID, &e.IP, &e.UserAgent, &e.Status, &e.DetailJSON,
		); err != nil {
			return nil, err
		}
		out = append(out, &e)
	}
	return out, rows.Err()
}

// CountAuditEvents returns the total matching the filter, ignoring limit
// + offset. Used to drive pagination.
func (s *Store) CountAuditEvents(ctx context.Context, f AuditFilter) (int64, error) {
	q, args := buildAuditQuery(f, true)
	var n int64
	if err := s.R.QueryRowContext(ctx, q, args...).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// buildAuditQuery composes the SELECT (or COUNT) with parameterised filter
// clauses. Builder pattern keeps the SQL trivially auditable.
func buildAuditQuery(f AuditFilter, count bool) (string, []any) {
	var (
		clauses []string
		args    []any
	)
	if f.UserID != "" {
		clauses = append(clauses, "user_id = ?")
		args = append(args, f.UserID)
	}
	if f.Action != "" {
		// Prefix match: querying "share." returns share.create / .revoke /
		// .access without the caller having to enumerate them.
		clauses = append(clauses, "action LIKE ?")
		args = append(args, f.Action+"%")
	}
	if f.BackendID != "" {
		clauses = append(clauses, "backend_id = ?")
		args = append(args, f.BackendID)
	}
	if f.Bucket != "" {
		clauses = append(clauses, "bucket = ?")
		args = append(args, f.Bucket)
	}
	if f.Status != "" {
		clauses = append(clauses, "status = ?")
		args = append(args, f.Status)
	}
	if !f.Since.IsZero() {
		clauses = append(clauses, "ts >= ?")
		args = append(args, f.Since)
	}
	if !f.Until.IsZero() {
		clauses = append(clauses, "ts <= ?")
		args = append(args, f.Until)
	}

	var b strings.Builder
	if count {
		b.WriteString(`SELECT COUNT(1) FROM audit_events`)
	} else {
		b.WriteString(`SELECT `)
		b.WriteString(auditCols)
		b.WriteString(` FROM audit_events`)
	}
	if len(clauses) > 0 {
		b.WriteString(" WHERE ")
		b.WriteString(strings.Join(clauses, " AND "))
	}
	if !count {
		b.WriteString(" ORDER BY ts DESC, id DESC")
		// SQLite ignores parameters in LIMIT only when prepared in some
		// drivers; modernc/sqlite accepts them, so we use placeholders.
		limit := f.Limit
		if limit <= 0 || limit > 1000 {
			limit = 200
		}
		b.WriteString(" LIMIT ?")
		args = append(args, limit)
		if f.Offset > 0 {
			b.WriteString(" OFFSET ?")
			args = append(args, f.Offset)
		}
	}
	return b.String(), args
}
