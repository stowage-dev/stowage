---
type: how-to
---

# Run on Kubernetes

The Helm chart at
[`deploy/chart/`](https://github.com/stowage-dev/stowage/tree/main/deploy/chart)
deploys the dashboard, the embedded SigV4 proxy, and the optional
operator wired together. The operator reconciles `S3Backend` and
`BucketClaim` CRDs, brokers virtual credentials via Kubernetes
Secrets, and makes Stowage useful as a multi-tenant platform.

## Pages

- [Overview](./overview.md) — what the chart deploys, where each
  thing lives.
- Install paths:
  - [Multi-tenant install (chart + operator)](./install/multi-tenant.md)
  - [Stowage only, no operator](./install/stowage-only.md)
  - [Operator only, against external Stowage](./install/operator-only.md)
- CRDs:
  - [`S3Backend`](./crds/s3backend.md)
  - [`BucketClaim`](./crds/bucketclaim.md)
- [Virtual credentials in Kubernetes](./virtual-credentials.md)
- [Anonymous read buckets](./anonymous.md)
- [Credential rotation](./rotation.md)
- [Webhook & cert-manager](./webhook.md)
- [Ingress](./ingress.md)
- [NetworkPolicy](./networkpolicy.md)
- [Image pull secrets](./image-pull-secrets.md)
- [Upgrade](./upgrade.md)
- [Operations on Kubernetes](./operations.md)
