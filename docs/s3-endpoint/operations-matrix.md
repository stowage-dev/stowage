---
type: reference
---

# Supported S3 operations matrix

What an authenticated SDK call to the proxy will and will not do.
Generated from
[`internal/s3proxy/server.go::classifyOperation`](https://github.com/stowage-dev/stowage/blob/main/internal/s3proxy/server.go).

## Forwarded to the upstream

These are recognised, scope-checked, and forwarded:

| Operation | HTTP shape | Notes |
|---|---|---|
| `GetObject` | `GET /<bucket>/<key>` | Range requests supported. |
| `HeadObject` | `HEAD /<bucket>/<key>` | |
| `PutObject` | `PUT /<bucket>/<key>` | Quota pre-check applies. |
| `DeleteObject` | `DELETE /<bucket>/<key>` | |
| `DeleteObjects` | `POST /<bucket>?delete` | Multi-key delete. |
| `CopyObject` | `PUT /<bucket>/<key>` with `x-amz-copy-source` | Same-backend only. |
| `ListObjects` / `ListObjectsV2` | `GET /<bucket>?...` | |
| `HeadBucket` | `HEAD /<bucket>` | |
| `GetBucketLocation` | `GET /<bucket>?location` | Region per backend config. |
| `CreateMultipartUpload` | `POST /<bucket>/<key>?uploads` | |
| `UploadPart` | `PUT /<bucket>/<key>?partNumber=<n>&uploadId=<id>` | |
| `CompleteMultipartUpload` | `POST /<bucket>/<key>?uploadId=<id>` | |
| `AbortMultipart` | `DELETE /<bucket>/<key>?uploadId=<id>` | |
| `ListMultipartUploads` | `GET /<bucket>?uploads` | |
| `ListParts` | `GET /<bucket>/<key>?uploadId=<id>` | |

## Synthesised by the proxy

`ListBuckets` (`GET /`) is built from your credential's bucket scope.
Never reaches the upstream.

## Rejected at the proxy

- **Bucket creation / deletion** (`PutBucket` / `DeleteBucket`) —
  not exposed to tenant credentials. Use the dashboard's bucket-CRUD
  for these.
- **Anything not in the recognised list** — returns
  `400 InvalidRequest`. The proxy tags the operation as `Unknown`
  for metrics.
- **Bucket configuration ops** (versioning, CORS, lifecycle, policy)
  — not exposed to tenant credentials. The dashboard's bucket-
  settings handlers go directly to the upstream via the
  `Backend` interface, not through the proxy.

## Anonymous read fast-path

Anonymous requests (no SigV4) are allowed only for these operations,
and only against buckets with an active anonymous binding:

- `GetObject`
- `HeadObject`
- `ListObjectsV2`

Everything else returns 401.

## Behaviour matrix per status path

| Path | Backend call | Audit row | Quota check |
|---|---|---|---|
| Authenticated successful read | yes | sampled (default no) | no |
| Authenticated successful write | yes | always | yes |
| Authenticated denied (scope) | no | always | n/a |
| Authenticated bad signature | no | always (auth_failure) | n/a |
| Anonymous read (allowlisted) | yes | sampled | no |
| Anonymous read (denied operation) | no | always | n/a |
| Quota exceeded on write | no | always (`507`) | yes |
| Rate-limited (429) | no | always | n/a |

## Performance characteristics

The proxy does not cache object data. Every forwarded request is one
round trip to the upstream. For the canonical numbers under matched
1 CPU / 200 MiB constraints, see
[Explanations → Benchmarks](../explanations/benchmarks.md).

The proxy *does* cache:

- SigV4 derived signing keys per `(akid, date, region, service)`.
- Per-credential decryption results, so a hot credential isn't
  re-decrypted on every request.
- The Kubernetes informer's view of operator-written Secrets.

These caches invalidate on credential mutation events; you don't need
to flush anything.
