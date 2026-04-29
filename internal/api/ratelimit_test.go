// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/stowage-dev/stowage/internal/auth"
)

// TestSessionRateLimit validates that the per-session limiter middleware
// produces 429 + Retry-After when a session blows past its window cap.
func TestSessionRateLimit(t *testing.T) {
	rl := auth.NewRateLimiter(3, time.Minute)
	mw := sessionRateLimit(rl)
	if mw == nil {
		t.Fatal("sessionRateLimit returned nil for non-nil limiter")
	}

	r := chi.NewRouter()
	// Inject a stub identity so the limiter has a session ID to key on.
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := auth.ContextWithIdentity(req.Context(), &auth.Identity{
				UserID: "u", Username: "u", Role: "user", SessionID: "sess-1",
			})
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})
	r.Use(mw)
	r.Get("/api/me", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	srv := httptest.NewServer(r)
	defer srv.Close()

	for i := 0; i < 3; i++ {
		resp, err := http.Get(srv.URL + "/api/me")
		if err != nil {
			t.Fatalf("req %d: %v", i, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("req %d: status=%d want 204", i, resp.StatusCode)
		}
	}
	resp, err := http.Get(srv.URL + "/api/me")
	if err != nil {
		t.Fatalf("4th req: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("4th req: status=%d want 429", resp.StatusCode)
	}
	ra := resp.Header.Get("Retry-After")
	if ra == "" {
		t.Fatalf("missing Retry-After header on 429")
	}
	if n, err := strconv.Atoi(ra); err != nil || n <= 0 {
		t.Fatalf("Retry-After=%q want a positive integer", ra)
	}
}

// TestSessionRateLimitNilDisables confirms passing a nil limiter yields a
// nil middleware so callers can detect "disabled" without an extra flag.
func TestSessionRateLimitNilDisables(t *testing.T) {
	if sessionRateLimit(nil) != nil {
		t.Fatal("expected nil middleware when limiter is nil")
	}
}

// TestSessionRateLimitIsolatesSessions confirms that two sessions exhaust
// their buckets independently.
func TestSessionRateLimitIsolatesSessions(t *testing.T) {
	rl := auth.NewRateLimiter(2, time.Minute)
	mw := sessionRateLimit(rl)

	r := chi.NewRouter()
	// Session ID is read from a request header so the test can switch it.
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := auth.ContextWithIdentity(req.Context(), &auth.Identity{
				UserID: "u", Username: "u", Role: "user",
				SessionID: req.Header.Get("X-Session"),
			})
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})
	r.Use(mw)
	r.Get("/api/me", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	srv := httptest.NewServer(r)
	defer srv.Close()

	hit := func(t *testing.T, sess string, want int) {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/me", nil)
		req.Header.Set("X-Session", sess)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("req: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != want {
			t.Fatalf("session %s: status=%d want %d", sess, resp.StatusCode, want)
		}
	}

	hit(t, "A", 204)
	hit(t, "A", 204)
	hit(t, "A", 429) // session A exhausted
	hit(t, "B", 204) // session B unaffected
	hit(t, "B", 204)
	hit(t, "B", 429)
}
