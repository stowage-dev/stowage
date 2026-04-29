// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/stowage-dev/stowage/internal/audit"
	"github.com/stowage-dev/stowage/internal/auth"
	"github.com/stowage-dev/stowage/internal/backend"
	"github.com/stowage-dev/stowage/internal/backend/memory"
	"github.com/stowage-dev/stowage/internal/quotas"
	"github.com/stowage-dev/stowage/internal/store/sqlite"
)

// phase2Server wires the backend routes with a stub admin identity so the
// integration test can focus on Phase 2 behaviour without pulling in the
// full auth stack. The spec acceptance flow from §5 Phase 2 runs against it.
func phase2Server(t *testing.T) (*httptest.Server, *memory.Backend) {
	srv, mem, _, _ := phase2ServerWithQuotas(t, false)
	return srv, mem
}

// phase2ServerWithQuotas is like phase2Server but optionally wires a real
// quota service backed by an on-disk SQLite. Returns the SQLite store so
// tests can configure quota rows.
func phase2ServerWithQuotas(t *testing.T, withQuotas bool) (*httptest.Server, *memory.Backend, *sqlite.Store, *quotas.Service) {
	t.Helper()
	mem := memory.New("mem", "Memory Test")
	reg := backend.NewRegistry()
	if err := reg.Register(mem); err != nil {
		t.Fatalf("register: %v", err)
	}

	var (
		store *sqlite.Store
		qsvc  *quotas.Service
	)
	if withQuotas {
		s, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
		if err != nil {
			t.Fatalf("sqlite: %v", err)
		}
		t.Cleanup(func() { _ = s.Close() })
		store = s
		limits := quotas.NewSQLiteLimitSource(store, slog.Default())
		_ = limits.Reload(context.Background())
		qsvc = quotas.New(limits, store, reg, slog.Default())
	}

	var rec audit.Recorder = audit.Noop{}
	if store != nil {
		rec = audit.NewSQLiteRecorder(store, slog.Default(), nil)
	}
	d := &BackendDeps{Registry: reg, Logger: slog.Default(), Quotas: qsvc, Audit: rec}

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := auth.ContextWithIdentity(req.Context(), &auth.Identity{
				UserID: "t", Username: "t", Role: "admin",
			})
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})
	r.Route("/api/backends", func(r chi.Router) {
		r.Get("/", d.handleListBackends)
		r.Route("/{bid}", func(r chi.Router) {
			r.Get("/", d.handleGetBackend)
			r.Get("/health", d.handleProbeBackend)
			r.Route("/buckets", func(r chi.Router) {
				r.Get("/", d.handleListBuckets)
				r.Post("/", d.handleCreateBucket)
				r.Route("/{bucket}", func(r chi.Router) {
					r.Delete("/", d.handleDeleteBucket)
					r.Get("/versioning", d.handleGetBucketVersioning)
					r.Put("/versioning", d.handlePutBucketVersioning)
					r.Get("/cors", d.handleGetBucketCORS)
					r.Put("/cors", d.handlePutBucketCORS)
					r.Get("/policy", d.handleGetBucketPolicy)
					r.Put("/policy", d.handlePutBucketPolicy)
					r.Delete("/policy", d.handleDeleteBucketPolicy)
					r.Get("/lifecycle", d.handleGetBucketLifecycle)
					r.Put("/lifecycle", d.handlePutBucketLifecycle)
					r.Get("/quota", d.handleGetQuota)
					r.Put("/quota", d.handlePutQuota)
					r.Delete("/quota", d.handleDeleteQuota)
					r.Post("/quota/recompute", d.handleRecomputeQuota)
					r.Get("/objects", d.handleListObjects)
					r.Post("/objects/delete", d.handleBulkDelete)
					r.Post("/objects/delete-prefix", d.handleDeletePrefix)
					r.Post("/objects/folder", d.handleCreateFolder)
					r.Post("/objects/copy-prefix", d.handleCopyPrefix)
					r.Get("/objects/zip", d.handleZipDownload)
					r.Get("/object", d.handleGetObject)
					r.Head("/object", d.handleHeadObject)
					r.Post("/object", d.handleUploadObject)
					r.Delete("/object", d.handleDeleteObject)
					r.Post("/object/copy", d.handleCopyObject)
					r.Get("/object/info", d.handleHeadObject)
					r.Get("/object/tags", d.handleGetObjectTags)
					r.Put("/object/tags", d.handlePutObjectTags)
					r.Put("/object/metadata", d.handleUpdateObjectMetadata)
					r.Get("/object/versions", d.handleListObjectVersions)
				})
			})
		})
	})

	return httptest.NewServer(r), mem, store, qsvc
}

