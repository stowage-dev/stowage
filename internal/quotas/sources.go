// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package quotas

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/stowage-dev/stowage/internal/store/sqlite"
)

// Limit is one configured per-bucket quota. Sourced from either the
// SQLite-managed `bucket_quotas` table (admin dashboard) or the operator-
// written Kubernetes Secret (BucketClaim.spec.quota field, future). The
// Source field carries the origin so the merged view can pick a winner on
// collision.
type Limit struct {
	SoftBytes int64
	HardBytes int64
	UpdatedAt time.Time
	UpdatedBy string
	Source    string // "sqlite" | "kubernetes"
}

// LimitKey is the (backend, bucket) tuple that names a configured limit.
// Used by the scanner to walk every quota-configured bucket.
type LimitKey struct {
	BackendID string
	Bucket    string
}

// LimitSource is the seam between the quota service and where limits live.
// Implementations cache their backing store in memory; the proxy hot path
// must not block on a database read.
//
// Subscribe is the change-propagation primitive used by the merged source
// to invalidate its own cache when a child source changes. Subscribers
// should be cheap; long-running work belongs in a goroutine.
type LimitSource interface {
	Get(backendID, bucket string) (*Limit, bool)
	List() []LimitKey
	Subscribe(fn func()) (unsubscribe func())
}

// ---------------------------------------------------------------------------
// SQLiteLimitSource

// SQLiteLimitSource serves limits from the bucket_quotas table backed by an
// in-memory cache. Reload() is called by the admin handlers after CRUD to
// refresh the cache without waiting for the next scheduled tick.
type SQLiteLimitSource struct {
	store  *sqlite.Store
	logger *slog.Logger

	mu    sync.RWMutex
	cache map[string]*Limit

	subsMu sync.Mutex
	subs   []func()
}

// NewSQLiteLimitSource constructs a SQLite-backed limit source. The cache
// starts empty — call Reload before serving traffic.
func NewSQLiteLimitSource(store *sqlite.Store, logger *slog.Logger) *SQLiteLimitSource {
	if logger == nil {
		logger = slog.Default()
	}
	return &SQLiteLimitSource{
		store:  store,
		logger: logger,
		cache:  map[string]*Limit{},
	}
}

func (s *SQLiteLimitSource) Get(backendID, bucket string) (*Limit, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	l, ok := s.cache[cacheKey(backendID, bucket)]
	if !ok {
		return nil, false
	}
	cp := *l
	return &cp, true
}

func (s *SQLiteLimitSource) List() []LimitKey {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]LimitKey, 0, len(s.cache))
	for k := range s.cache {
		i := indexOfSlash(k)
		if i < 0 {
			continue
		}
		out = append(out, LimitKey{BackendID: k[:i], Bucket: k[i+1:]})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].BackendID != out[j].BackendID {
			return out[i].BackendID < out[j].BackendID
		}
		return out[i].Bucket < out[j].Bucket
	})
	return out
}

// Reload rebuilds the cache from the underlying table. Subscribers fire
// after the swap so they observe consistent state.
func (s *SQLiteLimitSource) Reload(ctx context.Context) error {
	rows, err := s.store.ListAllQuotas(ctx)
	if err != nil {
		return fmt.Errorf("list bucket_quotas: %w", err)
	}
	next := make(map[string]*Limit, len(rows))
	for _, q := range rows {
		next[cacheKey(q.BackendID, q.Bucket)] = &Limit{
			SoftBytes: q.SoftBytes,
			HardBytes: q.HardBytes,
			UpdatedAt: q.UpdatedAt,
			UpdatedBy: q.UpdatedBy,
			Source:    "sqlite",
		}
	}
	s.mu.Lock()
	s.cache = next
	s.mu.Unlock()
	s.fire()
	return nil
}

func (s *SQLiteLimitSource) Subscribe(fn func()) func() {
	s.subsMu.Lock()
	defer s.subsMu.Unlock()
	s.subs = append(s.subs, fn)
	idx := len(s.subs) - 1
	return func() {
		s.subsMu.Lock()
		defer s.subsMu.Unlock()
		if idx < len(s.subs) {
			s.subs[idx] = nil
		}
	}
}

func (s *SQLiteLimitSource) fire() {
	s.subsMu.Lock()
	subs := append([]func(){}, s.subs...)
	s.subsMu.Unlock()
	for _, fn := range subs {
		if fn != nil {
			fn()
		}
	}
}

// ---------------------------------------------------------------------------
// KubernetesLimitSource

// KubernetesLimitSource is the operator-side limit source. v1 ships with
// stub reads (always missing) so behaviour is unchanged when the
// `BucketClaim.spec.quota` CRD field doesn't yet exist. Once the operator
// writes the limit into the consumer Secret data, this source's Reload
// will populate the cache from the same informer the credential source
// already runs.
type KubernetesLimitSource struct {
	mu     sync.RWMutex
	cache  map[string]*Limit
	subsMu sync.Mutex
	subs   []func()
	logger *slog.Logger
}

// NewKubernetesLimitSource constructs an empty source. Wiring code starts
// it only when s3_proxy.kubernetes.enabled is true.
func NewKubernetesLimitSource(logger *slog.Logger) *KubernetesLimitSource {
	if logger == nil {
		logger = slog.Default()
	}
	return &KubernetesLimitSource{
		cache:  map[string]*Limit{},
		logger: logger,
	}
}

// Set publishes a limit observed from the K8s informer. Tests use it
// directly; in production the s3proxy.KubernetesSource will wire its event
// handlers to this method once the operator-side CRD field lands.
func (s *KubernetesLimitSource) Set(backendID, bucket string, limit Limit) {
	limit.Source = "kubernetes"
	s.mu.Lock()
	s.cache[cacheKey(backendID, bucket)] = &limit
	s.mu.Unlock()
	s.fire()
}

