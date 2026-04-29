// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stowage-dev/stowage/internal/backend"
	"github.com/stowage-dev/stowage/internal/backend/memory"
	"github.com/stowage-dev/stowage/internal/secrets"
	"github.com/stowage-dev/stowage/internal/store/sqlite"
)

func newStore(t *testing.T) *sqlite.Store {
	t.Helper()
	store, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "hydrate.db"))
	if err != nil {
		t.Fatalf("sqlite: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func newSealer(t *testing.T) *secrets.Sealer {
	t.Helper()
	s, err := secrets.New(strings.Repeat("ab", 32))
	if err != nil {
		t.Fatalf("sealer: %v", err)
	}
	return s
}

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func insertRow(t *testing.T, store *sqlite.Store, sealer *secrets.Sealer, id string, enabled bool) {
	t.Helper()
	enc, err := sealer.Seal([]byte("SECRETKEY"))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	now := time.Now().UTC()
	row := &sqlite.Backend{
		ID:           id,
		Name:         id,
		Type:         "s3v4",
		Endpoint:     "https://s3.example.com",
		Region:       "us-east-1",
		PathStyle:    true,
		AccessKey:    "AKIA-" + id,
		SecretKeyEnc: enc,
		Enabled:      enabled,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := store.CreateBackend(context.Background(), row); err != nil {
		t.Fatalf("CreateBackend: %v", err)
	}
}

func TestHydrate_RegistersEnabledRowsAsDBSource(t *testing.T) {
	store := newStore(t)
	sealer := newSealer(t)
	insertRow(t, store, sealer, "alpha", true)
	insertRow(t, store, sealer, "bravo", true)

	reg := backend.NewRegistry()
	if err := hydrateFromStore(context.Background(), reg, store, sealer, discardLogger()); err != nil {
		t.Fatalf("hydrate: %v", err)
	}
	for _, id := range []string{"alpha", "bravo"} {
		src, ok := reg.Source(id)
		if !ok {
			t.Fatalf("%s missing from registry", id)
		}
		if src != backend.SourceDB {
			t.Fatalf("%s source=%q want db", id, src)
		}
	}
}

func TestHydrate_SkipsDisabledRows(t *testing.T) {
	store := newStore(t)
	sealer := newSealer(t)
	insertRow(t, store, sealer, "off", false)

	reg := backend.NewRegistry()
	if err := hydrateFromStore(context.Background(), reg, store, sealer, discardLogger()); err != nil {
		t.Fatalf("hydrate: %v", err)
	}
	if _, ok := reg.Get("off"); ok {
		t.Fatal("disabled row should not be registered")
	}
}

func TestHydrate_YAMLWinsOnIDCollision(t *testing.T) {
	store := newStore(t)
	sealer := newSealer(t)
	insertRow(t, store, sealer, "shared", true)

	reg := backend.NewRegistry()
	// Pretend a YAML-defined backend with the same id is already loaded.
	yamlBackend := memory.New("shared", "yaml-shared")
	if err := reg.RegisterWithSource(yamlBackend, backend.SourceConfig); err != nil {
		t.Fatalf("preregister: %v", err)
	}

	// Capture warn-level logs to confirm the collision was reported.
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))

	if err := hydrateFromStore(context.Background(), reg, store, sealer, logger); err != nil {
		t.Fatalf("hydrate: %v", err)
	}
	src, _ := reg.Source("shared")
	if src != backend.SourceConfig {
		t.Fatalf("source=%q want config (yaml should win)", src)
	}
	got, _ := reg.Get("shared")
	if got != yamlBackend {
		t.Fatal("registry slot should still hold the yaml backend, not be replaced")
	}
	if !strings.Contains(buf.String(), "shadowed by yaml") {
		t.Fatalf("expected collision warning in log, got: %s", buf.String())
	}
}

func TestHydrate_SealedSecretWithoutSealerErrors(t *testing.T) {
	store := newStore(t)
	sealer := newSealer(t)
	insertRow(t, store, sealer, "alpha", true)

	reg := backend.NewRegistry()
	err := hydrateFromStore(context.Background(), reg, store, nil, discardLogger())
	if err == nil {
		t.Fatal("hydrate should fail when row has a sealed secret but sealer is nil")
	}
	if !strings.Contains(err.Error(), "STOWAGE_SECRET_KEY") {
		t.Fatalf("error should mention STOWAGE_SECRET_KEY; got %v", err)
	}
}

func TestHydrate_TamperedCiphertextErrors(t *testing.T) {
	store := newStore(t)
	sealer := newSealer(t)
	insertRow(t, store, sealer, "alpha", true)

	// Corrupt the body of the sealed secret (not the version/key_id bytes —
	// those have their own dedicated errors). Flipping a byte in the GCM
	// tag region forces an authentication failure.
	row, _ := store.GetBackend(context.Background(), "alpha")
	row.SecretKeyEnc[len(row.SecretKeyEnc)-1] ^= 0x01
	patch := sqlite.BackendPatch{}
	patch.SetSecret(row.SecretKeyEnc)
	if err := store.UpdateBackend(context.Background(), "alpha", patch); err != nil {
		t.Fatalf("update: %v", err)
	}

	reg := backend.NewRegistry()
	err := hydrateFromStore(context.Background(), reg, store, sealer, discardLogger())
	if err == nil {
		t.Fatal("hydrate should fail on tampered ciphertext")
	}
}

func TestHydrate_NilStoreIsNoop(t *testing.T) {
	reg := backend.NewRegistry()
	if err := hydrateFromStore(context.Background(), reg, nil, nil, discardLogger()); err != nil {
		t.Fatalf("hydrate(nil store): %v", err)
	}
}

func TestHydrate_EmptyStoreIsNoop(t *testing.T) {
	store := newStore(t)
	reg := backend.NewRegistry()
	if err := hydrateFromStore(context.Background(), reg, store, newSealer(t), discardLogger()); err != nil {
		t.Fatalf("hydrate: %v", err)
	}
	if len(reg.List()) != 0 {
		t.Fatalf("registry should still be empty, got %d entries", len(reg.List()))
	}
}