func TestPhase2AcceptanceFlow(t *testing.T) {
	srv, _ := phase2Server(t)
	defer srv.Close()

	base := srv.URL

	// 1. /api/backends lists the memory backend.
	resp := mustGet(t, base+"/api/backends")
	assertStatus(t, resp, 200)
	var listResp struct {
		Backends []struct {
			ID      string `json:"id"`
			Healthy bool   `json:"healthy"`
		} `json:"backends"`
	}
	mustDecode(t, resp, &listResp)
	if len(listResp.Backends) != 1 || listResp.Backends[0].ID != "mem" {
		t.Fatalf("backend list wrong: %+v", listResp)
	}

	// 2. Create bucket "photos".
	resp = mustPostJSON(t, base+"/api/backends/mem/buckets", `{"name":"photos"}`)
	assertStatus(t, resp, 201)

	// 3. Upload a small file via multipart form.
	body := "hello world — this is the byte-identical test payload"
	req := newMultipartUpload(t, base+"/api/backends/mem/buckets/photos/object", "hello.txt", body, "text/plain")
	resp = mustDo(t, req)
	assertStatus(t, resp, 201)

	// 4. Download and verify byte-identical.
	resp = mustGet(t, base+"/api/backends/mem/buckets/photos/object?key=hello.txt")
	assertStatus(t, resp, 200)
	gotBody, _ := io.ReadAll(resp.Body)
	if string(gotBody) != body {
		t.Fatalf("downloaded bytes differ:\n got %q\nwant %q", gotBody, body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/plain" {
		t.Fatalf("content-type=%q want text/plain", ct)
	}
	if cd := resp.Header.Get("Content-Disposition"); !strings.Contains(cd, `filename="hello.txt"`) {
		t.Fatalf("content-disposition=%q missing filename", cd)
	}

	// 5. HEAD returns metadata.
	req, _ = http.NewRequest(http.MethodHead, base+"/api/backends/mem/buckets/photos/object?key=hello.txt", nil)
	resp = mustDo(t, req)
	assertStatus(t, resp, 200)

	// 6. Range request returns the requested slice.
	req, _ = http.NewRequest(http.MethodGet, base+"/api/backends/mem/buckets/photos/object?key=hello.txt", nil)
	req.Header.Set("Range", "bytes=0-4")
	resp = mustDo(t, req)
	assertStatus(t, resp, 206)
	slice, _ := io.ReadAll(resp.Body)
	if string(slice) != "hello" {
		t.Fatalf("range slice=%q want %q", slice, "hello")
	}

	// 7. List objects — expect the one we uploaded.
	resp = mustGet(t, base+"/api/backends/mem/buckets/photos/objects")
	assertStatus(t, resp, 200)
	var objList struct {
		Objects []struct {
			Key  string `json:"key"`
			Size int64  `json:"size"`
		} `json:"objects"`
	}
	mustDecode(t, resp, &objList)
	if len(objList.Objects) != 1 || objList.Objects[0].Key != "hello.txt" {
		t.Fatalf("object list wrong: %+v", objList)
	}

	// 8. Folder create writes a zero-byte key ending in /. With the default
	// delimiter the key surfaces as a common prefix; flat listing shows the
	// zero-byte object directly.
	resp = mustPostJSON(t, base+"/api/backends/mem/buckets/photos/objects/folder", `{"key":"dir"}`)
	assertStatus(t, resp, 201)

	resp = mustGet(t, base+"/api/backends/mem/buckets/photos/objects?delimiter=")
	var flatList struct {
		Objects []struct {
			Key  string `json:"key"`
			Size int64  `json:"size"`
		} `json:"objects"`
	}
	mustDecode(t, resp, &flatList)
	sawFolder := false
	for _, o := range flatList.Objects {
		if o.Key == "dir/" && o.Size == 0 {
			sawFolder = true
		}
	}
	if !sawFolder {
		t.Fatalf("folder create did not produce dir/ key; got %+v", flatList.Objects)
	}

	// 9. Bulk delete.
	resp = mustPostJSON(t, base+"/api/backends/mem/buckets/photos/objects/delete",
		`{"keys":[{"key":"hello.txt"},{"key":"dir/"}]}`)
	assertStatus(t, resp, 200)

	// 10. Delete bucket.
	req, _ = http.NewRequest(http.MethodDelete, base+"/api/backends/mem/buckets/photos", nil)
	resp = mustDo(t, req)
	assertStatus(t, resp, 204)
}

func TestCopyObjectRenameFlow(t *testing.T) {
	srv, _ := phase2Server(t)
	defer srv.Close()
	base := srv.URL

	// Setup: bucket + one object.
	resp := mustPostJSON(t, base+"/api/backends/mem/buckets", `{"name":"docs"}`)
	assertStatus(t, resp, 201)
	req := newMultipartUpload(t, base+"/api/backends/mem/buckets/docs/object", "old.txt", "contents", "text/plain")
	resp = mustDo(t, req)
	assertStatus(t, resp, 201)

	// Same-bucket rename via copy + delete-source is the caller's job; this
	// test just covers the copy leg. Destination defaults to source bucket.
	resp = mustPostJSON(t, base+"/api/backends/mem/buckets/docs/object/copy",
		`{"src_key":"old.txt","dst_key":"new.txt"}`)
	assertStatus(t, resp, 200)
	var copyResp struct {
		Bucket string `json:"bucket"`
		Object struct {
			Key  string `json:"key"`
			Size int64  `json:"size"`
		} `json:"object"`
	}
	mustDecode(t, resp, &copyResp)
	if copyResp.Bucket != "docs" || copyResp.Object.Key != "new.txt" {
		t.Fatalf("copy response wrong: %+v", copyResp)
	}

	// Both keys now exist; source survives until the caller deletes it.
	resp = mustGet(t, base+"/api/backends/mem/buckets/docs/object?key=old.txt")
	assertStatus(t, resp, 200)
	resp = mustGet(t, base+"/api/backends/mem/buckets/docs/object?key=new.txt")
	assertStatus(t, resp, 200)

	// Identical src/dst is a 400, not a silent no-op.
	resp = mustPostJSON(t, base+"/api/backends/mem/buckets/docs/object/copy",
		`{"src_key":"new.txt","dst_key":"new.txt"}`)
	assertStatus(t, resp, 400)

	// Missing fields → 400.
	resp = mustPostJSON(t, base+"/api/backends/mem/buckets/docs/object/copy",
		`{"src_key":"new.txt"}`)
	assertStatus(t, resp, 400)

	// Path-traversal keys are rejected by validObjectKey.
	resp = mustPostJSON(t, base+"/api/backends/mem/buckets/docs/object/copy",
		`{"src_key":"new.txt","dst_key":"a/../../etc"}`)
	assertStatus(t, resp, 400)
}

func TestCopyObjectCrossBucket(t *testing.T) {
	srv, _ := phase2Server(t)
	defer srv.Close()
	base := srv.URL

	// Two buckets, object in the first.
	for _, b := range []string{"src-bkt", "dst-bkt"} {
		resp := mustPostJSON(t, base+"/api/backends/mem/buckets", fmt.Sprintf(`{"name":%q}`, b))
		assertStatus(t, resp, 201)
	}
	req := newMultipartUpload(t, base+"/api/backends/mem/buckets/src-bkt/object", "photo.bin", "payload", "application/octet-stream")
	resp := mustDo(t, req)
	assertStatus(t, resp, 201)

	// Copy across buckets via dst_bucket.
	resp = mustPostJSON(t, base+"/api/backends/mem/buckets/src-bkt/object/copy",
		`{"src_key":"photo.bin","dst_key":"archive/photo.bin","dst_bucket":"dst-bkt"}`)
	assertStatus(t, resp, 200)
	var out struct {
		Bucket string `json:"bucket"`
		Object struct {
			Key string `json:"key"`
		} `json:"object"`
	}
	mustDecode(t, resp, &out)
	if out.Bucket != "dst-bkt" || out.Object.Key != "archive/photo.bin" {
		t.Fatalf("cross-bucket copy response wrong: %+v", out)
	}

	// Source still exists, destination also exists.
	resp = mustGet(t, base+"/api/backends/mem/buckets/src-bkt/object?key=photo.bin")
	assertStatus(t, resp, 200)
	resp = mustGet(t, base+"/api/backends/mem/buckets/dst-bkt/object?key=archive/photo.bin")
	assertStatus(t, resp, 200)

	// Invalid destination bucket name → 400.
	resp = mustPostJSON(t, base+"/api/backends/mem/buckets/src-bkt/object/copy",
		`{"src_key":"photo.bin","dst_key":"photo.bin","dst_bucket":"BAD_NAME"}`)
	assertStatus(t, resp, 400)
}

func TestZipDownload(t *testing.T) {
	srv, _ := phase2Server(t)
	defer srv.Close()
	base := srv.URL

	resp := mustPostJSON(t, base+"/api/backends/mem/buckets", `{"name":"zips"}`)
	assertStatus(t, resp, 201)

	// Two loose objects and two inside a prefix. Go's multipart parser
	// runs path.Base on filenames, so nested keys must be set explicitly
	// via the `key` form field instead of relying on the filename.
	uploads := []struct {
		key, body string
	}{
		{"root.txt", "top-level contents"},
		{"notes.md", "# notes"},
		{"sub/a.txt", "alpha"},
		{"sub/b.txt", "bravo"},
	}
	for _, u := range uploads {
		req := newMultipartUploadWithKey(t, base+"/api/backends/mem/buckets/zips/object", u.key, u.body, "text/plain")
		resp = mustDo(t, req)
		assertStatus(t, resp, 201)
	}

	// Mix: two explicit keys + a trailing-slash prefix that should expand
	// to its two children.
	url := base + "/api/backends/mem/buckets/zips/objects/zip?key=root.txt&key=notes.md&key=sub/"
	resp = mustGet(t, url)
	assertStatus(t, resp, 200)
	if ct := resp.Header.Get("Content-Type"); ct != "application/zip" {
		t.Fatalf("content-type=%q want application/zip", ct)
	}
	if cd := resp.Header.Get("Content-Disposition"); !strings.Contains(cd, ".zip") {
		t.Fatalf("content-disposition=%q missing .zip", cd)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatalf("not a valid zip: %v", err)
	}
	got := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open %q: %v", f.Name, err)
		}
		b, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("read %q: %v", f.Name, err)
		}
		got[f.Name] = string(b)
	}

	want := map[string]string{
		"root.txt":  "top-level contents",
		"notes.md":  "# notes",
		"sub/a.txt": "alpha",
		"sub/b.txt": "bravo",
	}
	if len(got) != len(want) {
		t.Fatalf("zip entries=%d want %d; got=%v", len(got), len(want), keysOf(got))
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("zip[%q]=%q want %q", k, got[k], v)
		}
	}

	// Missing key param → 400.
	resp = mustGet(t, base+"/api/backends/mem/buckets/zips/objects/zip")
	assertStatus(t, resp, 400)

	// Invalid key → 400 before any zip bytes are written.
	resp = mustGet(t, base+"/api/backends/mem/buckets/zips/objects/zip?key=a/../b")
	assertStatus(t, resp, 400)
}

