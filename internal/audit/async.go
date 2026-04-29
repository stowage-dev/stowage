// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package audit

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"
)

// AsyncRecorder wraps any Recorder so handlers don't pay the SQLite
// round-trip on the request hot path. Events are pushed onto a buffered
// channel; a single drainer goroutine pulls them off and hands each to the
// underlying Recorder. On buffer overflow the offending event is dropped
// and a counter increments so /metrics can surface a real backlog.
//
// Audit is best-effort by contract (the existing Noop is a Recorder), so
// dropping under sustained burst is acceptable; the alternative — blocking
// the response — is worse.
type AsyncRecorder struct {
	inner   Recorder
	logger  *slog.Logger
	queue   chan Event
	dropped atomic.Int64
}

// NewAsyncRecorder wraps inner with a buffered queue. queueSize sets the
// channel capacity; 4096 is a reasonable default.
func NewAsyncRecorder(inner Recorder, logger *slog.Logger, queueSize int) *AsyncRecorder {
	if logger == nil {
		logger = slog.Default()
	}
	if queueSize <= 0 {
		queueSize = 4096
	}
	return &AsyncRecorder{
		inner:  inner,
		logger: logger,
		queue:  make(chan Event, queueSize),
	}
}

// Record enqueues e for the drainer. Returns nil even when the queue is
// full — audit is best-effort and the bookkeeping is via Dropped().
func (a *AsyncRecorder) Record(_ context.Context, e Event) error {
	if a == nil {
		return nil
	}
	select {
	case a.queue <- e:
	default:
		a.dropped.Add(1)
	}
	return nil
}

// Dropped returns the count of events dropped since process start.
func (a *AsyncRecorder) Dropped() int64 {
	if a == nil {
		return 0
	}
	return a.dropped.Load()
}

// Inner returns the wrapped recorder. Used by RecordRequest to reach the
// SQLiteRecorder's ProxyTrust without coupling the audit package to net/http
// inside this file.
func (a *AsyncRecorder) Inner() Recorder {
	if a == nil {
		return nil
	}
	return a.inner
}

// drainBatchMax caps how many events one flush can absorb. Picked so the
// transaction stays short (a few ms of WAL writes at the bench's ~700
// events/sec) but large enough that steady-state load fills it on every
// drain, amortising the SQLite fsync over the whole batch.
const drainBatchMax = 256

// Run blocks until ctx is done, draining the queue through the underlying
// recorder. On shutdown it drains anything still pending so the audit log
// reflects the final session of activity.
//
// When the inner recorder implements BatchRecorder, the drainer
// opportunistically pulls every event already queued behind the head one
// and commits the whole burst in a single transaction. That cuts the
// per-event SQLite cost (one fsync per INSERT → one fsync per up-to-256
// INSERTs) to a level where audit stops competing meaningfully with the
// proxy hot path under sustained load.
func (a *AsyncRecorder) Run(ctx context.Context) {
	if a == nil {
		return
	}
	bRec, _ := a.inner.(BatchRecorder)
	batch := make([]Event, 0, drainBatchMax)

	flush := func() {
		if len(batch) == 0 {
			return
		}
		if bRec != nil {
			_ = bRec.RecordBatch(context.Background(), batch)
		} else {
			for _, e := range batch {
				_ = a.inner.Record(context.Background(), e)
			}
		}
		batch = batch[:0]
	}

	logTicker := time.NewTicker(time.Minute)
	defer logTicker.Stop()
	var lastLogged int64

	for {
		select {
		case <-ctx.Done():
			a.drainInto(&batch)
			flush()
			return
		case e := <-a.queue:
			batch = append(batch, e)
			// Pull anything else already queued so we flush a real burst,
			// not one event at a time. The non-blocking select ensures we
			// never wait for an additional event — this is purely
			// opportunistic coalescing of events that are already enqueued.
		drainMore:
			for len(batch) < drainBatchMax {
				select {
				case e2 := <-a.queue:
					batch = append(batch, e2)
				default:
					break drainMore
				}
			}
			flush()
		case <-logTicker.C:
			cur := a.dropped.Load()
			if cur > lastLogged {
				a.logger.Warn("audit events dropped",
					"new_dropped", cur-lastLogged,
					"total_dropped", cur,
				)
				lastLogged = cur
			}
		}
	}
}

// drainInto pulls every queued event into batch without blocking. Used
// only on shutdown — the steady-state path coalesces inline in Run.
func (a *AsyncRecorder) drainInto(batch *[]Event) {
	for {
		select {
		case e := <-a.queue:
			*batch = append(*batch, e)
		default:
			return
		}
	}
}
