---
type: reference
---

# Wire-contract Secret data fields

The single source of truth for what the operator writes and what the
proxy reads on Kubernetes Secrets. The contract is split across two
files; both must agree.

- [`internal/operator/vcstore/labels.go`](https://github.com/stowage-dev/stowage/blob/main/internal/operator/vcstore/labels.go)
  — what the operator writes.
- [`internal/s3proxy/source_kubernetes.go`](https://github.com/stowage-dev/stowage/blob/main/internal/s3proxy/source_kubernetes.go)
  — what the proxy reads.

## Labels (selectable on every Secret)

| Label | Notes |
|---|---|
| `broker.stowage.io/role` | `virtual-credential` (operator namespace) \| `consumer-secret` (claim namespace) \| `anonymous-binding`. |
| `broker.stowage.io/claim-namespace` | The namespace of the owning `BucketClaim`. |
| `broker.stowage.io/claim-name` | The name of the owning `BucketClaim`. |
| `broker.stowage.io/claim-uid` | The UID of the owning `BucketClaim`. |
| `broker.stowage.io/access-key-id` | Access key ID (also a data field). |
| `broker.stowage.io/backend` | Name of the `S3Backend`. |
| `broker.stowage.io/bucket` | Name of the bucket. |
| `broker.stowage.io/rotation-generation` | Increments on each rotation. |

## Annotations

| Annotation | Notes |
|---|---|
| `broker.stowage.io/expires-at` | Optional credential expiry in RFC3339. |

## Internal Secret data (in operator namespace)

The proxy informer reads these. Sealed at rest by the API server's
encryption-at-rest if you have it configured; not double-sealed by
Stowage's AES key on this side (it's a per-cluster wire-contract).

| Key | Required | Notes |
|---|---|---|
| `access_key_id` | yes | The credential's public ID. |
| `secret_access_key` | yes | Plaintext (within the Secret). Treat the Secret as sensitive. |
| `bucket_name` | yes | Primary bucket for legacy single-scope readers. |
| `backend` | yes | `S3Backend` name. |
| `claim_uid` | yes | Owning claim's UID. |
| `bucket_scopes` | optional | JSON-encoded `[]BucketScope`. **When present, readers prefer this** over the singular fields. Authoritative scope list. |
| `quota_soft_bytes` | optional | Decimal byte count. |
| `quota_hard_bytes` | optional | Decimal byte count. |
| `anonymous_mode` | for `role=anonymous-binding` | `None` \| `ReadOnly`. |
| `anonymous_per_source_ip_rps` | optional | Per-binding override of `s3_proxy.anonymous_rps`. |

## Consumer Secret data (in claim namespace)

What tenant Pods consume. AWS-shaped env-var names so the standard
SDK credential providers pick them up automatically.

| Key | Notes |
|---|---|
| `AWS_ACCESS_KEY_ID` | Same value as `access_key_id` on the internal side. |
| `AWS_SECRET_ACCESS_KEY` | Same value as `secret_access_key`. |
| `AWS_REGION` | Region per the `S3Backend`. |
| `AWS_ENDPOINT_URL` | Stowage proxy URL. |
| `AWS_ENDPOINT_URL_S3` | Same value, separated for SDKs that prefer the S3-specific name. |
| `BUCKET_NAME` | The real bucket name. |
| `S3_ADDRESSING_STYLE` | `path` \| `virtual`. |

## Why both sides match

The operator and the proxy compile to separate binaries from the same
Go module. Changing a field name on one side without the other
silently breaks the integration — the informer keeps cycling, no
errors logged at INFO. Both sides use the constants from
`vcstore/labels.go` to avoid stringly-typed drift.

If you fork the operator or the proxy, keep these constants in lock-
step.
