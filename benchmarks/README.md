# benchmarks

A throughput / latency / ops benchmark for the most commonly-hit stowage
HTTP endpoints — the dashboard `/api/*` surface **and** the embedded S3
SigV4 proxy — run inside a CPU- and memory-constrained Docker container.

## Layout

```
benchmarks/
  bench.go                 dashboard HTTP load generator + report renderer
  s3proxybench/main.go     S3 proxy SigV4 load generator (admin VC + anon binding)
  miniobench/main.go       MinIO-direct comparison bench (isolates proxy overhead)
  check/main.go            regression gate: results.json vs baseline.json
  config.bench.yaml        stowage config used inside the container
  Dockerfile.bench         packages a pre-built static stowage into an alpine image
  docker-compose.bench.yml minio + stowage (1 CPU / 200 MiB cgroup limit, with the
                           S3 proxy listener exposed on 18090)
  entrypoint.sh            seeds the bench admin then exec's `stowage serve`
  run.sh                   one-shot driver: build → up → dashboard bench →
                           S3 proxy bench → down
  results.md               dashboard run output
  results-s3proxy.md       S3 proxy run output
  results-minio.md         MinIO-direct comparison run output (manual)
  .bin/stowage             built artefact (gitignored)
```

## What gets measured

Per endpoint, for a fixed wall-clock duration and worker count, the
benchmark reports:

- **Throughput** — successful ops per second
- **Latency** — min / p50 / p95 / p99 / max / mean (ms)
- **Total ops** and error count

### Dashboard endpoints (`bench.go`)

| Endpoint | Auth | Backend hit | Notes |
|---|---|---|---|
| `GET /healthz` | none | none | sentinel |
| `GET /readyz` | none | none | sentinel |
| `GET /metrics` | none | none | Prometheus scrape |
| `GET /api/auth/config` | none | none | public auth modes |
| `GET /api/me` | session | sqlite | identity lookup |
| `GET /api/backends` | session | none | static config |
| `GET /api/backends/{id}/buckets` | session | s3 ListBuckets | |
| `GET /api/backends/{id}/buckets/{b}/objects` | session | s3 ListObjectsV2 | |
| `HEAD /api/backends/{id}/buckets/{b}/object` | session | s3 HeadObject | |
| `GET /api/backends/{id}/buckets/{b}/object` | session | s3 GetObject | 1 KiB body |
| `POST /auth/login/local` | none | sqlite + argon2id | rate-limited |

### S3 proxy endpoints (`s3proxybench/main.go`)

The proxy bench logs in as admin via the dashboard, mints a virtual
credential scoped to two buckets, registers a ReadOnly anonymous binding
for one of them, then drives the proxy listener with the AWS SDK v2 SigV4
client. The result set deliberately covers the full proxy code surface —
auth-success, auth-failure, scope-violation, anonymous fast-path, and
upstream-bound IO at two object sizes.

| Case | Auth | Upstream | Notes |
|---|---|---|---|
| `Proxy ListBuckets` | SigV4 | none | synthesised in-proxy from the credential's scope |
| `Proxy HeadBucket` | SigV4 | s3 HeadBucket | |
| `Proxy ListObjectsV2` | SigV4 | s3 ListObjectsV2 | |
| `Proxy HeadObject` | SigV4 | s3 HeadObject | |
| `Proxy GetObject 1 KiB` | SigV4 | s3 GetObject | latency-dominated read |
| `Proxy GetObject 1 MiB` | SigV4 | s3 GetObject | throughput-dominated read |
| `Proxy PutObject 1 KiB` | SigV4 | s3 PutObject | small write, signed payload |
| `Proxy PutObject 1 MiB` | SigV4 | s3 PutObject | streams aws-chunked through the verifier |
| `Proxy DeleteObject` | SigV4 | s3 DeleteObject | non-existent key, idempotent 204 |
| `Proxy GetObject (presigned)` | presigned URL | s3 GetObject | reuses one URL per case |
| `Proxy GetObject (anonymous)` | none | s3 GetObject | unauthenticated fast-path |
| `Proxy Auth Failure (bad sig)` | forged | none | reject-path latency, expects 403 |
| `Proxy Scope Violation` | SigV4 | none | bucket out of scope, expects 403 |

