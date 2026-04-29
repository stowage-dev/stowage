// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package oidc

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stowage-dev/stowage/internal/store/sqlite"

	"github.com/oklog/ulid/v2"
)

// openTestStore brings up a temp SQLite with all migrations applied.
func openTestStore(t *testing.T) *sqlite.Store {
	t.Helper()
	store, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "oidc.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// TestUpsertOIDCUserByPreferredUsernameDoesNotHijack is the regression guard
// for F-2: an IdP that supplies a different `sub` but the same
// `preferred_username` as an existing OIDC user must NOT be allowed to take
// over that user's session. The new user gets a fresh row with a suffixed
// username instead.
func TestUpsertOIDCUserByPreferredUsernameDoesNotHijack(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	// First, real user: sub=abc-123, username=alice.
	first, err := upsertOIDCUser(ctx, store, "abc-123", "alice", "alice@corp", "user", "oidc:idp")
	if err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	if !first.OIDCSubject.Valid || first.OIDCSubject.String != "abc-123" {
		t.Fatalf("first user missing sub: %+v", first.OIDCSubject)
	}

	// Attacker logs in claiming the same preferred_username but a different
	// sub (because the IdP is hostile or misconfigured). Stowage must NOT
	// return the same row.
	attacker, err := upsertOIDCUser(ctx, store, "evil-999", "alice", "evil@bad", "admin", "oidc:idp")
	if err != nil {
		t.Fatalf("attacker upsert: %v", err)
	}
	if attacker.ID == first.ID {
		t.Fatalf("attacker hijacked alice's row (same id %s)", first.ID)
	}
	if attacker.Username == "alice" {
		t.Fatalf("attacker should have got a suffixed username; got %q", attacker.Username)
	}
	if attacker.OIDCSubject.String != "evil-999" {
		t.Errorf("attacker row missing its own sub: %+v", attacker.OIDCSubject)
	}
	// And alice's row is untouched.
	again, err := store.GetUserByID(ctx, first.ID)
	if err != nil {
		t.Fatalf("alice lookup: %v", err)
	}
	if again.Role != "user" {
		t.Errorf("alice's role mutated: %q", again.Role)
	}
}

// TestUpsertOIDCUserStableBySub confirms the canonical path: the same sub on
// repeated logins always returns the same row, even when the IdP renames the
// preferred_username.
func TestUpsertOIDCUserStableBySub(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	first, err := upsertOIDCUser(ctx, store, "stable-1", "old-name", "n@x", "user", "oidc:idp")
	if err != nil {
		t.Fatalf("first: %v", err)
	}

	// Same sub, renamed.
	second, err := upsertOIDCUser(ctx, store, "stable-1", "new-name", "n@x", "user", "oidc:idp")
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("same sub returned a new row; first=%s second=%s", first.ID, second.ID)
	}
	if second.Username != "new-name" {
		t.Errorf("rename not applied; got %q", second.Username)
	}
}

// TestUpsertOIDCUserAdoptsLegacyRowOnce covers the migration-v6 fallback:
// when a pre-migration row exists with a NULL oidc_subject, the first
// matching login pins the sub. Subsequent attempts with a *different* sub
// must not be able to re-adopt it.
func TestUpsertOIDCUserAdoptsLegacyRowOnce(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	// Inject a pre-migration-v6-style row directly: identity_source=oidc:idp,
	// oidc_subject = NULL.
	now := time.Now().UTC()
	legacy := &sqlite.User{
		ID:             ulid.Make().String(),
		Username:       "legacy-bob",
		Role:           "admin",
		IdentitySource: "oidc:idp",
		Enabled:        true,
		CreatedAt:      now,
		PWChangedAt:    now,
	}
	if err := store.CreateUser(ctx, legacy); err != nil {
		t.Fatalf("seed legacy: %v", err)
	}

	// First sign-in after migration links sub=bob-real.
	bound, err := upsertOIDCUser(ctx, store, "bob-real", "legacy-bob", "", "admin", "oidc:idp")
	if err != nil {
		t.Fatalf("bind: %v", err)
	}
	if bound.ID != legacy.ID {
		t.Fatalf("legacy row was not adopted: ids %s vs %s", bound.ID, legacy.ID)
	}
	if bound.OIDCSubject.String != "bob-real" {
		t.Fatalf("sub not pinned: %+v", bound.OIDCSubject)
	}

	// Second sign-in claiming the same username but a *different* sub must
	// not adopt the now-pinned row — that would be the F-2 hijack.
	attacker, err := upsertOIDCUser(ctx, store, "bob-fake", "legacy-bob", "", "admin", "oidc:idp")
	if err != nil {
		t.Fatalf("attacker upsert: %v", err)
	}
	if attacker.ID == legacy.ID {
		t.Fatalf("attacker hijacked legacy row")
	}
}
