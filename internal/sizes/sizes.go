// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package sizes computes and caches per-bucket and per-prefix recursive
// byte totals. It exists so the bucket browser can render folder sizes
// without each tab walking the bucket itself, and so the bucket listing
// can show a top-level usage figure next to the name.
//
// Two cache layers:
//
//   - Bucket-root usage, refreshed by a periodic Run() loop over every
//     enabled bucket. Read-mostly, served by Get().
//   - Per-prefix usage, computed on demand by PrefixSize() with a short
//     TTL and singleflight-style coalescing so concurrent tabs share one
//     upstream walk.
//
// The opt-out lives in the bucket_size_tracking table — defaults to
// "tracked" for every bucket; admins can disable per-bucket from the
// settings page.
package sizes

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

// ErrTrackingDisabled is returned by PrefixSize when the bucket has the
// size-tracking toggle off.
var ErrTrackingDisabled = errors.New("size tracking disabled for this bucket")

// scanPageSize bounds memory during a recursive walk.
const scanPageSize = 1000

// prefixCacheTTL is how long a prefix-size result stays fresh. Short
// enough that mutations are reflected quickly, long enough to absorb a
// listing's worth of UI requests for the same folder.
const prefixCacheTTL = 60 * time.Second

// Usage is the cached total for a bucket root.
type Usage struct {
	Bytes       int64
	ObjectCount int64
	ComputedAt  time.Time
}

// Service owns the size scanner + on-demand prefix walker.
type Service struct {
	Store    *sqlite.Store
	Backends *backend.Registry
	Logger   *slog.Logger

	mu          sync.RWMutex
	bucketCache map[string]*Usage // key: backendID + "/" + bucket

	prefixMu    sync.Mutex
	prefixCache map[string]*prefixEntry // key: backendID + "/" + bucket + "/" + prefix
}

// prefixEntry is one slot of the prefix-size cache. The mutex coalesces
// concurrent computations so two tabs hitting the same folder share one
// upstream walk.
type prefixEntry struct {
	mu        sync.Mutex
	bytes     int64
	count     int64
	expiresAt time.Time
}

func New(store *sqlite.Store, registry *backend.Registry, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		Store:       store,
		Backends:    registry,
		Logger:      logger,
		bucketCache: make(map[string]*Usage),
		prefixCache: make(map[string]*prefixEntry),
	}
}

func bucketKey(backendID, bucket string) string { return backendID + "/" + bucket }

// IsTracked reports whether size tracking is on for the bucket. Defaults
// to true when no explicit row exists.
func (s *Service) IsTracked(ctx context.Context, backendID, bucket string) (bool, error) {
	if s.Store == nil {
		return true, nil
	}
	return s.Store.IsBucketSizeTracked(ctx, backendID, bucket)
}

// Get returns the cached bucket-root usage, or nil if the bucket has not
// yet been scanned. Doesn't trigger a scan.
func (s *Service) Get(backendID, bucket string) *Usage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u := s.bucketCache[bucketKey(backendID, bucket)]
	if u == nil {
		return nil
	}
	cp := *u
	return &cp
}

// Forget removes the cached entries for a bucket. Called when a bucket
// is deleted or has tracking turned off, so the listing stops showing a
// stale figure.
func (s *Service) Forget(backendID, bucket string) {
	s.mu.Lock()
	delete(s.bucketCache, bucketKey(backendID, bucket))
	s.mu.Unlock()

	prefix := bucketKey(backendID, bucket) + "/"
	s.prefixMu.Lock()
	for k := range s.prefixCache {
		if len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			delete(s.prefixCache, k)
		}
	}
	s.prefixMu.Unlock()
}

// Recorded bumps the cached bucket total after a successful upload so
// the listing reflects the new value before the next scheduled scan.
// No-op when the bucket has not been scanned yet — there's no baseline
// to update.
func (s *Service) Recorded(backendID, bucket string, addBytes int64) {
	if addBytes <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	u := s.bucketCache[bucketKey(backendID, bucket)]
	if u == nil {
		return
	}
	u.Bytes += addBytes
	u.ObjectCount++
}

