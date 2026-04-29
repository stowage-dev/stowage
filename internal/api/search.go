// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/stowage-dev/stowage/internal/backend"
)

// Unified search across all configured backends — Phase 8 v1.0.
//
// Cost is bounded by:
//   - one ListBuckets per backend (already cached in callers via the
//     registry's last successful probe state, but we re-list here so a
//     fresh search reflects new buckets);
//   - one ListObjects per (backend, bucket) capped at maxObjectHits keys.
//
// For a typical fleet (3 backends × 10 buckets) that's ~30 cheap calls,
// well under a second wall-time. The caller can drop the per-bucket scan
// by passing q < minQueryLen.

const (
	minQueryLen   = 2
	maxObjectHits = 10
	defaultLimit  = 50
	maxLimit      = 200
	searchTimeout = 5 * time.Second
)

type searchBucketHit struct {
	BackendID string `json:"backend_id"`
	Bucket    string `json:"bucket"`
}

type searchObjectHit struct {
	BackendID    string `json:"backend_id"`
	Bucket       string `json:"bucket"`
	Key          string `json:"key"`
	Size         int64  `json:"size"`
	LastModified string `json:"last_modified,omitempty"`
}

type searchResponse struct {
	Query   string            `json:"query"`
	Buckets []searchBucketHit `json:"buckets"`
	Objects []searchObjectHit `json:"objects"`
	// Truncated reports whether one or both result lists hit the limit and
	// were trimmed. The UI surfaces this as a "more results — refine your
	// search" hint.
	Truncated bool `json:"truncated"`
}

func (d *BackendDeps) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	limit := defaultLimit
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			if n > maxLimit {
				n = maxLimit
			}
			limit = n
		}
	}
	if len(q) < minQueryLen {
		// Empty slices, not nil — frontend treats nil as a missing field
		// and crashes on .length. JSON-marshal preserves the distinction.
		writeJSON(w, http.StatusOK, searchResponse{
			Query:   q,
			Buckets: []searchBucketHit{},
			Objects: []searchObjectHit{},
		})
		return
	}

	qLower := strings.ToLower(q)
	ctx, cancel := context.WithTimeout(r.Context(), searchTimeout)
	defer cancel()

	type collected struct {
		mu      sync.Mutex
		buckets []searchBucketHit
		objects []searchObjectHit
	}
	out := &collected{}

	var wg sync.WaitGroup
	for _, e := range d.Registry.List() {
		bid := e.Backend.ID()
		// ListBuckets per backend so the search reflects the live set
		// rather than whatever the last probe saw.
		bks, err := e.Backend.ListBuckets(ctx)
		if err != nil {
			// One unhealthy backend shouldn't blank the entire search.
			d.Logger.Warn("search: list buckets failed", "backend", bid, "err", err.Error())
			continue
		}
		for _, bk := range bks {
			name := bk.Name
			if strings.Contains(strings.ToLower(name), qLower) {
				out.mu.Lock()
				out.buckets = append(out.buckets, searchBucketHit{BackendID: bid, Bucket: name})
				out.mu.Unlock()
			}
			wg.Add(1)
			go func(b backend.Backend, bid, bucket string) {
				defer wg.Done()
				res, err := b.ListObjects(ctx, backend.ListObjectsRequest{
					Bucket:    bucket,
					Prefix:    q,
					Delimiter: "",
					MaxKeys:   maxObjectHits,
				})
				if err != nil {
					return
				}
				if len(res.Objects) == 0 {
					return
				}
				rows := make([]searchObjectHit, 0, len(res.Objects))
				for _, o := range res.Objects {
					hit := searchObjectHit{
						BackendID: bid, Bucket: bucket,
						Key:  o.Key,
						Size: o.Size,
					}
					if !o.LastModified.IsZero() {
						hit.LastModified = o.LastModified.UTC().Format(time.RFC3339)
					}
					rows = append(rows, hit)
				}
				out.mu.Lock()
				out.objects = append(out.objects, rows...)
				out.mu.Unlock()
			}(e.Backend, bid, name)
		}
	}
	wg.Wait()

	resp := searchResponse{Query: q, Buckets: out.buckets, Objects: out.objects}
	if len(resp.Buckets) > limit {
		resp.Buckets = resp.Buckets[:limit]
		resp.Truncated = true
	}
	if len(resp.Objects) > limit {
		resp.Objects = resp.Objects[:limit]
		resp.Truncated = true
	}
	if resp.Buckets == nil {
		resp.Buckets = []searchBucketHit{}
	}
	if resp.Objects == nil {
		resp.Objects = []searchObjectHit{}
	}
	writeJSON(w, http.StatusOK, resp)
}
