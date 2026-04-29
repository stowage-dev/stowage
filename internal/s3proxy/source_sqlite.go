// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package s3proxy

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/stowage-dev/stowage/internal/secrets"
	"github.com/stowage-dev/stowage/internal/store/sqlite"
)

// SQLiteSource is the dashboard-managed credential source. The proxy hot
// path consults an in-memory map; persistence lives in the s3_credentials
// and s3_anonymous_bindings tables. The map is rebuilt on every Reload()
// (called by the admin handlers after CRUD) and on a fixed-interval ticker
// to bound staleness if the admin path is bypassed.
type SQLiteSource struct {
	store  *sqlite.Store
	sealer *secrets.Sealer
	logger *slog.Logger

	mu       sync.RWMutex
	byAKID   map[string]*VirtualCredential
	byBucket map[string]*AnonymousBinding

	// onReload, if set, fires after every successful Reload. Used by the
	// server to evict stale signing-key cache entries when a credential is
	// disabled / deleted / rotated.
	onReload func()
}

// SetOnReload registers a callback invoked after each successful Reload.
// nil clears the callback. Safe to call from outside the proxy package.
func (s *SQLiteSource) SetOnReload(fn func()) {
	s.onReload = fn
}

// NewSQLiteSource constructs a SQLiteSource. sealer is required — without
// it secret_key_enc cannot be opened and no credential is usable. The
// returned source starts empty; call Reload before serving traffic.
func NewSQLiteSource(store *sqlite.Store, sealer *secrets.Sealer, logger *slog.Logger) *SQLiteSource {
	if logger == nil {
		logger = slog.Default()
	}
	return &SQLiteSource{
		store:    store,
		sealer:   sealer,
		logger:   logger,
		byAKID:   map[string]*VirtualCredential{},
		byBucket: map[string]*AnonymousBinding{},
	}
}

// Lookup returns a copy of the virtual credential bound to akid, or false
// when the credential is unknown, disabled, or expired.
func (s *SQLiteSource) Lookup(akid string) (*VirtualCredential, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	vc, ok := s.byAKID[akid]
	if !ok {
		return nil, false
	}
	if vc.ExpiresAt != nil && time.Now().After(*vc.ExpiresAt) {
		return nil, false
	}
	out := *vc
	return &out, true
}

// LookupAnon returns a copy of the anonymous binding for bucket, or false.
// Bucket lookups are case-insensitive (matches S3 bucket-name rules).
func (s *SQLiteSource) LookupAnon(bucket string) (*AnonymousBinding, bool) {
	if bucket == "" {
		return nil, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	b, ok := s.byBucket[strings.ToLower(bucket)]
	if !ok {
		return nil, false
	}
	out := *b
	return &out, true
}

// Size returns the number of cached virtual credentials. Used for the
// proxy_credential_cache_size gauge.
func (s *SQLiteSource) Size() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.byAKID)
}

// Reload rebuilds the in-memory cache from the underlying store. Safe to
// call concurrently — the new map is built outside the lock and swapped
// in atomically.
func (s *SQLiteSource) Reload(ctx context.Context) error {
	if s.sealer == nil {
		return fmt.Errorf("s3proxy: SQLiteSource requires a sealer; STOWAGE_SECRET_KEY or server.secret_key_file must be set")
	}

	creds, err := s.store.ListS3Credentials(ctx)
	if err != nil {
		return fmt.Errorf("list s3_credentials: %w", err)
	}
	bindings, err := s.store.ListS3AnonymousBindings(ctx)
	if err != nil {
		return fmt.Errorf("list s3_anonymous_bindings: %w", err)
	}

	newAKID := make(map[string]*VirtualCredential, len(creds))
	for _, c := range creds {
		if !c.Enabled {
			continue
		}
		if c.ExpiresAt.Valid && time.Now().After(c.ExpiresAt.Time) {
			continue
		}
		secret, err := s.sealer.Open(c.SecretKeyEnc)
		if err != nil {
			s.logger.Warn("s3proxy: skip credential — secret unseal failed",
				"access_key", c.AccessKey, "err", err.Error())
			continue
		}
		buckets, err := c.UnmarshalBuckets()
		if err != nil {
			s.logger.Warn("s3proxy: skip credential — buckets JSON malformed",
				"access_key", c.AccessKey, "err", err.Error())
			continue
		}
		scopes := make([]BucketScope, 0, len(buckets))
		for _, b := range buckets {
			scopes = append(scopes, BucketScope{BucketName: b, BackendName: c.BackendID})
		}
		vc := &VirtualCredential{
			AccessKeyID:     c.AccessKey,
			SecretAccessKey: string(secret),
			BucketScopes:    scopes,
			BackendName:     c.BackendID,
			Source:          "sqlite",
		}
		if c.UserID.Valid {
			vc.UserID = c.UserID.String
		}
		if c.ExpiresAt.Valid {
			t := c.ExpiresAt.Time
			vc.ExpiresAt = &t
		}
		newAKID[c.AccessKey] = vc
	}

	newBucket := make(map[string]*AnonymousBinding, len(bindings))
	for _, b := range bindings {
		newBucket[strings.ToLower(b.Bucket)] = &AnonymousBinding{
			BackendName:    b.BackendID,
			BucketName:     b.Bucket,
			Mode:           b.Mode,
			PerSourceIPRPS: float64(b.PerSourceIPRPS),
			Source:         "sqlite",
		}
	}

	s.mu.Lock()
	s.byAKID = newAKID
	s.byBucket = newBucket
	s.mu.Unlock()
	if s.onReload != nil {
		s.onReload()
	}
	return nil
}

// Run periodically calls Reload so the in-memory cache reflects external
// edits (database imports, manual SQL) within `interval`. Returns when
// ctx is cancelled. Errors are logged and the next tick retries.
func (s *SQLiteSource) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		return
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := s.Reload(ctx); err != nil {
				s.logger.Warn("s3proxy: scheduled reload failed", "err", err.Error())
			}
		}
	}
}