func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestObjectTagsRoundTrip(t *testing.T) {
	srv, _ := phase2Server(t)
	defer srv.Close()
	base := srv.URL

	resp := mustPostJSON(t, base+"/api/backends/mem/buckets", `{"name":"tagged"}`)
	assertStatus(t, resp, 201)
	req := newMultipartUpload(t, base+"/api/backends/mem/buckets/tagged/object", "doc.txt", "hi", "text/plain")
	resp = mustDo(t, req)
	assertStatus(t, resp, 201)

	// Initially empty.
	resp = mustGet(t, base+"/api/backends/mem/buckets/tagged/object/tags?key=doc.txt")
	assertStatus(t, resp, 200)
	var get struct {
		Tags map[string]string `json:"tags"`
	}
	mustDecode(t, resp, &get)
	if len(get.Tags) != 0 {
		t.Fatalf("expected empty tags; got %+v", get.Tags)
	}

	// Set tags.
	putReq, _ := http.NewRequest(http.MethodPut,
		base+"/api/backends/mem/buckets/tagged/object/tags?key=doc.txt",
		strings.NewReader(`{"tags":{"env":"prod","owner":"alice"}}`))
	putReq.Header.Set("Content-Type", "application/json")
	resp = mustDo(t, putReq)
	assertStatus(t, resp, 204)

	// Read back.
	resp = mustGet(t, base+"/api/backends/mem/buckets/tagged/object/tags?key=doc.txt")
	mustDecode(t, resp, &get)
	if get.Tags["env"] != "prod" || get.Tags["owner"] != "alice" || len(get.Tags) != 2 {
		t.Fatalf("tags round-trip wrong: %+v", get.Tags)
	}

	// Empty map clears. Decode into a fresh var — json.Decode merges into
	// existing maps rather than replacing them.
	putReq, _ = http.NewRequest(http.MethodPut,
		base+"/api/backends/mem/buckets/tagged/object/tags?key=doc.txt",
		strings.NewReader(`{"tags":{}}`))
	putReq.Header.Set("Content-Type", "application/json")
	resp = mustDo(t, putReq)
	assertStatus(t, resp, 204)
	resp = mustGet(t, base+"/api/backends/mem/buckets/tagged/object/tags?key=doc.txt")
	var cleared struct {
		Tags map[string]string `json:"tags"`
	}
	mustDecode(t, resp, &cleared)
	if len(cleared.Tags) != 0 {
		t.Fatalf("clear failed; got %+v", cleared.Tags)
	}

	// Too many tags → 400.
	payload := `{"tags":{"a":"1","b":"2","c":"3","d":"4","e":"5","f":"6","g":"7","h":"8","i":"9","j":"10","k":"11"}}`
	putReq, _ = http.NewRequest(http.MethodPut,
		base+"/api/backends/mem/buckets/tagged/object/tags?key=doc.txt",
		strings.NewReader(payload))
	putReq.Header.Set("Content-Type", "application/json")
	resp = mustDo(t, putReq)
	assertStatus(t, resp, 400)

	// Invalid key rejected.
	resp = mustGet(t, base+"/api/backends/mem/buckets/tagged/object/tags?key=a/../b")
	assertStatus(t, resp, 400)
}

