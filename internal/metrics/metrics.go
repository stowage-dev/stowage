// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package metrics is the in-process request counter used by the admin
// dashboard.
//
// Scope is intentionally small: hourly request counts over the last 24
// hours, per-backend tallies, and a fixed-size ring of recent server-side
// errors. Lost on restart — Phase 7 introduces a real audit log persisted
// to SQLite for cases where durability matters.
package metrics

import (
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/stowage-dev/stowage/internal/auth"
)

// Service is the singleton metrics collector. Safe for concurrent use.
//
// The hot Record path is mostly atomic: the slot's Total/Errors counters and
// each per-backend counter are atomic.Int64s. The slot's UnixHour is also
// atomic — when it diverges from the current hour we acquire the slot mutex
// to rotate it, but that's at most once per hour per slot. errors and the
// errors mutex protect the recent-errors ring (low-frequency 5xx events).
type Service struct {
	// errMu guards the recent-errors ring. Separated from the hourly path
	// so the (much hotter) Record loop never contends with RecordError.
	errMu  sync.Mutex
	errors []ErrorEvent
	maxErr int

	// hourly is the rolling 24-bucket counter. Index = unix-hour mod 24.
	hourly [24]*hourBucket

	// Clock injects time for tests; defaults to time.Now.
	Clock func() time.Time

	// Prom is the optional Prometheus collector hooked into Middleware.
	// Nil disables /metrics observations without affecting the dashboard.
	Prom *Prom
}

// hourBucket is one slot of the rolling histogram. UnixHour, Total, and
// Errors are atomic; ByBackend is a sync.Map of *atomic.Int64 keyed by
// backend id. The slotMu serialises the rare slot-rotation step (when a
// recorded hour rolls past the slot's current UnixHour).
type hourBucket struct {
	slotMu    sync.Mutex
	UnixHour  atomic.Int64
	Total     atomic.Int64
	Errors    atomic.Int64
	ByBackend sync.Map // string → *atomic.Int64
}

// ErrorEvent is one row of the recent-errors ring.
type ErrorEvent struct {
	When    time.Time `json:"when"`
	Path    string    `json:"path"`
	Method  string    `json:"method"`
	Status  int       `json:"status"`
	UserID  string    `json:"user_id,omitempty"`
	Backend string    `json:"backend,omitempty"`
}

// HourlyPoint is one entry in the 24-hour series returned by Snapshot.
type HourlyPoint struct {
	UnixHour int64 `json:"unix_hour"`
	Requests int64 `json:"requests"`
	Errors   int64 `json:"errors"`
}

// Snapshot is the read-side view consumed by the dashboard handler.
type Snapshot struct {
	Total24h     int64            `json:"total_24h"`
	Errors24h    int64            `json:"errors_24h"`
	Hourly       []HourlyPoint    `json:"hourly"`     // length 24, oldest first
	ByBackend    map[string]int64 `json:"by_backend"` // total requests per backend, last 24h
	RecentErrors []ErrorEvent     `json:"recent_errors"`
}

// New returns a Service with the standard 50-entry error buffer.
func New() *Service {
	s := &Service{maxErr: 50}
	for i := range s.hourly {
		s.hourly[i] = &hourBucket{}
	}
	return s
}

func (s *Service) now() time.Time {
	if s.Clock != nil {
		return s.Clock()
	}
	return time.Now()
}

// Record bumps the counter for the current hour. backend may be empty. The
// fast path is fully atomic; only the rare hour-rotation step takes the slot
// mutex.
func (s *Service) Record(backend string, status int) {
	hour := s.now().Unix() / 3600
	b := s.hourly[hour%24]

	if cur := b.UnixHour.Load(); cur != hour {
		// The slot represents a stale hour (or has never been used) —
		// rotate it. Multiple goroutines may race here; the first one
		// through resets, the rest see UnixHour already current.
		b.slotMu.Lock()
		if b.UnixHour.Load() != hour {
			b.UnixHour.Store(hour)
			b.Total.Store(0)
			b.Errors.Store(0)
			b.ByBackend = sync.Map{}
		}
		b.slotMu.Unlock()
	}

	b.Total.Add(1)
	if status >= 500 {
		b.Errors.Add(1)
	}
	if backend != "" {
		c, ok := b.ByBackend.Load(backend)
		if !ok {
			n := new(atomic.Int64)
			actual, _ := b.ByBackend.LoadOrStore(backend, n)
			c = actual
		}
		c.(*atomic.Int64).Add(1)
	}
}

