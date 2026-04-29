// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package store defines the persistence interface used for sessions, users,
// audit events, share links, and preferences. SQLite is the default
// implementation (internal/store/sqlite) with PostgreSQL pluggable later.
package store

import "context"

// Store is the root persistence handle. Individual resource repositories are
// layered on top as this file grows in later phases.
type Store interface {
	Ping(ctx context.Context) error
	Close() error
}
