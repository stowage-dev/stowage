// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package quotas implements proxy-enforced per-bucket storage quotas. The
// proxy doesn't trust the backend to enforce limits — many S3-compatible
// servers don't expose any quota concept — so it scans configured buckets on
// a schedule and intercepts uploads before they hit a hard quota.
package quotas

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/stowage-dev/stowage/internal/backend"
	"github.com/stowage-dev/stowage/internal/store/sqlite"
)

// ErrQuotaExceeded is returned by CheckUpload when the projected post-write
// size would exceed the bucket's hard quota. Handlers should map this to
// HTTP 507 Insufficient Storage.
var ErrQuotaExceeded = errors.New("bucket hard quota exceeded")

// Usage is one row of the in-memory cache. ComputedAt is the wall-clock
// time of the last successful scan; readers can decide for themselves how
// stale they tolerate.
type Usage struct {
	Bytes       int64
	ObjectCount int64
	ComputedAt  time.Time
}

// Status is the combined snapshot returned by Status() — limit plus
// current usage plus derived helpers. Both halves may be missing.
type Status struct {
	Limit *Limit
	Usage *Usage
}

// Service owns the quota enforcement loop. Limits are read through the
// configured LimitSource (in-memory cache); usage is computed by the
// scanner against the live backend.
type Service struct {
	Limits   LimitSource
	Store    *sqlite.Store
	Backends *backend.Registry
	Logger   *slog.Logger

	// scanPageSize bounds memory during a scan.
	scanPageSize int

	mu    sync.RWMutex
	cache map[string]*Usage
}

// ReloadLimits rebuilds the in-memory limit cache from any reloadable
// underlying source. The admin CRUD handlers call this after upsert /
// delete so the next request sees the new limit immediately. K8s sources
// are event-driven and ignore this call.
func (s *Service) ReloadLimits(ctx context.Context) error {
	if r, ok := s.Limits.(Reloadable); ok {
		return r.Reload(ctx)
	}
	return nil
}

// New constructs a quota service. limits provides the in-memory limit
// lookup (typically a MergedLimitSource fronting both SQLite and K8s);
// store is still needed for the admin CRUD path and as the scanner's
// canonical writer for usage. logger may be nil.
func New(limits LimitSource, store *sqlite.Store, registry *backend.Registry, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	if limits == nil {
		// Defensive: callers should never pass nil. Build a SQLite-only
		// source so we still function, and log loudly so a misconfiguration
		// is visible.
		logger.Warn("quotas.New called with nil LimitSource; falling back to SQLite-only source")
		limits = NewSQLiteLimitSource(store, logger)
	}
	return &Service{
		Limits:       limits,
		Store:        store,
		Backends:     registry,
		Logger:       logger,
		scanPageSize: 1000,
		cache:        make(map[string]*Usage),
	}
}

func cacheKey(backendID, bucket string) string { return backendID + "/" + bucket }

// Get returns the cached usage for a bucket, or nil if it hasn't been
// scanned yet. Doesn't trigger a scan.
func (s *Service) Get(backendID, bucket string) *Usage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u := s.cache[cacheKey(backendID, bucket)]
	if u == nil {
		return nil
	}
	cp := *u
	return &cp
}

// Status combines the configured limit (if any) with the cached usage
// (if any) into one snapshot — used by the admin API.
func (s *Service) Status(_ context.Context, backendID, bucket string) (*Status, error) {
	out := &Status{Usage: s.Get(backendID, bucket)}
	if l, ok := s.Limits.Get(backendID, bucket); ok {
		out.Limit = l
	}
	return out, nil
}

// CheckUpload is called by the upload handlers before letting bytes touch
// the backend. addBytes is the projected on-disk delta (single PUT size,
// or one part for multipart).
//
// Returns ErrQuotaExceeded only when a hard quota is configured AND the
// projected new total would exceed it. No quota or no cached usage = pass:
// we don't want to refuse uploads just because the scanner hasn't run yet,
// or because the admin hasn't set a limit.
func (s *Service) CheckUpload(ctx context.Context, backendID, bucket string, addBytes int64) error {
	limit, ok := s.Limits.Get(backendID, bucket)
	if !ok || limit.HardBytes <= 0 {
		return nil
	}
	usage := s.Get(backendID, bucket)
	if usage == nil {
		// First scan hasn't run yet. Best-effort scan synchronously so we
		// don't wave through writes for the first 30 minutes after boot.
		// Cap the scan time so a slow backend can't make uploads hang.
		scanCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		if u, scanErr := s.Scan(scanCtx, backendID, bucket); scanErr == nil {
			usage = u
		} else {
			// Scan failed; let the upload through rather than hard-fail
			// every write. The scheduled scanner will catch up later.
			s.Logger.Warn("quota: synchronous scan failed, allowing upload",
				"backend", backendID, "bucket", bucket, "err", scanErr.Error())
			return nil
		}
	}
	if usage.Bytes+addBytes > limit.HardBytes {
		return fmt.Errorf("%w: %d + %d > %d", ErrQuotaExceeded, usage.Bytes, addBytes, limit.HardBytes)
	}
	return nil
}