// RecordError pushes onto the recent-errors ring, dropping the oldest when
// full. Only 5xx events should reach here; 4xx are usually client mistakes
// that don't deserve a permanent slot.
func (s *Service) RecordError(e ErrorEvent) {
	s.errMu.Lock()
	defer s.errMu.Unlock()
	s.errors = append(s.errors, e)
	if len(s.errors) > s.maxErr {
		s.errors = s.errors[len(s.errors)-s.maxErr:]
	}
}

// Snapshot returns a deep copy suitable for JSON marshalling.
func (s *Service) Snapshot() Snapshot {
	now := s.now()
	hour := now.Unix() / 3600

	out := Snapshot{
		Hourly:    make([]HourlyPoint, 24),
		ByBackend: map[string]int64{},
	}
	// Walk oldest-to-newest so the dashboard renders left-to-right.
	for i := 0; i < 24; i++ {
		h := hour - int64(23-i)
		slot := s.hourly[h%24]
		var p HourlyPoint
		p.UnixHour = h
		if slot.UnixHour.Load() == h {
			p.Requests = slot.Total.Load()
			p.Errors = slot.Errors.Load()
			slot.ByBackend.Range(func(k, v any) bool {
				out.ByBackend[k.(string)] += v.(*atomic.Int64).Load()
				return true
			})
		}
		out.Hourly[i] = p
		out.Total24h += p.Requests
		out.Errors24h += p.Errors
	}

	s.errMu.Lock()
	out.RecentErrors = make([]ErrorEvent, len(s.errors))
	copy(out.RecentErrors, s.errors)
	s.errMu.Unlock()
	return out
}

// ---- Middleware ---------------------------------------------------------

// Middleware wraps a handler, counting every request after it returns.
// Backend ID is extracted from the chi route context — relies on the
// router using {bid} as the parameter name.
//
// Side effects per request:
//   - Bumps the in-memory hourly histogram (drives the admin dashboard).
//   - When Prom is wired, records a Prometheus counter / duration / bytes
//     observation.
//   - Records 5xx events into the recent-errors ring buffer.
func (s *Service) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		start := s.now()
		next.ServeHTTP(rec, r)
		elapsed := s.now().Sub(start)

		bid := chi.URLParamFromCtx(r.Context(), "bid")
		s.Record(bid, rec.status)
		if s.Prom != nil {
			s.Prom.Observe(r.Method, bid, rec.status, elapsed.Seconds(), rec.bytes)
		}
		if rec.status >= 500 {
			ev := ErrorEvent{
				When:    s.now().UTC(),
				Path:    truncate(r.URL.Path, 200),
				Method:  r.Method,
				Status:  rec.status,
				Backend: bid,
			}
			if id := auth.IdentityFrom(r.Context()); id != nil {
				ev.UserID = id.UserID
			}
			s.RecordError(ev)
		}
	})
}

// statusRecorder shadows Write/WriteHeader so we can read the status code
// after the handler runs and tally bytes for the response_bytes histogram.
// Implements http.Flusher so streaming endpoints keep working.
type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
	bytes       int64
}

func (r *statusRecorder) WriteHeader(code int) {
	if !r.wroteHeader {
		r.status = code
		r.wroteHeader = true
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if !r.wroteHeader {
		r.wroteHeader = true
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += int64(n)
	return n, err
}

func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if !strings.HasSuffix(s, "…") {
		return s[:n] + "…"
	}
	return s[:n]
}
