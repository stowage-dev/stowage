// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package sqlite_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stowage-dev/stowage/internal/store/sqlite"
)

func newBucketCORS(backend, bucket, rules string) *sqlite.S3BucketCORS {
	now := time.Now().UTC()
	return &sqlite.S3BucketCORS{
		BackendID: backend,
		Bucket:    bucket,
		Rules:     rules,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func TestS3BucketCORS_UpsertReplaces(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	first := newBucketCORS("minio", "uploads", `[{"allowed_origins":["https://a.example"]}]`)
	if err := store.UpsertS3BucketCORS(ctx, first); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	second := newBucketCORS("minio", "uploads", `[{"allowed_origins":["https://b.example"]}]`)
	if err := store.UpsertS3BucketCORS(ctx, second); err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	got, err := store.GetS3BucketCORS(ctx, "minio", "uploads")
	if err != nil {
		t.Fatalf("GetS3BucketCORS: %v", err)
	}
	if got.Rules != second.Rules {
		t.Fatalf("rules = %q, want second upsert payload", got.Rules)
	}
}

func TestS3BucketCORS_GetMissing(t *testing.T) {
	store := openTestStore(t)
	_, err := store.GetS3BucketCORS(context.Background(), "minio", "no-such")
	if !errors.Is(err, sqlite.ErrS3BucketCORSNotFound) {
		t.Fatalf("err = %v, want ErrS3BucketCORSNotFound", err)
	}
}

func TestS3BucketCORS_Delete(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	row := newBucketCORS("minio", "uploads", `[{}]`)
	if err := store.UpsertS3BucketCORS(ctx, row); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteS3BucketCORS(ctx, "minio", "uploads"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := store.DeleteS3BucketCORS(ctx, "minio", "uploads"); !errors.Is(err, sqlite.ErrS3BucketCORSNotFound) {
		t.Fatalf("second delete should be not-found, got %v", err)
	}
}

func TestS3BucketCORS_ListOrdered(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	rows := []*sqlite.S3BucketCORS{
		newBucketCORS("minio", "uploads", `[]`),
		newBucketCORS("garage", "logs", `[]`),
		newBucketCORS("minio", "downloads", `[]`),
	}
	for _, r := range rows {
		if err := store.UpsertS3BucketCORS(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	out, err := store.ListS3BucketCORS(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(out) != 3 {
		t.Fatalf("len = %d, want 3", len(out))
	}
	// ORDER BY backend_id, bucket → garage:logs, minio:downloads, minio:uploads
	want := []string{"garage/logs", "minio/downloads", "minio/uploads"}
	for i, r := range out {
		got := r.BackendID + "/" + r.Bucket
		if got != want[i] {
			t.Fatalf("row %d = %q, want %q", i, got, want[i])
		}
	}
}
