// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package auth

import (
	"context"
	"sync"
	"time"

	"github.com/stowage-dev/stowage/internal/store/sqlite"
)

// TouchBatcher coalesces TouchSession writes so authenticated GETs don't pay
// the SQLite round-trip on every hit past the half-idle threshold. The
// in-memory session expiry is still bumped synchronously by the caller; this
// type only defers the persistence side.
//
// The latest (lastSeen, expiresAt) per session id wins — earlier values for
// the same id are dropped, which is exactly what TouchSession would have
// done anyway.
type TouchBatcher struct {
	store    *sqlite.Store
	interval time.Duration

	mu      sync.Mutex
	pending map[string]touchEntry
	// signals an early flush so tests don't have to wait for the tick.
	wake chan struct{}
}

type touchEntry struct {
	at        time.Time
	expiresAt time.Time
}

// NewTouchBatcher constructs a batcher. Call Run to start its drainer
// goroutine; the returned batcher's Enqueue method is a no-op until then,
// but is safe to call.
func NewTouchBatcher(store *sqlite.Store, interval time.Duration) *TouchBatcher {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	return &TouchBatcher{
		store:    store,
		interval: interval,
		pending:  make(map[string]touchEntry),
		wake:     make(chan struct{}, 1),
	}
}

// Enqueue records that sessionID was last seen at now with new expiry. The
// most recent values win when multiple are queued before the next flush.
func (b *TouchBatcher) Enqueue(sessionID string, now, expiresAt time.Time) {
	if b == nil {
		return
	}
	b.mu.Lock()
	b.pending[sessionID] = touchEntry{at: now, expiresAt: expiresAt}
	b.mu.Unlock()
}

// Run blocks until ctx is done, periodically flushing the pending set. Always
// runs one final flush on shutdown so in-flight touches are persisted.
func (b *TouchBatcher) Run(ctx context.Context) {
	if b == nil {
		return
	}
	t := time.NewTicker(b.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			b.flush(context.Background())
			return
		case <-t.C:
			b.flush(ctx)
		case <-b.wake:
			b.flush(ctx)
		}
	}
}

// Flush forces an immediate write of all pending entries. Returns the number
// persisted.
func (b *TouchBatcher) Flush(ctx context.Context) int {
	if b == nil {
		return 0
	}
	return b.flush(ctx)
}

func (b *TouchBatcher) flush(ctx context.Context) int {
	b.mu.Lock()
	if len(b.pending) == 0 {
		b.mu.Unlock()
		return 0
	}
	batch := b.pending
	b.pending = make(map[string]touchEntry, len(batch))
	b.mu.Unlock()

	// One TouchSession per entry. The single-writer SQLite pool serialises
	// these anyway; running them inside an explicit transaction would just
	// add a BEGIN/COMMIT pair, which on WAL is roughly equivalent.
	for id, e := range batch {
		_ = b.store.TouchSession(ctx, id, e.at, e.expiresAt)
	}
	return len(batch)
}
