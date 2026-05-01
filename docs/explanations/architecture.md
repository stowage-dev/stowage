---
type: explanation
---

# Architecture overview

Stowage is one Go binary. It serves the dashboard and embedded S3
proxy via `stowage serve`, and — when `operator.enabled` is set in
config — also runs the Kubernetes operator manager (S3Backend /
BucketClaim reconcilers + admission webhook) inside the same Pod.
A `stowage operator` subcommand exists for headless deployments
that want only the K8s control-plane work without the dashboard.

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

## The stowage process

```
              ┌─────────────────────────────────────────────┐
              │ stowage  (cmd/stowage)                      │
              ├─────────────────────────────────────────────┤
              │  HTTP listener  :8080                       │
              │   ├─ chi router (internal/api)              │
              │   ├─ embedded SvelteKit (web/dist)          │
              │   ├─ /metrics (Prometheus)                  │
              │   ├─ /healthz, /readyz                      │
              │   └─ /s/<code>/* (public shares)            │
              │                                             │
              │  HTTP listener  :8090   (optional)          │
              │   └─ S3 SigV4 proxy (internal/s3proxy)      │
              │                                             │
              │  HTTPS listener :9443   (when operator on)  │
              │   └─ admission webhook                      │
              │                                             │
              │  Operator manager (when operator on)        │
              │   ├─ S3Backend reconciler   (status)        │
              │   ├─ S3Backend reconciler   (registry sync) │
              │   ├─ BucketClaim reconciler                 │
              │   ├─ vcstore writer (Secrets)               │
              │   └─ admin client (bucket lifecycle)        │
              │                                             │
              │  SQLite  (internal/store/sqlite)            │
              │   ├─ users, sessions, audit, shares,        │
              │   │   pinned buckets                        │
              │   ├─ sealed endpoint secrets                │
              │   ├─ virtual credentials                    │
              │   └─ anonymous bindings                     │
              │                                             │
              │  Backend registry  (internal/backend)       │
              │   ├─ s3v4 driver                            │
              │   ├─ memory driver (tests)                  │
              │   ├─ probe scheduler                        │
              │   └─ sources: config | db | k8s             │
              └─────────────────────────────────────────────┘
```

## How K8s state reaches the dashboard

Three integration points share the same in-cluster client:

1. **Backend registry** — when `operator.enabled`, the registry
   reconciler watches `S3Backend` CRs and registers them as
   read-only entries (`Source: k8s`). They appear in the admin UI
   alongside config.yaml and DB-managed entries; PATCH/DELETE
   refuse with `k8s_managed`.
2. **Virtual-credential cache** — the embedded S3 proxy's
   `KubernetesSource` informer watches operator-written tenant
   Secrets so the proxy can verify SigV4 signatures and resolve
   bucket scopes without a SQLite hit.
3. **Quota cache** — the same informer feeds quota updates from
   `BucketClaim.spec.quota` into the merged limit cache, shadowing
   dashboard-managed quotas for the same bucket.

The wire contract Secret data/label fields are documented in
[Reference → Secret data fields](../reference/secret-fields.md).

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
