---
type: explanation
---

# Roadmap

What's deferred from v1.0, with the gating condition for each. Source
of truth: `README.md` "Known follow-ups".

## Deferred from in-phase scope

None of these block v1.0. They're conditional on either user demand,
upstream changes, or a logical predecessor landing first.

### Failed-login audit rows

Stowage's per-IP rate limiter on `/auth/login/local` already covers
brute-force defense. Recording every failed attempt in the audit log
is on the wishlist for forensic completeness; the rate limiter just
means it isn't urgent.

### Async audit recorder by default

The synchronous SQLite recorder is fine at current scale. The
`BatchRecorder` (which is async + batched) is already used on the
proxy hot path. Switching the dashboard's audit handlers to async
would reduce p99 tail at the cost of a small drop window on crash.
Today's tradeoff favours sync for forensic guarantees on the smaller
volume.

### Quota scanner walking every bucket

Today the scanner walks only buckets with a configured quota. The
dashboard's storage card therefore excludes untracked buckets. A
"walk every bucket" mode would unify the storage view at the cost
of more `ListObjectsV2` traffic on bucket-heavy backends.

### Native admin-API user / key / policy screens

Gated on `Capabilities.AdminAPI != ""`. Today every driver returns
`""`. Implementing this requires writing dedicated `minio`,
`garage`, and `seaweedfs` drivers that expose their native admin
APIs through a sibling interface (`AdminBackend` in
[`internal/backend/backend.go`](https://github.com/stowage-dev/stowage/blob/main/internal/backend/backend.go)).

The gating order is:

1. Driver implementation per backend.
2. Hooked into the dashboard's user / policy / key screens.

Until then, manage backend-native identities with the upstream's own
tools (`mc admin`, the Garage CLI, `weed s3 …`).

### Recursive folder move / copy / delete

The current `copy-prefix` and `delete-prefix` handlers fan out per-
key. Server-side enumeration would make recursive operations cheap
on bucket-heavy upstreams, but it requires careful pagination and
error handling. The per-key fan-out works for any size; the deferred
work is the optimisation.

### Active-uploads + active-sessions Prometheus gauges

Currently neither is exposed as a gauge. The data is in-memory but
isn't wired to a `prometheus.GaugeFunc`. Wiring requires routing
some shared state through the metrics service.

### CSV streaming pagination for very large audit exports

The `/admin/audit.csv` handler today buffers in memory before
writing the response. For a few hundred thousand rows this is fine;
for multi-million-row exports it isn't. The fix is server-side
streaming via `io.Pipe` and `csv.NewWriter`. Workaround for very
large exports: query the SQLite directly off a backup snapshot.

## Multi-replica Stowage

Bigger than the items above. Would require:

- Externalising sessions to Redis or similar.
- Externalising the rate limiter.
- Replacing SQLite with Postgres for the multi-writer story.
- Distributed credential and anonymous-binding caches.

Not on the v1.0 roadmap. The
[Single binary](./single-binary.md) page explains the current
single-replica positioning.

## Postgres backend

`db.driver: postgres` is reserved in the config schema and validation
explicitly rejects it. Implementing it requires:

- A second `Store` implementation under
  `internal/store/postgres/`.
- Cross-driver test coverage for the schema migrations.
- Migration tooling for users moving from SQLite to Postgres.

Not on the v1.0 roadmap.

## Outbound signer replacement

The proxy's outbound side uses `aws-sdk-go-v2/v4.Signer`. A
hand-rolled signer that shares the verifier's signing-key cache
would save 3-4 % of allocations and a small amount of CPU. The cost
is mirroring the SDK's bytewise canonicalization for strict S3
paths. Marginal value; on the wishlist if proxy CPU ever becomes a
practical bottleneck.

## Pool the response-stream copy buffer

`io.Copy` allocates a fresh 32 KiB buffer per request. Using a
`sync.Pool` of byte slices and `io.CopyBuffer` would halve
allocations on the read path. Marginal CPU win, cleaner heap
profile. Easy fix; not yet done.

## Audit DB on its own SQLite file

Today's audit table shares the writer mutex with the main DB.
Splitting audit to `audit.db` removes that contention. Win at high
concurrent write volume; transparent at typical bench load. Easy in
principle, modest disruption for the migration.

## See also

- [`README.md` "Known follow-ups"](https://github.com/stowage-dev/stowage/blob/main/README.md)
- [`benchmarks/results-comparison-proxy.md`](https://github.com/stowage-dev/stowage/blob/main/benchmarks/results-comparison-proxy.md)
  — the perf-related deferred items live there with numbers.
