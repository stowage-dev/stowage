// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/stowage-dev/stowage/internal/backend"
	"github.com/stowage-dev/stowage/internal/backend/s3v4"
	"github.com/stowage-dev/stowage/internal/config"
	"github.com/stowage-dev/stowage/internal/secrets"
	"github.com/stowage-dev/stowage/internal/store/sqlite"
)

// buildRegistry resolves each backend config into a concrete driver and
// returns a populated Registry. It does not probe — callers decide when.
func buildRegistry(ctx context.Context, cfgs []config.BackendConfig) (*backend.Registry, error) {
	reg := backend.NewRegistry()
	for _, c := range cfgs {
		b, err := buildBackend(ctx, c)
		if err != nil {
			return nil, fmt.Errorf("backend %q: %w", c.ID, err)
		}
		if err := reg.Register(b); err != nil {
			return nil, err
		}
	}
	return reg, nil
}

// hydrateFromStore registers enabled DB-managed backends into reg, layered
// on top of the YAML entries already present. YAML wins on id collisions —
// the colliding DB row is logged and skipped so ops automation can't be
// silently overridden by a UI edit. Disabled rows are skipped entirely;
// they re-register through the admin API when an admin flips Enabled.
//
// Fails fast if any row carries a sealed secret but no Sealer was loaded —
// that's an operator config error (STOWAGE_SECRET_KEY missing) and starting
// silently would orphan every UI-managed endpoint.
func hydrateFromStore(ctx context.Context, reg *backend.Registry, store *sqlite.Store, sealer *secrets.Sealer, logger *slog.Logger) error {
	if store == nil {
		return nil
	}
	rows, err := store.ListBackends(ctx)
	if err != nil {
		return fmt.Errorf("list db backends: %w", err)
	}
	for _, row := range rows {
		if !row.Enabled {
			continue
		}
		if _, exists := reg.Get(row.ID); exists {
			logger.Warn("db backend shadowed by yaml entry; skipping",
				"id", row.ID)
			continue
		}
		secret, err := unsealSecret(row, sealer)
		if err != nil {
			return fmt.Errorf("backend %q: %w", row.ID, err)
		}
		b, err := buildBackendFromStored(ctx, row, secret)
		if err != nil {
			return fmt.Errorf("backend %q: %w", row.ID, err)
		}
		if err := reg.RegisterWithSource(b, backend.SourceDB); err != nil {
			return fmt.Errorf("backend %q: register: %w", row.ID, err)
		}
	}
	return nil
}

func unsealSecret(row *sqlite.Backend, sealer *secrets.Sealer) (string, error) {
	if len(row.SecretKeyEnc) == 0 {
		return "", nil
	}
	if sealer == nil {
		return "", fmt.Errorf("sealed secret on disk but STOWAGE_SECRET_KEY is unset")
	}
	pt, err := sealer.Open(row.SecretKeyEnc)
	if err != nil {
		return "", fmt.Errorf("unseal: %w", err)
	}
	return string(pt), nil
}

func buildBackendFromStored(ctx context.Context, row *sqlite.Backend, secret string) (backend.Backend, error) {
	switch row.Type {
	case "s3v4", "":
		return s3v4.New(ctx, s3v4.Config{
			ID:        row.ID,
			Name:      valueOr(row.Name, row.ID),
			Endpoint:  row.Endpoint,
			Region:    row.Region,
			AccessKey: row.AccessKey,
			SecretKey: secret,
			PathStyle: row.PathStyle,
		})
	default:
		return nil, fmt.Errorf("unknown backend type %q (supported: s3v4)", row.Type)
	}
}

func buildBackend(ctx context.Context, c config.BackendConfig) (backend.Backend, error) {
	switch c.Type {
	case "s3v4", "":
		return s3v4.New(ctx, s3v4.Config{
			ID:        c.ID,
			Name:      valueOr(c.Name, c.ID),
			Endpoint:  c.Endpoint,
			Region:    c.Region,
			AccessKey: envOrLiteral(c.AccessKeyEnv),
			SecretKey: envOrLiteral(c.SecretKeyEnv),
			PathStyle: c.PathStyle,
		})
	default:
		return nil, fmt.Errorf("unknown backend type %q (supported: s3v4)", c.Type)
	}
}

func envOrLiteral(name string) string {
	if name == "" {
		return ""
	}
	return os.Getenv(name)
}

func valueOr(s, fallback string) string {
	if s != "" {
		return s
	}
	return fallback
}
