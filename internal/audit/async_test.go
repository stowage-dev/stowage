// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package audit_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stowage-dev/stowage/internal/audit"
)

type sliceRecorder struct {
	mu     sync.Mutex
	events []audit.Event
	err    error
}

func (s *sliceRecorder) Record(_ context.Context, e audit.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	s.events = append(s.events, e)
	return nil
}

func (s *sliceRecorder) snapshot() []audit.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]audit.Event, len(s.events))
	copy(out, s.events)
	return out
}

func TestAsyncRecorderDrainsEvents(t *testing.T) {
	inner := &sliceRecorder{}
	rec := audit.NewAsyncRecorder(inner, nil, 16)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { rec.Run(ctx); close(done) }()

	for i := 0; i < 5; i++ {
		_ = rec.Record(context.Background(), audit.Event{Action: "t", UserID: "u"})
	}

	// Stop and let Run drain.
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatalf("drainer did not exit")
	}

	got := inner.snapshot()
	if len(got) != 5 {
		t.Fatalf("expected 5 events drained, got %d", len(got))
	}
}

func TestAsyncRecorderDropsOnOverflow(t *testing.T) {
	// Block the inner recorder so the queue saturates.
	blocker := &blockingRecorder{ready: make(chan struct{})}
	rec := audit.NewAsyncRecorder(blocker, nil, 4)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go rec.Run(ctx)

	// Send more than queueSize+1; the first one is in-flight inside Run,
	// the next queueSize fill the channel, the rest drop.
	for i := 0; i < 200; i++ {
		_ = rec.Record(context.Background(), audit.Event{Action: "t"})
	}
	if rec.Dropped() == 0 {
		t.Fatalf("expected drops once the queue saturates")
	}
	close(blocker.ready)
}

type blockingRecorder struct {
	ready chan struct{}
}

func (b *blockingRecorder) Record(_ context.Context, _ audit.Event) error {
	<-b.ready
	return errors.New("dummy") // exercise the Record-error path
}

func TestAsyncRecorderInnerExposed(t *testing.T) {
	inner := &sliceRecorder{}
	rec := audit.NewAsyncRecorder(inner, nil, 4)
	if rec.Inner() != inner {
		t.Fatalf("Inner() did not return the wrapped recorder")
	}
}
