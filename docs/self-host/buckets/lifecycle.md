---
type: how-to
---

# Lifecycle rules

S3 lifecycle rules expire objects after a set time, transition them
to colder storage classes, or abort multipart uploads that linger.

## Edit

Open the bucket, then **Settings → Lifecycle**. Each rule has:

- **ID** — string identifier for the rule.
- **Prefix** — only objects with keys starting with this prefix are
  affected.
- **Enabled** — toggle without deleting.
- **Expiration days** — delete the current version after N days.
- **Noncurrent expiration days** — delete non-current versions after
  N days. Useful with versioning.
- **Abort incomplete multipart days** — clean up dangling multipart
  uploads.
- **Transition days / storage class** — move to colder storage.

Save runs `SetBucketLifecycle`. Audit action: `bucket.lifecycle.set`.

## What gets cleaned up by abort-incomplete

Stowage's multipart upload queue retries, but if a client crashes
mid-upload, the upstream retains the partial parts indefinitely until
either:

- The dashboard's "Multipart uploads" view aborts them by hand.
- A lifecycle rule with **abort incomplete multipart days** does it
  automatically.

A 7-day rule is a sensible default for any bucket with regular
multipart traffic.

## Storage class transitions

What's accepted as a storage-class string depends on the backend:

| Backend | Accepted classes |
|---|---|
| AWS S3 | `STANDARD_IA`, `INTELLIGENT_TIERING`, `ONEZONE_IA`, `GLACIER`, `DEEP_ARCHIVE`, `GLACIER_IR` |
| MinIO | Custom labels per `mc admin tier add` |
| Garage / SeaweedFS | Lifecycle support is partial; check upstream docs |

Stowage doesn't pre-validate — invalid combinations surface as the
upstream's own error.

## Backend support

`Capabilities.Lifecycle=true` gates the UI.
