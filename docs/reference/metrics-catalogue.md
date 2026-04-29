---
type: reference
---

# Prometheus metrics

Stowage exposes metrics on the dashboard listener at `/metrics`. The
endpoint is unauthenticated by design; restrict at the reverse-proxy
layer if you don't want it world-readable.

## Dashboard metrics

Source:
[`internal/metrics/prom.go`](https://github.com/stowage-dev/stowage/blob/main/internal/metrics/prom.go).

| Metric | Type | Labels | Description |
|---|---|---|---|
| `stowage_requests_total` | counter | `method`, `status_class`, `backend` | Total HTTP requests handled. `status_class` is `1xx..5xx`. |
| `stowage_request_duration_seconds` | histogram | `method`, `backend` | End-to-end handler duration. Buckets: 5 ms .. 30 s. |
| `stowage_response_bytes` | histogram | `method`, `backend` | Bytes written to response body. Buckets: 1 KiB .. 16 GiB. |
| `stowage_sqlite_db_bytes` | gauge | — | Size of the SQLite main file (WAL/SHM excluded). |

Plus the standard Go runtime collectors:

- `process_cpu_seconds_total`, `process_resident_memory_bytes`,
  `process_open_fds`, etc.
- `go_goroutines`, `go_gc_duration_seconds`, `go_memstats_*`, etc.

Cardinality bounds:

- `method` is a fixed set (GET/POST/PUT/DELETE/HEAD/PATCH/OPTIONS).
- `status_class` is a fixed set.
- `backend` is bounded by the number of configured backends.
- Bucket and key are deliberately **not** labels — they would
  explode TSDB cardinality.

## S3 proxy metrics

Source:
[`internal/s3proxy/metrics.go`](https://github.com/stowage-dev/stowage/blob/main/internal/s3proxy/metrics.go).
Namespace: `stowage_s3`.

| Metric | Type | Labels | Description |
|---|---|---|---|
| `stowage_s3_request_total` | counter | `method`, `operation`, `status`, `result`, `auth_mode` | One per proxy request. |
| `stowage_s3_request_duration_seconds` | histogram | `operation` | End-to-end request time. |
| `stowage_s3_upstream_duration_seconds` | histogram | `operation` | Time spent talking to the upstream. |
| `stowage_s3_bytes_in_total` | counter | `operation` | Bytes received from clients. |
| `stowage_s3_bytes_out_total` | counter | `operation` | Bytes sent to clients. |
| `stowage_s3_auth_failure_total` | counter | `reason` | SigV4 verification failures. |
| `stowage_s3_scope_violation_total` | counter | — | 403s due to bucket-scope mismatch. |
| `stowage_s3_anonymous_request_total` | counter | `operation`, `status` | Per-anonymous-binding request. |
| `stowage_s3_anonymous_reject_total` | counter | `reason` | Anonymous requests rejected. |
| `stowage_s3_credential_cache_size` | gauge | — | Number of credentials in the proxy's in-memory cache. |
| `stowage_s3_inflight_requests` | gauge | — | Currently in-flight proxy requests. |

## Audit recorder metrics

Today the audit recorder doesn't emit Prometheus metrics directly.
The dashboard at `/admin/dashboard` shows the same data over a 24h
window via the SQLite audit table.

## Useful queries

Request rate per status class:

```promql
sum by (status_class) (rate(stowage_requests_total[5m]))
```

P99 dashboard latency:

```promql
histogram_quantile(0.99,
  sum by (le, method) (rate(stowage_request_duration_seconds_bucket[5m])))
```

P99 proxy latency by operation:

```promql
histogram_quantile(0.99,
  sum by (le, operation) (rate(stowage_s3_request_duration_seconds_bucket[5m])))
```

5xx in the last 5 minutes:

```promql
sum(increase(stowage_requests_total{status_class="5xx"}[5m]))
```

SQLite growth rate (bytes per second):

```promql
deriv(stowage_sqlite_db_bytes[10m])
```

## Sample Grafana dashboard

[`deploy/grafana/stowage.json`](https://github.com/stowage-dev/stowage/blob/main/deploy/grafana/stowage.json)
ships a starter dashboard with:

- Request rate by status class.
- Duration p50 / p95 / p99.
- Response bytes/sec per backend.
- 5xx in last 5 minutes.
- SQLite DB size.
- Process memory + goroutines.
- Top backends by request rate.

Filterable by the `backend` template variable.

## Scrape config

```yaml
scrape_configs:
  - job_name: stowage
    static_configs:
      - targets: ['stowage.internal:8080']
    metrics_path: /metrics
    scrape_interval: 15s
```
