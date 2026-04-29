// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package sqlite_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/stowage-dev/stowage/internal/store/sqlite"
)

func newBackend(id string) *sqlite.Backend {
	now := time.Now().UTC()
	return &sqlite.Backend{
		ID:           id,
		Name:         "Backend " + id,
		Type:         "s3v4",
		Endpoint:     "https://s3.example.com",
		Region:       "us-east-1",
		PathStyle:    true,
		AccessKey:    "AKIA-" + id,
		SecretKeyEnc: []byte{0x01, 0x00, 0xde, 0xad, 0xbe, 0xef},
		Enabled:      true,
		CreatedAt:    now,
		UpdatedAt:    now,
		CreatedBy:    sql.NullString{String: "admin", Valid: true},
		UpdatedBy:    sql.NullString{String: "admin", Valid: true},
	}
}

func TestBackendCreateAndGet(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	b := newBackend("alpha")
	if err := store.CreateBackend(ctx, b); err != nil {
		t.Fatalf("CreateBackend: %v", err)
	}

	got, err := store.GetBackend(ctx, "alpha")
	if err != nil {
		t.Fatalf("GetBackend: %v", err)
	}
	if got.ID != b.ID || got.Name != b.Name || got.Endpoint != b.Endpoint {
		t.Fatalf("got %+v want %+v", got, b)
	}
	if got.PathStyle != true || got.Enabled != true {
		t.Fatalf("bool round-trip wrong: pathStyle=%v enabled=%v", got.PathStyle, got.Enabled)
	}
	if !bytes.Equal(got.SecretKeyEnc, b.SecretKeyEnc) {
		t.Fatalf("secret round-trip mismatch: got %x want %x", got.SecretKeyEnc, b.SecretKeyEnc)
	}
}

func TestBackendNotFound(t *testing.T) {
	store := openTestStore(t)
	_, err := store.GetBackend(context.Background(), "missing")
	if !errors.Is(err, sqlite.ErrBackendNotFound) {
		t.Fatalf("err=%v want ErrBackendNotFound", err)
	}
}

func TestBackendDuplicateID(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	if err := store.CreateBackend(ctx, newBackend("alpha")); err != nil {
		t.Fatalf("first create: %v", err)
	}
	err := store.CreateBackend(ctx, newBackend("alpha"))
	if !errors.Is(err, sqlite.ErrBackendIDTaken) {
		t.Fatalf("err=%v want ErrBackendIDTaken", err)
	}
}

func TestBackendList(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	for _, id := range []string{"charlie", "alpha", "bravo"} {
		if err := store.CreateBackend(ctx, newBackend(id)); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
	}
	got, err := store.ListBackends(ctx)
	if err != nil {
		t.Fatalf("ListBackends: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len=%d want 3", len(got))
	}
	want := []string{"alpha", "bravo", "charlie"}
	for i, b := range got {
		if b.ID != want[i] {
			t.Fatalf("position %d: got %s want %s", i, b.ID, want[i])
		}
	}
}

func TestBackendUpdateScalarFields(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	if err := store.CreateBackend(ctx, newBackend("alpha")); err != nil {
		t.Fatalf("create: %v", err)
	}

	newName := "renamed"
	disabled := false
	pathStyle := false
	region := "eu-west-1"
	patch := sqlite.BackendPatch{
		Name:      &newName,
		Region:    &region,
		PathStyle: &pathStyle,
		Enabled:   &disabled,
		UpdatedBy: sql.NullString{String: "admin2", Valid: true},
	}
	if err := store.UpdateBackend(ctx, "alpha", patch); err != nil {
		t.Fatalf("update: %v", err)
	}

	got, _ := store.GetBackend(ctx, "alpha")
	if got.Name != "renamed" || got.Region != "eu-west-1" || got.PathStyle || got.Enabled {
		t.Fatalf("scalar update didn't take: %+v", got)
	}
	if got.UpdatedBy.String != "admin2" {
		t.Fatalf("UpdatedBy=%q want admin2", got.UpdatedBy.String)
	}
}

func TestBackendUpdatePreservesSecretWhenNotSet(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	b := newBackend("alpha")
	if err := store.CreateBackend(ctx, b); err != nil {
		t.Fatalf("create: %v", err)
	}
	newName := "renamed"
	if err := store.UpdateBackend(ctx, "alpha", sqlite.BackendPatch{Name: &newName}); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ := store.GetBackend(ctx, "alpha")
	if !bytes.Equal(got.SecretKeyEnc, b.SecretKeyEnc) {
		t.Fatalf("secret was clobbered: got %x want %x", got.SecretKeyEnc, b.SecretKeyEnc)
	}
}

func TestBackendUpdateReplaceSecret(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	if err := store.CreateBackend(ctx, newBackend("alpha")); err != nil {
		t.Fatalf("create: %v", err)
	}
	fresh := []byte{0x01, 0x00, 0x99, 0x88}
	patch := sqlite.BackendPatch{}
	patch.SetSecret(fresh)
	if err := store.UpdateBackend(ctx, "alpha", patch); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ := store.GetBackend(ctx, "alpha")
	if !bytes.Equal(got.SecretKeyEnc, fresh) {
		t.Fatalf("secret not replaced: got %x want %x", got.SecretKeyEnc, fresh)
	}
}

func TestBackendUpdateClearSecret(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	if err := store.CreateBackend(ctx, newBackend("alpha")); err != nil {
		t.Fatalf("create: %v", err)
	}
	patch := sqlite.BackendPatch{}
	patch.SetSecret(nil)
	if err := store.UpdateBackend(ctx, "alpha", patch); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ := store.GetBackend(ctx, "alpha")
	if got.SecretKeyEnc != nil {
		t.Fatalf("secret should be nil after clear, got %x", got.SecretKeyEnc)
	}
}

func TestBackendUpdateMissing(t *testing.T) {
	store := openTestStore(t)
	name := "x"
	err := store.UpdateBackend(context.Background(), "ghost", sqlite.BackendPatch{Name: &name})
	if !errors.Is(err, sqlite.ErrBackendNotFound) {
		t.Fatalf("err=%v want ErrBackendNotFound", err)
	}
}

func TestBackendUpdateNoFieldsIsNoop(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	if err := store.CreateBackend(ctx, newBackend("alpha")); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := store.UpdateBackend(ctx, "alpha", sqlite.BackendPatch{}); err != nil {
		t.Fatalf("empty patch should be noop, got: %v", err)
	}
}

func TestBackendDelete(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	if err := store.CreateBackend(ctx, newBackend("alpha")); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := store.DeleteBackend(ctx, "alpha"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	_, err := store.GetBackend(ctx, "alpha")
	if !errors.Is(err, sqlite.ErrBackendNotFound) {
		t.Fatalf("after delete: err=%v want ErrBackendNotFound", err)
	}
	if err := store.DeleteBackend(ctx, "alpha"); !errors.Is(err, sqlite.ErrBackendNotFound) {
		t.Fatalf("delete missing: err=%v want ErrBackendNotFound", err)
	}
}
