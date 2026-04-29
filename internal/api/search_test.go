// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/stowage-dev/stowage/internal/auth"
	"github.com/stowage-dev/stowage/internal/backend"
	"github.com/stowage-dev/stowage/internal/backend/memory"
)

// TestUnifiedSearch covers the bucket-name + object-prefix matching
// across multiple backends: a single query should pick up both kinds of
// hits in one round trip.
func TestUnifiedSearch(t *testing.T) {
	ctx := context.Background()

	a := memory.New("alpha", "Alpha")
	b := memory.New("beta", "Beta")
	reg := backend.NewRegistry()
	_ = reg.Register(a)
	_ = reg.Register(b)

	// Bucket name match: "reports-2026" on alpha.
	_ = a.CreateBucket(ctx, "reports-2026", "")
	_ = a.CreateBucket(ctx, "misc", "")
	_ = b.CreateBucket(ctx, "logs", "")

	// Object prefix match: "reports/q1.csv" inside misc + logs.
	for _, put := range []struct {
		bk     *memory.Backend
		bucket string
		key    string
	}{
		{a, "misc", "reports/q1.csv"},
		{a, "misc", "other.txt"},
		{b, "logs", "reports/q2.csv"},
	} {
		_, err := put.bk.PutObject(ctx, backend.PutObjectRequest{
			Bucket: put.bucket, Key: put.key,
			Body: strings.NewReader("data"), Size: 4,
		})
		if err != nil {
			t.Fatalf("seed %s/%s: %v", put.bucket, put.key, err)
		}
	}

	d := &BackendDeps{Registry: reg, Logger: slog.Default()}
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := auth.ContextWithIdentity(req.Context(), &auth.Identity{
				UserID: "u", Username: "u", Role: "admin",
			})
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})
	r.Get("/api/search", d.handleSearch)
	srv := httptest.NewServer(r)
	defer srv.Close()

	resp := mustGet(t, srv.URL+"/api/search?q=reports")
	assertStatus(t, resp, 200)
	var out struct {
		Query   string            `json:"query"`
		Buckets []searchBucketHit `json:"buckets"`
		Objects []searchObjectHit `json:"objects"`
	}
	mustDecode(t, resp, &out)

	// One bucket-name match (reports-2026 on alpha).
	if len(out.Buckets) != 1 || out.Buckets[0].Bucket != "reports-2026" {
		t.Fatalf("bucket hits wrong: %+v", out.Buckets)
	}
	// Two object-prefix matches (one per backend).
	keys := map[string]bool{}
	for _, o := range out.Objects {
		keys[o.BackendID+"/"+o.Bucket+"/"+o.Key] = true
	}
	if !keys["alpha/misc/reports/q1.csv"] || !keys["beta/logs/reports/q2.csv"] {
		t.Fatalf("missing object hits: %+v", out.Objects)
	}

	// Sub-minimum query returns empty without error.
	resp = mustGet(t, srv.URL+"/api/search?q=r")
	assertStatus(t, resp, 200)
	mustDecode(t, resp, &out)
	if len(out.Buckets) != 0 || len(out.Objects) != 0 {
		t.Fatalf("short query should return empty, got %+v", out)
	}
}