## How to run

From the repository root:

```bash
benchmarks/run.sh
```

That script:

1. Builds a static `stowage` binary at `benchmarks/.bin/stowage` with
   `CGO_ENABLED=0`. (Building outside Docker keeps the image small and
   avoids needing alpine package fetches inside the build sandbox; the
   pure-Go `modernc.org/sqlite` driver lets us go fully static.)
2. `docker compose build`s the bench image and brings up MinIO + stowage.
   The compose file enables `s3_proxy` on `:8090` (host `18090`) so both
   suites have a target.
3. Waits for `/readyz`.
4. Runs the dashboard bench (`go run ./benchmarks`) against `localhost:18080`,
   writing `benchmarks/results.md`.
5. Runs the S3 proxy bench (`go run ./benchmarks/s3proxybench`) against
   `localhost:18090` (with `localhost:18080` for the admin REST setup),
   writing `benchmarks/results-s3proxy.md`.
6. `docker compose down -v` to clean up.

To run the proxy bench alone — skipping the dashboard cases and the
container build — bring the stack up first, then:

```bash
go run ./benchmarks/s3proxybench \
    -dashboard http://localhost:18080 \
    -proxy http://localhost:18090 \
    -username admin -password "$BENCH_ADMIN_PASS" \
    -duration 15s -concurrency 16
```

Tunable env knobs:

| Env | Default | Meaning |
|---|---|---|
| `BENCH_DURATION` | `15s` | per-endpoint duration |
| `BENCH_CONCURRENCY` | `16` | worker count for read endpoints |
| `BENCH_LOGIN_DURATION` | `10s` | duration for the login case |
| `BENCH_LOGIN_CONCURRENCY` | `1` | see "argon2id and the 200 MiB cap" below |
| `BENCH_ADMIN_PASS` | `B3nchm@rk-Pa55w0rd` | admin password (must be ≥ 8 chars, matches `config.bench.yaml`) |

## Resource envelope

Both stowage *and* MinIO run under a hard cgroup cap of **1 CPU and
200 MiB RAM**, set in `docker-compose.bench.yml` via both the v3
long-form (`deploy.resources.limits`) and the v2 short-form (`cpus:` /
`mem_limit:`) so it works on any compose client. Matching the limits on
both sides is what makes `results-comparison.md` apples-to-apples; an
earlier run with MinIO unconstrained mostly measured "MinIO has more
cores".

`GOMEMLIMIT=180MiB` is exported into both containers so the Go GC runs
against a hint that stays inside the cgroup, rather than discovering the
limit only when the kernel SIGKILLs the process. (Both stowage and
MinIO are Go binaries; without this MinIO will OOM under sustained
list/get load at the 200 MiB cap.)

## argon2id and the 200 MiB cap

`POST /auth/login/local` calls `argon2.IDKey` with `m=65536` — that's
**~64 MiB per concurrent verification**. Two concurrent logins plus the
Go runtime + sqlite working set is enough to push the container past
200 MiB and trigger an OOM-kill, so the bench defaults
`-login-concurrency=1`. The endpoint is *also* gated by a hardcoded
10-attempts-per-15-minutes per-IP limiter
(`internal/server/server.go: auth.NewRateLimiter(10, 15*time.Minute)`),
so the bench worker exits after the first 429. The reported
throughput / latency therefore reflect only the successful attempts.

If you raise the container limit, increase `BENCH_LOGIN_CONCURRENCY` in
tandem and restart between runs (or wait 15 min) so the IP limiter has
budget for fresh attempts.

## Known sources of noise

- **Cold caches.** Each case runs a 2 s warmup before measurement to let
  Go inline + JIT, fill SQLite page cache, and prime the MinIO HTTP keep-
  alive pool.
- **Compose port mapping.** Traffic crosses the host loopback +
  iptables/nftables NAT for the `18080:8080` mapping. On macOS this is
  via the linuxkit VM. Numbers are best treated as *relative*, not
  absolute SLOs.
- **Single benchmarking host.** The bench client and stowage compete for
  CPU on the same machine (only stowage is throttled). For an air-tight
  number you'd want a separate load-gen host.
