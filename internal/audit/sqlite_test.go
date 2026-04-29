// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package audit_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stowage-dev/stowage/internal/audit"
	"github.com/stowage-dev/stowage/internal/store/sqlite"
)

func setup(t *testing.T) (*sqlite.Store, audit.Recorder, context.Context) {
	t.Helper()
	ctx := context.Background()
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "audit.db"))
	if err != nil {
		t.Fatalf("sqlite: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store, audit.NewSQLiteRecorder(store, nil, nil), ctx
}

func TestRecordAndList(t *testing.T) {
	store, rec, ctx := setup(t)

	for i, a := range []string{"auth.login", "share.create", "share.access", "object.delete"} {
		err := rec.Record(ctx, audit.Event{
			Action:    a,
			UserID:    "u1",
			Backend:   "alpha",
			Bucket:    "docs",
			Status:    "ok",
			Timestamp: time.Date(2026, 4, 25, 10, i, 0, 0, time.UTC),
		})
		if err != nil {
			t.Fatalf("record %s: %v", a, err)
		}
	}

	all, err := store.ListAuditEvents(ctx, sqlite.AuditFilter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != 4 {
		t.Fatalf("expected 4 events, got %d", len(all))
	}
	// Newest first.
	if all[0].Action != "object.delete" {
		t.Fatalf("ordering wrong: %s", all[0].Action)
	}
}

func TestFilterByPrefix(t *testing.T) {
	store, rec, ctx := setup(t)
	for _, a := range []string{"auth.login", "share.create", "share.revoke", "object.delete"} {
		_ = rec.Record(ctx, audit.Event{Action: a, Status: "ok"})
	}
	got, err := store.ListAuditEvents(ctx, sqlite.AuditFilter{Action: "share."})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("share-prefix events=%d want 2", len(got))
	}
}

func TestFilterByUserAndBucket(t *testing.T) {
	store, rec, ctx := setup(t)
	_ = rec.Record(ctx, audit.Event{Action: "object.delete", UserID: "u1", Backend: "a", Bucket: "x"})
	_ = rec.Record(ctx, audit.Event{Action: "object.delete", UserID: "u2", Backend: "a", Bucket: "x"})
	_ = rec.Record(ctx, audit.Event{Action: "object.delete", UserID: "u1", Backend: "a", Bucket: "y"})

	mineX, _ := store.ListAuditEvents(ctx, sqlite.AuditFilter{UserID: "u1", Bucket: "x"})
	if len(mineX) != 1 {
		t.Fatalf("u1+x events=%d want 1", len(mineX))
	}
	allX, _ := store.ListAuditEvents(ctx, sqlite.AuditFilter{Bucket: "x"})
	if len(allX) != 2 {
		t.Fatalf("bucket x events=%d want 2", len(allX))
	}
}

func TestCount(t *testing.T) {
	store, rec, ctx := setup(t)
	for i := 0; i < 5; i++ {
		_ = rec.Record(ctx, audit.Event{Action: "x", Status: "ok"})
	}
	for i := 0; i < 2; i++ {
		_ = rec.Record(ctx, audit.Event{Action: "x", Status: "error"})
	}
	n, err := store.CountAuditEvents(ctx, sqlite.AuditFilter{Status: "error"})
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 2 {
		t.Fatalf("error count=%d want 2", n)
	}
}
