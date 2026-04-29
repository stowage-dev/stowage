// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"sync"
	"time"

	"github.com/stowage-dev/stowage/internal/backend"
)

// bucketsCacheTTL caps how long a ListBuckets result lives. Bucket sets on a
// real S3 change rarely; the UI hits this endpoint on every page load and
// every sidebar refresh. A 2s window cuts most of those down to a memory
// hit without misleading anyone — Create/Delete invalidate explicitly.
const bucketsCacheTTL = 2 * time.Second

// bucketsCache memoises (backendID → []backend.Bucket) for bucketsCacheTTL.
// Concurrent requests for the same backend coalesce on a single call: a
// per-backend mutex ensures only one goroutine performs the upstream
// ListBuckets while siblings wait for the result.
type bucketsCache struct {
	mu      sync.Mutex
	entries map[string]*bucketsCacheEntry
}

type bucketsCacheEntry struct {
	mu        sync.Mutex
	buckets   []backend.Bucket
	expiresAt time.Time
}

func newBucketsCache() *bucketsCache {
	return &bucketsCache{entries: make(map[string]*bucketsCacheEntry)}
}

// get returns the cached buckets when the entry is still fresh; otherwise
// it returns ok=false and the caller should consult upstream and Put the
// result. The per-backend lock is held across the upstream call so the
// inevitable simultaneous page-load fan-out collapses to one round-trip.
func (c *bucketsCache) entry(backendID string) *bucketsCacheEntry {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[backendID]
	if !ok {
		e = &bucketsCacheEntry{}
		c.entries[backendID] = e
	}
	return e
}

// invalidate drops the cached value for one backend so subsequent reads go
// to upstream. Called from Create/Delete handlers.
func (c *bucketsCache) invalidate(backendID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, backendID)
}
