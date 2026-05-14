// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package s3proxy

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/stowage-dev/stowage/internal/secrets"
	"github.com/stowage-dev/stowage/internal/store/sqlite"
)

// openTestStore opens a fresh sqlite store under t.TempDir. Mirrors the
// helper in internal/store/sqlite/pins_test.go but lives in this package
// to avoid an import cycle.
func openTestStore(t *testing.T) *sqlite.Store {
	t.Helper()
	store, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "s3proxy.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func newTestSealer(t *testing.T) *secrets.Sealer {
	t.Helper()
	// 32 bytes hex-encoded — never a real key.
	const k = "0011223344556677889900112233445566778899001122334455667788990011"
	s, err := secrets.New(k)
	require.NoError(t, err)
	return s
}

// insertCred is a small fixture for SQLite source tests. It writes a row
// directly via the store CRUD so the source has something to read.
func insertCred(t *testing.T, store *sqlite.Store, sealer *secrets.Sealer,
	akid, secret, backend string, buckets []string, enabled bool, expires *time.Time,
) {
	t.Helper()
	enc, err := sealer.Seal([]byte(secret))
	require.NoError(t, err)
	now := time.Now().UTC()
	c := &sqlite.S3Credential{
		AccessKey:    akid,
		SecretKeyEnc: enc,
		BackendID:    backend,
		Enabled:      enabled,
		CreatedAt:    now,
		UpdatedAt:    now,
		CreatedBy:    sql.NullString{String: "admin", Valid: true},
		UpdatedBy:    sql.NullString{String: "admin", Valid: true},
	}
	require.NoError(t, c.MarshalBuckets(buckets))
	if expires != nil {
		c.ExpiresAt = sql.NullTime{Time: *expires, Valid: true}
	}
	require.NoError(t, store.CreateS3Credential(context.Background(), c))
}

func TestSQLiteSource_RoundTrip(t *testing.T) {
	store := openTestStore(t)
	sealer := newTestSealer(t)
	src := NewSQLiteSource(store, sealer, nil)

	insertCred(t, store, sealer, "AKIASRC1", "supersecret", "minio",
		[]string{"uploads", "archive"}, true, nil)

	require.NoError(t, src.Reload(context.Background()))
	require.Equal(t, 1, src.Size())

	got, ok := src.Lookup("AKIASRC1")
	require.True(t, ok)
	require.Equal(t, "supersecret", got.SecretAccessKey)
	require.Equal(t, "minio", got.BackendName)
	require.Equal(t, "sqlite", got.Source)
	require.Len(t, got.BucketScopes, 2)
	require.Equal(t, "uploads", got.BucketScopes[0].BucketName)
}

func TestSQLiteSource_DisabledSkipped(t *testing.T) {
	store := openTestStore(t)
	sealer := newTestSealer(t)
	src := NewSQLiteSource(store, sealer, nil)

	insertCred(t, store, sealer, "AKIAOFF", "secret", "minio",
		[]string{"a"}, false, nil)
	require.NoError(t, src.Reload(context.Background()))

	_, ok := src.Lookup("AKIAOFF")
	require.False(t, ok, "disabled credential should not be cached")
}

func TestSQLiteSource_ExpiredSkippedAtReload(t *testing.T) {
	store := openTestStore(t)
	sealer := newTestSealer(t)
	src := NewSQLiteSource(store, sealer, nil)

	expired := time.Now().UTC().Add(-1 * time.Hour)
	insertCred(t, store, sealer, "AKIAEXPIRED", "secret", "minio",
		[]string{"a"}, true, &expired)
	require.NoError(t, src.Reload(context.Background()))
	_, ok := src.Lookup("AKIAEXPIRED")
	require.False(t, ok, "expired credential should not be cached at Reload")
}

func TestSQLiteSource_AnonymousBindingsRoundTrip(t *testing.T) {
	store := openTestStore(t)
	sealer := newTestSealer(t)
	src := NewSQLiteSource(store, sealer, nil)

	now := time.Now().UTC()
	require.NoError(t, store.UpsertS3AnonymousBinding(context.Background(),
		&sqlite.S3AnonymousBinding{
			BackendID: "minio", Bucket: "Public", Mode: "ReadOnly",
			PerSourceIPRPS: 50, CreatedAt: now,
			CreatedBy: sql.NullString{String: "admin", Valid: true},
		}))

	require.NoError(t, src.Reload(context.Background()))

	got, ok := src.LookupAnon("public") // case-insensitive
	require.True(t, ok)
	require.Equal(t, "Public", got.BucketName)
	require.Equal(t, "minio", got.BackendName)
	require.InDelta(t, 50, got.PerSourceIPRPS, 0.001)
}

