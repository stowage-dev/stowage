---
type: explanation
---

# Architecture overview

Stowage is one Go binary plus an optional second binary
(`stowage-operator`). Both compile from the same module and share
internal packages. The dashboard binary speaks HTTP on two listeners;
the operator binary watches Kubernetes CRDs.

## The single load-bearing seam

`internal/backend/backend.go` is the single seam between every UI
feature and the upstream S3 API:

```go
type Backend interface {
    // Identity
    ID() string
    DisplayName() string
    Capabilities() Capabilities

    // Bucket / object / multipart / presign / admin operations.
    ...
}
```

Every dashboard handler that touches storage goes through the
`Backend` interface. Every backend driver lives in a sibling
sub-package (`internal/backend/s3v4/`, `internal/backend/memory/`).
Adding a backend with native admin features (e.g. native MinIO
admin API) means writing a new driver under that interface, not
weaving conditional logic across the dashboard.

The sibling seam — `ProxyTargetProvider` in
`internal/backend/proxy_target.go` — is the only other point of
contact between the dashboard's view of a backend and the embedded
SigV4 proxy. The proxy reaches the upstream via that interface, not
via the `Backend` interface itself, so dashboard probes and proxy
forwards can have different lifetimes and different connection
pools.

## The dashboard process

```
                   ┌─────────────────────────────────────────┐
                   │ stowage  (cmd/stowage)                  │
                   ├─────────────────────────────────────────┤
                   │  HTTP listener  :8080                   │
                   │   ├─ chi router (internal/api)          │
                   │   ├─ embedded SvelteKit (web/dist)      │
                   │   ├─ /metrics (Prometheus)              │
                   │   ├─ /healthz, /readyz                  │
                   │   └─ /s/<code>/* (public shares)        │
                   │                                         │
                   │  HTTP listener  :8090   (optional)      │
                   │   └─ S3 SigV4 proxy (internal/s3proxy)  │
                   │                                         │
                   │  SQLite  (internal/store/sqlite)        │
                   │   ├─ users, sessions, audit, shares,    │
                   │   │   pinned buckets                    │
                   │   ├─ sealed endpoint secrets            │
                   │   ├─ virtual credentials                │
                   │   └─ anonymous bindings                 │
                   │                                         │
                   │  Backend registry  (internal/backend)   │
                   │   ├─ s3v4 driver                        │
                   │   ├─ memory driver (tests)              │
                   │   └─ probe scheduler                    │
                   └─────────────────────────────────────────┘
```

## The operator process

```
                   ┌──────────────────────────────────────────┐
                   │ stowage-operator  (cmd/operator)         │
                   ├──────────────────────────────────────────┤
                   │  controller-runtime manager              │
                   │   ├─ S3Backend reconciler                │
                   │   ├─ BucketClaim reconciler              │
                   │   ├─ admission webhook                   │
                   │   └─ leader election (off by default —   │
                   │       single replica)                    │
                   │                                          │
                   │  internal/operator/credentials/          │
                   │   reads admin Secret, mints VC pairs     │
                   │                                          │
                   │  internal/operator/vcstore/              │
                   │   writes:                                │
                   │   - internal Secret (operator namespace) │
                   │   - consumer Secret (claim namespace)    │
                   │                                          │
                   │  internal/operator/backend/              │
                   │   talks S3 admin API to the upstream:    │
                   │   create / empty / delete bucket         │
                   └──────────────────────────────────────────┘
```

## Wire contract between dashboard and operator

The two binaries don't talk to each other directly. They communicate
through Kubernetes Secrets:

- The operator writes `internal Secret`s in its namespace.
- The dashboard's S3 proxy runs an in-cluster informer over those
  Secrets when `s3_proxy.kubernetes.enabled: true`.
- The data and label fields are documented in
  [Reference → Secret data fields](../reference/secret-fields.md).

This keeps the operator independent of the dashboard's HTTP API and
lets either side run without the other.

## Lifecycle of a tenant request

```
1. Tenant SDK signs a request with virtual creds.
2. Reverse proxy terminates TLS, forwards to stowage:8090.
3. Proxy classifies the operation (router + classifyOperation).
4. Proxy verifies the SigV4 signature against the cache.
5. Proxy looks up the credential's bucket scope.
6. For writes: proxy pre-checks the bucket quota.
7. Proxy rewrites Host / URI to the real upstream bucket.
8. Proxy re-signs with the upstream admin credentials.
9. Proxy forwards via a pooled keep-alive connection.
10. Proxy streams the response back to the client.
11. (For non-sampled audit) recorder writes a row.
```

## Lifecycle of a dashboard request

```
1. User loads the SPA from /.
2. SPA hits /api/me to attach the session.
3. User clicks an action; the SPA POSTs with the CSRF header.
4. chi runs middleware: proxy-trust → security headers → request log →
   session attach → CSRF check → password-rotation gate → rate limit.
5. Handler invokes a Backend method (or the share / audit / quota
   service).
6. Backend driver makes the upstream call, streams bytes back.
7. Audit recorder is invoked for mutations.
```

## Storage abstractions

| Interface | Implementations |
|---|---|
| `backend.Backend` | `s3v4.Driver`, `memory.Driver` |
| `audit.Recorder` | sync SQLite, async batched, noop |
| `s3proxy.Source` | `SQLiteSource`, `KubernetesSource`, `MergedSource` |
| `quotas.LimitSource` | `SQLite`, `Kubernetes`, `Merged` |

Every interface ships at least two implementations. Tests use the
in-memory driver against the same interface the production drivers
satisfy.

## Why one replica

- SQLite has one writer.
- The session rate limiter is in-process.
- The audit recorder is in-process.
- Caches (signing keys, credentials, anonymous bindings) are
  in-process.

Multi-replica Stowage would need to externalise all of those to a
shared store. Today the project trades horizontal scale for the
single-binary, single-process operability story.
