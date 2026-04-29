---
type: how-to
---

# Quotas

Stowage enforces per-bucket storage quotas at the proxy layer,
independent of any quota the upstream backend provides. Soft caps
warn; hard caps reject uploads with HTTP 507.

## Set a quota

In the dashboard, open the bucket, then **Settings → Quota**. Set:

- **Soft cap** — kilobytes / mebibytes / gibibytes. The proxy still
  allows the upload but emits a warning audit row and surfaces an
  in-browser banner.
- **Hard cap** — uploads that would push usage past this are rejected
  with `507 Insufficient Storage` and audit code `quota_exceeded`.

When both are set, soft must be ≤ hard.

Save runs `PUT /api/backends/{id}/buckets/{bucket}/quota` and emits
`quota.set`. Removing it emits `quota.delete`.

## How enforcement works

1. **Pre-check.** Before forwarding an upload upstream, the proxy
   asks the limit source for the bucket's current usage and checks
   whether `usage + content_length` would exceed the cap.
2. **Post-commit update.** After a successful upload, the proxy
   bumps the usage counter atomically.
3. **Scheduled scan.** A goroutine runs every `quotas.scan_interval`
   (default 30m) and re-counts every quota-configured bucket end-to-
   end via `ListObjectsV2`. This corrects drift caused by out-of-band
   writes (e.g. someone hitting the upstream directly).

The same pre-check applies to dashboard uploads, SDK uploads through
the embedded SigV4 proxy, and cross-backend transfers' destination
side. There is no path that bypasses it.

## Recompute on demand

Click **Recompute** on the quota pane to force a synchronous scan.
The handler is `POST /api/backends/{id}/buckets/{bucket}/quota/recompute`.

## Wire format

```json
{
  "soft_bytes": 8589934592,
  "hard_bytes": 10737418240,
  "used_bytes": 7345921024,
  "object_count": 4123,
  "scanned_at": "2026-04-26T02:06:35Z"
}
```

## Disabling

Click **Remove quota** to delete the quota row. Future uploads are
unconstrained (subject to the upstream's own quotas, if any).

## Limits

- The current implementation tracks aggregate bytes per bucket, not
  per-prefix, per-user, or per-credential.
- The scanner walks only quota-configured buckets, so the dashboard's
  storage card excludes untracked buckets. See
  [Roadmap](../../explanations/roadmap.md).
- A bucket using server-side encryption with separate-of-storage
  semantics may report sizes that drift from billed storage on the
  upstream by a small percentage.

## Source

- API handlers: [`internal/api/quotas.go`](https://github.com/stowage-dev/stowage/blob/main/internal/api/quotas.go)
- Enforcement engine: [`internal/quotas/`](https://github.com/stowage-dev/stowage/tree/main/internal/quotas)