// Recorded is called after a successful write so the in-memory cache
// reflects the new total without waiting for the next scheduled scan. The
// hard-quota check on the next upload uses this updated value.
func (s *Service) Recorded(backendID, bucket string, addBytes int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u := s.cache[cacheKey(backendID, bucket)]
	if u == nil {
		// Without a baseline we can't update; the next scan will reconcile.
		return
	}
	u.Bytes += addBytes
	u.ObjectCount++
}

// Scan paginates through the backend's bucket and computes the live size
// + count. Result is stored in the cache and returned. Skips buckets with
// no configured quota — the scanner is the only place that calls this and
// only for configured rows.
func (s *Service) Scan(ctx context.Context, backendID, bucket string) (*Usage, error) {
	b, ok := s.Backends.Get(backendID)
	if !ok {
		return nil, fmt.Errorf("quota scan: backend %q not registered", backendID)
	}
	var bytes, count int64
	token := ""
	for {
		res, err := b.ListObjects(ctx, backend.ListObjectsRequest{
			Bucket:            bucket,
			Delimiter:         "",
			ContinuationToken: token,
			MaxKeys:           s.scanPageSize,
		})
		if err != nil {
			return nil, fmt.Errorf("quota scan list: %w", err)
		}
		for _, o := range res.Objects {
			bytes += o.Size
			count++
		}
		if !res.IsTruncated || res.NextContinuationToken == "" {
			break
		}
		token = res.NextContinuationToken
	}
	usage := &Usage{Bytes: bytes, ObjectCount: count, ComputedAt: time.Now().UTC()}
	s.mu.Lock()
	s.cache[cacheKey(backendID, bucket)] = usage
	s.mu.Unlock()
	return usage, nil
}

// ScanAll runs Scan over every limit-configured bucket and logs failures.
// Used by the scheduler and as the implementation of an admin "recompute
// everything" operation. Iterates the merged limit source so K8s-only and
// SQLite-only entries are both walked.
func (s *Service) ScanAll(ctx context.Context) {
	for _, k := range s.Limits.List() {
		if ctx.Err() != nil {
			return
		}
		if _, err := s.Scan(ctx, k.BackendID, k.Bucket); err != nil {
			s.Logger.Warn("quota scanner: scan failed",
				"backend", k.BackendID, "bucket", k.Bucket, "err", err.Error())
		}
	}
}

// BucketUsage is one row of the dashboard's "top buckets" view.
type BucketUsage struct {
	BackendID string
	Bucket    string
	Bytes     int64
	Objects   int64
}

// BackendTotal is one row of the dashboard's per-backend storage card.
type BackendTotal struct {
	BackendID string
	Bytes     int64
	Objects   int64
	Buckets   int
}

// TopBuckets returns the n biggest cached buckets in descending size order.
// Reads the existing scan cache; no network calls. Buckets that have never
// been scanned simply don't appear.
func (s *Service) TopBuckets(n int) []BucketUsage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	all := make([]BucketUsage, 0, len(s.cache))
	for k, u := range s.cache {
		i := indexOfSlash(k)
		if i < 0 {
			continue
		}
		all = append(all, BucketUsage{
			BackendID: k[:i],
			Bucket:    k[i+1:],
			Bytes:     u.Bytes,
			Objects:   u.ObjectCount,
		})
	}
	// Insertion sort is fine — caches typically hold at most a few hundred
	// entries on a single proxy, and we only want the top-N.
	for i := 1; i < len(all); i++ {
		for j := i; j > 0 && all[j].Bytes > all[j-1].Bytes; j-- {
			all[j], all[j-1] = all[j-1], all[j]
		}
	}
	if n > 0 && len(all) > n {
		all = all[:n]
	}
	return all
}

// BackendTotals aggregates the cache by backend. Buckets without a cached
// row contribute nothing — admins should set quotas to populate the cache.
func (s *Service) BackendTotals() map[string]*BackendTotal {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := map[string]*BackendTotal{}
	for k, u := range s.cache {
		i := indexOfSlash(k)
		if i < 0 {
			continue
		}
		bid := k[:i]
		row := out[bid]
		if row == nil {
			row = &BackendTotal{BackendID: bid}
			out[bid] = row
		}
		row.Bytes += u.Bytes
		row.Objects += u.ObjectCount
		row.Buckets++
	}
	return out
}

func indexOfSlash(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] == '/' {
			return i
		}
	}
	return -1
}

// Run is the long-lived scheduler loop. Returns when ctx is done.
func (s *Service) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 30 * time.Minute
	}
	// Kick once at startup so the cache populates without waiting a full
	// interval — important for hard-quota enforcement on uploads that
	// arrive shortly after boot.
	s.ScanAll(ctx)

	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.ScanAll(ctx)
		}
	}
}
