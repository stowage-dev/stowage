// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"context"
	"encoding/json"
	"io"
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
	"github.com/stowage-dev/stowage/internal/shares"
	"github.com/stowage-dev/stowage/internal/store/sqlite"
)

// shareServer spins up a chi router with just the share routes (authed +
// public), a stubbed admin identity for the /api/shares group, and a real
// SQLite store. Kept separate from phase2Server because shares need auth
// context plus the service, while the backend endpoints don't.
func shareServer(t *testing.T) (*httptest.Server, *shares.Service, *memory.Backend) {
	t.Helper()
	ctx := context.Background()

	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatalf("sqlite: %v", err)
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
		Bucket: "docs", Key: "hello.txt",
		Body: strings.NewReader("hi there"), Size: 8,
	}); err != nil {
		t.Fatalf("put: %v", err)
	}

	svc := &shares.Service{Store: store, Backends: reg, Logger: slog.Default()}
	deps := &ShareDeps{
		Service: svc,
		RateLim: auth.NewRateLimiter(100, time.Minute),
		Logger:  slog.Default(),
		Unlock:  MustNewUnlockSigner(),
	}

	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				ctx := auth.ContextWithIdentity(req.Context(), &auth.Identity{
					UserID: "user-1", Username: "alice", Role: "user",
				})
				next.ServeHTTP(w, req.WithContext(ctx))
			})
		})
		r.Route("/api/shares", func(r chi.Router) {
			r.Get("/", deps.handleListShares)
			r.Post("/", deps.handleCreateShare)
			r.Delete("/{id}", deps.handleRevokeShare)
		})
	})
	r.Get("/s/{code}/info", deps.handleShareInfo)
	r.Post("/s/{code}/unlock", deps.handleShareUnlock)
	r.Get("/s/{code}/raw", deps.handleShareRaw)

	return httptest.NewServer(r), svc, mem
}

func TestShareHTTPLifecycle(t *testing.T) {
	srv, _, _ := shareServer(t)
	defer srv.Close()

	// 1. Create.
	body := `{"backend_id":"mem","bucket":"docs","key":"hello.txt"}`
	resp := mustPostJSON(t, srv.URL+"/api/shares/", body)
	assertStatus(t, resp, 201)
	var created shareDTO
	mustDecode(t, resp, &created)
	if created.Code == "" || created.URL != "/s/"+created.Code {
		t.Fatalf("unexpected create response: %+v", created)
	}

	// 2. /info returns metadata without consuming a download.
	resp = mustGet(t, srv.URL+"/s/"+created.Code+"/info")
	assertStatus(t, resp, 200)
	var info shareInfoDTO
	mustDecode(t, resp, &info)
	if info.Name != "hello.txt" || info.Size != 8 {
		t.Fatalf("info wrong: %+v", info)
	}
	if info.HasPassword || info.RawURL != "/s/"+created.Code+"/raw" {
		t.Fatalf("info wrong: %+v", info)
	}

	// 3. /raw streams the byte-identical body and bumps the download count.
	resp = mustGet(t, srv.URL+"/s/"+created.Code+"/raw")
	assertStatus(t, resp, 200)
	got, _ := io.ReadAll(resp.Body)
	if string(got) != "hi there" {
		t.Fatalf("body=%q want %q", got, "hi there")
	}

	// 4. List mine: should include our share with download_count=1.
	resp = mustGet(t, srv.URL+"/api/shares/")
	assertStatus(t, resp, 200)
	var list struct {
		Shares []shareDTO `json:"shares"`
	}
	mustDecode(t, resp, &list)
	if len(list.Shares) != 1 || list.Shares[0].ID != created.ID {
		t.Fatalf("list mine wrong: %+v", list)
	}
	if list.Shares[0].DownloadCount != 1 {
		t.Fatalf("download count should be 1 after one /raw, got %d", list.Shares[0].DownloadCount)
	}

	// 5. Revoke.
	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/shares/"+created.ID, nil)
	resp = mustDo(t, req)
	assertStatus(t, resp, 204)

	// 6. /info after revoke → 410 Gone with structured error.
	resp = mustGet(t, srv.URL+"/s/"+created.Code+"/info")
	assertStatus(t, resp, 410)
	// 7. /raw after revoke → 410 Gone.
	resp = mustGet(t, srv.URL+"/s/"+created.Code+"/raw")
	assertStatus(t, resp, 410)
}

