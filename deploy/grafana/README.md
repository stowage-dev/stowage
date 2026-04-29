# Stowage Grafana dashboard

`stowage.json` is a starter dashboard for the Prometheus metrics stowage
exposes at `/metrics`.

## Importing

1. In Grafana: **Dashboards → New → Import**.
2. Upload `stowage.json` (or paste its contents).
3. Pick the Prometheus datasource that scrapes your stowage instance.

## Prometheus scrape config

```yaml
scrape_configs:
  - job_name: stowage
    static_configs:
      - targets: ['stowage.internal:8080']
    metrics_path: /metrics
```

Stowage exposes `/metrics` without authentication. Restrict access at the
reverse-proxy / network policy layer if you don't want it world-readable.

## Panels

- **Request rate by status class** — total req/s, broken out by 2xx/3xx/4xx/5xx.
- **Request duration p50/p95/p99** — latency percentiles from the
  `stowage_request_duration_seconds` histogram.
- **Response bytes per second** — egress throughput per backend.
- **5xx in last 5 minutes** — quick alert-style stat panel.
- **SQLite DB size** — size of the main DB file (WAL/SHM excluded).
- **Process memory + goroutines** — runtime sanity check.
- **Top backends by request rate** — table of the 10 noisiest backends.

The `backend` template variable filters everything except the runtime
panel. Pin it to one backend if you've got a noisy neighbour.
