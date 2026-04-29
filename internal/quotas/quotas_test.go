// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package quotas_test

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
	"github.com/stowage-dev/stowage/internal/quotas"
	"github.com/stowage-dev/stowage/internal/store/sqlite"
)

func setup(t *testing.T) (*quotas.Service, *memory.Backend, context.Context) {
	t.Helper()
	ctx := context.Background()
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "q.db"))
	if err != nil {
		t.Fatalf("sqlite: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	reg := backend.NewRegistry()
	mem := memory.New("mem", "Memory")
	if err := reg.Register(mem); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := mem.CreateBucket(ctx, "data", ""); err != nil {
		t.Fatalf("bucket: %v", err)
	}
	limits := quotas.NewSQLiteLimitSource(store, slog.Default())
	if err := limits.Reload(ctx); err != nil {
		t.Fatalf("limits.Reload: %v", err)
	}
	return quotas.New(limits, store, reg, slog.Default()), mem, ctx
}

func putKB(t *testing.T, mem *memory.Backend, key string, kb int) {
	t.Helper()
	body := strings.Repeat("x", kb*1024)
	_, err := mem.PutObject(context.Background(), backend.PutObjectRequest{
		Bucket: "data", Key: key,
		Body: strings.NewReader(body), Size: int64(len(body)),
	})
	if err != nil {
		t.Fatalf("put %s: %v", key, err)
	}
}

func TestScanComputesBytesAndCount(t *testing.T) {
	svc, mem, ctx := setup(t)
	putKB(t, mem, "a.bin", 4)
	putKB(t, mem, "sub/b.bin", 6)
	putKB(t, mem, "sub/c.bin", 1)

	u, err := svc.Scan(ctx, "mem", "data")
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	wantBytes := int64((4 + 6 + 1) * 1024)
	if u.Bytes != wantBytes {
		t.Fatalf("bytes=%d want %d", u.Bytes, wantBytes)
	}
	if u.ObjectCount != 3 {
		t.Fatalf("count=%d want 3", u.ObjectCount)
	}
	// Cache should now serve the same value without another scan.
	cached := svc.Get("mem", "data")
	if cached == nil || cached.Bytes != wantBytes {
		t.Fatalf("cache wrong: %+v", cached)
	}
}

func TestCheckUploadEnforcesHardQuota(t *testing.T) {
	svc, mem, ctx := setup(t)
	putKB(t, mem, "existing.bin", 8) // 8 KiB used

	// Configure a 10 KiB hard quota.
	if err := svc.Store.UpsertQuota(ctx, &sqlite.BucketQuota{
		BackendID: "mem", Bucket: "data",
		HardBytes: 10 * 1024,
		UpdatedAt: time.Now(), UpdatedBy: "admin",
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := svc.ReloadLimits(ctx); err != nil {
		t.Fatalf("reload: %v", err)
	}

	// Without a baseline scan, CheckUpload triggers a synchronous scan
	// and then evaluates. 1 KiB more is fine (8 + 1 ≤ 10).
	if err := svc.CheckUpload(ctx, "mem", "data", 1*1024); err != nil {
		t.Fatalf("expected ok, got %v", err)
	}
	// 4 KiB more would push to 12 KiB > 10 KiB → ErrQuotaExceeded.
	err := svc.CheckUpload(ctx, "mem", "data", 4*1024)
	if !errors.Is(err, quotas.ErrQuotaExceeded) {
		t.Fatalf("expected ErrQuotaExceeded, got %v", err)
	}
}

func TestRecordedBumpsCacheBetweenScans(t *testing.T) {
	svc, mem, ctx := setup(t)
	putKB(t, mem, "a.bin", 1)

	// Force initial scan.
	if _, err := svc.Scan(ctx, "mem", "data"); err != nil {
		t.Fatalf("scan: %v", err)
	}

	if err := svc.Store.UpsertQuota(ctx, &sqlite.BucketQuota{
		BackendID: "mem", Bucket: "data",
		HardBytes: 5 * 1024,
		UpdatedAt: time.Now(), UpdatedBy: "admin",
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := svc.ReloadLimits(ctx); err != nil {
		t.Fatalf("reload: %v", err)
	}

	// 3 KiB upload — would put us at 4 KiB, fine.
	if err := svc.CheckUpload(ctx, "mem", "data", 3*1024); err != nil {
		t.Fatalf("expected ok, got %v", err)
	}
	svc.Recorded("mem", "data", 3*1024)

	// Without a fresh scan, a 2 KiB upload should now trip the cap (1 + 3 + 2 > 5).
	err := svc.CheckUpload(ctx, "mem", "data", 2*1024)
	if !errors.Is(err, quotas.ErrQuotaExceeded) {
		t.Fatalf("expected ErrQuotaExceeded after Recorded, got %v", err)
	}
}

func TestNoQuotaAllowsAllUploads(t *testing.T) {
	svc, _, ctx := setup(t)
	// No quota row at all.
	if err := svc.CheckUpload(ctx, "mem", "data", 1<<30); err != nil {
		t.Fatalf("unconfigured bucket should allow upload, got %v", err)
	}
}
