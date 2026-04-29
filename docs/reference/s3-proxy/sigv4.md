---
type: reference
---

# SigV4 signature handling

The proxy verifies inbound AWS-SigV4 signatures and re-signs the
outbound request with the upstream's admin credentials. Stdlib-only
verifier; no external SDK dependency.

Source:
[`internal/sigv4verifier/`](https://github.com/stowage-dev/stowage/tree/main/internal/sigv4verifier).

## Accepted signature shapes

| Shape | How |
|---|---|
| Header-signed | `Authorization: AWS4-HMAC-SHA256 Credential=…, SignedHeaders=…, Signature=…` |
| Query-signed (presigned URL) | All signature fields in query parameters: `X-Amz-Algorithm`, `X-Amz-Credential`, `X-Amz-Date`, `X-Amz-Expires`, `X-Amz-SignedHeaders`, `X-Amz-Signature`. |
| Chunked uploads | `Authorization` + `Transfer-Encoding: aws-chunked` + `X-Amz-Content-Sha256: STREAMING-AWS4-HMAC-SHA256-PAYLOAD`. |
| Unsigned payload | `X-Amz-Content-Sha256: UNSIGNED-PAYLOAD`. |

## Verification steps

1. Parse the `Credential=` field to extract `(access_key_id,
   date, region, service, terminator="aws4_request")`.
2. Look up `access_key_id` in the merged source (SQLite + Kubernetes
   informer).
3. Derive the signing key:
   `kSecret = "AWS4"+secretAccessKey` →
   `kDate = HMAC(kSecret, date)` →
   `kRegion = HMAC(kDate, region)` →
   `kService = HMAC(kRegion, service)` →
   `kSigning = HMAC(kService, "aws4_request")`.
4. Build the canonical request and the StringToSign.
5. Compute `HMAC(kSigning, StringToSign)`.
6. Compare in constant time against the inbound signature.

## Signing-key cache

The proxy caches `kSigning` per
`(access_key_id, date, region, service)` keyed-by-secret-fingerprint.
Cache hits skip the four-step HMAC chain and only do the final
`HMAC(kSigning, StringToSign)`.

The cache is bounded by date — old entries fall out as the date
parameter rolls over each day.

The fingerprint binding ensures that if a credential is rotated,
cached signing keys for the old secret can't be used. There's no
explicit cache invalidation path for routine rotation; the new
secret produces a new fingerprint and a new cache entry.

## Outbound re-signing

After authorization, the proxy:

1. Looks up the upstream admin credentials for the resolved
   `S3Backend`.
2. Rewrites the request's `Host`, `URI`, and any bucket-name path
   segments to match the real upstream bucket.
3. Re-signs with `aws-sdk-go-v2/v4.Signer`. The SDK signer has its
   own derived-key cache, so the outbound HMAC chain isn't a hot
   loop.

## Performance characteristics

From the [benchmarks](../../explanations/benchmarks.md):

- Bad-signature reject path: ~10k rps single core (the proxy never
  reaches the upstream).
- Authenticated `GetObject` 1 KiB: ~875 rps with ~7.9 ms p50.
- The verifier is rounding error compared to the upstream's response
  time on forwarded requests.

## Anonymous requests

Requests that arrive without any `Authorization` header skip the
verifier entirely. The anonymous fast-path looks up the bucket's
binding directly. See
[Anonymous endpoints](../../s3-endpoint/anonymous.md).
