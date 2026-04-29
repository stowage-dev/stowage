// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package backend

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// Registry holds the set of configured Backends, keyed by their stable ID,
// alongside a probe-driven health Status. It is safe for concurrent use.
//
// The Get hot path is lock-free: it loads a snapshot of the (id → Backend)
// map via atomic.Pointer and indexes into it directly. Mutations
// (Register, Unregister, Replace) take the slow-path mutex, copy-mutate the
// map, and atomically publish the new pointer. Status / List / History reads
// still use the mutex because they access mutable per-entry fields.
type Registry struct {
	// fast is the snapshot used by Get. Writers replace it under mu.
	fast atomic.Pointer[map[string]Backend]

	mu      sync.RWMutex
	entries map[string]*entry
}

type entry struct {
	backend Backend
	source  Source
	status  Status
	history []ProbeRecord // ring; oldest first
}

// Source distinguishes how a backend got into the registry. The admin API
// uses this to gate edits — only SourceDB entries can be modified through
// the UI; SourceConfig entries are owned by config.yaml and read-only.
type Source string

const (
	SourceConfig Source = "config"
	SourceDB     Source = "db"
)

// historyMax caps each backend's probe history. The dashboard renders a
// 20-bin sparkline so 20 is sufficient.
const historyMax = 20

// Status is the last-known probe result for a backend.
type Status struct {
	Healthy     bool
	LastProbeAt time.Time
	LastError   string
	LastLatency time.Duration
}

// ProbeRecord is one entry of the per-backend probe-history ring.
type ProbeRecord struct {
	At      time.Time
	Healthy bool
	Latency time.Duration
	Error   string
}

func NewRegistry() *Registry {
	r := &Registry{entries: make(map[string]*entry)}
	empty := make(map[string]Backend)
	r.fast.Store(&empty)
	return r
}

// publishFast rebuilds and publishes the lock-free snapshot. Must be called
// while holding r.mu.
func (r *Registry) publishFast() {
	snap := make(map[string]Backend, len(r.entries))
	for id, e := range r.entries {
		snap[id] = e.backend
	}
	r.fast.Store(&snap)
}

// Register adds a config-sourced backend (the historical default). Use
// RegisterWithSource for DB-sourced entries so the admin API can tell them
// apart later.
func (r *Registry) Register(b Backend) error {
	return r.RegisterWithSource(b, SourceConfig)
}

// RegisterWithSource adds a backend tagged with its origin. Returns an error
// if the ID is empty, nil, or already registered.
func (r *Registry) RegisterWithSource(b Backend, source Source) error {
	if b == nil {
		return fmt.Errorf("backend: nil backend")
	}
	id := b.ID()
	if id == "" {
		return fmt.Errorf("backend: empty ID")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.entries[id]; exists {
		return fmt.Errorf("backend: %q already registered", id)
	}
	r.entries[id] = &entry{backend: b, source: source}
	r.publishFast()
	return nil
}

// ErrNotRegistered is returned by Unregister and Replace when no entry exists
// for the given id.
var ErrNotRegistered = fmt.Errorf("backend: not registered")

// Unregister removes a backend from the registry, dropping its status and
// probe history. Returns ErrNotRegistered if the id was unknown.
func (r *Registry) Unregister(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.entries[id]; !ok {
		return ErrNotRegistered
	}
	delete(r.entries, id)
	r.publishFast()
	return nil
}

// Replace swaps the Backend for an existing id while preserving its Source.
// Status and probe history are reset because the new client may point at a
// different endpoint or use new credentials, making historical readings
// misleading. Returns ErrNotRegistered if the id is unknown, or an error if
// the new backend's ID doesn't match.
func (r *Registry) Replace(id string, b Backend) error {
	if b == nil {
		return fmt.Errorf("backend: nil backend")
	}
	if b.ID() != id {
		return fmt.Errorf("backend: id mismatch (registry=%q, backend=%q)", id, b.ID())
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.entries[id]
	if !ok {
		return ErrNotRegistered
	}
	e.backend = b
	e.status = Status{}
	e.history = nil
	r.publishFast()
	return nil
}

// Source reports how the entry was registered. Returns ("", false) when the
// id is unknown.
func (r *Registry) Source(id string) (Source, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.entries[id]
	if !ok {
		return "", false
	}
	return e.source, true
}

func (r *Registry) Get(id string) (Backend, bool) {
	snap := r.fast.Load()
	if snap == nil {
		return nil, false
	}
	b, ok := (*snap)[id]
	return b, ok
}

// List returns all registered backends paired with their last-known status,
// sorted by ID for deterministic output.
func (r *Registry) List() []Entry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Entry, 0, len(r.entries))
	for _, e := range r.entries {
		out = append(out, Entry{Backend: e.backend, Source: e.source, Status: e.status})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Backend.ID() < out[j].Backend.ID() })
	return out
}

// Entry is the public view of a registered backend plus its status.
type Entry struct {
	Backend Backend
	Source  Source
	Status  Status
}

func (r *Registry) Status(id string) (Status, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.entries[id]
	if !ok {
		return Status{}, false
	}
	return e.status, true
}

func (r *Registry) SetStatus(id string, s Status) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e, ok := r.entries[id]; ok {
		e.status = s
	}
}

// History returns a copy of the probe history for one backend, oldest
// first. Empty when nothing has probed yet.
func (r *Registry) History(id string) []ProbeRecord {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.entries[id]
	if !ok {
		return nil
	}
	out := make([]ProbeRecord, len(e.history))
	copy(out, e.history)
	return out
}

// recordProbe pushes a fresh result into the ring under the lock.
func (r *Registry) recordProbe(id string, rec ProbeRecord, status Status) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.entries[id]
	if !ok {
		return
	}
	e.status = status
	e.history = append(e.history, rec)
	if len(e.history) > historyMax {
		e.history = e.history[len(e.history)-historyMax:]
	}
}

// Probe runs ListBuckets against one backend with a timeout. It does not
// modify registry state — callers typically feed the result into SetStatus.
func Probe(ctx context.Context, b Backend, timeout time.Duration) Status {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	start := time.Now()
	_, err := b.ListBuckets(ctx)
	s := Status{
		LastProbeAt: time.Now().UTC(),
		LastLatency: time.Since(start),
	}
	if err != nil {
		s.LastError = err.Error()
		return s
	}
	s.Healthy = true
	return s
}

// ProbeAll probes every registered backend in parallel and stores results
// + a history record per backend.
func (r *Registry) ProbeAll(ctx context.Context, timeout time.Duration) {
	r.mu.RLock()
	backends := make([]Backend, 0, len(r.entries))
	for _, e := range r.entries {
		backends = append(backends, e.backend)
	}
	r.mu.RUnlock()

	var wg sync.WaitGroup
	for _, b := range backends {
		wg.Add(1)
		go func(b Backend) {
			defer wg.Done()
			s := Probe(ctx, b, timeout)
			rec := ProbeRecord{
				At:      s.LastProbeAt,
				Healthy: s.Healthy,
				Latency: s.LastLatency,
				Error:   s.LastError,
			}
			r.recordProbe(b.ID(), rec, s)
		}(b)
	}
	wg.Wait()
}
