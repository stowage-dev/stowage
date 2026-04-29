// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package auth

import (
	"hash/fnv"
	"net"
	"net/http"
	"sync"
	"time"
)

// rateLimiterShards keeps the per-key state map under N independent mutexes
// so 16 concurrent worker requests don't all serialise on a single lock.
// Bounded by config-defined keys (session ids), so this is plenty.
const rateLimiterShards = 16

// RateLimiter is a simple in-memory per-key token bucket. It is intentionally
// modest: good enough for login endpoints on a single node, and trivially
// replaceable with a shared store when horizontal scale is needed.
type RateLimiter struct {
	shards [rateLimiterShards]rateShard
	// Max events allowed in the Window.
	Max    int
	Window time.Duration
}

type rateShard struct {
	mu      sync.Mutex
	buckets map[string]*bucket
}

type bucket struct {
	events []time.Time
}

func NewRateLimiter(max int, window time.Duration) *RateLimiter {
	r := &RateLimiter{Max: max, Window: window}
	for i := range r.shards {
		r.shards[i].buckets = make(map[string]*bucket)
	}
	return r
}

func (r *RateLimiter) shardFor(key string) *rateShard {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return &r.shards[h.Sum32()%rateLimiterShards]
}

// Allow returns true if the caller keyed by key is under the limit.
func (r *RateLimiter) Allow(key string) bool {
	now := time.Now()
	cutoff := now.Add(-r.Window)

	sh := r.shardFor(key)
	sh.mu.Lock()
	defer sh.mu.Unlock()

	b, ok := sh.buckets[key]
	if !ok {
		b = &bucket{}
		sh.buckets[key] = b
	}
	// prune
	i := 0
	for ; i < len(b.events); i++ {
		if b.events[i].After(cutoff) {
			break
		}
	}
	b.events = b.events[i:]
	if len(b.events) >= r.Max {
		return false
	}
	b.events = append(b.events, now)
	return true
}

// Middleware rejects requests that exceed the rate with 429. keyFn extracts
// the bucket key from the request; if nil, the remote IP is used.
func (r *RateLimiter) Middleware(keyFn func(*http.Request) string, onLimit http.HandlerFunc) func(http.Handler) http.Handler {
	if keyFn == nil {
		keyFn = func(req *http.Request) string {
			host, _, err := net.SplitHostPort(clientIP(req))
			if err != nil {
				return clientIP(req)
			}
			return host
		}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if !r.Allow(keyFn(req)) {
				onLimit(w, req)
				return
			}
			next.ServeHTTP(w, req)
		})
	}
}
