// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package audit records who did what. Every state-changing action in the
// proxy is expected to go through a Recorder.
package audit

import (
	"context"
	"time"
)

// Event is one audit record.
type Event struct {
	Timestamp time.Time
	UserID    string
	Action    string
	Backend   string
	Bucket    string
	Key       string
	RequestID string
	IP        string
	UserAgent string
	Status    string // "ok" | "denied" | "error"
	Detail    map[string]any
}

// Recorder persists audit events.
type Recorder interface {
	Record(ctx context.Context, e Event) error
}

// BatchRecorder is an optional capability a Recorder can implement to
// commit several events in one transaction. AsyncRecorder uses it when
// available so its drainer can flush an opportunistic batch in a single
// SQLite round-trip rather than one INSERT per event.
type BatchRecorder interface {
	Recorder
	RecordBatch(ctx context.Context, events []Event) error
}

// Noop is a Recorder that discards events. Useful for tests and for the
// initial scaffold before a backing store is wired up.
type Noop struct{}

func (Noop) Record(context.Context, Event) error { return nil }
