// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/stowage-dev/stowage/internal/auth"
	"github.com/stowage-dev/stowage/internal/backend"
	"github.com/stowage-dev/stowage/internal/backend/memory"
	"github.com/stowage-dev/stowage/internal/metrics"
	"github.com/stowage-dev/stowage/internal/quotas"
	"github.com/stowage-dev/stowage/internal/store/sqlite"
)

func TestDashboardEndpoint(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "dash.db"))
	if err != nil {
		t.Fatalf("sqlite: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	reg := backend.NewRegistry()
	mem := memory.New("alpha", "Alpha")
	if err := reg.Register(mem); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := mem.CreateBucket(ctx, "primary", ""); err != nil {
		t.Fatalf("create bucket: %v", err)
	}
	for i := 0; i < 3; i++ {
		_, err := mem.PutObject(ctx, backend.PutObjectRequest{
			Bucket: "primary", Key: "f" + string(rune('0'+i)),
			Body: strings.NewReader("xyz"), Size: 3,
		})
		if err != nil {
			t.Fatalf("put: %v", err)
		}
	}

	limits := quotas.NewSQLiteLimitSource(store, slog.Default())
	if err := store.UpsertQuota(ctx, &sqlite.BucketQuota{
		BackendID: "alpha", Bucket: "primary",
		HardBytes: 1 << 30,
		UpdatedAt: time.Now(), UpdatedBy: "admin",
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := limits.Reload(ctx); err != nil {
		t.Fatalf("limits.Reload: %v", err)
	}
	qsvc := quotas.New(limits, store, reg, slog.Default())
	if _, err := qsvc.Scan(ctx, "alpha", "primary"); err != nil {
		t.Fatalf("scan: %v", err)
	}

	msvc := metrics.New()
	msvc.Clock = func() time.Time { return time.Date(2026, 4, 25, 10, 0, 0, 0, time.UTC) }
	for i := 0; i < 4; i++ {
		msvc.Record("alpha", 200)
	}
	msvc.Record("alpha", 503)

	dash := &DashboardDeps{Metrics: msvc, Quotas: qsvc}
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := auth.ContextWithIdentity(req.Context(), &auth.Identity{
				UserID: "u", Username: "u", Role: "admin",
			})
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})
	r.Get("/api/admin/dashboard", dash.handleDashboard)

	srv := httptest.NewServer(r)
	defer srv.Close()

	resp := mustGet(t, srv.URL+"/api/admin/dashboard")
	assertStatus(t, resp, 200)
	var got struct {
		Requests struct {
			Total24h  int64            `json:"total_24h"`
			Errors24h int64            `json:"errors_24h"`
			ByBackend map[string]int64 `json:"by_backend"`
			Hourly    []struct {
				Requests int64 `json:"requests"`
			} `json:"hourly"`
		} `json:"requests"`
		Storage struct {
			ByBackend []struct {
				BackendID string `json:"backend_id"`
				Bytes     int64  `json:"bytes"`
				Objects   int64  `json:"objects"`
				Buckets   int    `json:"buckets"`
			} `json:"by_backend"`
			TopBuckets []struct {
				BackendID string `json:"backend_id"`
				Bucket    string `json:"bucket"`
				Bytes     int64  `json:"bytes"`
			} `json:"top_buckets"`
			CacheNote string `json:"cache_note"`
		} `json:"storage"`
	}
	mustDecode(t, resp, &got)

	if got.Requests.Total24h != 5 {
		t.Fatalf("total=%d want 5", got.Requests.Total24h)
	}
	if got.Requests.Errors24h != 1 {
		t.Fatalf("errors=%d want 1", got.Requests.Errors24h)
	}
	if got.Requests.ByBackend["alpha"] != 5 {
		t.Fatalf("alpha=%d want 5", got.Requests.ByBackend["alpha"])
	}
	if len(got.Requests.Hourly) != 24 {
		t.Fatalf("hourly len=%d want 24", len(got.Requests.Hourly))
	}
	if len(got.Storage.ByBackend) != 1 || got.Storage.ByBackend[0].BackendID != "alpha" ||
		got.Storage.ByBackend[0].Objects != 3 || got.Storage.ByBackend[0].Bytes != 9 {
		t.Fatalf("storage by_backend wrong: %+v", got.Storage.ByBackend)
	}
	if len(got.Storage.TopBuckets) != 1 || got.Storage.TopBuckets[0].Bucket != "primary" {
		t.Fatalf("top buckets wrong: %+v", got.Storage.TopBuckets)
	}
	if got.Storage.CacheNote == "" {
		t.Fatalf("expected cache note")
	}
}
