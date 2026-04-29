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
	"github.com/stowage-dev/stowage/internal/secrets"
	"github.com/stowage-dev/stowage/internal/store/sqlite"
)

// adminServer wires the /api/admin/backends routes against a real SQLite
// store + sealer, with a memory-backed driver builder so tests don't need
// a live S3. Returns the live registry so individual tests can pre-seed it
// (e.g. with a config-sourced entry to verify the read-only gate).
func adminServer(t *testing.T) (*httptest.Server, *backend.Registry, *sqlite.Store) {
	t.Helper()
	store, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "admin.db"))
	if err != nil {
		t.Fatalf("sqlite: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	sealer, err := secrets.New(strings.Repeat("ab", 32))
	if err != nil {
		t.Fatalf("sealer: %v", err)
	}

	reg := backend.NewRegistry()
	d := &BackendDeps{
		Registry: reg,
		Logger:   slog.Default(),
		Audit:    audit.Noop{},
		Store:    store,
		Sealer:   sealer,
		BuildBackend: func(_ context.Context, row *sqlite.Backend, _ string) (backend.Backend, error) {
			return memory.New(row.ID, row.Name), nil
		},
	}

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := auth.ContextWithIdentity(req.Context(), &auth.Identity{
				UserID: "tester", Username: "tester", Role: "admin",
			})
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})
	r.Route("/api/admin/backends", func(r chi.Router) {
		r.Get("/", d.handleAdminListBackends)
		r.Post("/", d.handleAdminCreateBackend)
		r.Post("/test", d.handleAdminTestBackend)
		r.Get("/{bid}", d.handleAdminGetBackend)
		r.Patch("/{bid}", d.handleAdminPatchBackend)
		r.Delete("/{bid}", d.handleAdminDeleteBackend)
	})

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv, reg, store
}

func adminCreate(t *testing.T, srv *httptest.Server, body string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/admin/backends",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return mustDo(t, req)
}

const validCreateBody = `{
  "id": "alpha",
  "name": "Alpha",
  "endpoint": "https://s3.example.com",
  "region": "us-east-1",
  "path_style": true,
  "access_key": "AKIATEST",
  "secret_key": "SECRETTEST"
}`

func TestAdminBackendsCreate_Persists_Registers_AndElidesSecret(t *testing.T) {
	srv, reg, store := adminServer(t)

	resp := adminCreate(t, srv, validCreateBody)
	assertStatus(t, resp, 201)
	var got adminBackendDTO
	mustDecode(t, resp, &got)
	if got.ID != "alpha" || got.Source != "db" || !got.SecretSet {
		t.Fatalf("dto=%+v", got)
	}

	// Persisted with sealed secret, not plaintext.
	row, err := store.GetBackend(context.Background(), "alpha")
	if err != nil {
		t.Fatalf("GetBackend: %v", err)
	}
	if string(row.SecretKeyEnc) == "SECRETTEST" {
		t.Fatal("secret stored in cleartext")
	}
	if len(row.SecretKeyEnc) < 16 {
		t.Fatalf("secret envelope suspiciously short: %d bytes", len(row.SecretKeyEnc))
	}

	// Registered as DB-sourced.
	src, ok := reg.Source("alpha")
	if !ok || src != backend.SourceDB {
		t.Fatalf("registry source for alpha = %q ok=%v", src, ok)
	}
}

func TestAdminBackendsCreate_RejectsBadID(t *testing.T) {
	srv, _, _ := adminServer(t)
	resp := adminCreate(t, srv, `{"id":"BAD ID","endpoint":"https://x","access_key":"a","secret_key":"b"}`)
	assertStatus(t, resp, 400)
}

func TestAdminBackendsCreate_RejectsBadEndpoint(t *testing.T) {
	srv, _, _ := adminServer(t)
	resp := adminCreate(t, srv, `{"id":"alpha","endpoint":"ftp://x","access_key":"a","secret_key":"b"}`)
	assertStatus(t, resp, 400)
}

