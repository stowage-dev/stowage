// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package sqlite_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/stowage-dev/stowage/internal/store/sqlite"
)

func newAnon(backend, bucket string, rps int) *sqlite.S3AnonymousBinding {
	return &sqlite.S3AnonymousBinding{
		BackendID:      backend,
		Bucket:         bucket,
		Mode:           "ReadOnly",
		PerSourceIPRPS: rps,
		CreatedAt:      time.Now().UTC(),
		CreatedBy:      sql.NullString{String: "admin", Valid: true},
	}
}

func TestS3Anonymous_UpsertReplaces(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	if err := store.UpsertS3AnonymousBinding(ctx, newAnon("minio", "public", 10)); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	if err := store.UpsertS3AnonymousBinding(ctx, newAnon("minio", "public", 99)); err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	got, err := store.GetS3AnonymousBinding(ctx, "minio", "public")
	if err != nil {
		t.Fatalf("GetS3AnonymousBinding: %v", err)
	}
	if got.PerSourceIPRPS != 99 {
		t.Fatalf("upsert did not replace rps: got %d", got.PerSourceIPRPS)
	}
}

func TestS3Anonymous_NotFound(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	_, err := store.GetS3AnonymousBinding(ctx, "minio", "missing")
	if !errors.Is(err, sqlite.ErrS3AnonymousBindingNotFound) {
		t.Fatalf("want ErrS3AnonymousBindingNotFound, got %v", err)
	}
}

func TestS3Anonymous_ListAndDelete(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	for _, b := range []*sqlite.S3AnonymousBinding{
		newAnon("minio", "alpha", 10),
		newAnon("minio", "beta", 20),
		newAnon("garage", "gamma", 30),
	} {
		if err := store.UpsertS3AnonymousBinding(ctx, b); err != nil {
			t.Fatalf("upsert %s/%s: %v", b.BackendID, b.Bucket, err)
		}
	}
	all, err := store.ListS3AnonymousBindings(ctx)
	if err != nil {
		t.Fatalf("ListS3AnonymousBindings: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("want 3 bindings, got %d", len(all))
	}
	// Ordered by (backend, bucket): garage/gamma, minio/alpha, minio/beta.
	want := []string{"garage/gamma", "minio/alpha", "minio/beta"}
	for i, w := range want {
		got := all[i].BackendID + "/" + all[i].Bucket
		if got != w {
			t.Fatalf("position %d: got %s want %s", i, got, w)
		}
	}

	if err := store.DeleteS3AnonymousBinding(ctx, "minio", "alpha"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := store.DeleteS3AnonymousBinding(ctx, "minio", "alpha"); !errors.Is(err, sqlite.ErrS3AnonymousBindingNotFound) {
		t.Fatalf("second delete should miss: %v", err)
	}
}