func TestObjectMetadataUpdate(t *testing.T) {
	srv, _ := phase2Server(t)
	defer srv.Close()
	base := srv.URL

	resp := mustPostJSON(t, base+"/api/backends/mem/buckets", `{"name":"metas"}`)
	assertStatus(t, resp, 201)
	req := newMultipartUpload(t, base+"/api/backends/mem/buckets/metas/object", "file.bin", "body", "application/octet-stream")
	resp = mustDo(t, req)
	assertStatus(t, resp, 201)

	// Update metadata.
	putReq, _ := http.NewRequest(http.MethodPut,
		base+"/api/backends/mem/buckets/metas/object/metadata?key=file.bin",
		strings.NewReader(`{"metadata":{"project":"stowage","sha256":"abc"}}`))
	putReq.Header.Set("Content-Type", "application/json")
	resp = mustDo(t, putReq)
	assertStatus(t, resp, 200)
	var out struct {
		Key      string            `json:"key"`
		Metadata map[string]string `json:"metadata"`
	}
	mustDecode(t, resp, &out)
	if out.Metadata["project"] != "stowage" || out.Metadata["sha256"] != "abc" {
		t.Fatalf("metadata not reflected in response: %+v", out)
	}

	// HEAD via the JSON-returning GET /object/info surfaces it.
	resp = mustGet(t, base+"/api/backends/mem/buckets/metas/object/info?key=file.bin")
	assertStatus(t, resp, 200)
	mustDecode(t, resp, &out)
	if out.Metadata["project"] != "stowage" {
		t.Fatalf("metadata not persisted: %+v", out.Metadata)
	}

	// Clearing metadata.
	putReq, _ = http.NewRequest(http.MethodPut,
		base+"/api/backends/mem/buckets/metas/object/metadata?key=file.bin",
		strings.NewReader(`{"metadata":{}}`))
	putReq.Header.Set("Content-Type", "application/json")
	resp = mustDo(t, putReq)
	assertStatus(t, resp, 200)

	// Control-char keys rejected — the \x01 escape in the key name is
	// intentional (verifies the server rejects control chars in metadata keys).
	putReq, _ = http.NewRequest(http.MethodPut,
		base+"/api/backends/mem/buckets/metas/object/metadata?key=file.bin",
		strings.NewReader("{\"metadata\":{\"bad\x01\":\"x\"}}"))
	putReq.Header.Set("Content-Type", "application/json")
	resp = mustDo(t, putReq)
	assertStatus(t, resp, 400)
}

func TestListObjectVersions(t *testing.T) {
	srv, _ := phase2Server(t)
	defer srv.Close()
	base := srv.URL

	resp := mustPostJSON(t, base+"/api/backends/mem/buckets", `{"name":"vers"}`)
	assertStatus(t, resp, 201)

	// Two objects that share a common prefix — the handler must filter to
	// exact-key matches.
	for _, key := range []string{"doc.txt", "doc.txt.bak"} {
		req := newMultipartUpload(t, base+"/api/backends/mem/buckets/vers/object", key, "body", "text/plain")
		resp = mustDo(t, req)
		assertStatus(t, resp, 201)
	}

	resp = mustGet(t, base+"/api/backends/mem/buckets/vers/object/versions?key=doc.txt")
	assertStatus(t, resp, 200)
	var out struct {
		Versions []struct {
			Key      string `json:"key"`
			IsLatest bool   `json:"is_latest"`
			Size     int64  `json:"size"`
		} `json:"versions"`
	}
	mustDecode(t, resp, &out)
	if len(out.Versions) != 1 {
		t.Fatalf("expected 1 version for doc.txt, got %d: %+v", len(out.Versions), out.Versions)
	}
	if out.Versions[0].Key != "doc.txt" || !out.Versions[0].IsLatest {
		t.Fatalf("version row wrong: %+v", out.Versions[0])
	}

	// Missing/invalid key → 400.
	resp = mustGet(t, base+"/api/backends/mem/buckets/vers/object/versions")
	assertStatus(t, resp, 400)
	resp = mustGet(t, base+"/api/backends/mem/buckets/vers/object/versions?key=a/../b")
	assertStatus(t, resp, 400)
}