func TestAdminBackendsCreate_AcceptsNonstandardPort(t *testing.T) {
	srv, _, _ := adminServer(t)
	resp := adminCreate(t, srv,
		`{"id":"minio","endpoint":"http://192.168.1.10:9000","access_key":"a","secret_key":"b"}`)
	assertStatus(t, resp, 201)
}

func TestAdminBackendsCreate_RejectsOutOfRangePort(t *testing.T) {
	srv, _, _ := adminServer(t)
	resp := adminCreate(t, srv,
		`{"id":"alpha","endpoint":"http://host:99999","access_key":"a","secret_key":"b"}`)
	assertStatus(t, resp, 400)
}

func TestAdminBackendsCreate_RejectsZeroPort(t *testing.T) {
	srv, _, _ := adminServer(t)
	resp := adminCreate(t, srv,
		`{"id":"alpha","endpoint":"http://host:0","access_key":"a","secret_key":"b"}`)
	assertStatus(t, resp, 400)
}

func TestAdminBackendsCreate_RejectsEndpointWithPath(t *testing.T) {
	srv, _, _ := adminServer(t)
	resp := adminCreate(t, srv,
		`{"id":"alpha","endpoint":"https://s3.example.com/api","access_key":"a","secret_key":"b"}`)
	assertStatus(t, resp, 400)
}

func TestAdminBackendsCreate_RequiresKeys(t *testing.T) {
	srv, _, _ := adminServer(t)
	resp := adminCreate(t, srv, `{"id":"alpha","endpoint":"https://x"}`)
	assertStatus(t, resp, 400)
}

func TestAdminBackendsCreate_RejectsDuplicateID(t *testing.T) {
	srv, _, _ := adminServer(t)
	assertStatus(t, adminCreate(t, srv, validCreateBody), 201)
	resp := adminCreate(t, srv, validCreateBody)
	assertStatus(t, resp, 409)
}

func TestAdminBackendsCreate_RejectsYAMLIDCollision(t *testing.T) {
	srv, reg, _ := adminServer(t)
	// Pretend a YAML-defined backend is already serving.
	if err := reg.RegisterWithSource(memory.New("alpha", "Alpha-yaml"), backend.SourceConfig); err != nil {
		t.Fatalf("preregister: %v", err)
	}
	resp := adminCreate(t, srv, validCreateBody)
	assertStatus(t, resp, 409)
	// And nothing got persisted.
	listResp, err := http.Get(srv.URL + "/api/admin/backends")
	if err != nil {
		t.Fatalf("GET list: %v", err)
	}
	defer listResp.Body.Close()
	var listed struct {
		Backends []adminBackendDTO `json:"backends"`
	}
	mustDecode(t, listResp, &listed)
	for _, b := range listed.Backends {
		if b.ID == "alpha" && b.Source == "db" {
			t.Fatal("DB row was persisted despite YAML collision")
		}
	}
}

