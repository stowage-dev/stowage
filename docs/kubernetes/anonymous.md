---
type: how-to
---

# Anonymous read buckets

Open a bucket to unauthenticated reads through the proxy, without
exposing the upstream bucket directly.

## Why bother

Anonymous reads through the proxy give you:

- A tight, hard-coded read-only operation allowlist (`GetObject`,
  `HeadObject`, `ListObjectsV2`). The bucket is **not** publicly
  writable.
- Per-source-IP rate limiting, configurable per bucket.
- Audit rows for every anonymous read (subject to the same sampling
  knob as authenticated reads).

The underlying upstream bucket stays private; only the proxy can
reach it.

## Enable from the BucketClaim

```yaml
spec:
  anonymousAccess:
    mode: ReadOnly
    perSourceIPRPS: 10
```

| Field | Default | Notes |
|---|---|---|
| `mode` | `None` | `None` (closed) or `ReadOnly`. |
| `perSourceIPRPS` | 0 | RPS per client IP. 0 means inherit `s3_proxy.anonymous_rps` (default 20). |

## Cluster-wide kill switch

`s3_proxy.anonymous_enabled: false` in the Stowage config disables
the anonymous fast-path entirely, regardless of any
`BucketClaim.spec.anonymousAccess` settings.

The chart exposes this through the `config:` value:

```yaml
config:
  s3_proxy:
    anonymous_enabled: false
```

If you turn this off, every anonymous request gets a 401 — useful
for incident response if a misconfigured bucket is leaking.

## Reach an anonymous bucket

```
GET https://stowage.example.com:8090/<bucket>/<key>
```

No `Authorization` header. The proxy:

1. Looks up the bucket in its anonymous-bindings cache (informer-fed
   in Kubernetes mode).
2. Applies the per-IP rate limit.
3. Forwards a re-signed request to the upstream with the admin
   credentials.

Audit action: `s3.proxy.getobject` (or `s3.proxy.headobject`,
`s3.proxy.listobjectsv2`) with `auth_mode=anonymous` recorded in the
detail JSON.

## Direct dashboard management

Outside Kubernetes, the same surface is exposed in the dashboard at
`/admin/s3-proxy` → **Anonymous bindings**. Add a binding by picking
a `(backend, bucket)` pair and the per-IP RPS cap.

## What's NOT supported

- **Anonymous writes.** Not implementable safely without a quota
  budget per anonymous client, which today doesn't exist.
- **Anonymous SigV4-anonymous-auth (zero-length sig).** The proxy
  ignores the auth header entirely on anonymous routes.
- **Per-key access control.** Anonymous bindings are per-bucket, not
  per-prefix or per-key. Use a separate bucket if you need finer
  granularity.

## Source

- Anonymous handler: [`internal/s3proxy/anonymous.go`](https://github.com/stowage-dev/stowage/blob/main/internal/s3proxy/anonymous.go)
- Per-IP limiter: [`internal/s3proxy/iplimiter.go`](https://github.com/stowage-dev/stowage/blob/main/internal/s3proxy/iplimiter.go)
- BucketClaim spec: [`internal/operator/api/v1alpha1/bucketclaim_types.go`](https://github.com/stowage-dev/stowage/blob/main/internal/operator/api/v1alpha1/bucketclaim_types.go)
