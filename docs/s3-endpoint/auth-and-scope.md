---
type: how-to
---

# Authentication and bucket scope

The proxy verifies AWS-SigV4 signatures, then enforces a per-credential
bucket scope before forwarding to the upstream.

## SigV4

The proxy accepts:

- **Header-signed** requests (`Authorization: AWS4-HMAC-SHA256 ...`).
- **Query-signed** (presigned) URLs.
- **Chunked uploads** (`Transfer-Encoding: aws-chunked`).
- **Unsigned-payload** uploads with `X-Amz-Content-Sha256:
  UNSIGNED-PAYLOAD` (rare, sometimes used by SDKs over TLS for
  performance).

Verification uses
[`internal/sigv4verifier/`](https://github.com/stowage-dev/stowage/tree/main/internal/sigv4verifier)
— stdlib-only, with a derived-signing-key cache so the HMAC chain
isn't re-computed for every request from the same key/date pair.

## Bucket scope

Each virtual credential carries one or more **bucket scopes**. A
scope is a `(backend, bucket)` pair. The proxy checks the requested
bucket against the credential's scope list before forwarding:

- **Match found** → request is forwarded to the upstream.
- **No match** → 403 `forbidden`. Audit row records `scope_violation`.

The scope is stored alongside the credential, sealed under
`STOWAGE_SECRET_KEY`. From the tenant's perspective it's set at
credential creation time and isn't modifiable client-side.

## ListBuckets

`ListBuckets` is a service-level call (no bucket in the URL). The
proxy synthesises the response from the credential's scope list,
returning each scoped bucket as if it were a top-level bucket the
upstream owned.

Effects:

- Tenants only see buckets they're authorised for.
- The upstream is never called for `ListBuckets`.
- The synthesised response is faster than a real `ListBuckets` (the
  bench shows ~9k rps for the synthesised path versus ~2k for raw
  MinIO).

## ListBuckets and multi-bucket credentials

A credential with two scopes — say `(prod, uploads)` and
`(prod, downloads)` — gets a synthesised `ListBuckets` showing both,
even though they live on the same upstream bucket. The proxy's view
of "buckets" is "scopes", not "real buckets".

## Failure modes

| Status | Code | Meaning |
|---|---|---|
| 401 `Unauthorized` | (S3-style) | SigV4 signature didn't verify, access key unknown, or credential disabled. |
| 403 `Forbidden` | `AccessDenied` | The credential is valid but doesn't have the bucket in scope. |
| 507 `EntityTooLarge` | `EntityTooLarge` | Bucket quota exceeded. See [Quota errors](./quota-errors.md). |
| 429 `Too Many Requests` | `SlowDown` | Per-credential or global RPS limit hit. |

Authenticated requests that pass scope and quota checks then forward
to the upstream; whatever the upstream returns is forwarded back to
the client.

## Per-credential rate limiting

Two knobs on the proxy side:

- `s3_proxy.global_rps` — total RPS ceiling across every credential.
  0 = unlimited.
- `s3_proxy.per_key_rps` — per-credential RPS ceiling.

Both are applied additively. Hitting either returns 429 with a
`Retry-After` header.

## Per-source-IP rate limiting (anonymous only)

Anonymous bindings (see [Anonymous endpoints](./anonymous.md)) carry
their own per-source-IP RPS cap, configured per binding.

## Credential expiry

A credential can carry an expiry. After expiry the proxy returns
401 `expired`. Issue a new credential to continue.

## Audit trail

Every request emits an `s3.proxy.<operation>` audit row (subject to
sampling). Operators can reconstruct who-did-what by access key ID
across the entire SDK surface.

## Best practices

- **Don't share credentials between unrelated workloads.** Mint one
  per workload, scope it tightly.
- **Rotate.** Use `BucketClaim.spec.rotationPolicy` on Kubernetes or
  the dashboard's regenerate button outside.
- **Don't put credentials in source control.** Use Kubernetes
  Secrets, Vault, etc.
- **Don't log the access key in app logs at INFO.** It's the user-
  facing identifier; treat it like an AWS access key.
