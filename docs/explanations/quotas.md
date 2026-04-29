---
type: explanation
---

# How quotas are enforced

Stowage enforces per-bucket storage quotas at the proxy layer,
independent of any quota the upstream provides. The point is to give
operators one place to set the cap and one place to investigate
exhaustion.

## The model

A bucket has two optional caps:

- **Soft** — a warning. Uploads still go through. Audit log records
  a warning.
- **Hard** — a wall. Uploads that would push usage past it are
  rejected with HTTP 507 `EntityTooLarge`.

Both are byte counts. The dashboard accepts them as Quantities (`8Gi`,
`500Mi`); on the wire and in storage they're decimal byte values.

## Enforcement points

The same caps apply at three points:

1. **Dashboard uploads** via `/api/backends/{bid}/buckets/{bucket}/object`.
2. **SDK uploads** via the embedded SigV4 proxy.
3. **Cross-backend transfers** via `/api/backends/{bid}/object/copy`.

There is no path that bypasses the check.

## How the check works

Before forwarding the upload to the upstream:

```
expected = current_used_bytes + content_length
if hard != 0 and expected > hard:
    return 507 EntityTooLarge
```

The `current_used_bytes` value comes from a per-bucket counter
maintained by:

- **Post-commit updates.** After each successful upload, the proxy
  bumps the counter atomically.
- **Scheduled scans.** A goroutine runs every
  `quotas.scan_interval` (default 30m) and re-counts every quota-
  configured bucket end-to-end via `ListObjectsV2`. This corrects
  drift caused by out-of-band writes (e.g. someone hitting the
  upstream directly with `mc`).

## The limit source

The proxy reads quotas from a `LimitSource` interface:

| Source | Where the data lives |
|---|---|
| `SQLite` | Dashboard-set quotas. |
| `Kubernetes` | `BucketClaim.spec.quota.{soft,hard}` copied to the consumer Secret. |
| `Merged` | Both, with Kubernetes winning on conflict. |

In Kubernetes deployments where both exist, **Kubernetes shadows the
dashboard** for the same bucket. The reasoning: the `BucketClaim` is
the declarative source of truth in that environment; manual
dashboard tweaks shouldn't drift the claim's intent.

## Why a scheduled scan and not just incremental counts

Three reasons:

1. **Out-of-band writes.** Anyone with upstream credentials can
   write directly. The counter doesn't see those.
2. **Soft state recovery.** The counter is in-process; a crash + restart
   resets it until the next scan re-establishes ground truth.
3. **Correction.** Multipart uploads that fail to complete leave
   "phantom" parts that count against MinIO's storage but not against
   the counter. Scans catch this.

The scan is bounded by `quotas.scan_interval`. Set it to a negative
duration to disable; admins can still trigger an ad-hoc recompute via
the dashboard.

## What the SDK sees on rejection

```xml
<?xml version="1.0" encoding="UTF-8"?>
<Error>
  <Code>EntityTooLarge</Code>
  <Message>bucket quota exceeded</Message>
  <BucketName>my-bucket</BucketName>
</Error>
```

`507` is the same status AWS S3 returns for storage-class issues.
SDK-side, treat it as "out of space" rather than transient.

## What the dashboard sees on warning

A banner appears at the top of the bucket view: "This bucket has
exceeded its soft quota of 8 GiB." Dismissing the banner is per-
session.

## Limits

- **Per-bucket only.** No per-prefix, per-user, or per-credential
  caps. If you need per-credential quotas, give each credential its
  own bucket.
- **Aggregate bytes only.** No object-count cap.
- **Snapshot-bound.** The post-commit update is best-effort under
  high concurrency; the scan corrects drift but you may see a small
  over-shoot between scans on extremely concurrent write workloads.
- **Scanner walks only quota-configured buckets.** The dashboard's
  storage card excludes untracked buckets. See
  [Roadmap](./roadmap.md).

## Source

- API: [`internal/api/quotas.go`](https://github.com/stowage-dev/stowage/blob/main/internal/api/quotas.go)
- Engine: [`internal/quotas/`](https://github.com/stowage-dev/stowage/tree/main/internal/quotas)
