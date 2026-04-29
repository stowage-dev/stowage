// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package metrics

import (
	"net/http"
	"os"
	"strconv"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Prom owns the Prometheus collectors used by the /metrics endpoint.
//
// Label cardinality rules:
//   - method ∈ {GET,POST,PUT,DELETE,HEAD,PATCH,OPTIONS} — bounded.
//   - status_class ∈ {1xx..5xx} — bounded.
//   - backend ∈ configured backend IDs — bounded by config.
//   - bucket / object key are NOT used as labels — unbounded.
//
// Bucket-level visibility is the dashboard's job; Prometheus is for trend
// + alert traffic where exploding cardinality kills the TSDB.
type Prom struct {
	Registry  *prometheus.Registry
	requests  *prometheus.CounterVec
	duration  *prometheus.HistogramVec
	respBytes *prometheus.HistogramVec

	// childCache memoises the per-(method, status_class, backend) child
	// collectors so the hot path skips the WithLabelValues lookup —
	// label hashing + map lookups inside client_golang are the dominant
	// allocation in the metrics middleware.
	childCache sync.Map // childKey → *promChildren
}

type childKey struct {
	method, statusClass, backend string
}

type promChildren struct {
	requestCounter prometheus.Counter
	durationHist   prometheus.Observer
	bytesHist      prometheus.Observer
}

// NewProm constructs the registry + collectors. dbPath is stat()'d on each
// scrape; pass "" to skip the SQLite-size gauge.
func NewProm(dbPath string) *Prom {
	reg := prometheus.NewRegistry()
	p := &Prom{Registry: reg}

	p.requests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "stowage_requests_total",
		Help: "Total HTTP requests handled by the proxy.",
	}, []string{"method", "status_class", "backend"})
	reg.MustRegister(p.requests)

	// Default histogram buckets are tuned for fast API calls; uploads will
	// land in the upper bin and that's fine — we have separate latency
	// monitoring via P95 alerts at the operator's discretion.
	p.duration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "stowage_request_duration_seconds",
		Help:    "End-to-end handler duration.",
		Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10, 30},
	}, []string{"method", "backend"})
	reg.MustRegister(p.duration)

	// Bytes histograms cover everything from JSON responses (~kB) to large
	// downloads (~GB) — the buckets span 5 orders of magnitude.
	p.respBytes = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "stowage_response_bytes",
		Help:    "Bytes written to the response body.",
		Buckets: []float64{1 << 10, 64 << 10, 1 << 20, 16 << 20, 256 << 20, 1 << 30, 16 << 30},
	}, []string{"method", "backend"})
	reg.MustRegister(p.respBytes)

	if dbPath != "" {
		// GaugeFunc evaluates on every scrape — no background poller needed.
		// Returns 0 when the file is missing rather than failing the whole
		// scrape; operators usually catch this via process collectors instead.
		dbGauge := prometheus.NewGaugeFunc(
			prometheus.GaugeOpts{
				Name: "stowage_sqlite_db_bytes",
				Help: "Size of the SQLite database file (main file only; WAL/SHM excluded).",
			},
			func() float64 {
				st, err := os.Stat(dbPath)
				if err != nil {
					return 0
				}
				return float64(st.Size())
			},
		)
		reg.MustRegister(dbGauge)
	}

	// Standard process / Go runtime collectors — heap size, goroutines,
	// GC pauses, FD count. Free with the registry.
	reg.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	reg.MustRegister(collectors.NewGoCollector())

	return p
}

// Observe records a finished request. Called by the metrics middleware
// after the handler returns. backend may be empty for non-bucket-scoped
// routes.
func (p *Prom) Observe(method, backend string, status int, durationSeconds float64, bytesWritten int64) {
	if p == nil {
		return
	}
	c := p.children(method, statusClass(status), backend)
	c.requestCounter.Inc()
	c.durationHist.Observe(durationSeconds)
	if bytesWritten > 0 {
		c.bytesHist.Observe(float64(bytesWritten))
	}
}

// children returns the cached child collectors for the given label tuple,
// resolving them via WithLabelValues only on the first miss. Safe for
// concurrent callers: sync.Map handles that natively, and the rare
// double-resolve under contention just costs an extra label lookup — both
// values point at the same underlying counter / histogram.
func (p *Prom) children(method, statusClass, backend string) *promChildren {
	k := childKey{method: method, statusClass: statusClass, backend: backend}
	if v, ok := p.childCache.Load(k); ok {
		return v.(*promChildren)
	}
	c := &promChildren{
		requestCounter: p.requests.WithLabelValues(method, statusClass, backend),
		durationHist:   p.duration.WithLabelValues(method, backend),
		bytesHist:      p.respBytes.WithLabelValues(method, backend),
	}
	actual, _ := p.childCache.LoadOrStore(k, c)
	return actual.(*promChildren)
}

// Handler returns the http.Handler that serves /metrics in the Prometheus
// text exposition format.
func (p *Prom) Handler() http.Handler {
	if p == nil {
		// Fallback so the route stays mounted but surfaces 503 if the
		// collector wasn't constructed.
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "metrics not initialised", http.StatusServiceUnavailable)
		})
	}
	return promhttp.HandlerFor(p.Registry, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	})
}

func statusClass(status int) string {
	if status < 100 || status >= 600 {
		return "unknown"
	}
	return strconv.Itoa(status/100) + "xx"
}
