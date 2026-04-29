// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package sqlite_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stowage-dev/stowage/internal/store/sqlite"
)

func openTestStore(t *testing.T) *sqlite.Store {
	t.Helper()
	store, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "pins.db"))
	if err != nil {
		t.Fatalf("sqlite: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestPinInsertListDelete(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	for _, b := range []string{"alpha", "beta", "alpha"} {
		err := store.InsertPin(ctx, &sqlite.BucketPin{
			UserID: "u1", BackendID: b, Bucket: "data",
			CreatedAt: time.Now().UTC(),
		})
		if err != nil {
			t.Fatalf("insert: %v", err)
		}
	}

	got, err := store.ListPinsByUser(ctx, "u1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 pins (alpha duplicate is idempotent), got %d", len(got))
	}

	if err := store.DeletePin(ctx, "u1", "alpha", "data"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	got, _ = store.ListPinsByUser(ctx, "u1")
	if len(got) != 1 || got[0].BackendID != "beta" {
		t.Fatalf("after delete=%+v", got)
	}
}

func TestPinIsolatedByUser(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	for _, u := range []string{"u1", "u2"} {
		_ = store.InsertPin(ctx, &sqlite.BucketPin{
			UserID: u, BackendID: "alpha", Bucket: "shared",
			CreatedAt: time.Now().UTC(),
		})
	}
	a, _ := store.ListPinsByUser(ctx, "u1")
	if len(a) != 1 {
		t.Fatalf("u1 pins=%d want 1", len(a))
	}
	// u1 deleting their pin must not affect u2.
	_ = store.DeletePin(ctx, "u1", "alpha", "shared")
	b, _ := store.ListPinsByUser(ctx, "u2")
	if len(b) != 1 {
		t.Fatalf("u2 pins=%d want 1 after u1 deletion", len(b))
	}
}
