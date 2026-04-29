// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package auth

import (
	"testing"
	"time"
)

func TestIdentityCacheHitMissExpiry(t *testing.T) {
	c := NewIdentityCache(50 * time.Millisecond)
	now := time.Now().UTC()
	id := Identity{UserID: "u1", Username: "alice", Role: "user", SessionID: "s1", CSRFToken: "tok"}
	c.Put("s1", id, now.Add(time.Hour), now, now)

	if got, _, _, ok := c.Get("s1", now, time.Minute); !ok || got.UserID != "u1" {
		t.Fatalf("expected hit, got ok=%v id=%+v", ok, got)
	}

	// Past TTL → miss.
	if _, _, _, ok := c.Get("s1", now.Add(time.Second), time.Minute); ok {
		t.Fatalf("expected miss after ttl")
	}
}

func TestIdentityCacheSessionExpiryEnforced(t *testing.T) {
	c := NewIdentityCache(time.Hour)
	now := time.Now().UTC()
	id := Identity{UserID: "u1", SessionID: "s1"}
	c.Put("s1", id, now.Add(-time.Second), now, now) // session already expired

	if _, _, _, ok := c.Get("s1", now, time.Minute); ok {
		t.Fatalf("expected miss for expired session")
	}
}

func TestIdentityCacheInvalidateUser(t *testing.T) {
	c := NewIdentityCache(time.Hour)
	now := time.Now().UTC()
	c.Put("s1", Identity{UserID: "alice", SessionID: "s1"}, now.Add(time.Hour), now, now)
	c.Put("s2", Identity{UserID: "alice", SessionID: "s2"}, now.Add(time.Hour), now, now)
	c.Put("s3", Identity{UserID: "bob", SessionID: "s3"}, now.Add(time.Hour), now, now)

	c.InvalidateUser("alice")
	if _, _, _, ok := c.Get("s1", now, time.Minute); ok {
		t.Errorf("expected s1 evicted")
	}
	if _, _, _, ok := c.Get("s2", now, time.Minute); ok {
		t.Errorf("expected s2 evicted")
	}
	if _, _, _, ok := c.Get("s3", now, time.Minute); !ok {
		t.Errorf("expected s3 still cached")
	}
}

func TestIdentityCacheShouldTouchOnce(t *testing.T) {
	c := NewIdentityCache(time.Hour)
	base := time.Now().UTC()
	c.Put("s1", Identity{SessionID: "s1"}, base.Add(time.Hour), base, base)

	// idleHalf=30s. Within the window (10s elapsed) → no touch.
	if _, _, touch, ok := c.Get("s1", base.Add(10*time.Second), 30*time.Second); !ok || touch {
		t.Fatalf("unexpected touch within window: ok=%v touch=%v", ok, touch)
	}
	// Past idleHalf → touch should be requested once.
	when := base.Add(40 * time.Second)
	if _, _, touch, ok := c.Get("s1", when, 30*time.Second); !ok || !touch {
		t.Fatalf("expected touch past half window: ok=%v touch=%v", ok, touch)
	}
	// Subsequent cache hit at the same instant should not re-trigger touch
	// because the entry's lastSeen has already been bumped.
	if _, _, touch, ok := c.Get("s1", when, 30*time.Second); !ok || touch {
		t.Fatalf("expected no double-touch: ok=%v touch=%v", ok, touch)
	}
}

func TestIdentityCacheNilSafe(t *testing.T) {
	var c *IdentityCache
	c.Put("s1", Identity{}, time.Now(), time.Now(), time.Now())
	c.InvalidateUser("u1")
	c.Invalidate("s1")
	c.Reap(time.Now())
	if _, _, _, ok := c.Get("s1", time.Now(), time.Minute); ok {
		t.Fatalf("expected miss on nil cache")
	}
}

func TestIdentityCacheZeroTTLDisabled(t *testing.T) {
	c := NewIdentityCache(0)
	now := time.Now().UTC()
	c.Put("s1", Identity{SessionID: "s1"}, now.Add(time.Hour), now, now)
	if _, _, _, ok := c.Get("s1", now, time.Minute); ok {
		t.Fatalf("expected zero-TTL cache to always miss")
	}
}
