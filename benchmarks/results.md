# stowage benchmark results

- target: `http://localhost:18080`
- concurrency (default): 16
- duration (default): 15s
- concurrency (login): 1
- duration (login): 30s
- container limits: 1 CPU, 200 MB memory (stowage only)
- captured: 2026-04-26T02:06:35Z UTC

## Summary

| Endpoint | Conc | Ops | Errs | Throughput (req/s) | Mean (ms) | p50 (ms) | p95 (ms) | p99 (ms) | Min (ms) | Max (ms) |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| `GET /healthz` | 16 | 87885 | 0 | 5858.4 | 2.73 | 1.97 | 4.96 | 24.68 | 0.13 | 32.78 |
| `GET /readyz` | 16 | 88492 | 0 | 5898.5 | 2.71 | 1.98 | 5.13 | 23.57 | 0.13 | 33.52 |
| `GET /metrics` | 16 | 15387 | 0 | 1025.4 | 15.59 | 7.91 | 60.36 | 65.42 | 1.01 | 78.89 |
| `GET /api/auth/config` | 16 | 84245 | 0 | 5615.5 | 2.85 | 2.06 | 5.68 | 23.57 | 0.15 | 34.04 |
| `GET /api/me` | 16 | 66701 | 0 | 4445.8 | 3.60 | 2.65 | 6.86 | 26.22 | 0.18 | 32.63 |
| `GET /api/backends` | 16 | 80426 | 0 | 5360.8 | 2.98 | 2.16 | 5.79 | 24.55 | 0.15 | 35.01 |
| `GET /api/backends/{id}/buckets` | 16 | 16176 | 0 | 1078.0 | 14.84 | 7.15 | 61.99 | 69.54 | 0.73 | 82.86 |
| `GET /api/backends/{id}/.../objects` | 16 | 11885 | 0 | 791.8 | 20.21 | 9.03 | 72.36 | 80.05 | 1.00 | 94.19 |
| `HEAD /api/backends/{id}/object` | 16 | 14542 | 0 | 965.4 | 16.52 | 7.50 | 66.31 | 73.24 | 0.83 | 94.15 |
| `GET /api/backends/{id}/object` | 16 | 12112 | 0 | 807.2 | 19.82 | 9.33 | 69.03 | 77.27 | 1.00 | 91.79 |
| `POST /auth/login/local` | 1 | 9 | 0 | 5.9 | 169.78 | 110.51 | 331.41 | 331.41 | 105.15 | 389.46 |

## Notes
- Throughput = successful ops / wall duration of the case.
- Latencies are computed only over successful responses (status == expected).
- `POST /auth/login/local` is capped by a hardcoded 10-attempts / 15-min /
  IP limiter in `internal/server/server.go`; the bench worker stops after
  the first 429 so the latency / throughput row reflects only the
  successful attempts. argon2id verification uses `m=65536` (~64 MiB)
  per hash, so login concurrency cannot safely exceed 1 inside a 200 MiB
  container without OOM-killing the server.
- Object endpoints are exercised against a 1 KiB seeded object on a co-located MinIO backend.
- `GET /metrics` is the slowest read because Prometheus has to serialise
  every request-histogram bucket plus the Go runtime collectors on each
  scrape. A 5–15s scrape interval in production keeps it negligible.
