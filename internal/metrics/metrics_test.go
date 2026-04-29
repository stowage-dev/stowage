// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package metrics

import (
	"testing"
	"time"
)

func TestRecordTotalsAndPerBackend(t *testing.T) {
	s := New()
	s.Clock = func() time.Time { return time.Date(2026, 4, 25, 10, 30, 0, 0, time.UTC) }

	for i := 0; i < 5; i++ {
		s.Record("alpha", 200)
	}
	for i := 0; i < 3; i++ {
		s.Record("beta", 200)
	}
	s.Record("alpha", 503)

	snap := s.Snapshot()
	if snap.Total24h != 9 {
		t.Fatalf("total=%d want 9", snap.Total24h)
	}
	if snap.Errors24h != 1 {
		t.Fatalf("errors=%d want 1", snap.Errors24h)
	}
	if snap.ByBackend["alpha"] != 6 {
		t.Fatalf("alpha=%d want 6", snap.ByBackend["alpha"])
	}
	if snap.ByBackend["beta"] != 3 {
		t.Fatalf("beta=%d want 3", snap.ByBackend["beta"])
	}
	if len(snap.Hourly) != 24 {
		t.Fatalf("hourly len=%d want 24", len(snap.Hourly))
	}
	// The newest bin is the last one.
	if snap.Hourly[23].Requests != 9 {
		t.Fatalf("newest hour=%d want 9", snap.Hourly[23].Requests)
	}
}

func TestRecordRollsOldHoursToZero(t *testing.T) {
	s := New()
	now := time.Date(2026, 4, 25, 10, 30, 0, 0, time.UTC)
	s.Clock = func() time.Time { return now }

	for i := 0; i < 4; i++ {
		s.Record("alpha", 200)
	}
	// Jump 25 hours forward — yesterday's bucket must not contribute.
	now = now.Add(25 * time.Hour)
	s.Record("alpha", 200)

	snap := s.Snapshot()
	if snap.Total24h != 1 {
		t.Fatalf("total=%d want 1 (old hour should have rolled off)", snap.Total24h)
	}
}

func TestSnapshotZerosUnusedHours(t *testing.T) {
	s := New()
	now := time.Date(2026, 4, 25, 10, 0, 0, 0, time.UTC)
	s.Clock = func() time.Time { return now }
	s.Record("alpha", 200)

	now = now.Add(2 * time.Hour)
	snap := s.Snapshot()
	// Hours -23 .. -1 from now should be 0; the only non-zero bin is the
	// one we wrote to two hours ago.
	if snap.Total24h != 1 {
		t.Fatalf("total=%d want 1", snap.Total24h)
	}
	hits := 0
	for _, h := range snap.Hourly {
		if h.Requests > 0 {
			hits++
		}
	}
	if hits != 1 {
		t.Fatalf("non-zero bins=%d want 1", hits)
	}
}

func TestRecordErrorRing(t *testing.T) {
	s := New()
	s.Clock = func() time.Time { return time.Date(2026, 4, 25, 10, 0, 0, 0, time.UTC) }
	for i := 0; i < 60; i++ {
		s.RecordError(ErrorEvent{Status: 500, Path: "/", When: time.Now()})
	}
	snap := s.Snapshot()
	if len(snap.RecentErrors) != 50 {
		t.Fatalf("ring size=%d want 50", len(snap.RecentErrors))
	}
}
