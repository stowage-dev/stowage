// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package auth

import (
	"sync"
	"sync/atomic"
	"time"
)

// IdentityCache memoises (sessionID → Identity) for a short TTL so the auth
// middleware doesn't need to hit SQLite three times on every authenticated
// request. Entries also carry the session's expiry so we can short-circuit
// expired sessions without a DB round-trip.
//
// Cache validity windows are short by design (30 s default): the slack means
// admin actions like "disable a user" propagate within that window even if
// the explicit invalidation hooks below miss a path. Logout, password change,
// admin user mutations, and DeleteUserSessions all call Invalidate or
// InvalidateUser explicitly so the common cases are immediate.
type IdentityCache struct {
	ttl time.Duration

	mu      sync.RWMutex
	entries map[string]*cacheEntry
}

type cacheEntry struct {
	id            Identity
	cachedAt      time.Time
	cacheExpires  time.Time
	sessionExpiry time.Time
	// lastSeenAt is the wall-clock at which we most recently observed this
	// session; bumped on every cache hit so the caller can decide whether
	// to enqueue a TouchSession write back. Stored under atomic so cache
	// hits don't need the write lock.
	lastSeenUnixNano atomic.Int64
}

// NewIdentityCache returns a cache with the given TTL. A zero or negative TTL
// disables caching (Get always reports a miss).
func NewIdentityCache(ttl time.Duration) *IdentityCache {
	return &IdentityCache{
		ttl:     ttl,
		entries: make(map[string]*cacheEntry),
	}
}

// Get returns a cached identity if present and not expired. The bool also
// signals whether the caller should enqueue a TouchSession write — true when
// the session's last_seen_at is older than half its idle window.
//
// idleHalf is half the SessionManager.IdleTimeout — passed in so the cache
// stays decoupled from session config.
func (c *IdentityCache) Get(sessionID string, now time.Time, idleHalf time.Duration) (Identity, time.Time, bool, bool) {
	if c == nil || c.ttl <= 0 {
		return Identity{}, time.Time{}, false, false
	}
	c.mu.RLock()
	e, ok := c.entries[sessionID]
	c.mu.RUnlock()
	if !ok {
		return Identity{}, time.Time{}, false, false
	}
	if now.After(e.cacheExpires) || now.After(e.sessionExpiry) {
		// Stale or session-expired; fall back to the slow path which will
		// also evict on session expiry.
		return Identity{}, time.Time{}, false, false
	}
	lastSeen := time.Unix(0, e.lastSeenUnixNano.Load())
	shouldTouch := false
	if idleHalf > 0 && now.Sub(lastSeen) > idleHalf {
		// Bump in-memory lastSeen so concurrent readers don't all queue
		// duplicate touches.
		e.lastSeenUnixNano.Store(now.UnixNano())
		shouldTouch = true
	}
	return e.id, e.sessionExpiry, shouldTouch, true
}

// Put inserts or refreshes a cache entry. lastSeen is the session's persisted
// last_seen_at (so the next Get can decide whether enough time has elapsed to
// merit a TouchSession write).
func (c *IdentityCache) Put(sessionID string, id Identity, sessionExpiry, lastSeen, now time.Time) {
	if c == nil || c.ttl <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[sessionID]
	if !ok {
		e = &cacheEntry{}
		c.entries[sessionID] = e
	}
	e.id = id
	e.cachedAt = now
	e.cacheExpires = now.Add(c.ttl)
	e.sessionExpiry = sessionExpiry
	e.lastSeenUnixNano.Store(lastSeen.UnixNano())
}

// Invalidate evicts a single session id. Safe to call with an unknown id.
func (c *IdentityCache) Invalidate(sessionID string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	delete(c.entries, sessionID)
	c.mu.Unlock()
}

// InvalidateUser drops every cached entry whose identity matches userID. Used
// by password-change and admin-mutation paths so revocations take effect on
// the very next request.
func (c *IdentityCache) InvalidateUser(userID string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for sid, e := range c.entries {
		if e.id.UserID == userID {
			delete(c.entries, sid)
		}
	}
}

// Reap evicts entries whose cache window or session expiry has passed.
// Intended to be called periodically; safe to call from any goroutine.
func (c *IdentityCache) Reap(now time.Time) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for sid, e := range c.entries {
		if now.After(e.cacheExpires) || now.After(e.sessionExpiry) {
			delete(c.entries, sid)
		}
	}
}

// Len returns the current entry count; used by tests.
func (c *IdentityCache) Len() int {
	if c == nil {
		return 0
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}
