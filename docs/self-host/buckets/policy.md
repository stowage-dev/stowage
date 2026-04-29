---
type: how-to
---

# Bucket policy

A bucket policy is a JSON document the upstream backend interprets to
grant or deny access at the bucket level. Stowage presents it as a
JSON editor.

## Edit

Open the bucket, then **Settings → Policy**. Stowage shows the
current policy text from the upstream and lets you edit it freeform.
On save, Stowage calls `SetBucketPolicy`; on clear, it calls
`DeleteBucketPolicy`.

Audit actions: `bucket.policy.set` and `bucket.policy.delete`.

## What Stowage does and does not do

- **Does** pass the JSON through to the backend untouched.
- **Does** surface upstream errors (e.g. invalid policy syntax) in the
  UI as a 400 `invalid_policy`.
- **Does not** validate the policy semantically. Policy languages vary
  between backends — AWS S3 supports the full `aws:` condition keys,
  Garage supports a subset, MinIO recognises its own extensions.
- **Does not** combine policy with the embedded SigV4 proxy's
  per-credential bucket scope. The two sit at different layers:
  bucket policy is enforced by the upstream; bucket scope is enforced
  before the proxy ever calls the upstream.

## When to use what

| Need | Tool |
|---|---|
| Make a bucket public-read | Bucket policy. |
| Limit a tenant SDK key to specific buckets | Embedded proxy [bucket scope](../../s3-endpoint/auth-and-scope.md). |
| Limit a tenant by remote IP | Bucket policy on AWS / MinIO; Stowage doesn't have its own IP-allowlist UI. |
| Allow only object-level access patterns through Stowage | Don't grant the upstream key directly to tenants — issue a Stowage virtual credential instead. |

## Backend support

`Capabilities.BucketPolicy=true` gates the UI. AWS S3, MinIO, and
Garage all support it; Cloudflare R2 and SeaweedFS support a subset.
Read your upstream's docs before assuming a particular policy clause
will work.
