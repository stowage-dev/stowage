// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package sqlite

import (
	"context"
	"fmt"
)

// migrations are applied once, in order, and recorded in schema_migrations.
// Only ever append to this slice — never rewrite history.
var migrations = []struct {
	Version int
	Name    string
	SQL     string
}{
	{
		Version: 1,
		Name:    "initial",
		SQL: `
CREATE TABLE users (
  id              TEXT PRIMARY KEY,
  username        TEXT NOT NULL COLLATE NOCASE,
  email           TEXT COLLATE NOCASE,
  password_hash   TEXT NOT NULL DEFAULT '',
  role            TEXT NOT NULL CHECK(role IN ('admin','user','readonly')),
  identity_source TEXT NOT NULL DEFAULT 'local',
  enabled         INTEGER NOT NULL DEFAULT 1,
  must_change_pw  INTEGER NOT NULL DEFAULT 0,
  failed_attempts INTEGER NOT NULL DEFAULT 0,
  locked_until    TIMESTAMP,
  created_at      TIMESTAMP NOT NULL,
  created_by      TEXT,
  last_login_at   TIMESTAMP,
  pw_changed_at   TIMESTAMP NOT NULL
);
CREATE UNIQUE INDEX users_username_unique ON users(username);
CREATE UNIQUE INDEX users_email_unique ON users(email) WHERE email IS NOT NULL;

CREATE TABLE sessions (
  id              TEXT PRIMARY KEY,
  user_id         TEXT NOT NULL,
  identity_source TEXT NOT NULL,
  csrf_token      TEXT NOT NULL,
  ip              TEXT,
  user_agent      TEXT,
  flags           INTEGER NOT NULL DEFAULT 0,
  created_at      TIMESTAMP NOT NULL,
  last_seen_at    TIMESTAMP NOT NULL,
  expires_at      TIMESTAMP NOT NULL
);
CREATE INDEX sessions_user_id   ON sessions(user_id);
CREATE INDEX sessions_expires   ON sessions(expires_at);

CREATE TABLE pw_reset_tokens (
  token_hash TEXT PRIMARY KEY,
  user_id    TEXT NOT NULL,
  created_at TIMESTAMP NOT NULL,
  expires_at TIMESTAMP NOT NULL,
  used_at    TIMESTAMP,
  FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
);
`,
	},
	{
		Version: 2,
		Name:    "shares",
		SQL: `
CREATE TABLE shares (
  id               TEXT PRIMARY KEY,
  code             TEXT NOT NULL,
  backend_id       TEXT NOT NULL,
  bucket           TEXT NOT NULL,
  object_key       TEXT NOT NULL,
  created_by       TEXT NOT NULL,
  created_at       TIMESTAMP NOT NULL,
  expires_at       TIMESTAMP,
  password_hash    TEXT NOT NULL DEFAULT '',
  max_downloads    INTEGER,
  download_count   INTEGER NOT NULL DEFAULT 0,
  revoked          INTEGER NOT NULL DEFAULT 0,
  revoked_at       TIMESTAMP,
  last_accessed_at TIMESTAMP,
  disposition      TEXT NOT NULL DEFAULT 'attachment'
);
CREATE UNIQUE INDEX shares_code_unique ON shares(code);
CREATE INDEX shares_created_by        ON shares(created_by);
CREATE INDEX shares_expires_at        ON shares(expires_at);
`,
	},
	{
		Version: 3,
		Name:    "bucket_quotas",
		SQL: `
CREATE TABLE bucket_quotas (
  backend_id  TEXT NOT NULL,
  bucket      TEXT NOT NULL,
  soft_bytes  INTEGER NOT NULL DEFAULT 0,
  hard_bytes  INTEGER NOT NULL DEFAULT 0,
  updated_at  TIMESTAMP NOT NULL,
  updated_by  TEXT NOT NULL,
  PRIMARY KEY (backend_id, bucket)
);
`,
	},
	{
		Version: 4,
		Name:    "audit_events",
		SQL: `
CREATE TABLE audit_events (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  ts          TIMESTAMP NOT NULL,
  user_id     TEXT,
  action      TEXT NOT NULL,
  backend_id  TEXT,
  bucket      TEXT,
  object_key  TEXT,
  request_id  TEXT,
  ip          TEXT,
  user_agent  TEXT,
  status      TEXT NOT NULL DEFAULT 'ok',
  detail      TEXT
);
CREATE INDEX audit_ts_desc      ON audit_events(ts DESC);
CREATE INDEX audit_user         ON audit_events(user_id);
CREATE INDEX audit_action       ON audit_events(action);
CREATE INDEX audit_backend      ON audit_events(backend_id, bucket);
`,
	},
	{
		Version: 5,
		Name:    "bucket_pins",
		SQL: `
CREATE TABLE bucket_pins (
  user_id    TEXT NOT NULL,
  backend_id TEXT NOT NULL,
  bucket     TEXT NOT NULL,
  created_at TIMESTAMP NOT NULL,
  PRIMARY KEY (user_id, backend_id, bucket)
);
CREATE INDEX bucket_pins_user ON bucket_pins(user_id, created_at DESC);
`,
	},
	{
		Version: 6,
		Name:    "users_oidc_subject",
		// Pin OIDC users to the immutable 'sub' claim instead of the
		// mutable 'preferred_username'. Existing rows have NULL until the
		// next OIDC login wires the value up. The composite unique index
		// prevents two rows from claiming the same (issuer, sub).
		SQL: `
ALTER TABLE users ADD COLUMN oidc_subject TEXT;
CREATE UNIQUE INDEX users_oidc_subject_unique
  ON users(identity_source, oidc_subject)
  WHERE oidc_subject IS NOT NULL;
`,
	},
	{
		Version: 7,
		Name:    "backends",
		// secret_key_enc is sealed by internal/secrets (AES-GCM); the
		// envelope carries its own version byte so this column can stay
		// BLOB across future key rotations. NULL means "no secret stored"
		// (e.g. backends authenticated via the SDK's default credential
		// chain). YAML-defined backends never appear here — those live in
		// config and shadow any DB row of the same id at startup.
		SQL: `
CREATE TABLE backends (
  id              TEXT PRIMARY KEY,
  name            TEXT NOT NULL,
  type            TEXT NOT NULL DEFAULT 's3v4',
  endpoint        TEXT NOT NULL,
  region          TEXT NOT NULL DEFAULT '',
  path_style      INTEGER NOT NULL DEFAULT 0,
  access_key      TEXT NOT NULL DEFAULT '',
  secret_key_enc  BLOB,
  enabled         INTEGER NOT NULL DEFAULT 1,
  created_at      TIMESTAMP NOT NULL,
  created_by      TEXT,
  updated_at      TIMESTAMP NOT NULL,
  updated_by      TEXT
);
`,
	},
	{
		Version: 8,
		Name:    "bucket_size_tracking",
		// Per-bucket opt-out for the proxy-computed size feature. Absence of
		// a row means "tracked" — the table only stores explicit overrides
		// so we don't need a row for every bucket on every backend. The
		// scanner walks every bucket that doesn't have enabled=0 here.
		SQL: `
CREATE TABLE bucket_size_tracking (
  backend_id TEXT NOT NULL,
  bucket     TEXT NOT NULL,
  enabled    INTEGER NOT NULL DEFAULT 1,
  updated_at TIMESTAMP NOT NULL,
  updated_by TEXT,
  PRIMARY KEY (backend_id, bucket)
);
`,
	},
	{
		Version: 9,
		Name:    "s3_credentials",
		// Per-tenant virtual S3 credentials handed out from the dashboard.
		// secret_key_enc is sealed by internal/secrets (AES-GCM); the
		// envelope carries its own version byte so this column can stay BLOB
		// across future key rotations. buckets is a JSON array of bucket
		// names the credential is scoped to (one-element array for a 1:1
		// claim, N elements for an N:1 grant). user_id is optional and used
		// only for audit attribution. Kubernetes-sourced credentials never
		// land here — those live in K8s Secrets and are read through the
		// informer source; on access-key collision the K8s entry wins per
		// the merged-source policy.
		SQL: `
CREATE TABLE s3_credentials (
  access_key      TEXT PRIMARY KEY,
  secret_key_enc  BLOB NOT NULL,
  backend_id      TEXT NOT NULL,
  buckets         TEXT NOT NULL DEFAULT '[]',
  user_id         TEXT,
  description     TEXT NOT NULL DEFAULT '',
  enabled         INTEGER NOT NULL DEFAULT 1,
  expires_at      TIMESTAMP,
  created_at      TIMESTAMP NOT NULL,
  created_by      TEXT,
  updated_at      TIMESTAMP NOT NULL,
  updated_by      TEXT
);
CREATE INDEX s3_credentials_backend ON s3_credentials(backend_id);
CREATE INDEX s3_credentials_user    ON s3_credentials(user_id) WHERE user_id IS NOT NULL;
`,
	},
	{
		Version: 10,
		Name:    "s3_anonymous_bindings",
		// Buckets exposed for unauthenticated S3 reads through the proxy.
		// Mode is currently always "ReadOnly" (the only safe default) but is
		// kept as a TEXT column so future modes don't need a migration.
		// per_source_ip_rps is the per-IP token-bucket rate the anonymous
		// path enforces; the cluster-level kill switch lives in YAML
		// (s3_proxy.anonymous_enabled) so an admin can disable everything
		// without touching this table.
		SQL: `
CREATE TABLE s3_anonymous_bindings (
  backend_id        TEXT NOT NULL,
  bucket            TEXT NOT NULL,
  mode              TEXT NOT NULL DEFAULT 'ReadOnly',
  per_source_ip_rps INTEGER NOT NULL DEFAULT 20,
  created_at        TIMESTAMP NOT NULL,
  created_by        TEXT,
  PRIMARY KEY (backend_id, bucket)
);
`,
	},
	{
		Version: 11,
		Name:    "s3_bucket_cors",
		// Per-bucket CORS rules enforced by the embedded proxy. Replaces the
		// cluster-wide s3_proxy.cors YAML knob. rules is a JSON array of
		// CORSRule objects (allowed_origins, allowed_methods, allowed_headers,
		// expose_headers, max_age_seconds). The proxy keeps a bucket-keyed
		// in-memory cache fed by SQLiteSource.Reload — preflights never
		// round-trip to disk or the upstream backend.
		SQL: `
CREATE TABLE s3_bucket_cors (
  backend_id TEXT NOT NULL,
  bucket     TEXT NOT NULL,
  rules      TEXT NOT NULL,
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL,
  PRIMARY KEY (backend_id, bucket)
);
`,
	},
}

func (s *Store) migrate(ctx context.Context) error {
	if _, err := s.DB.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS schema_migrations (
  version    INTEGER PRIMARY KEY,
  name       TEXT NOT NULL,
  applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);`); err != nil {
		return err
	}

	for _, m := range migrations {
		var exists int
		if err := s.DB.QueryRowContext(ctx,
			"SELECT COUNT(1) FROM schema_migrations WHERE version = ?", m.Version,
		).Scan(&exists); err != nil {
			return err
		}
		if exists > 0 {
			continue
		}

		tx, err := s.DB.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, m.SQL); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migration %d (%s): %w", m.Version, m.Name, err)
		}
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO schema_migrations(version, name) VALUES (?, ?)", m.Version, m.Name,
		); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}