func TestBucketSettingsRoundTrips(t *testing.T) {
	srv, _ := phase2Server(t)
	defer srv.Close()
	base := srv.URL

	resp := mustPostJSON(t, base+"/api/backends/mem/buckets", `{"name":"settings-bk"}`)
	assertStatus(t, resp, 201)

	// ---- Versioning round-trip ----
	resp = mustGet(t, base+"/api/backends/mem/buckets/settings-bk/versioning")
	assertStatus(t, resp, 200)
	var ver struct {
		Enabled bool `json:"enabled"`
	}
	mustDecode(t, resp, &ver)
	if ver.Enabled {
		t.Fatalf("fresh bucket should have versioning disabled; got %+v", ver)
	}
	putReq, _ := http.NewRequest(http.MethodPut,
		base+"/api/backends/mem/buckets/settings-bk/versioning",
		strings.NewReader(`{"enabled":true}`))
	putReq.Header.Set("Content-Type", "application/json")
	resp = mustDo(t, putReq)
	assertStatus(t, resp, 200)

	resp = mustGet(t, base+"/api/backends/mem/buckets/settings-bk/versioning")
	mustDecode(t, resp, &ver)
	if !ver.Enabled {
		t.Fatalf("versioning should be enabled after PUT; got %+v", ver)
	}

	// ---- CORS round-trip ----
	corsBody := `{"rules":[{"allowed_origins":["https://app.example.com"],"allowed_methods":["GET","PUT"],"max_age_seconds":3600}]}`
	putReq, _ = http.NewRequest(http.MethodPut,
		base+"/api/backends/mem/buckets/settings-bk/cors",
		strings.NewReader(corsBody))
	putReq.Header.Set("Content-Type", "application/json")
	resp = mustDo(t, putReq)
	assertStatus(t, resp, 204)

	resp = mustGet(t, base+"/api/backends/mem/buckets/settings-bk/cors")
	assertStatus(t, resp, 200)
	var cors struct {
		Rules []struct {
			AllowedOrigins []string `json:"allowed_origins"`
			AllowedMethods []string `json:"allowed_methods"`
			MaxAgeSeconds  int      `json:"max_age_seconds"`
		} `json:"rules"`
	}
	mustDecode(t, resp, &cors)
	if len(cors.Rules) != 1 || cors.Rules[0].MaxAgeSeconds != 3600 {
		t.Fatalf("cors round-trip wrong: %+v", cors)
	}

	// CORS with disallowed method → 400.
	bogus := `{"rules":[{"allowed_origins":["*"],"allowed_methods":["TRACE"]}]}`
	putReq, _ = http.NewRequest(http.MethodPut,
		base+"/api/backends/mem/buckets/settings-bk/cors",
		strings.NewReader(bogus))
	putReq.Header.Set("Content-Type", "application/json")
	resp = mustDo(t, putReq)
	assertStatus(t, resp, 400)

	// ---- Policy round-trip ----
	policy := `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":"*","Action":"s3:GetObject","Resource":"arn:aws:s3:::settings-bk/*"}]}`
	putReq, _ = http.NewRequest(http.MethodPut,
		base+"/api/backends/mem/buckets/settings-bk/policy",
		strings.NewReader(`{"policy":`+strconvQuote(policy)+`}`))
	putReq.Header.Set("Content-Type", "application/json")
	resp = mustDo(t, putReq)
	assertStatus(t, resp, 204)

	resp = mustGet(t, base+"/api/backends/mem/buckets/settings-bk/policy")
	assertStatus(t, resp, 200)
	var pol struct {
		Policy string `json:"policy"`
	}
	mustDecode(t, resp, &pol)
	if pol.Policy != policy {
		t.Fatalf("policy round-trip wrong:\n got %q\nwant %q", pol.Policy, policy)
	}

	// Invalid JSON policy → 400.
	putReq, _ = http.NewRequest(http.MethodPut,
		base+"/api/backends/mem/buckets/settings-bk/policy",
		strings.NewReader(`{"policy":"not json {"}`))
	putReq.Header.Set("Content-Type", "application/json")
	resp = mustDo(t, putReq)
	assertStatus(t, resp, 400)

	// Delete clears it.
	delReq, _ := http.NewRequest(http.MethodDelete,
		base+"/api/backends/mem/buckets/settings-bk/policy", nil)
	resp = mustDo(t, delReq)
	assertStatus(t, resp, 204)

	resp = mustGet(t, base+"/api/backends/mem/buckets/settings-bk/policy")
	mustDecode(t, resp, &pol)
	if pol.Policy != "" {
		t.Fatalf("policy should be empty after delete; got %q", pol.Policy)
	}

	// ---- Lifecycle round-trip ----
	lifecycleBody := `{"rules":[{"id":"expire-old","prefix":"tmp/","enabled":true,"expiration_days":30,"abort_incomplete_days":7}]}`
	putReq, _ = http.NewRequest(http.MethodPut,
		base+"/api/backends/mem/buckets/settings-bk/lifecycle",
		strings.NewReader(lifecycleBody))
	putReq.Header.Set("Content-Type", "application/json")
	resp = mustDo(t, putReq)
	assertStatus(t, resp, 204)

	resp = mustGet(t, base+"/api/backends/mem/buckets/settings-bk/lifecycle")
	assertStatus(t, resp, 200)
	var lc struct {
		Rules []struct {
			ID             string `json:"id"`
			Prefix         string `json:"prefix"`
			Enabled        bool   `json:"enabled"`
			ExpirationDays int    `json:"expiration_days"`
		} `json:"rules"`
	}
	mustDecode(t, resp, &lc)
	if len(lc.Rules) != 1 || lc.Rules[0].ExpirationDays != 30 || lc.Rules[0].ID != "expire-old" {
		t.Fatalf("lifecycle round-trip wrong: %+v", lc)
	}

	// Lifecycle rule with no actions → 400.
	noAction := `{"rules":[{"id":"empty","prefix":"x/","enabled":true}]}`
	putReq, _ = http.NewRequest(http.MethodPut,
		base+"/api/backends/mem/buckets/settings-bk/lifecycle",
		strings.NewReader(noAction))
	putReq.Header.Set("Content-Type", "application/json")
	resp = mustDo(t, putReq)
	assertStatus(t, resp, 400)
}

