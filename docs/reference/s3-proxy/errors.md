---
type: reference
---

# Proxy error responses

Unlike the dashboard API (JSON), the proxy returns AWS-shaped XML so
SDKs treat it as a normal S3 error.

## Wire shape

```xml
<?xml version="1.0" encoding="UTF-8"?>
<Error>
  <Code>AccessDenied</Code>
  <Message>credential not authorised for bucket</Message>
  <BucketName>my-bucket</BucketName>
  <Resource>/my-bucket/path/to/object</Resource>
  <RequestId>17b1c8a3</RequestId>
</Error>
```

Compatible with every AWS SDK error parser.

## Common codes the proxy emits

| HTTP | Code | Cause |
|---|---|---|
| 400 | `InvalidRequest` | Operation classification failed; request shape unrecognised. |
| 400 | `MalformedXML` | Body parse failure on `DeleteObjects` / `CompleteMultipartUpload`. |
| 401 | `InvalidAccessKeyId` | The access key is unknown. |
| 401 | `SignatureDoesNotMatch` | The signature didn't verify. Check secret + clock skew. |
| 403 | `AccessDenied` | Scope violation, anonymous-disallowed operation, expired credential. |
| 403 | `RequestTimeTooSkewed` | Date in the request is outside the verifier's tolerance. |
| 404 | `NoSuchBucket` | The bucket isn't known to the upstream. |
| 404 | `NoSuchKey` | Object doesn't exist (passed through from upstream). |
| 411 | `MissingContentLength` | A streaming PUT didn't carry a length header. |
| 429 | `SlowDown` | Per-credential, global, or per-source-IP rate limit. `Retry-After` header. |
| 500 | `InternalError` | Unrecoverable proxy or upstream failure. |
| 502 | `BadGateway` | Upstream returned an unparseable response. |
| 503 | `ServiceUnavailable` | Upstream temporarily unavailable; transient. |
| 507 | `EntityTooLarge` | Bucket hard-cap exceeded. **Do not retry blindly.** See [Quota errors](../../s3-endpoint/quota-errors.md). |

## Auth-failure reasons (in metrics)

`stowage_s3_auth_failure_total` is labeled by `reason`. Non-exhaustive:

| `reason` | Meaning |
|---|---|
| `unknown_access_key` | The access key isn't in the cache. |
| `bad_signature` | StringToSign didn't match. |
| `disabled` | Credential is in the cache but flagged disabled. |
| `expired` | Credential carries an `ExpiresAt` and it's past. |
| `bad_authorization_format` | The header didn't parse as SigV4. |
| `bad_credential_field` | The Credential= field couldn't be split. |
| `bad_date` | The `X-Amz-Date` is malformed. |
| `time_skew` | Date is outside the allowed skew window. |

These map onto a 401 `InvalidAccessKeyId` or `SignatureDoesNotMatch`
response depending on the cause.

## Scope-violation result

`stowage_s3_scope_violation_total` increments any time an
authenticated request is rejected because the bucket is not in the
credential's scope. The wire response is 403 `AccessDenied`.

## Anonymous-reject reasons

`stowage_s3_anonymous_reject_total{reason}` is labeled with:

| `reason` | Meaning |
|---|---|
| `disabled_globally` | `s3_proxy.anonymous_enabled: false`. |
| `no_binding` | The bucket has no anonymous binding. |
| `operation_not_allowed` | Operation is outside the read-only allowlist. |
| `rate_limited` | Per-IP RPS exceeded. |
