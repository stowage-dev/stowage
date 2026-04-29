# stowage vs MinIO upstream — head-to-head

Captured 2026-04-26 against the same docker-compose stack as
`results.md` and `results-minio.md`. **Both stowage and MinIO are
constrained to 1 CPU and 200 MiB** (cgroup limits via
`benchmarks/docker-compose.bench.yml`, with `GOMEMLIMIT=180MiB`
on each so Go's GC stays inside the cap). Bench client and both
servers shared the same host. 16 concurrent workers per case,
15 s per case.

The numbers below come straight from those two reports — this
file just lines up the four endpoints that exist on both sides
plus the unauthenticated health probes for context.

## Summary (matched 1 CPU / 200 MiB)

| Operation | stowage (req/s) | minio (req/s) | minio / stowage | stowage p50 (ms) | minio p50 (ms) | stowage p99 (ms) | minio p99 (ms) |
|---|---:|---:|---:|---:|---:|---:|---:|
| Health probe | 5609.6 (`/healthz`) | 6532.3 (`/minio/health/live`) | 1.16× | 2.04 | 0.68 | 25.53 | 59.00 |
| List buckets | 1073.3 | 2155.9 | 2.01× | 7.15 | 2.49 | 69.07 | 69.07 |
| List objects | 883.7 | 1054.4 | 1.19× | 8.06 | 4.94 | 76.31 | 79.37 |
| HEAD object | 984.3 | 1406.7 | 1.43× | 7.33 | 3.66 | 73.02 | 74.95 |
| GET object (1 KiB) | 829.2 | 1158.9 | 1.40× | 9.05 | 4.43 | 76.88 | 78.36 |

## Per-operation overhead added by stowage

The four S3-shaped endpoints all do *exactly* one upstream call to
MinIO; the difference is what stowage adds on top: SigV4 signing of the
outgoing request, session lookup + CSRF check on the incoming request,
audit-log write, Prometheus middleware, JSON re-serialisation, and a
second HTTP hop.

| Operation | Δ p50 (stowage − minio) | Δ p99 | Throughput cost |
|---|---:|---:|---:|
| List buckets | +4.7 ms | ±0 ms | −50% |
| List objects | +3.1 ms | −3 ms | −16% |
| HEAD object | +3.7 ms | −2 ms | −30% |
| GET object | +4.6 ms | −1 ms | −28% |

With identical 1-CPU envelopes, **stowage costs roughly 3–5 ms of extra
p50 latency and ~16–50 % of throughput** versus going straight to MinIO.
Notably, **p99 latencies are now in the same ballpark on both sides** —
the long tail is dominated by CPU contention inside a 1-CPU cgroup, and
both processes hit the wall at about the same point.

## How this changed vs the previous (asymmetric) run

In the earlier comparison MinIO was unconstrained, which made the
proxy look like a 3× throughput tax. With MinIO also pinned to 1 CPU,
that gap collapses considerably:

| Operation | minio/stowage *before* (MinIO unconstrained) | minio/stowage *now* (matched) |
|---|---:|---:|
| Health probe | 4.4× | 1.16× |
| List buckets | 3.4× | 2.01× |
| List objects | 2.4× | 1.19× |
| HEAD object | 2.8× | 1.43× |
| GET object | 3.0× | 1.40× |

The earlier ratio was mostly measuring "MinIO has more cores", not
"stowage is slow". Under matched constraints stowage is within 1.2–2.0×
of raw MinIO throughput on the S3-shaped routes, while still doing all
the auth / CSRF / audit / RBAC / quota / metrics work MinIO doesn't.

## Calibration: what's "fair" here

A few things to keep in mind before reading too much into the ratio:

- **Both containers are now CPU- and memory-bound to the same envelope.**
  MinIO's `GOMEMLIMIT=180MiB` was added so its GC also stays inside the
  cgroup; without it MinIO would OOM under sustained list/get load.
- **Stowage's hot path includes work MinIO doesn't do**: per-request
  session attach (sqlite hit), CSRF middleware, RBAC, audit recorder,
  Prometheus metrics middleware, and a JSON envelope. None of those
  are free; they're features the proxy is there to provide.
- **The four endpoints above are the ones with a 1:1 backend mapping.**
  stowage's `/api/me`, `/api/auth/config`, `/api/backends`, and
  `/auth/login/local` have no MinIO equivalent — see `results.md`.
- **Same host, loopback, no TLS.** Real deployments add reverse-proxy
  TLS termination on both sides (the `+~4 ms` cost stays roughly
  constant; ratio shrinks further as upstream RTT grows).
- **Single 15 s sample per case.** Treat ±10–15 % as noise; only the
  ratios matter, not the headline req/s.

## Take-away

Under identical 1 CPU / 200 MiB constraints, stowage sustains roughly
**830–1100 req/s of S3-shaped traffic** with p50 in the 7–9 ms band.
Raw MinIO at the same envelope manages **1050–2150 req/s** with p50
2.5–5 ms. The proxy adds ~3–5 ms median per request — the price of
auth, audit, RBAC, quotas, and a unified UI on top of the bucket — and
loses 16–50 % of throughput depending on the call. The p99 long tail is
the same on both sides because both are simply CPU-saturated.