// strconvQuote wraps a Go string in JSON-quoted form via the encoding/json
// path so embedded control chars and quotes are escaped correctly.
func strconvQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func TestQuotaEnforcedOnUpload(t *testing.T) {
	srv, _, store, qsvc := phase2ServerWithQuotas(t, true)
	defer srv.Close()
	base := srv.URL

	resp := mustPostJSON(t, base+"/api/backends/mem/buckets", `{"name":"capped"}`)
	assertStatus(t, resp, 201)

	// Upload a 4 KiB object — succeeds (no quota yet).
	body := strings.Repeat("x", 4*1024)
	req := newMultipartUpload(t, base+"/api/backends/mem/buckets/capped/object", "first.bin", body, "application/octet-stream")
	resp = mustDo(t, req)
	assertStatus(t, resp, 201)

	// Configure a 6 KiB hard quota directly via the store.
	if err := store.UpsertQuota(context.Background(), &sqlite.BucketQuota{
		BackendID: "mem", Bucket: "capped",
		HardBytes: 6 * 1024,
		UpdatedAt: time.Now(), UpdatedBy: "admin",
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	// In production the PUT /quota handler calls ReloadLimits; bypassing
	// that here means the in-memory cache is stale, so do it manually.
	if err := qsvc.ReloadLimits(context.Background()); err != nil {
		t.Fatalf("reload: %v", err)
	}

	// 1 KiB upload — fits (4 + 1 ≤ 6).
	body = strings.Repeat("y", 1*1024)
	req = newMultipartUpload(t, base+"/api/backends/mem/buckets/capped/object", "second.bin", body, "application/octet-stream")
	resp = mustDo(t, req)
	assertStatus(t, resp, 201)

	// 3 KiB upload — would push to 8 KiB, hits the cap → 507.
	body = strings.Repeat("z", 3*1024)
	req = newMultipartUpload(t, base+"/api/backends/mem/buckets/capped/object", "third.bin", body, "application/octet-stream")
	resp = mustDo(t, req)
	assertStatus(t, resp, http.StatusInsufficientStorage)
	bodyBytes, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(bodyBytes), "quota_exceeded") {
		t.Fatalf("expected quota_exceeded in body, got %s", bodyBytes)
	}

	// Quota CRUD round-trip via the API.
	resp = mustGet(t, base+"/api/backends/mem/buckets/capped/quota")
	assertStatus(t, resp, 200)
	var got struct {
		Configured bool  `json:"configured"`
		HardBytes  int64 `json:"hard_bytes"`
		HasUsage   bool  `json:"has_usage"`
		UsageBytes int64 `json:"usage_bytes"`
	}
	mustDecode(t, resp, &got)
	if !got.Configured || got.HardBytes != 6*1024 {
		t.Fatalf("quota status wrong: %+v", got)
	}
	if !got.HasUsage || got.UsageBytes != 5*1024 {
		// 4 KiB initial + 1 KiB second upload = 5 KiB recorded.
		t.Fatalf("usage wrong: %+v", got)
	}

	// Recompute via API forces a fresh scan.
	resp, err := http.Post(base+"/api/backends/mem/buckets/capped/quota/recompute", "application/json", nil)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	assertStatus(t, resp, 200)

	// DELETE clears the quota.
	delReq, _ := http.NewRequest(http.MethodDelete, base+"/api/backends/mem/buckets/capped/quota", nil)
	resp = mustDo(t, delReq)
	assertStatus(t, resp, 204)

	// After clearing, the previously-rejected upload should succeed.
	body = strings.Repeat("z", 3*1024)
	req = newMultipartUpload(t, base+"/api/backends/mem/buckets/capped/object", "third.bin", body, "application/octet-stream")
	resp = mustDo(t, req)
	assertStatus(t, resp, 201)
}

// TestCrossBackendTransfer wires two memory backends through the proxy and
// asserts that streaming copy works end-to-end + records the right audit
// + quota events.
func TestCrossBackendTransfer(t *testing.T) {
	t.Helper()
	ctx := context.Background()

	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "xfer.db"))
	if err != nil {
		t.Fatalf("sqlite: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	src := memory.New("src", "Source")
	dst := memory.New("dst", "Destination")
	reg := backend.NewRegistry()
	_ = reg.Register(src)
	_ = reg.Register(dst)
	_ = src.CreateBucket(ctx, "in", "")
	_ = dst.CreateBucket(ctx, "out", "")
	if _, err := src.PutObject(ctx, backend.PutObjectRequest{
		Bucket: "in", Key: "report.txt",
		Body: strings.NewReader("hello cross-backend"), Size: 19,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	limits := quotas.NewSQLiteLimitSource(store, slog.Default())
	_ = limits.Reload(context.Background())
	qsvc := quotas.New(limits, store, reg, slog.Default())
	d := &BackendDeps{
		Registry: reg,
		Logger:   slog.Default(),
		Quotas:   qsvc,
		Audit:    audit.NewSQLiteRecorder(store, slog.Default(), nil),
	}

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := auth.ContextWithIdentity(req.Context(), &auth.Identity{
				UserID: "u", Username: "u", Role: "admin",
			})
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})
	r.Route("/api/backends/{bid}/buckets/{bucket}", func(r chi.Router) {
		r.Post("/object/copy", d.handleCopyObject)
	})
	srv := httptest.NewServer(r)
	defer srv.Close()

	body := `{"src_key":"report.txt","dst_key":"archive/report.txt","dst_backend":"dst","dst_bucket":"out"}`
	resp := mustPostJSON(t, srv.URL+"/api/backends/src/buckets/in/object/copy", body)
	assertStatus(t, resp, 200)

	var out struct {
		Backend string `json:"backend"`
		Bucket  string `json:"bucket"`
		Object  struct {
			Key  string `json:"key"`
			Size int64  `json:"size"`
		} `json:"object"`
	}
	mustDecode(t, resp, &out)
	if out.Backend != "dst" || out.Bucket != "out" || out.Object.Key != "archive/report.txt" {
		t.Fatalf("transfer response wrong: %+v", out)
	}

	// Bytes landed at the destination.
	rd, err := dst.GetObject(ctx, "out", "archive/report.txt", "", nil)
	if err != nil {
		t.Fatalf("dst get: %v", err)
	}
	got, _ := io.ReadAll(rd)
	rd.Close()
	if string(got) != "hello cross-backend" {
		t.Fatalf("dst bytes=%q want %q", got, "hello cross-backend")
	}

	// Source still has the object — transfer is non-destructive.
	if _, err := src.HeadObject(ctx, "in", "report.txt", ""); err != nil {
		t.Fatalf("src head: %v", err)
	}

	// Audit row should be tagged object.transfer.
	rows, _ := store.ListAuditEvents(ctx, sqlite.AuditFilter{Action: "object.transfer"})
	if len(rows) == 0 {
		t.Fatalf("expected an object.transfer audit row")
	}
}

func TestObjectDeleteEmitsAuditRow(t *testing.T) {
	srv, _, store, _ := phase2ServerWithQuotas(t, true)
	defer srv.Close()
	base := srv.URL

	resp := mustPostJSON(t, base+"/api/backends/mem/buckets", `{"name":"audited"}`)
	assertStatus(t, resp, 201)
	req := newMultipartUpload(t, base+"/api/backends/mem/buckets/audited/object", "f.txt", "x", "text/plain")
	resp = mustDo(t, req)
	assertStatus(t, resp, 201)

	delReq, _ := http.NewRequest(http.MethodDelete,
		base+"/api/backends/mem/buckets/audited/object?key=f.txt", nil)
	resp = mustDo(t, delReq)
	assertStatus(t, resp, 204)

	// The phase2 harness uses a stub identity; both the upload and the
	// delete should produce audit rows on the wired store.
	events, err := store.ListAuditEvents(context.Background(), sqlite.AuditFilter{})
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	gotActions := map[string]int{}
	for _, e := range events {
		gotActions[e.Action]++
	}
	if gotActions["object.upload"] == 0 {
		t.Fatalf("expected at least one object.upload event, got %+v", gotActions)
	}
	if gotActions["object.delete"] == 0 {
		t.Fatalf("expected at least one object.delete event, got %+v", gotActions)
	}
}

func TestInvalidBucketNameRejected(t *testing.T) {
	srv, _ := phase2Server(t)
	defer srv.Close()
	resp := mustPostJSON(t, srv.URL+"/api/backends/mem/buckets", `{"name":"UPPER"}`)
	assertStatus(t, resp, 400)
}

func TestUnknownBackend404(t *testing.T) {
	srv, _ := phase2Server(t)
	defer srv.Close()
	resp := mustGet(t, srv.URL+"/api/backends/ghost/buckets")
	assertStatus(t, resp, 404)
}

// ---- copy-prefix / delete-prefix ---------------------------------------

// readNDJSON parses the streamed response body into a slice of decoded
// events. The handler emits one JSON object per line.
func readNDJSON(t *testing.T, r *http.Response) []map[string]any {
	t.Helper()
	defer r.Body.Close()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("read ndjson: %v", err)
	}
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(string(body)), "\n") {
		if line == "" {
			continue
		}
		var ev map[string]any
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("decode ndjson line %q: %v", line, err)
		}
		out = append(out, ev)
	}
	return out
}