func TestShareHTTPPasswordProtected(t *testing.T) {
	srv, _, _ := shareServer(t)
	defer srv.Close()

	body := `{"backend_id":"mem","bucket":"docs","key":"hello.txt","password":"let-me-in"}`
	resp := mustPostJSON(t, srv.URL+"/api/shares/", body)
	assertStatus(t, resp, 201)
	var created shareDTO
	mustDecode(t, resp, &created)

	// /info without unlock cookie → 401 password_required (no metadata leak).
	resp = mustGet(t, srv.URL+"/s/"+created.Code+"/info")
	assertStatus(t, resp, 401)

	// /raw without unlock cookie → 401.
	resp = mustGet(t, srv.URL+"/s/"+created.Code+"/raw")
	assertStatus(t, resp, 401)

	// /unlock with wrong password → 401 password_mismatch.
	resp = mustPostJSON(t, srv.URL+"/s/"+created.Code+"/unlock", `{"password":"nope"}`)
	assertStatus(t, resp, 401)

	// /unlock with correct password → 200 + Set-Cookie.
	resp = mustPostJSON(t, srv.URL+"/s/"+created.Code+"/unlock", `{"password":"let-me-in"}`)
	assertStatus(t, resp, 200)
	var infoAfterUnlock shareInfoDTO
	mustDecode(t, resp, &infoAfterUnlock)
	if infoAfterUnlock.Name != "hello.txt" {
		t.Fatalf("expected metadata in unlock response, got %+v", infoAfterUnlock)
	}
	cookies := resp.Cookies()
	var unlockCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "stowage_unlock_"+created.Code {
			unlockCookie = c
			break
		}
	}
	if unlockCookie == nil {
		t.Fatalf("expected stowage_unlock_<code> cookie, got %+v", cookies)
	}

	// /info with the cookie → 200 metadata.
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/s/"+created.Code+"/info", nil)
	req.AddCookie(unlockCookie)
	resp = mustDo(t, req)
	assertStatus(t, resp, 200)

	// /raw with the cookie → 200 bytes.
	req, _ = http.NewRequest(http.MethodGet, srv.URL+"/s/"+created.Code+"/raw", nil)
	req.AddCookie(unlockCookie)
	resp = mustDo(t, req)
	assertStatus(t, resp, 200)
	got, _ := io.ReadAll(resp.Body)
	if string(got) != "hi there" {
		t.Fatalf("body=%q want %q", got, "hi there")
	}

	// Tampered cookie value → still 401.
	tampered := *unlockCookie
	tampered.Value = "9999999999.deadbeef"
	req, _ = http.NewRequest(http.MethodGet, srv.URL+"/s/"+created.Code+"/raw", nil)
	req.AddCookie(&tampered)
	resp = mustDo(t, req)
	assertStatus(t, resp, 401)
}

func TestShareHTTPNonOwnerCannotRevoke(t *testing.T) {
	// Custom server with a second user path.
	ctx := context.Background()
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatalf("sqlite: %v", err)
	}
	defer store.Close()

	reg := backend.NewRegistry()
	mem := memory.New("mem", "Memory")
	_ = reg.Register(mem)
	_ = mem.CreateBucket(ctx, "docs", "")
	_, _ = mem.PutObject(ctx, backend.PutObjectRequest{
		Bucket: "docs", Key: "hello.txt",
		Body: strings.NewReader("hi"), Size: 2,
	})

	svc := &shares.Service{Store: store, Backends: reg, Logger: slog.Default()}
	deps := &ShareDeps{Service: svc, RateLim: auth.NewRateLimiter(100, time.Minute), Logger: slog.Default()}

	acting := &auth.Identity{UserID: "user-1", Username: "alice", Role: "user"}
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			next.ServeHTTP(w, req.WithContext(auth.ContextWithIdentity(req.Context(), acting)))
		})
	})
	r.Post("/api/shares/", deps.handleCreateShare)
	r.Delete("/api/shares/{id}", deps.handleRevokeShare)

	srv := httptest.NewServer(r)
	defer srv.Close()

	// User 1 creates a share.
	resp, _ := http.Post(srv.URL+"/api/shares/", "application/json",
		strings.NewReader(`{"backend_id":"mem","bucket":"docs","key":"hello.txt"}`))
	var created shareDTO
	_ = json.NewDecoder(resp.Body).Decode(&created)
	resp.Body.Close()

	// Swap to a different non-admin user and try to revoke.
	*acting = auth.Identity{UserID: "user-2", Username: "bob", Role: "user"}
	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/shares/"+created.ID, nil)
	r2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer r2.Body.Close()
	// Service returns ErrNotFound → 404, not 403 (don't leak existence).
	if r2.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", r2.StatusCode)
	}
}

