---
type: explanation
---

# The SigV4 proxy

The embedded S3 SigV4 proxy is the "tenant SDK" half of Stowage. The
dashboard is what humans use; the proxy is what `aws-cli` and AWS
SDKs use.

This page explains why it exists and the design decisions behind it.

## What problem it solves

Without the proxy, the only way to give a tenant SDK access to a
bucket on the upstream is to:

- Hand them an upstream IAM key (which means they hold credentials
  with backend-level scope), or
- Generate presigned URLs for every operation (which doesn't work
  for browser SDKs that want long-lived auth, and limits you to the
  presign payload limits).

Both options also bypass the dashboard's audit trail and quota
enforcement. The proxy plugs that gap.

## What the proxy does on every request

1. **Verify the SigV4 signature** against the credential's secret.
2. **Enforce the credential's bucket scope** — a credential scoped
   to `(prod, uploads)` gets 403 if it asks for any other bucket.
3. **Pre-check the destination bucket's quota** for writes.
4. **Re-sign the outbound request** with the upstream's admin
   credentials.
5. **Forward to the upstream** over a pooled keep-alive connection.
6. **Stream the response back** without buffering.
7. **Emit an audit row** (sampled for successful reads, always for
   writes / errors / denies).
8. **Update Prometheus metrics** with operation, status, auth mode.

## What re-signing buys you

Tenants never see the upstream credentials. The credential they
hold is a Stowage-issued virtual credential. Lose it, rotate it.
The upstream admin key stays sealed in `STOWAGE_SECRET_KEY`-encrypted
storage and never crosses a network.

The signing-key cache (per `(akid, date, region, service)` with
secret-fingerprint binding) skips the 4-step HMAC chain on hits, so
the verifier overhead is rounding error compared to the upstream RTT
on forwarded calls. See
[`benchmarks/results-comparison-proxy.md`](https://github.com/stowage-dev/stowage/blob/main/benchmarks/results-comparison-proxy.md).

## Why ListBuckets is synthesised

A tenant should only see the buckets they were granted. If
`ListBuckets` were forwarded, two problems:

- The tenant would see every bucket on the upstream — including
  ones owned by other tenants.
- The proxy would have to filter the response, which means buffering
  the XML and editing it.

Synthesising the response from the credential's scope list avoids
both. The benefit is performance too — the synthesised path runs at
~9k rps because it never touches the upstream.

## Why two SigV4 implementations

The verifier is bespoke (`internal/sigv4verifier/`, stdlib-only).
The signer is `aws-sdk-go-v2/v4.Signer`. Different reasons:

- **Verifier:** the canonicalisation has to be byte-exact with what
  AWS clients produce. Pulling in the SDK's signer for the verifier
  side is overkill — the SDK has lots of code that's irrelevant
  here. The stdlib-only verifier is also faster (one extra HMAC on
  cache hit).
- **Signer:** the SDK's signer handles every weird canonicalisation
  case that strict S3 servers care about. Replacing it would mean
  mirroring the SDK's bytewise behaviour for marginal gain.

There's a [roadmap](./roadmap.md) item to replace the outbound signer
with a hand-rolled one that shares the verifier's signing-key cache.
That's a 3-4 % allocation win, not a fundamental design change.

## Why scope is a list, not a single bucket

`bucket_scopes` (JSON-encoded `[]BucketScope`) lets one credential
span multiple buckets without the operator minting N credentials.
Useful for:

- A workload that writes to a primary bucket and reads from a backup
  bucket.
- Migration: a credential that bridges old and new bucket names
  during a rename.

A single-bucket credential is just a list of one. Older deployments
predate the field; readers fall back to `bucket_name` when
`bucket_scopes` isn't present.

## Why per-credential rate limiting

`s3_proxy.global_rps` and `s3_proxy.per_key_rps` cap traffic. They
serve two distinct purposes:

- **`global_rps`** protects the upstream from a runaway proxy.
- **`per_key_rps`** prevents one noisy tenant from starving others.

Both are leaky-bucket. Neither persists across restarts; the bucket
is in-process. For multi-replica fairness this would need
externalising — see [Single binary](./single-binary.md).

## Why anonymous reads are a separate fast-path

Anonymous reads have no signature to verify, no per-credential cache
to look up, just a bucket-binding lookup and a per-source-IP rate
limit. Putting them on the same path as authenticated requests
would force the verifier to deal with "no auth header" as an
implicit case, which makes the security review harder.

The fast-path is in
[`internal/s3proxy/anonymous.go`](https://github.com/stowage-dev/stowage/blob/main/internal/s3proxy/anonymous.go).
The dispatch decision is one branch on whether `Authorization` is
set.

## Why the cluster-wide kill switch

`s3_proxy.anonymous_enabled: false` cuts off every anonymous read,
even for buckets with active bindings. It's a panic button:

> "We just discovered a bucket was misconfigured; turn off all
> anonymous access until we figure it out."

The per-bucket `mode: None` covers normal operation. The global
switch is for incident response.

## Performance work that's already landed

Three perf passes worth, each driven by pprof under bench load:

| Stage | Fix |
|---|---|
| 1 | Bespoke `http.Transport` (256 idle/host, HTTP/2). Batched audit recorder. |
| 2 | SigV4 derived signing-key cache with secret-fingerprint binding. |
| 3 | `audit.sampling.proxy_success_read_rate` defaults to 0.0. |

See `benchmarks/results-comparison-proxy.md` for the numbers. The
take-away: under matched 1 CPU / 200 MiB constraints, the proxy
adds **+1–3 ms p50 / 0–11 % throughput** vs talking directly to
MinIO, and is *faster* on PutObject.

## Why this isn't an S3 server

The proxy doesn't store object data. It verifies, scopes, quotas,
re-signs, and forwards. This is by design:

- It keeps Stowage stateless about object data.
- It lets the upstream (MinIO, Garage, S3, …) do the storage thing
  it's good at.
- It avoids consistency problems between Stowage's view of objects
  and the upstream's.

If you want a real S3 server, run MinIO or Garage. If you want
single-pane-of-glass governance over them, run Stowage in front.