// findEvent returns the first event whose "event" field matches name.
func findEvent(events []map[string]any, name string) map[string]any {
	for _, ev := range events {
		if ev["event"] == name {
			return ev
		}
	}
	return nil
}

func TestCopyPrefixSameBackend(t *testing.T) {
	srv, mem := phase2Server(t)
	defer srv.Close()
	base := srv.URL
	ctx := context.Background()

	if err := mem.CreateBucket(ctx, "src", ""); err != nil {
		t.Fatalf("create bucket: %v", err)
	}
	for _, k := range []string{"reports/q1/a.txt", "reports/q1/sub/b.txt", "reports/q1/", "reports/other.txt"} {
		if _, err := mem.PutObject(ctx, backend.PutObjectRequest{
			Bucket: "src", Key: k, Body: strings.NewReader("x"), Size: 1,
		}); err != nil {
			t.Fatalf("seed %s: %v", k, err)
		}
	}

	resp := mustPostJSON(t, base+"/api/backends/mem/buckets/src/objects/copy-prefix",
		`{"src_prefix":"reports/q1/","dst_prefix":"archive/q1/"}`)
	assertStatus(t, resp, 200)
	if ct := resp.Header.Get("Content-Type"); ct != "application/x-ndjson" {
		t.Fatalf("content-type=%q want application/x-ndjson", ct)
	}
	events := readNDJSON(t, resp)
	done := findEvent(events, "done")
	if done == nil {
		t.Fatalf("missing done event in %+v", events)
	}
	// Three keys under reports/q1/ — two regular + the placeholder.
	if got := done["copied"]; got != float64(3) {
		t.Fatalf("copied=%v want 3", got)
	}
	if got := done["failed"]; got != float64(0) {
		t.Fatalf("failed=%v want 0", got)
	}

	// Destination tree exists.
	for _, k := range []string{"archive/q1/a.txt", "archive/q1/sub/b.txt", "archive/q1/"} {
		if _, err := mem.HeadObject(ctx, "src", k, ""); err != nil {
			t.Errorf("dst missing %s: %v", k, err)
		}
	}
	// Source preserved (copy is non-destructive).
	if _, err := mem.HeadObject(ctx, "src", "reports/q1/a.txt", ""); err != nil {
		t.Errorf("src disappeared: %v", err)
	}
	// Sibling outside the prefix untouched.
	if _, err := mem.HeadObject(ctx, "src", "reports/other.txt", ""); err != nil {
		t.Errorf("sibling disappeared: %v", err)
	}
}

func TestCopyPrefixCrossBucket(t *testing.T) {
	srv, mem := phase2Server(t)
	defer srv.Close()
	base := srv.URL
	ctx := context.Background()

	for _, b := range []string{"src", "dst"} {
		if err := mem.CreateBucket(ctx, b, ""); err != nil {
			t.Fatalf("create bucket %s: %v", b, err)
		}
	}
	for _, k := range []string{"docs/a.txt", "docs/nested/b.txt"} {
		if _, err := mem.PutObject(ctx, backend.PutObjectRequest{
			Bucket: "src", Key: k, Body: strings.NewReader("hi"), Size: 2,
		}); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	resp := mustPostJSON(t, base+"/api/backends/mem/buckets/src/objects/copy-prefix",
		`{"src_prefix":"docs/","dst_prefix":"copies/","dst_bucket":"dst"}`)
	assertStatus(t, resp, 200)
	events := readNDJSON(t, resp)
	if done := findEvent(events, "done"); done == nil || done["copied"] != float64(2) {
		t.Fatalf("done event wrong: %+v", done)
	}
	for _, k := range []string{"copies/a.txt", "copies/nested/b.txt"} {
		if _, err := mem.HeadObject(ctx, "dst", k, ""); err != nil {
			t.Errorf("dst missing %s: %v", k, err)
		}
	}
}

func TestCopyPrefixCrossBackend(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	src := memory.New("src", "Source")
	dst := memory.New("dst", "Destination")
	reg := backend.NewRegistry()
	_ = reg.Register(src)
	_ = reg.Register(dst)
	_ = src.CreateBucket(ctx, "in", "")
	_ = dst.CreateBucket(ctx, "out", "")
	for _, k := range []string{"folder/a.txt", "folder/sub/b.txt"} {
		if _, err := src.PutObject(ctx, backend.PutObjectRequest{
			Bucket: "in", Key: k, Body: strings.NewReader("payload"), Size: 7,
		}); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	d := &BackendDeps{Registry: reg, Logger: slog.Default(), Audit: audit.Noop{}}
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			c := auth.ContextWithIdentity(req.Context(), &auth.Identity{
				UserID: "t", Username: "t", Role: "admin",
			})
			next.ServeHTTP(w, req.WithContext(c))
		})
	})
	r.Route("/api/backends/{bid}/buckets/{bucket}", func(r chi.Router) {
		r.Post("/objects/copy-prefix", d.handleCopyPrefix)
	})
	srv := httptest.NewServer(r)
	defer srv.Close()

	resp := mustPostJSON(t, srv.URL+"/api/backends/src/buckets/in/objects/copy-prefix",
		`{"src_prefix":"folder/","dst_prefix":"mirror/","dst_backend":"dst","dst_bucket":"out"}`)
	assertStatus(t, resp, 200)
	events := readNDJSON(t, resp)
	done := findEvent(events, "done")
	if done == nil || done["copied"] != float64(2) {
		t.Fatalf("done=%+v events=%+v", done, events)
	}
	for _, k := range []string{"mirror/a.txt", "mirror/sub/b.txt"} {
		if _, err := dst.HeadObject(ctx, "out", k, ""); err != nil {
			t.Errorf("dst missing %s: %v", k, err)
		}
	}
	// Source kept.
	if _, err := src.HeadObject(ctx, "in", "folder/a.txt", ""); err != nil {
		t.Errorf("src dropped: %v", err)
	}
}

