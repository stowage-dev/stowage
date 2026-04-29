// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/stowage-dev/stowage/internal/auth"
	"github.com/stowage-dev/stowage/internal/s3proxy"
	"github.com/stowage-dev/stowage/internal/store/sqlite"
)

// fakeOperatorSnapshotter is a static stand-in for a live K8s informer.
// It lets the merged-view handler under test run without spinning up envtest
// or kubeconfig plumbing.
type fakeOperatorSnapshotter struct {
	creds []*s3proxy.VirtualCredential
	anon  []*s3proxy.AnonymousBinding
}

func (f *fakeOperatorSnapshotter) SnapshotCredentials() []*s3proxy.VirtualCredential {
	return f.creds
}

func (f *fakeOperatorSnapshotter) SnapshotAnonymousBindings() []*s3proxy.AnonymousBinding {
	return f.anon
}

func mountProxyView(t *testing.T, store *sqlite.Store, op OperatorSourceSnapshotter) *httptest.Server {
	t.Helper()
	deps := &S3ProxyViewDeps{Store: store, OperatorSource: op, Logger: slog.Default()}
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := auth.ContextWithIdentity(req.Context(), &auth.Identity{
				UserID: "tester", Username: "tester", Role: "admin",
			})
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})
	r.Get("/api/admin/s3-proxy/credentials", deps.handleListCredentials)
	r.Get("/api/admin/s3-proxy/anonymous", deps.handleListAnonymous)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv
}

func TestS3ProxyView_MergesSqliteAndOperatorCredentials(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "view.db"))
	if err != nil {
		t.Fatalf("sqlite: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	row := &sqlite.S3Credential{
		AccessKey:    "AKIAUI",
		SecretKeyEnc: []byte{0x01},
		BackendID:    "minio-prod",
		Description:  "tooling",
		Enabled:      true,
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
		UserID:       sql.NullString{String: "alice", Valid: true},
	}
	if err := row.MarshalBuckets([]string{"reports"}); err != nil {
		t.Fatalf("marshal buckets: %v", err)
	}
	if err := store.CreateS3Credential(ctx, row); err != nil {
		t.Fatalf("create: %v", err)
	}

	op := &fakeOperatorSnapshotter{
		creds: []*s3proxy.VirtualCredential{
			{
				AccessKeyID:    "AKIAOP",
				BackendName:    "minio-prod",
				BucketScopes:   []s3proxy.BucketScope{{BucketName: "claim-bucket", BackendName: "minio-prod"}},
				ClaimNamespace: "team-a",
				ClaimName:      "reports",
				Source:         "kubernetes",
			},
		},
	}

	srv := mountProxyView(t, store, op)
	resp, err := http.Get(srv.URL + "/api/admin/s3-proxy/credentials")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d", resp.StatusCode)
	}

	var got struct {
		Credentials []s3CredViewDTO `json:"credentials"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Credentials) != 2 {
		t.Fatalf("want 2 rows, got %d: %+v", len(got.Credentials), got.Credentials)
	}

	bySource := map[string]s3CredViewDTO{}
	for _, c := range got.Credentials {
		bySource[c.Source] = c
	}
	sqliteRow, ok := bySource["sqlite"]
	if !ok {
		t.Fatalf("missing sqlite row")
	}
	if sqliteRow.AccessKey != "AKIAUI" || sqliteRow.UserID != "alice" {
		t.Errorf("sqlite row mismatch: %+v", sqliteRow)
	}
	kubeRow, ok := bySource["kubernetes"]
	if !ok {
		t.Fatalf("missing kubernetes row")
	}
	if kubeRow.AccessKey != "AKIAOP" || kubeRow.ClaimNamespace != "team-a" || kubeRow.ClaimName != "reports" {
		t.Errorf("k8s row mismatch: %+v", kubeRow)
	}
	if len(kubeRow.Buckets) != 1 || kubeRow.Buckets[0] != "claim-bucket" {
		t.Errorf("k8s buckets: %+v", kubeRow.Buckets)
	}
}

func TestS3ProxyView_AnonymousMerges(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "view.db"))
	if err != nil {
		t.Fatalf("sqlite: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if err := store.UpsertS3AnonymousBinding(ctx, &sqlite.S3AnonymousBinding{
		BackendID:      "minio-prod",
		Bucket:         "ui-public",
		Mode:           "ReadOnly",
		PerSourceIPRPS: 5,
		CreatedAt:      time.Now().UTC(),
	}); err != nil {
		t.Fatalf("upsert anon: %v", err)
	}

	op := &fakeOperatorSnapshotter{
		anon: []*s3proxy.AnonymousBinding{
			{
				BackendName:    "minio-prod",
				BucketName:     "k8s-public",
				Mode:           "ReadOnly",
				PerSourceIPRPS: 12,
				Source:         "kubernetes",
			},
		},
	}

	srv := mountProxyView(t, store, op)
	resp, err := http.Get(srv.URL + "/api/admin/s3-proxy/anonymous")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d", resp.StatusCode)
	}

	var got struct {
		Bindings []s3AnonViewDTO `json:"bindings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Bindings) != 2 {
		t.Fatalf("want 2 bindings, got %d", len(got.Bindings))
	}
	bySource := map[string]s3AnonViewDTO{}
	for _, b := range got.Bindings {
		bySource[b.Source] = b
	}
	if bySource["sqlite"].Bucket != "ui-public" {
		t.Errorf("sqlite bucket: %+v", bySource["sqlite"])
	}
	if bySource["kubernetes"].Bucket != "k8s-public" || bySource["kubernetes"].PerSourceIPRPS != 12 {
		t.Errorf("k8s binding: %+v", bySource["kubernetes"])
	}
}
