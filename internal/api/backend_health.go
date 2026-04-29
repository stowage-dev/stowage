// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"net/http"
	"time"
)

// Backend probe-history endpoint — Phase 8.
//
// The dashboard already shows per-backend storage / request stats; this
// admin route surfaces the rolling probe history so operators can see at
// a glance which backend has been flapping. Mounted admin-only.

type probeRecordDTO struct {
	At        string `json:"at"`
	Healthy   bool   `json:"healthy"`
	LatencyMS int64  `json:"latency_ms"`
	Error     string `json:"error,omitempty"`
}

type backendHealthDTO struct {
	ID          string           `json:"id"`
	Name        string           `json:"name"`
	Healthy     bool             `json:"healthy"`
	LastProbeAt string           `json:"last_probe_at,omitempty"`
	LastError   string           `json:"last_error,omitempty"`
	LatencyMS   int64            `json:"latency_ms"`
	History     []probeRecordDTO `json:"history"`
}

func (d *BackendDeps) handleBackendHealth(w http.ResponseWriter, _ *http.Request) {
	entries := d.Registry.List()
	out := make([]backendHealthDTO, 0, len(entries))
	for _, e := range entries {
		dto := backendHealthDTO{
			ID:        e.Backend.ID(),
			Name:      e.Backend.DisplayName(),
			Healthy:   e.Status.Healthy,
			LastError: e.Status.LastError,
			LatencyMS: e.Status.LastLatency.Milliseconds(),
		}
		if !e.Status.LastProbeAt.IsZero() {
			dto.LastProbeAt = e.Status.LastProbeAt.UTC().Format(time.RFC3339)
		}
		hist := d.Registry.History(e.Backend.ID())
		dto.History = make([]probeRecordDTO, 0, len(hist))
		for _, p := range hist {
			dto.History = append(dto.History, probeRecordDTO{
				At:        p.At.UTC().Format(time.RFC3339),
				Healthy:   p.Healthy,
				LatencyMS: p.Latency.Milliseconds(),
				Error:     p.Error,
			})
		}
		out = append(out, dto)
	}
	writeJSON(w, http.StatusOK, map[string]any{"backends": out})
}