func TestCopyPrefixRejectsSelfOverlap(t *testing.T) {
	srv, mem := phase2Server(t)
	defer srv.Close()
	ctx := context.Background()
	_ = mem.CreateBucket(ctx, "src", "")

	// dst inside src on the same backend+bucket would loop.
	resp := mustPostJSON(t, srv.URL+"/api/backends/mem/buckets/src/objects/copy-prefix",
		`{"src_prefix":"a/","dst_prefix":"a/b/"}`)
	assertStatus(t, resp, 400)
	// Identical also rejected.
	resp = mustPostJSON(t, srv.URL+"/api/backends/mem/buckets/src/objects/copy-prefix",
		`{"src_prefix":"a/","dst_prefix":"a/"}`)
	assertStatus(t, resp, 400)
	// Cross-bucket overlap is fine.
	_ = mem.CreateBucket(ctx, "other", "")
	resp = mustPostJSON(t, srv.URL+"/api/backends/mem/buckets/src/objects/copy-prefix",
		`{"src_prefix":"a/","dst_prefix":"a/","dst_bucket":"other"}`)
	assertStatus(t, resp, 200)
}

func TestCopyPrefixRejectsBadPrefix(t *testing.T) {
	srv, mem := phase2Server(t)
	defer srv.Close()
	_ = mem.CreateBucket(context.Background(), "src", "")
	// No trailing slash.
	resp := mustPostJSON(t, srv.URL+"/api/backends/mem/buckets/src/objects/copy-prefix",
		`{"src_prefix":"reports","dst_prefix":"archive/"}`)
	assertStatus(t, resp, 400)
	resp = mustPostJSON(t, srv.URL+"/api/backends/mem/buckets/src/objects/copy-prefix",
		`{"src_prefix":"reports/","dst_prefix":"archive"}`)
	assertStatus(t, resp, 400)
}

func TestDeletePrefix(t *testing.T) {
	srv, mem := phase2Server(t)
	defer srv.Close()
	base := srv.URL
	ctx := context.Background()

	_ = mem.CreateBucket(ctx, "src", "")
	for _, k := range []string{"trash/a.txt", "trash/sub/b.txt", "trash/", "keep/c.txt"} {
		if _, err := mem.PutObject(ctx, backend.PutObjectRequest{
			Bucket: "src", Key: k, Body: strings.NewReader("z"), Size: 1,
		}); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	resp := mustPostJSON(t, base+"/api/backends/mem/buckets/src/objects/delete-prefix",
		`{"prefix":"trash/"}`)
	assertStatus(t, resp, 200)
	events := readNDJSON(t, resp)
	done := findEvent(events, "done")
	if done == nil || done["deleted"] != float64(3) {
		t.Fatalf("done=%+v", done)
	}
	for _, k := range []string{"trash/a.txt", "trash/sub/b.txt", "trash/"} {
		if _, err := mem.HeadObject(ctx, "src", k, ""); err == nil {
			t.Errorf("expected %s gone", k)
		}
	}
	// Sibling outside prefix untouched.
	if _, err := mem.HeadObject(ctx, "src", "keep/c.txt", ""); err != nil {
		t.Errorf("sibling vanished: %v", err)
	}
}

func TestDeletePrefixRejectsEmpty(t *testing.T) {
	srv, mem := phase2Server(t)
	defer srv.Close()
	_ = mem.CreateBucket(context.Background(), "src", "")
	// Empty prefix would target the entire bucket — refused.
	resp := mustPostJSON(t, srv.URL+"/api/backends/mem/buckets/src/objects/delete-prefix",
		`{"prefix":""}`)
	assertStatus(t, resp, 400)
	// No trailing slash.
	resp = mustPostJSON(t, srv.URL+"/api/backends/mem/buckets/src/objects/delete-prefix",
		`{"prefix":"trash"}`)
	assertStatus(t, resp, 400)
}

// ---- helpers ------------------------------------------------------------

func mustGet(t *testing.T, url string) *http.Response {
	t.Helper()
	r, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	return r
}

func mustDo(t *testing.T, req *http.Request) *http.Response {
	t.Helper()
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", req.Method, req.URL, err)
	}
	return r
}

func mustPostJSON(t *testing.T, url, body string) *http.Response {
	t.Helper()
	r, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	return r
}

func mustDecode(t *testing.T, r *http.Response, v any) {
	t.Helper()
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

func assertStatus(t *testing.T, r *http.Response, want int) {
	t.Helper()
	if r.StatusCode != want {
		body, _ := io.ReadAll(r.Body)
		t.Fatalf("status=%d want %d; body=%s", r.StatusCode, want, body)
	}
}

func newMultipartUpload(t *testing.T, url, filename, body, contentType string) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	hdr := make(map[string][]string)
	hdr["Content-Disposition"] = []string{
		fmt.Sprintf(`form-data; name="file"; filename="%s"`, filename),
	}
	hdr["Content-Type"] = []string{contentType}
	part, err := mw.CreatePart(hdr)
	if err != nil {
		t.Fatalf("create part: %v", err)
	}
	if _, err := part.Write([]byte(body)); err != nil {
		t.Fatalf("write part: %v", err)
	}
	_ = mw.Close()

	req, err := http.NewRequest(http.MethodPost, url, &buf)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req
}

// newMultipartUploadWithKey sets the `key` form field so the server stores
// the object at a nested path (Go's multipart parser calls path.Base on the
// filename, so we can't rely on filename alone).
func newMultipartUploadWithKey(t *testing.T, url, key, body, contentType string) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.WriteField("key", key); err != nil {
		t.Fatalf("write field: %v", err)
	}
	hdr := make(map[string][]string)
	hdr["Content-Disposition"] = []string{
		fmt.Sprintf(`form-data; name="file"; filename="%s"`, path.Base(key)),
	}
	hdr["Content-Type"] = []string{contentType}
	part, err := mw.CreatePart(hdr)
	if err != nil {
		t.Fatalf("create part: %v", err)
	}
	if _, err := part.Write([]byte(body)); err != nil {
		t.Fatalf("write part: %v", err)
	}
	_ = mw.Close()

	req, err := http.NewRequest(http.MethodPost, url, &buf)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req
}
