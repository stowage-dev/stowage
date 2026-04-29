// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/stowage-dev/stowage/internal/auth"
	"github.com/stowage-dev/stowage/internal/backend"
	"github.com/stowage-dev/stowage/internal/backend/memory"
)

func TestBackendHealthEndpoint(t *testing.T) {
	ctx := context.Background()
	mem := memory.New("alpha", "Alpha")
	reg := backend.NewRegistry()
	_ = reg.Register(mem)

	// One probe round populates history and current status.
	reg.ProbeAll(ctx, time.Second)

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
	r.Get("/api/admin/backends/health", d.handleBackendHealth)
	srv := httptest.NewServer(r)
	defer srv.Close()

	resp := mustGet(t, srv.URL+"/api/admin/backends/health")
	assertStatus(t, resp, 200)
	var got struct {
		Backends []struct {
			ID      string `json:"id"`
			Healthy bool   `json:"healthy"`
			History []struct {
				Healthy bool `json:"healthy"`
			} `json:"history"`
		} `json:"backends"`
	}
	mustDecode(t, resp, &got)
	if len(got.Backends) != 1 || got.Backends[0].ID != "alpha" {
		t.Fatalf("unexpected backends: %+v", got.Backends)
	}
	if !got.Backends[0].Healthy {
		t.Fatalf("expected healthy after a probe")
	}
	if len(got.Backends[0].History) != 1 || !got.Backends[0].History[0].Healthy {
		t.Fatalf("history wrong: %+v", got.Backends[0].History)
	}
}
