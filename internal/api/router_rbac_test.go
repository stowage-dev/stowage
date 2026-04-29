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

	"github.com/stowage-dev/stowage/internal/audit"
	"github.com/stowage-dev/stowage/internal/auth"
	"github.com/stowage-dev/stowage/internal/backend"
	"github.com/stowage-dev/stowage/internal/backend/memory"
	"github.com/stowage-dev/stowage/internal/shares"
	"github.com/stowage-dev/stowage/internal/store/sqlite"
)

// rbacServer wires the same write-gate middleware NewRouter uses onto the
// real handlers, but with a role-switchable stub identity so a single test
// can exercise admin / user / readonly callers. Mirrors the route shape of
// NewRouter for the endpoints we care about.
func rbacServer(t *testing.T) (*httptest.Server, *auth.Identity) {
	t.Helper()
	ctx := context.Background()

	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "rbac.db"))
	if err != nil {
		t.Fatalf("sqlite: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	mem := memory.New("mem", "Memory Test")
	reg := backend.NewRegistry()
	if err := reg.Register(mem); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := mem.CreateBucket(ctx, "docs", ""); err != nil {
		t.Fatalf("create bucket: %v", err)
	}
	if _, err := mem.PutObject(ctx, backend.PutObjectRequest{
		Bucket: "docs", Key: "hello.txt",
		Body: strings.NewReader("hi"), Size: 2,
	}); err != nil {
		t.Fatalf("put: %v", err)
	}

	bdeps := &BackendDeps{Registry: reg, Logger: slog.Default(), Audit: audit.Noop{}}
	sdeps := &ShareDeps{
		Service: &shares.Service{Store: store, Backends: reg, Logger: slog.Default()},
		RateLim: auth.NewRateLimiter(1000, time.Minute),
		Logger:  slog.Default(),
		Unlock:  MustNewUnlockSigner(),
		Audit:   audit.Noop{},
	}
	adeps := &AuthDeps{
		// Service is unused by the pin handlers we exercise — they reach
		// directly into Service.Store. Stub the bare minimum.
		Service: &auth.Service{Store: store},
		Logger:  slog.Default(),
		Audit:   audit.Noop{},
	}

	acting := &auth.Identity{UserID: "u-default", Username: "default", Role: "admin"}

	reject403 := func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusForbidden, "forbidden", "insufficient privileges", "")
	}
	requireWriter := auth.RequireRole(reject403, "admin", "user")

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := auth.ContextWithIdentity(req.Context(), acting)
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})

	r.Route("/api", func(r chi.Router) {
		r.Route("/me/pins", func(r chi.Router) {
			r.Get("/", adeps.handleListPins)
			r.With(requireWriter).Post("/", adeps.handleCreatePin)
			r.With(requireWriter).Delete("/{bid}/{bucket}", adeps.handleDeletePin)
		})
		r.Route("/shares", func(r chi.Router) {
			r.Get("/", sdeps.handleListShares)
			r.With(requireWriter).Post("/", sdeps.handleCreateShare)
			r.With(requireWriter).Delete("/{id}", sdeps.handleRevokeShare)
		})
		r.Route("/backends/{bid}/buckets/{bucket}", func(r chi.Router) {
			r.Get("/objects", bdeps.handleListObjects)
			r.With(requireWriter).Post("/objects/delete", bdeps.handleBulkDelete)
			r.With(requireWriter).Post("/objects/delete-prefix", bdeps.handleDeletePrefix)
			r.With(requireWriter).Post("/objects/folder", bdeps.handleCreateFolder)
			r.With(requireWriter).Post("/objects/copy-prefix", bdeps.handleCopyPrefix)
			r.Get("/object", bdeps.handleGetObject)
			r.With(requireWriter).Post("/object", bdeps.handleUploadObject)
			r.With(requireWriter).Delete("/object", bdeps.handleDeleteObject)
			r.With(requireWriter).Post("/object/copy", bdeps.handleCopyObject)
			r.With(requireWriter).Put("/object/tags", bdeps.handlePutObjectTags)
			r.With(requireWriter).Put("/object/metadata", bdeps.handleUpdateObjectMetadata)
			r.Route("/multipart", func(r chi.Router) {
				r.Get("/", bdeps.handleMultipartList)
				r.With(requireWriter).Post("/", bdeps.handleMultipartCreate)
				r.With(requireWriter).Delete("/", bdeps.handleMultipartAbort)
				r.With(requireWriter).Post("/complete", bdeps.handleMultipartComplete)
				r.With(requireWriter).Put("/parts/{part}", bdeps.handleMultipartUploadPart)
			})
		})
	})

	return httptest.NewServer(r), acting
}