// Delete removes a previously-published limit.
func (s *KubernetesLimitSource) Delete(backendID, bucket string) {
	s.mu.Lock()
	delete(s.cache, cacheKey(backendID, bucket))
	s.mu.Unlock()
	s.fire()
}

func (s *KubernetesLimitSource) Get(backendID, bucket string) (*Limit, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	l, ok := s.cache[cacheKey(backendID, bucket)]
	if !ok {
		return nil, false
	}
	cp := *l
	return &cp, true
}

func (s *KubernetesLimitSource) List() []LimitKey {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]LimitKey, 0, len(s.cache))
	for k := range s.cache {
		i := indexOfSlash(k)
		if i < 0 {
			continue
		}
		out = append(out, LimitKey{BackendID: k[:i], Bucket: k[i+1:]})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].BackendID != out[j].BackendID {
			return out[i].BackendID < out[j].BackendID
		}
		return out[i].Bucket < out[j].Bucket
	})
	return out
}

func (s *KubernetesLimitSource) Subscribe(fn func()) func() {
	s.subsMu.Lock()
	defer s.subsMu.Unlock()
	s.subs = append(s.subs, fn)
	idx := len(s.subs) - 1
	return func() {
		s.subsMu.Lock()
		defer s.subsMu.Unlock()
		if idx < len(s.subs) {
			s.subs[idx] = nil
		}
	}
}

func (s *KubernetesLimitSource) fire() {
	s.subsMu.Lock()
	subs := append([]func(){}, s.subs...)
	s.subsMu.Unlock()
	for _, fn := range subs {
		if fn != nil {
			fn()
		}
	}
}

// ---------------------------------------------------------------------------
// MergedLimitSource

// MergedLimitSource consults its sources in order. The first non-nil hit
// wins, matching the K8s-wins policy used by the credential source. Lower-
// precedence sources whose entries are shadowed are logged once per key.
type MergedLimitSource struct {
	sources []LimitSource
	logger  *slog.Logger

	shadowMu sync.Mutex
	shadowed map[string]struct{}
}

// NewMergedLimitSource builds a merged view. Pass sources in precedence
// order (e.g. K8s first, SQLite second). Nil entries are skipped so the
// wiring layer can always pass `[k8sLimits, sqliteLimits]` regardless of
// whether the K8s source is configured.
func NewMergedLimitSource(logger *slog.Logger, sources ...LimitSource) *MergedLimitSource {
	if logger == nil {
		logger = slog.Default()
	}
	m := &MergedLimitSource{
		logger:   logger,
		shadowed: map[string]struct{}{},
	}
	for _, s := range sources {
		if s != nil {
			m.sources = append(m.sources, s)
		}
	}
	return m
}

func (m *MergedLimitSource) Get(backendID, bucket string) (*Limit, bool) {
	var winner *Limit
	for _, s := range m.sources {
		l, ok := s.Get(backendID, bucket)
		if !ok {
			continue
		}
		if winner == nil {
			winner = l
			continue
		}
		// Shadowed: winner came from an earlier source.
		m.warnShadow(backendID, bucket, winner.Source, l.Source)
	}
	if winner == nil {
		return nil, false
	}
	return winner, true
}

func (m *MergedLimitSource) List() []LimitKey {
	seen := map[string]struct{}{}
	var out []LimitKey
	for _, s := range m.sources {
		for _, k := range s.List() {
			ck := cacheKey(k.BackendID, k.Bucket)
			if _, dup := seen[ck]; dup {
				continue
			}
			seen[ck] = struct{}{}
			out = append(out, k)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].BackendID != out[j].BackendID {
			return out[i].BackendID < out[j].BackendID
		}
		return out[i].Bucket < out[j].Bucket
	})
	return out
}

// Subscribe wires fn to every backing source. The returned unsubscribe
// detaches from all of them.
func (m *MergedLimitSource) Subscribe(fn func()) func() {
	cancels := make([]func(), 0, len(m.sources))
	for _, s := range m.sources {
		cancels = append(cancels, s.Subscribe(fn))
	}
	return func() {
		for _, c := range cancels {
			c()
		}
	}
}

// Reloadable is implemented by limit sources that can rebuild their cache
// on demand. Admin handlers call this after CRUD so the next request sees
// the new limit without waiting for a scheduled tick.
type Reloadable interface {
	Reload(ctx context.Context) error
}

// Reload fans out to every backing source that implements Reloadable.
// Returns the first error; remaining sources are still attempted.
func (m *MergedLimitSource) Reload(ctx context.Context) error {
	var firstErr error
	for _, s := range m.sources {
		if r, ok := s.(Reloadable); ok {
			if err := r.Reload(ctx); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

func (m *MergedLimitSource) warnShadow(backendID, bucket, winner, loser string) {
	key := cacheKey(backendID, bucket) + "|" + winner + "|" + loser
	m.shadowMu.Lock()
	if _, seen := m.shadowed[key]; seen {
		m.shadowMu.Unlock()
		return
	}
	m.shadowed[key] = struct{}{}
	m.shadowMu.Unlock()
	m.logger.Warn("quotas: limit shadowed",
		"backend", backendID,
		"bucket", bucket,
		"winner_source", winner,
		"loser_source", loser,
	)
}

// ErrNoLimitSource signals New was called without a limit source. Surfaces
// as a programmer error — the wiring layer must always pass at least one
// source even if it's an empty SQLiteLimitSource.
var ErrNoLimitSource = errors.New("quotas: nil LimitSource")
