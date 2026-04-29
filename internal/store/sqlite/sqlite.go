// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package sqlite implements the default stowage persistence backend using
// modernc.org/sqlite (CGo-free, keeps the single-binary promise).
package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// Store wraps two pools against the same WAL-mode SQLite file:
//
//   - DB: the single-connection writer. SQLite serialises writes anyway, so
//     this guarantees ordering and avoids "database is locked" under load.
//   - R:  a small reader pool. WAL allows concurrent readers, so authenticated
//     GETs (GetSession, GetUserByID, listings) can run in parallel with the
//     writer instead of head-of-line-blocking behind audit inserts and
//     session touches.
//
// Reads use s.R; writes, transactions, and migrations use s.DB.
type Store struct {
	DB *sql.DB
	R  *sql.DB
}

// readerPoolSize caps concurrent reader connections. WAL handles readers
// well, but past ~CPU count we'd just queue at the OS scheduler. Eight is
// comfortable for a 1-CPU container and headroom for a few-CPU host.
const readerPoolSize = 8

// Open opens (and creates if needed) a SQLite database at path, applies
// pragmas tuned for a small web service, and runs migrations.
func Open(ctx context.Context, path string) (*Store, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve sqlite path: %w", err)
	}

	q := url.Values{}
	q.Set("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "foreign_keys(ON)")
	q.Add("_pragma", "synchronous(NORMAL)")

	dsn := "file:" + abs + "?" + q.Encode()

	writer, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite (writer): %w", err)
	}
	// SQLite is single-writer; cap pool aggressively to avoid "database is locked".
	writer.SetMaxOpenConns(1)
	writer.SetMaxIdleConns(1)

	if err := writer.PingContext(ctx); err != nil {
		_ = writer.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	reader, err := sql.Open("sqlite", dsn)
	if err != nil {
		_ = writer.Close()
		return nil, fmt.Errorf("open sqlite (reader): %w", err)
	}
	reader.SetMaxOpenConns(readerPoolSize)
	reader.SetMaxIdleConns(readerPoolSize)

	if err := reader.PingContext(ctx); err != nil {
		_ = writer.Close()
		_ = reader.Close()
		return nil, fmt.Errorf("ping sqlite (reader): %w", err)
	}

	s := &Store{DB: writer, R: reader}
	if err := s.migrate(ctx); err != nil {
		_ = writer.Close()
		_ = reader.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

func (s *Store) Close() error {
	werr := s.DB.Close()
	if s.R != nil {
		if rerr := s.R.Close(); rerr != nil && werr == nil {
			werr = rerr
		}
	}
	return werr
}

func (s *Store) Ping(ctx context.Context) error { return s.DB.PingContext(ctx) }