// writeEndpoint is one row in the RBAC matrix below. Body/contentType only
// matter for verbs that read a body; the requireWriter check fires before
// the handler reads it, so empty bodies suffice for a "did the gate fire?"
// assertion.
type writeEndpoint struct {
	method string
	path   string
}

// writeEndpoints lists every mutating endpoint requireWriter should gate.
// Keep this in sync with router.go: a missing entry here means a regression
// is silent.
var writeEndpoints = []writeEndpoint{
	{"POST", "/api/me/pins"},
	{"DELETE", "/api/me/pins/mem/docs"},
	{"POST", "/api/shares"},
	{"DELETE", "/api/shares/some-id"},
	{"POST", "/api/backends/mem/buckets/docs/objects/delete"},
	{"POST", "/api/backends/mem/buckets/docs/objects/delete-prefix"},
	{"POST", "/api/backends/mem/buckets/docs/objects/folder"},
	{"POST", "/api/backends/mem/buckets/docs/objects/copy-prefix"},
	{"POST", "/api/backends/mem/buckets/docs/object"},
	{"DELETE", "/api/backends/mem/buckets/docs/object?key=hello.txt"},
	{"POST", "/api/backends/mem/buckets/docs/object/copy"},
	{"PUT", "/api/backends/mem/buckets/docs/object/tags?key=hello.txt"},
	{"PUT", "/api/backends/mem/buckets/docs/object/metadata?key=hello.txt"},
	{"POST", "/api/backends/mem/buckets/docs/multipart?key=foo"},
	{"DELETE", "/api/backends/mem/buckets/docs/multipart?key=foo&upload_id=abc"},
	{"POST", "/api/backends/mem/buckets/docs/multipart/complete?key=foo&upload_id=abc"},
	{"PUT", "/api/backends/mem/buckets/docs/multipart/parts/1?key=foo&upload_id=abc"},
}

// TestReadonlyRoleBlockedFromMutations is the regression guard for F-1: any
// of these endpoints accepting a readonly caller is the bug we just fixed.
func TestReadonlyRoleBlockedFromMutations(t *testing.T) {
	srv, acting := rbacServer(t)
	defer srv.Close()
	*acting = auth.Identity{UserID: "u-ro", Username: "ronly", Role: "readonly"}

	for _, ep := range writeEndpoints {
		req, err := http.NewRequest(ep.method, srv.URL+ep.path, strings.NewReader("{}"))
		if err != nil {
			t.Fatalf("build %s %s: %v", ep.method, ep.path, err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", ep.method, ep.path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s %s: status=%d want 403 (readonly should be denied)",
				ep.method, ep.path, resp.StatusCode)
		}
	}
}

// TestUserRoleAllowedThroughWriteGate confirms requireWriter doesn't over-
// reach: role=user must clear the gate and reach the actual handler. We
// don't care what the handler does after — only that we got past 403.
func TestUserRoleAllowedThroughWriteGate(t *testing.T) {
	srv, acting := rbacServer(t)
	defer srv.Close()
	*acting = auth.Identity{UserID: "u-user", Username: "alice", Role: "user"}

	for _, ep := range writeEndpoints {
		req, err := http.NewRequest(ep.method, srv.URL+ep.path, strings.NewReader("{}"))
		if err != nil {
			t.Fatalf("build %s %s: %v", ep.method, ep.path, err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", ep.method, ep.path, err)
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusForbidden {
			t.Errorf("%s %s: 403 for role=user (gate is over-strict)",
				ep.method, ep.path)
		}
	}
}

// TestReadonlyRoleAllowedReads confirms requireWriter is scoped narrowly:
// readonly callers retain GET access on listing endpoints.
func TestReadonlyRoleAllowedReads(t *testing.T) {
	srv, acting := rbacServer(t)
	defer srv.Close()
	*acting = auth.Identity{UserID: "u-ro", Username: "ronly", Role: "readonly"}

	reads := []string{
		"/api/me/pins",
		"/api/shares",
		"/api/backends/mem/buckets/docs/objects",
		"/api/backends/mem/buckets/docs/object?key=hello.txt",
		"/api/backends/mem/buckets/docs/multipart",
	}
	for _, p := range reads {
		resp, err := http.Get(srv.URL + p)
		if err != nil {
			t.Fatalf("GET %s: %v", p, err)
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusForbidden {
			t.Errorf("GET %s: 403 for readonly (should be allowed)", p)
		}
	}
}
