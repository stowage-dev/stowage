---
type: how-to
---

# Webhook & cert-manager

The chart's admission webhook validates `S3Backend` and `BucketClaim`
mutations against the kubebuilder XValidation rules in the CRD types.
It enforces invariants the JSONSchema in the CRD itself can't
express — for example, "TimeBased rotation requires intervalDays ≥ 7"
and "forceDelete=true requires deletionPolicy=Delete".

## Default: self-signed cert

```yaml
webhook:
  enabled: true
  selfSigned:
    enabled: true
    validityDays: 3650
```

The chart generates a self-signed CA + leaf cert at install time,
embeds the CA bundle in the `ValidatingWebhookConfiguration`, and
mounts the leaf into the operator Pod. The cert is valid for
`validityDays` (default 10 years).

Pros: zero dependencies, works on any cluster.
Cons: rotating the cert means re-installing or re-running the chart's
cert template (which preserves the existing cert via `helm lookup`).

## Alternative: cert-manager

```yaml
webhook:
  enabled: true
  selfSigned:
    enabled: false
  certManager:
    enabled: true
    issuerRef:
      kind: Issuer            # or ClusterIssuer
      name: stowage-ca
```

The chart creates a `Certificate` resource pointing at your
`Issuer` / `ClusterIssuer`. cert-manager handles renewal.

You must already have cert-manager installed in the cluster. The
chart does not install it.

## Disabling

```yaml
webhook:
  enabled: false
```

CRD validation falls back to the OpenAPI schema in the CRD itself,
which catches type / pattern / enum violations but not the cross-field
rules. Enabling the webhook is strongly recommended for production.

## What the webhook validates

`BucketClaim`:

- `quota.soft` ≤ `quota.hard` when both are set.
- `forceDelete: true` requires `deletionPolicy: Delete`.
- `rotationPolicy.mode: TimeBased` requires `intervalDays ≥ 7`.
- `bucketName` is immutable once set.

`S3Backend`:

- `endpoint` matches `^https?://.+`.
- `addressingStyle` is `path` or `virtual`.

These rules are encoded as `+kubebuilder:validation:XValidation` tags
in
[`internal/operator/api/v1alpha1/`](https://github.com/stowage-dev/stowage/tree/main/internal/operator/api/v1alpha1).

## Failure policy

```yaml
webhook:
  failurePolicy: Fail   # or Ignore
```

`Fail` (default) rejects mutations if the webhook is unavailable.
`Ignore` lets them through.

`Fail` is the safer default. `Ignore` is appropriate during cluster
upgrades when you don't want the webhook outage to block other
workloads — but consider whether that's worth letting bad CR shapes
through.

## Custom CA bundle

If you're injecting your own CA bundle (e.g. via an external secret
management system), set `webhook.selfSigned.enabled=false`,
`webhook.certManager.enabled=false`, and supply the bundle directly:

```yaml
webhook:
  enabled: true
  selfSigned:
    enabled: false
  caBundle: |
    -----BEGIN CERTIFICATE-----
    ...
    -----END CERTIFICATE-----
```

The chart wires the bundle into the `ValidatingWebhookConfiguration`.
You're responsible for making sure the corresponding leaf cert is
mounted into the operator Pod.
