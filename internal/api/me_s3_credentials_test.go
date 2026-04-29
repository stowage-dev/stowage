// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
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
	"github.com/stowage-dev/stowage/internal/secrets"
	"github.com/stowage-dev/stowage/internal/store/sqlite"
)

// mountMyCreds wires the /api/me/s3-credentials routes against a sealer-equipped
// store. The middleware injects an Identity for `userID` so handlers can scope.
func mountMyCreds(t *testing.T, userID string) (*httptest.Server, *sqlite.Store, *S3CredentialDeps) {
	t.Helper()
	ctx := context.Background()
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "me.db"))
	if err != nil {
		t.Fatalf("sqlite: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	sealer, err := secrets.New(strings.Repeat("ab", 32))
	if err != nil {
		t.Fatalf("sealer: %v", err)
	}

	deps := &S3CredentialDeps{
		Store:  store,
		Sealer: sealer,
		Audit:  audit.Noop{},
		Logger: slog.Default(),
	}

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := auth.ContextWithIdentity(req.Context(), &auth.Identity{
				UserID: userID, Username: userID, Role: "user",
			})
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})
	r.Route("/api/me/s3-credentials", func(r chi.Router) {
		r.Get("/", deps.handleMyList)
		r.Post("/", deps.handleMyCreate)
		r.Patch("/{akid}", deps.handleMyPatch)
		r.Delete("/{akid}", deps.handleMyDelete)
	})

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv, store, deps
}

func TestMyS3Credentials_CreateThenList(t *testing.T) {
	srv, _, _ := mountMyCreds(t, "alice")

	body := `{"backend_id":"minio-prod","buckets":["reports"],"description":"weekly export"}`
	resp, err := http.Post(srv.URL+"/api/me/s3-credentials/", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status: %d", resp.StatusCode)
	}
	var created s3CredDTO
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	resp.Body.Close()
	if created.SecretKey == "" {
		t.Fatalf("secret_key should be returned on create")
	}
	if created.UserID != "alice" {
		t.Fatalf("created.user_id = %q, want alice", created.UserID)
	}

	resp, err = http.Get(srv.URL + "/api/me/s3-credentials/")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	var list struct {
		Credentials []s3CredDTO `json:"credentials"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list.Credentials) != 1 {
		t.Fatalf("expect 1 cred, got %d", len(list.Credentials))
	}
	if list.Credentials[0].SecretKey != "" {
		t.Fatalf("list rows must not include secret_key")
	}
}

func TestMyS3Credentials_DoesNotShowOthersCreds(t *testing.T) {
	ctx := context.Background()
	srv, store, _ := mountMyCreds(t, "alice")

	// Bob's credential — alice should never see it.
	bob := &sqlite.S3Credential{
		AccessKey:    "AKIABOB",
		SecretKeyEnc: []byte{0x00},
		BackendID:    "minio-prod",
		Enabled:      true,
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
		UserID:       sql.NullString{String: "bob", Valid: true},
	}
	if err := bob.MarshalBuckets([]string{"shared"}); err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := store.CreateS3Credential(ctx, bob); err != nil {
		t.Fatalf("seed bob: %v", err)
	}

	resp, err := http.Get(srv.URL + "/api/me/s3-credentials/")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	var list struct {
		Credentials []s3CredDTO `json:"credentials"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&list)
	if len(list.Credentials) != 0 {
		t.Fatalf("alice must not see bob's creds; got %+v", list.Credentials)
	}

	// Alice deleting bob's key returns 404 (existence not disclosed).
	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/me/s3-credentials/AKIABOB", nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status: %d, want 404", resp.StatusCode)
	}
}

func TestMyS3Credentials_PatchOwn(t *testing.T) {
	srv, _, _ := mountMyCreds(t, "alice")

	body := `{"backend_id":"minio-prod","buckets":["reports"]}`
	resp, _ := http.Post(srv.URL+"/api/me/s3-credentials/", "application/json", bytes.NewBufferString(body))
	var created s3CredDTO
	_ = json.NewDecoder(resp.Body).Decode(&created)
	resp.Body.Close()

	patch := `{"enabled":false,"description":"paused"}`
	req, _ := http.NewRequest(http.MethodPatch, srv.URL+"/api/me/s3-credentials/"+created.AccessKey, bytes.NewBufferString(patch))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d", resp.StatusCode)
	}
	var updated s3CredDTO
	_ = json.NewDecoder(resp.Body).Decode(&updated)
	if updated.Enabled {
		t.Errorf("enabled should be false: %+v", updated)
	}
	if updated.Description != "paused" {
		t.Errorf("description: %q", updated.Description)
	}
}