// Scan walks one bucket from the root and replaces its cache entry.
// Returns the fresh Usage so callers can act on it without a second
// Get() round-trip.
func (s *Service) Scan(ctx context.Context, backendID, bucket string) (*Usage, error) {
	b, ok := s.Backends.Get(backendID)
	if !ok {
		return nil, fmt.Errorf("size scan: backend %q not registered", backendID)
	}
	bytes, count, err := walkPrefix(ctx, b, bucket, "")
	if err != nil {
		return nil, err
	}
	usage := &Usage{Bytes: bytes, ObjectCount: count, ComputedAt: time.Now().UTC()}
	s.mu.Lock()
	s.bucketCache[bucketKey(backendID, bucket)] = usage
	s.mu.Unlock()
	return usage, nil
}

// PrefixSize returns the recursive byte total under prefix. Hits the
// per-prefix cache when fresh, otherwise walks live and caches the
// result. Tracking-disabled buckets short-circuit with
// ErrTrackingDisabled — the same gate the bucket-root scanner respects.
func (s *Service) PrefixSize(ctx context.Context, backendID, bucket, prefix string) (int64, int64, error) {
	tracked, err := s.IsTracked(ctx, backendID, bucket)
	if err != nil {
		return 0, 0, err
	}
	if !tracked {
		return 0, 0, ErrTrackingDisabled
	}

	key := bucketKey(backendID, bucket) + "/" + prefix
	s.prefixMu.Lock()
	e, ok := s.prefixCache[key]
	if !ok {
		e = &prefixEntry{}
		s.prefixCache[key] = e
	}
	s.prefixMu.Unlock()

	e.mu.Lock()
	defer e.mu.Unlock()
	if time.Now().Before(e.expiresAt) {
		return e.bytes, e.count, nil
	}

	b, ok := s.Backends.Get(backendID)
	if !ok {
		return 0, 0, fmt.Errorf("prefix size: backend %q not registered", backendID)
	}
	bytes, count, err := walkPrefix(ctx, b, bucket, prefix)
	if err != nil {
		return 0, 0, err
	}
	e.bytes = bytes
	e.count = count
	e.expiresAt = time.Now().Add(prefixCacheTTL)
	return bytes, count, nil
}

// walkPrefix paginates through every object under prefix and returns the
// (bytes, count) totals. Empty prefix means "the whole bucket".
func walkPrefix(ctx context.Context, b backend.Backend, bucket, prefix string) (int64, int64, error) {
	var bytes, count int64
	token := ""
	for {
		res, err := b.ListObjects(ctx, backend.ListObjectsRequest{
			Bucket:            bucket,
			Prefix:            prefix,
			Delimiter:         "",
			ContinuationToken: token,
			MaxKeys:           scanPageSize,
		})
		if err != nil {
			return 0, 0, fmt.Errorf("walk prefix: %w", err)
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
	return bytes, count, nil
}

// scanAll walks every registered backend's bucket list, skips buckets
// flagged Enabled=false in the store, and Scans the rest. Backends
// without ListBuckets (or with errors) are logged and skipped — one
// flaky backend should not block size data for the others.
func (s *Service) scanAll(ctx context.Context) {
	if s.Backends == nil {
		return
	}
	disabled := map[string]struct{}{}
	if s.Store != nil {
		var err error
		disabled, err = s.Store.ListDisabledSizeTracking(ctx)
		if err != nil {
			s.Logger.Warn("size scanner: list disabled failed", "err", err.Error())
			disabled = map[string]struct{}{}
		}
	}

	for _, e := range s.Backends.List() {
		if ctx.Err() != nil {
			return
		}
		bid := e.Backend.ID()
		buckets, err := e.Backend.ListBuckets(ctx)
		if err != nil {
			s.Logger.Warn("size scanner: list buckets failed",
				"backend", bid, "err", err.Error())
			continue
		}
		for _, bk := range buckets {
			if ctx.Err() != nil {
				return
			}
			if _, off := disabled[bid+"/"+bk.Name]; off {
				continue
			}
			if _, err := s.Scan(ctx, bid, bk.Name); err != nil {
				s.Logger.Warn("size scanner: scan failed",
					"backend", bid, "bucket", bk.Name, "err", err.Error())
			}
		}
	}
}

// Run is the long-lived scheduler. Returns when ctx is done.
func (s *Service) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 30 * time.Minute
	}
	// Initial scan so the cache populates without waiting a full interval.
	s.scanAll(ctx)

	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.scanAll(ctx)
		}
	}
}
