// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package shares_test

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stowage-dev/stowage/internal/backend"
	"github.com/stowage-dev/stowage/internal/backend/memory"
	"github.com/stowage-dev/stowage/internal/shares"
	"github.com/stowage-dev/stowage/internal/store/sqlite"
)

func setup(t *testing.T) (*shares.Service, context.Context) {
	t.Helper()
	ctx := context.Background()

	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("sqlite open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	reg := backend.NewRegistry()
	mem := memory.New("mem", "Memory Test")
	if err := reg.Register(mem); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := mem.CreateBucket(ctx, "docs", ""); err != nil {
		t.Fatalf("create bucket: %v", err)
	}
	if _, err := mem.PutObject(ctx, backend.PutObjectRequest{
		Bucket: "docs",
		Key:    "hello.txt",
		Body:   strings.NewReader("hi there"),
		Size:   8,
	}); err != nil {
		t.Fatalf("put: %v", err)
	}

	svc := &shares.Service{Store: store, Backends: reg, Logger: slog.Default()}
	return svc, ctx
}

func TestCreateAndResolveOK(t *testing.T) {
	svc, ctx := setup(t)

	sh, err := svc.Create(ctx, "user-1", shares.CreateParams{
		BackendID: "mem", Bucket: "docs", Key: "hello.txt",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if sh.Code == "" || len(sh.Code) < 10 {
		t.Fatalf("short code: %q", sh.Code)
	}
	if sh.Disposition != "attachment" {
		t.Fatalf("default disposition=%q", sh.Disposition)
	}

	got, err := svc.Resolve(ctx, sh.Code, "")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got.ID != sh.ID {
		t.Fatalf("wrong share resolved: %s want %s", got.ID, sh.ID)
	}
}

func TestCreateRejectsUnknownObject(t *testing.T) {
	svc, ctx := setup(t)
	_, err := svc.Create(ctx, "user-1", shares.CreateParams{
		BackendID: "mem", Bucket: "docs", Key: "ghost.txt",
	})
	if !errors.Is(err, shares.ErrInvalidParams) {
		t.Fatalf("expected ErrInvalidParams, got %v", err)
	}
}

func TestResolveNotFound(t *testing.T) {
	svc, ctx := setup(t)
	_, err := svc.Resolve(ctx, "does-not-exist", "")
	if !errors.Is(err, shares.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestPasswordFlow(t *testing.T) {
	svc, ctx := setup(t)
	sh, err := svc.Create(ctx, "user-1", shares.CreateParams{
		BackendID: "mem", Bucket: "docs", Key: "hello.txt",
		Password: "let-me-in",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// No password → ErrPasswordRequired.
	if _, err := svc.Resolve(ctx, sh.Code, ""); !errors.Is(err, shares.ErrPasswordRequired) {
		t.Fatalf("expected ErrPasswordRequired, got %v", err)
	}
	// Wrong password → ErrPasswordMismatch.
	if _, err := svc.Resolve(ctx, sh.Code, "nope"); !errors.Is(err, shares.ErrPasswordMismatch) {
		t.Fatalf("expected ErrPasswordMismatch, got %v", err)
	}
	// Right password → ok.
	if _, err := svc.Resolve(ctx, sh.Code, "let-me-in"); err != nil {
		t.Fatalf("correct password should resolve: %v", err)
	}
}

func TestExpiry(t *testing.T) {
	svc, ctx := setup(t)
	base := time.Date(2026, 4, 25, 10, 0, 0, 0, time.UTC)
	svc.Clock = func() time.Time { return base }

	future := base.Add(1 * time.Hour)
	sh, err := svc.Create(ctx, "user-1", shares.CreateParams{
		BackendID: "mem", Bucket: "docs", Key: "hello.txt",
		ExpiresAt: &future,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// Still valid.
	if _, err := svc.Resolve(ctx, sh.Code, ""); err != nil {
		t.Fatalf("pre-expiry: %v", err)
	}
	// Move past expiry.
	svc.Clock = func() time.Time { return future.Add(time.Second) }
	if _, err := svc.Resolve(ctx, sh.Code, ""); !errors.Is(err, shares.ErrExpired) {
		t.Fatalf("expected ErrExpired, got %v", err)
	}

	// Creating with expires_at in the past is rejected.
	past := base.Add(-1 * time.Minute)
	svc.Clock = func() time.Time { return base }
	if _, err := svc.Create(ctx, "u", shares.CreateParams{
		BackendID: "mem", Bucket: "docs", Key: "hello.txt", ExpiresAt: &past,
	}); !errors.Is(err, shares.ErrInvalidParams) {
		t.Fatalf("expected ErrInvalidParams for past expiry, got %v", err)
	}
}

func TestDownloadCap(t *testing.T) {
	svc, ctx := setup(t)
	sh, err := svc.Create(ctx, "user-1", shares.CreateParams{
		BackendID: "mem", Bucket: "docs", Key: "hello.txt",
		MaxDownloads: 2,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// First two downloads: resolve + record both succeed.
	for i := 0; i < 2; i++ {
		if _, err := svc.Resolve(ctx, sh.Code, ""); err != nil {
			t.Fatalf("resolve #%d: %v", i, err)
		}
		if err := svc.RecordAccess(ctx, sh.ID); err != nil {
			t.Fatalf("record #%d: %v", i, err)
		}
	}
	// Third resolve passes the "count" gate in memory (download_count
	// reflects last state), but the handler calls RecordAccess which is
	// the atomic gate — verify the UPDATE refuses.
	if _, err := svc.Resolve(ctx, sh.Code, ""); !errors.Is(err, shares.ErrExhausted) {
		t.Fatalf("expected ErrExhausted on 3rd resolve, got %v", err)
	}
}

func TestRevoke(t *testing.T) {
	svc, ctx := setup(t)
	sh, err := svc.Create(ctx, "user-1", shares.CreateParams{
		BackendID: "mem", Bucket: "docs", Key: "hello.txt",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Non-owner non-admin → ErrNotFound (don't leak existence).
	if err := svc.Revoke(ctx, "user-2", "user", sh.ID); !errors.Is(err, shares.ErrNotFound) {
		t.Fatalf("non-owner should see ErrNotFound, got %v", err)
	}
	// Owner can revoke.
	if err := svc.Revoke(ctx, "user-1", "user", sh.ID); err != nil {
		t.Fatalf("owner revoke: %v", err)
	}
	// Subsequent resolves return ErrRevoked.
	if _, err := svc.Resolve(ctx, sh.Code, ""); !errors.Is(err, shares.ErrRevoked) {
		t.Fatalf("expected ErrRevoked, got %v", err)
	}
	// Admin can revoke any; here it's idempotent.
	if err := svc.Revoke(ctx, "admin-x", "admin", sh.ID); err != nil {
		t.Fatalf("admin re-revoke: %v", err)
	}
}

func TestListMineIsolated(t *testing.T) {
	svc, ctx := setup(t)
	for _, uid := range []string{"a", "a", "b"} {
		_, err := svc.Create(ctx, uid, shares.CreateParams{
			BackendID: "mem", Bucket: "docs", Key: "hello.txt",
		})
		if err != nil {
			t.Fatalf("create for %s: %v", uid, err)
		}
	}
	as, err := svc.ListMine(ctx, "a")
	if err != nil {
		t.Fatalf("list mine a: %v", err)
	}
	if len(as) != 2 {
		t.Fatalf("user a should have 2 shares, got %d", len(as))
	}
	all, err := svc.ListAll(ctx)
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("all should have 3, got %d", len(all))
	}
}