// TestShareRawForcesAttachmentOnActiveContent is the regression guard for
// F-9: SVG / HTML / xhtml served from /s/<code>/raw must come down as
// attachment regardless of the share's stored disposition or a recipient-
// supplied ?inline=1, and must carry the nosniff + sandbox headers that
// neutralise script execution if the browser renders the bytes anyway.
func TestShareRawForcesAttachmentOnActiveContent(t *testing.T) {
	srv, _, mem := shareServer(t)
	defer srv.Close()
	ctx := context.Background()

	svg := `<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`
	if _, err := mem.PutObject(ctx, backend.PutObjectRequest{
		Bucket: "docs", Key: "x.svg",
		Body:        strings.NewReader(svg),
		Size:        int64(len(svg)),
		ContentType: "image/svg+xml",
	}); err != nil {
		t.Fatalf("put svg: %v", err)
	}

	// Owner explicitly asked for inline disposition. The raw handler must
	// override that for active content types.
	body := `{"backend_id":"mem","bucket":"docs","key":"x.svg","disposition":"inline"}`
	resp := mustPostJSON(t, srv.URL+"/api/shares/", body)
	assertStatus(t, resp, 201)
	var created shareDTO
	mustDecode(t, resp, &created)

	cases := []struct{ name, url string }{
		{"default", srv.URL + "/s/" + created.Code + "/raw"},
		{"inline-query-override", srv.URL + "/s/" + created.Code + "/raw?inline=1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resp := mustGet(t, c.url)
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status=%d want 200", resp.StatusCode)
			}
			disp := resp.Header.Get("Content-Disposition")
			if !strings.HasPrefix(disp, "attachment") {
				t.Errorf("Content-Disposition=%q; SVG must be served as attachment", disp)
			}
			if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
				t.Errorf("X-Content-Type-Options=%q want %q", got, "nosniff")
			}
			csp := resp.Header.Get("Content-Security-Policy")
			if !strings.Contains(csp, "sandbox") {
				t.Errorf("Content-Security-Policy=%q must include sandbox", csp)
			}
		})
	}
}

// TestShareUnlockCookieIsCodeBound is the regression guard for F-3: an
// unlock cookie minted for share A must not unlock share B even when the
// attacker renames the cookie to stowage_unlock_<B>.
func TestShareUnlockCookieIsCodeBound(t *testing.T) {
	srv, _, mem := shareServer(t)
	defer srv.Close()
	ctx := context.Background()

	// Two password-protected shares pointing at different objects so the
	// "wrong" share is observably wrong if the gate ever leaks.
	if _, err := mem.PutObject(ctx, backend.PutObjectRequest{
		Bucket: "docs", Key: "secret.txt",
		Body: strings.NewReader("classified"), Size: 10,
	}); err != nil {
		t.Fatalf("put: %v", err)
	}

	mkShare := func(key, password string) shareDTO {
		t.Helper()
		body := `{"backend_id":"mem","bucket":"docs","key":"` + key + `","password":"` + password + `"}`
		resp := mustPostJSON(t, srv.URL+"/api/shares/", body)
		assertStatus(t, resp, 201)
		var sh shareDTO
		mustDecode(t, resp, &sh)
		return sh
	}

	a := mkShare("hello.txt", "alpha-pw")
	b := mkShare("secret.txt", "bravo-pw")

	// Unlock A → grab the cookie.
	resp := mustPostJSON(t, srv.URL+"/s/"+a.Code+"/unlock", `{"password":"alpha-pw"}`)
	assertStatus(t, resp, 200)
	var unlockA *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == "stowage_unlock_"+a.Code {
			unlockA = c
		}
	}
	if unlockA == nil {
		t.Fatalf("missing unlock cookie for A")
	}

	// Replay A's signed value under B's cookie name.
	replay := &http.Cookie{Name: "stowage_unlock_" + b.Code, Value: unlockA.Value}
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/s/"+b.Code+"/raw", nil)
	req.AddCookie(replay)
	resp = mustDo(t, req)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("cross-code replay should be 401; got %d body=%s", resp.StatusCode, body)
	}
}

// TestShareRawIsActiveContentTypeUnit covers the helper directly: drives
// every branch including the parameter-suffixed Content-Type a backend may
// return (e.g. "text/html; charset=utf-8") and the extension-only path.
func TestShareRawIsActiveContentTypeUnit(t *testing.T) {
	cases := []struct {
		ct, key string
		want    bool
	}{
		{"image/svg+xml", "a.svg", true},
		{"image/SVG+XML", "a.bin", true},
		{"text/html; charset=utf-8", "a.bin", true},
		{"application/xhtml+xml", "a", true},
		{"application/xml", "a", true},
		{"text/xml", "a", true},
		{"", "a.svg", true},
		{"", "a.html", true},
		{"", "a.htm", true},
		{"", "a.xml", true},
		{"image/png", "a.png", false},
		{"text/plain", "a.txt", false},
		{"application/pdf", "a.pdf", false},
		{"", "a.bin", false},
	}
	for _, c := range cases {
		got := isActiveContentType(c.ct, c.key)
		if got != c.want {
			t.Errorf("isActiveContentType(%q,%q)=%v want %v", c.ct, c.key, got, c.want)
		}
	}
}