func TestSQLiteSource_CORSRulesRoundTrip(t *testing.T) {
	store := openTestStore(t)
	sealer := newTestSealer(t)
	src := NewSQLiteSource(store, sealer, nil)

	now := time.Now().UTC()
	require.NoError(t, store.UpsertS3BucketCORS(context.Background(),
		&sqlite.S3BucketCORS{
			BackendID: "minio", Bucket: "Uploads",
			Rules:     `[{"allowed_origins":["https://docs.example.com"],"allowed_methods":["POST"],"max_age_seconds":600}]`,
			CreatedAt: now, UpdatedAt: now,
		}))

	require.NoError(t, src.Reload(context.Background()))

	rules, ok := src.LookupCORS("uploads") // case-insensitive
	require.True(t, ok)
	require.Len(t, rules, 1)
	require.Equal(t, []string{"https://docs.example.com"}, rules[0].AllowedOrigins)
	require.Equal(t, 600, rules[0].MaxAgeSeconds)
}

func TestSQLiteSource_CORSUnionAcrossBackends(t *testing.T) {
	// Same bucket name on two backends → rules should be unioned, so a
	// preflight succeeds if any backend's rule covers the origin/method.
	store := openTestStore(t)
	sealer := newTestSealer(t)
	src := NewSQLiteSource(store, sealer, nil)

	now := time.Now().UTC()
	require.NoError(t, store.UpsertS3BucketCORS(context.Background(),
		&sqlite.S3BucketCORS{
			BackendID: "minio", Bucket: "shared",
			Rules:     `[{"allowed_origins":["https://a.example"],"allowed_methods":["GET"]}]`,
			CreatedAt: now, UpdatedAt: now,
		}))
	require.NoError(t, store.UpsertS3BucketCORS(context.Background(),
		&sqlite.S3BucketCORS{
			BackendID: "garage", Bucket: "shared",
			Rules:     `[{"allowed_origins":["https://b.example"],"allowed_methods":["POST"]}]`,
			CreatedAt: now, UpdatedAt: now,
		}))

	require.NoError(t, src.Reload(context.Background()))

	rules, ok := src.LookupCORS("shared")
	require.True(t, ok)
	require.Len(t, rules, 2, "rules from both backends must be unioned")
}

func TestSQLiteSource_CORSMalformedRowsSkipped(t *testing.T) {
	store := openTestStore(t)
	sealer := newTestSealer(t)
	src := NewSQLiteSource(store, sealer, nil)

	now := time.Now().UTC()
	require.NoError(t, store.UpsertS3BucketCORS(context.Background(),
		&sqlite.S3BucketCORS{
			BackendID: "minio", Bucket: "good",
			Rules:     `[{"allowed_origins":["*"],"allowed_methods":["GET"]}]`,
			CreatedAt: now, UpdatedAt: now,
		}))
	// Garbage rules JSON — the reload should warn and skip, not error.
	require.NoError(t, store.UpsertS3BucketCORS(context.Background(),
		&sqlite.S3BucketCORS{
			BackendID: "minio", Bucket: "broken",
			Rules:     `not json`,
			CreatedAt: now, UpdatedAt: now,
		}))

	require.NoError(t, src.Reload(context.Background()))

	_, ok := src.LookupCORS("good")
	require.True(t, ok)
	_, ok = src.LookupCORS("broken")
	require.False(t, ok)
}

func TestSQLiteSource_RequiresSealer(t *testing.T) {
	store := openTestStore(t)
	src := NewSQLiteSource(store, nil, nil)
	err := src.Reload(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "sealer")
}

func TestSQLiteSource_ReloadReplacesAtomically(t *testing.T) {
	store := openTestStore(t)
	sealer := newTestSealer(t)
	src := NewSQLiteSource(store, sealer, nil)

	insertCred(t, store, sealer, "AKIAOLD", "old", "minio", []string{"a"}, true, nil)
	require.NoError(t, src.Reload(context.Background()))
	require.Equal(t, 1, src.Size())

	// Replace: delete old, add new.
	require.NoError(t, store.DeleteS3Credential(context.Background(), "AKIAOLD"))
	insertCred(t, store, sealer, "AKIANEW", "new", "minio", []string{"b"}, true, nil)
	require.NoError(t, src.Reload(context.Background()))

	_, oldOK := src.Lookup("AKIAOLD")
	require.False(t, oldOK, "old credential should be evicted on Reload")
	got, newOK := src.Lookup("AKIANEW")
	require.True(t, newOK)
	require.Equal(t, "new", got.SecretAccessKey)
}