func TestAdminBackendsList_MergesRegistryAndStore(t *testing.T) {
	srv, reg, store := adminServer(t)

	// One YAML-only entry (registry, no DB row).
	if err := reg.RegisterWithSource(memory.New("yamlboi", "YAML"), backend.SourceConfig); err != nil {
		t.Fatalf("preregister: %v", err)
	}
	// One DB entry created via the API (registry + store).
	assertStatus(t, adminCreate(t, srv, validCreateBody), 201)
	// One DB entry that's disabled (store only, never registered).
	now := time.Now().UTC()
	disabled := &sqlite.Backend{
		ID: "disabled", Name: "Disabled", Type: "s3v4",
		Endpoint: "https://x", Enabled: false,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := store.CreateBackend(context.Background(), disabled); err != nil {
		t.Fatalf("CreateBackend: %v", err)
	}

	resp, err := http.Get(srv.URL + "/api/admin/backends")
	if err != nil {
		t.Fatalf("GET list: %v", err)
	}
	defer resp.Body.Close()
	assertStatus(t, resp, 200)
	var listed struct {
		Backends []adminBackendDTO `json:"backends"`
	}
	mustDecode(t, resp, &listed)

	bySrc := map[string]adminBackendDTO{}
	for _, b := range listed.Backends {
		bySrc[b.ID] = b
	}
	if bySrc["yamlboi"].Source != "config" {
		t.Fatalf("yamlboi source=%q want config", bySrc["yamlboi"].Source)
	}
	if bySrc["alpha"].Source != "db" {
		t.Fatalf("alpha source=%q want db", bySrc["alpha"].Source)
	}
	if bySrc["disabled"].Source != "db" || bySrc["disabled"].Enabled {
		t.Fatalf("disabled entry not surfaced correctly: %+v", bySrc["disabled"])
	}
}

func TestAdminBackendsPatch_UpdatesScalarsAndReplacesInRegistry(t *testing.T) {
	srv, reg, store := adminServer(t)
	assertStatus(t, adminCreate(t, srv, validCreateBody), 201)
	originalDriver, _ := reg.Get("alpha")

	body := `{"name":"Renamed","region":"eu-west-1"}`
	req, _ := http.NewRequest(http.MethodPatch, srv.URL+"/api/admin/backends/alpha",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp := mustDo(t, req)
	assertStatus(t, resp, 200)

	row, _ := store.GetBackend(context.Background(), "alpha")
	if row.Name != "Renamed" || row.Region != "eu-west-1" {
		t.Fatalf("row=%+v", row)
	}
	newDriver, _ := reg.Get("alpha")
	if newDriver == originalDriver {
		t.Fatal("registry should have been Replaced with a fresh driver")
	}
}

func TestAdminBackendsPatch_OmittedSecretIsPreserved(t *testing.T) {
	srv, _, store := adminServer(t)
	assertStatus(t, adminCreate(t, srv, validCreateBody), 201)
	before, _ := store.GetBackend(context.Background(), "alpha")

	req, _ := http.NewRequest(http.MethodPatch, srv.URL+"/api/admin/backends/alpha",
		strings.NewReader(`{"name":"Renamed"}`))
	req.Header.Set("Content-Type", "application/json")
	assertStatus(t, mustDo(t, req), 200)

	after, _ := store.GetBackend(context.Background(), "alpha")
	if string(after.SecretKeyEnc) != string(before.SecretKeyEnc) {
		t.Fatal("secret was rewritten despite being omitted from the patch")
	}
}

func TestAdminBackendsPatch_NewSecretIsResealed(t *testing.T) {
	srv, _, store := adminServer(t)
	assertStatus(t, adminCreate(t, srv, validCreateBody), 201)
	before, _ := store.GetBackend(context.Background(), "alpha")

	req, _ := http.NewRequest(http.MethodPatch, srv.URL+"/api/admin/backends/alpha",
		strings.NewReader(`{"secret_key":"ROTATED"}`))
	req.Header.Set("Content-Type", "application/json")
	assertStatus(t, mustDo(t, req), 200)

	after, _ := store.GetBackend(context.Background(), "alpha")
	if string(after.SecretKeyEnc) == string(before.SecretKeyEnc) {
		t.Fatal("secret should have changed after patch")
	}
	if string(after.SecretKeyEnc) == "ROTATED" {
		t.Fatal("secret stored in cleartext after patch")
	}
}

func TestAdminBackendsPatch_DisableUnregistersThenReenableRegisters(t *testing.T) {
	srv, reg, _ := adminServer(t)
	assertStatus(t, adminCreate(t, srv, validCreateBody), 201)

	// Disable.
	req, _ := http.NewRequest(http.MethodPatch, srv.URL+"/api/admin/backends/alpha",
		strings.NewReader(`{"enabled":false}`))
	req.Header.Set("Content-Type", "application/json")
	assertStatus(t, mustDo(t, req), 200)
	if _, ok := reg.Get("alpha"); ok {
		t.Fatal("disabled entry should be Unregistered")
	}

	// Re-enable.
	req, _ = http.NewRequest(http.MethodPatch, srv.URL+"/api/admin/backends/alpha",
		strings.NewReader(`{"enabled":true}`))
	req.Header.Set("Content-Type", "application/json")
	assertStatus(t, mustDo(t, req), 200)
	if _, ok := reg.Get("alpha"); !ok {
		t.Fatal("re-enabled entry should be Registered")
	}
}

func TestAdminBackendsPatch_RefusesYAMLSourced(t *testing.T) {
	srv, reg, _ := adminServer(t)
	if err := reg.RegisterWithSource(memory.New("yamlboi", "Y"), backend.SourceConfig); err != nil {
		t.Fatalf("preregister: %v", err)
	}
	req, _ := http.NewRequest(http.MethodPatch, srv.URL+"/api/admin/backends/yamlboi",
		strings.NewReader(`{"name":"hijack"}`))
	req.Header.Set("Content-Type", "application/json")
	assertStatus(t, mustDo(t, req), 409)
}

func TestAdminBackendsDelete_RemovesRowAndUnregisters(t *testing.T) {
	srv, reg, store := adminServer(t)
	assertStatus(t, adminCreate(t, srv, validCreateBody), 201)

	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/admin/backends/alpha", nil)
	assertStatus(t, mustDo(t, req), 204)

	if _, err := store.GetBackend(context.Background(), "alpha"); err == nil {
		t.Fatal("row should be gone")
	}
	if _, ok := reg.Get("alpha"); ok {
		t.Fatal("registry should be unregistered")
	}
}

func TestAdminBackendsDelete_RefusesYAMLSourced(t *testing.T) {
	srv, reg, _ := adminServer(t)
	if err := reg.RegisterWithSource(memory.New("yamlboi", "Y"), backend.SourceConfig); err != nil {
		t.Fatalf("preregister: %v", err)
	}
	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/admin/backends/yamlboi", nil)
	assertStatus(t, mustDo(t, req), 409)
}

func TestAdminBackendsDelete_NotFound(t *testing.T) {
	srv, _, _ := adminServer(t)
	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/admin/backends/ghost", nil)
	assertStatus(t, mustDo(t, req), 404)
}

func TestAdminBackendsTest_ProbesWithoutPersisting(t *testing.T) {
	srv, _, store := adminServer(t)
	body := `{
      "endpoint": "https://s3.example.com",
      "access_key": "a",
      "secret_key": "b"
    }`
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/admin/backends/test",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp := mustDo(t, req)
	assertStatus(t, resp, 200)
	var got map[string]any
	mustDecode(t, resp, &got)
	if _, hasHealthy := got["healthy"]; !hasHealthy {
		t.Fatalf("response missing healthy flag: %+v", got)
	}

	// Nothing persisted.
	rows, _ := store.ListBackends(context.Background())
	if len(rows) != 0 {
		t.Fatalf("test endpoint persisted rows: %d", len(rows))
	}
}

func TestAdminBackends_NoSealerReturns503(t *testing.T) {
	store, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "nosealer.db"))
	if err != nil {
		t.Fatalf("sqlite: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	d := &BackendDeps{
		Registry: backend.NewRegistry(),
		Logger:   slog.Default(),
		Audit:    audit.Noop{},
		Store:    store,
		Sealer:   nil,
	}
	r := chi.NewRouter()
	r.Post("/api/admin/backends", d.handleAdminCreateBackend)
	r.Patch("/api/admin/backends/{bid}", d.handleAdminPatchBackend)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	assertStatus(t, adminCreate(t, srv, validCreateBody), 503)
}
