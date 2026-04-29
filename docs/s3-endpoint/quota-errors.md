---
type: how-to
---

# Quota errors and retry semantics

## How the cap is enforced

Stowage's proxy enforces a per-bucket hard cap before forwarding the
upload. The check is:

```
expected_post_upload_bytes = current_used_bytes + content_length
if expected_post_upload_bytes > hard_cap:
    return 507 EntityTooLarge
```

The check is best-effort but tight: it runs on every `PutObject`
and the first part of every multipart upload. Multi-part uploads
that exceed the cap mid-flight are rejected on the next part with
the same status.

## What the SDK sees

A `PutObject` over the cap fails with:

```
HTTP/1.1 507 Insufficient Storage
Content-Type: application/xml

<?xml version="1.0" encoding="UTF-8"?>
<Error>
  <Code>EntityTooLarge</Code>
  <Message>bucket quota exceeded</Message>
  <BucketName>my-bucket</BucketName>
</Error>
```

`507` is the same status AWS S3 returns for storage-class issues, so
SDKs that treat S3 errors generically usually surface it as a clear
"out of space" rather than a transient failure.

## When to retry

**Don't retry blindly.** A 507 means the bucket is over its hard cap.
Retrying immediately will fail again.

The right responses are:

1. **Surface to the user.** "This bucket is full." Maybe the human
   needs to delete things.
2. **Switch buckets.** If the workload writes to multiple buckets,
   stop writing to the full one.
3. **Page the operator.** If the cap is administrative, the cap may
   need to be raised.

If the cap was hit in error (e.g. the scanner over-counted), an admin
can hit **Recompute** in the bucket's quota pane to re-scan the bucket
end-to-end.

## Soft cap warnings

If the upload would push the bucket past the **soft cap** but stay
under the **hard cap**, the proxy still allows the upload but emits a
warning audit row. There's no headers-level signal; the SDK sees a
normal 200.

If you want soft-cap visibility client-side, query the bucket's
quota status via the dashboard API (`GET /api/backends/{id}/buckets/{bucket}/quota`)
on a periodic basis.

## Rate-limit retry semantics

`429 SlowDown` returns include a `Retry-After` header in seconds.
Most AWS SDKs respect it natively. If you've written your own
HTTP client, honour the header rather than retrying immediately.

## Other failures the SDK might see

| Status | Meaning | Retry? |
|---|---|---|
| `400 BadRequest` | Malformed request, bad SigV4, bad bucket name | No — fix the request. |
| `401 Unauthorized` | Credential doesn't verify | No — get a new credential. |
| `403 Forbidden` (`AccessDenied`) | Bucket not in your credential's scope | No — request scope from your admin. |
| `404 NotFound` (`NoSuchBucket`/`NoSuchKey`) | The thing isn't there | No. |
| `429 SlowDown` | RPS limit | Yes, after `Retry-After`. |
| `5xx` | Upstream or proxy error | Yes, with exponential backoff. |
| `507 EntityTooLarge` | Bucket hard quota | No — see above. |

## Quota usage from the API

For tenant-side dashboards or admin tooling, the dashboard's
authenticated `/api/backends/{id}/buckets/{bucket}/quota` endpoint
returns the current usage, soft cap, hard cap, and last-scan
timestamp:

```json
{
  "soft_bytes": 8589934592,
  "hard_bytes": 10737418240,
  "used_bytes": 7345921024,
  "object_count": 4123,
  "scanned_at": "2026-04-26T02:06:35Z"
}
```

This requires a Stowage user session, not a virtual S3 credential.
Tenants without a Stowage user account can't query it; ask your admin
for periodic exports if you need visibility.
