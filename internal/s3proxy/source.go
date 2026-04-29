// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package s3proxy

import (
	"log/slog"
	"sync"
)

// Source is the lookup interface the proxy consumes. Implementations include
// the SQLite-backed admin source and the optional Kubernetes-informer
// source. A MergedSource fans out to multiple backing sources with
// well-defined precedence.
//
// All methods are called on the request hot path and must be safe for
// concurrent use. Implementations should be backed by an in-memory cache —
// no implementation should hit the database or apiserver per request.
type Source interface {
	Lookup(accessKeyID string) (*VirtualCredential, bool)
	LookupAnon(bucket string) (*AnonymousBinding, bool)
	Size() int
}

// MergedSource consults its sources in order and returns the first hit.
// The ordering convention is "Kubernetes first, SQLite second" so the
// operator's view wins on access-key collision (matches the
// `Kubernetes wins` policy from the design doc). Nil sources are skipped,
// which lets the wiring layer construct a single MergedSource regardless
// of whether the K8s source is configured.
type MergedSource struct {
	sources []Source
	logger  *slog.Logger

	// shadowedAKID logs collisions (DB row exists but K8s row took
	// precedence) at most once per key per process to avoid log spam if a
	// stale operator entry sits unfixed.
	mu           sync.Mutex
	shadowedAKID map[string]struct{}
}

// NewMergedSource builds a merged source. Pass sources in precedence order:
// the first non-nil source's hit wins. Pass logger=nil to disable shadow
// warnings.
func NewMergedSource(logger *slog.Logger, sources ...Source) *MergedSource {
	out := &MergedSource{logger: logger, shadowedAKID: map[string]struct{}{}}
	for _, s := range sources {
		if s != nil {
			out.sources = append(out.sources, s)
		}
	}
	return out
}

// Lookup returns the first matching credential and warns once per AKID when
// a lower-precedence source also has the key (i.e. shadowing happened).
func (m *MergedSource) Lookup(akid string) (*VirtualCredential, bool) {
	var winner *VirtualCredential
	for i, s := range m.sources {
		vc, ok := s.Lookup(akid)
		if !ok {
			continue
		}
		if winner == nil {
			winner = vc
			if i+1 == len(m.sources) {
				return vc, true
			}
			continue
		}
		// Shadowed: winner is from an earlier source (higher precedence).
		m.warnShadow(akid, winner.Source, vc.Source)
	}
	if winner == nil {
		return nil, false
	}
	return winner, true
}

// LookupAnon mirrors Lookup for anonymous bindings, keyed by bucket. Same
// precedence rules apply.
func (m *MergedSource) LookupAnon(bucket string) (*AnonymousBinding, bool) {
	for _, s := range m.sources {
		if a, ok := s.LookupAnon(bucket); ok {
			return a, true
		}
	}
	return nil, false
}

// Size returns the sum across sources. Used by the proxy_cache_size gauge —
// it's a coarse signal so over-counting on collisions is acceptable.
func (m *MergedSource) Size() int {
	n := 0
	for _, s := range m.sources {
		n += s.Size()
	}
	return n
}

func (m *MergedSource) warnShadow(akid, winnerSource, loserSource string) {
	if m.logger == nil {
		return
	}
	m.mu.Lock()
	if _, seen := m.shadowedAKID[akid]; seen {
		m.mu.Unlock()
		return
	}
	m.shadowedAKID[akid] = struct{}{}
	m.mu.Unlock()
	m.logger.Warn("s3proxy: virtual credential shadowed",
		"access_key", akid,
		"winner_source", winnerSource,
		"loser_source", loserSource,
	)
}
