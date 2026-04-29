// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package s3proxy

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics is the set of Prometheus instruments the data plane emits. All
// metrics are registered in the namespace `stowage_s3` so they sit
// alongside stowage's existing dashboard metrics in a single registry.
type Metrics struct {
	Requests          *prometheus.CounterVec
	Duration          *prometheus.HistogramVec
	AuthFailures      *prometheus.CounterVec
	ScopeViolations   prometheus.Counter
	BytesIn           *prometheus.CounterVec
	BytesOut          *prometheus.CounterVec
	Upstream          *prometheus.HistogramVec
	CacheSize         prometheus.Gauge
	Inflight          prometheus.Gauge
	AnonymousRejects  *prometheus.CounterVec
	AnonymousRequests *prometheus.CounterVec
}

// NewMetrics registers the proxy metrics against the given registerer.
// Stowage passes its existing Prometheus registry so /metrics surfaces
// dashboard and proxy instruments together.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	f := promauto.With(reg)
	const ns = "stowage_s3"
	return &Metrics{
		Requests: f.NewCounterVec(prometheus.CounterOpts{
			Namespace: ns,
			Name:      "request_total",
		}, []string{"method", "operation", "status", "result", "auth_mode"}),
		Duration: f.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: ns,
			Name:      "request_duration_seconds",
			Buckets:   prometheus.DefBuckets,
		}, []string{"operation"}),
		AuthFailures: f.NewCounterVec(prometheus.CounterOpts{
			Namespace: ns,
			Name:      "auth_failure_total",
		}, []string{"reason"}),
		ScopeViolations: f.NewCounter(prometheus.CounterOpts{
			Namespace: ns,
			Name:      "scope_violation_total",
		}),
		BytesIn: f.NewCounterVec(prometheus.CounterOpts{
			Namespace: ns,
			Name:      "bytes_in_total",
		}, []string{"operation"}),
		BytesOut: f.NewCounterVec(prometheus.CounterOpts{
			Namespace: ns,
			Name:      "bytes_out_total",
		}, []string{"operation"}),
		Upstream: f.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: ns,
			Name:      "upstream_duration_seconds",
			Buckets:   prometheus.DefBuckets,
		}, []string{"operation"}),
		CacheSize: f.NewGauge(prometheus.GaugeOpts{
			Namespace: ns,
			Name:      "credential_cache_size",
		}),
		Inflight: f.NewGauge(prometheus.GaugeOpts{
			Namespace: ns,
			Name:      "inflight_requests",
		}),
		AnonymousRejects: f.NewCounterVec(prometheus.CounterOpts{
			Namespace: ns,
			Name:      "anonymous_reject_total",
		}, []string{"reason"}),
		AnonymousRequests: f.NewCounterVec(prometheus.CounterOpts{
			Namespace: ns,
			Name:      "anonymous_request_total",
		}, []string{"operation", "status"}),
	}
}
