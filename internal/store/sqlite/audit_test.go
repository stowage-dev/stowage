// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package sqlite_test

import (
	"context"
	"testing"
	"time"

	"github.com/stowage-dev/stowage/internal/store/sqlite"
)

func TestPurgeAuditEventsBefore(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	now := time.Now().UTC()
	for _, age := range []time.Duration{72 * time.Hour, 48 * time.Hour, 1 * time.Hour} {
		if err := store.InsertAuditEvent(ctx, &sqlite.AuditEvent{
			Timestamp: now.Add(-age),
			Action:    "test.event",
			Status:    "ok",
		}); err != nil {
			t.Fatalf("insert (age %s): %v", age, err)
		}
	}

	cutoff := now.Add(-24 * time.Hour)
	n, err := store.PurgeAuditEventsBefore(ctx, cutoff)
	if err != nil {
		t.Fatalf("PurgeAuditEventsBefore: %v", err)
	}
	if n != 2 {
		t.Fatalf("purged %d rows, want 2", n)
	}

	remaining, err := store.CountAuditEvents(ctx, sqlite.AuditFilter{})
	if err != nil {
		t.Fatalf("CountAuditEvents: %v", err)
	}
	if remaining != 1 {
		t.Fatalf("remaining %d rows, want 1", remaining)
	}
}

func TestPurgeAuditEventsBefore_Empty(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	n, err := store.PurgeAuditEventsBefore(ctx, time.Now().UTC())
	if err != nil {
		t.Fatalf("PurgeAuditEventsBefore: %v", err)
	}
	if n != 0 {
		t.Fatalf("purged %d rows from empty table, want 0", n)
	}
}
