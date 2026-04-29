---
type: reference
---

# Reference

Exhaustive specifications. If you're trying to learn Stowage, start
in [Get started](../getting-started/) instead — reference pages are
optimised for lookup, not flow.

## Contents

### Surfaces

- [CLI](./cli/) — every subcommand, every flag.
- [HTTP API](./api.md) — every `/api/*` route.
- [Configuration](./config.md) — every YAML key with type and default.
- [Helm chart values](./helm-values.md) — every value the chart accepts.

### Catalogues

- [Backend capabilities](./backend-capabilities.md)
- [Audit event catalogue](./audit-catalogue.md)
- [Prometheus metrics](./metrics-catalogue.md)
- [Error codes](./error-codes.md)

### Kubernetes-specific

- [`S3Backend` CRD reference](./crds/s3backend.md)
- [`BucketClaim` CRD reference](./crds/bucketclaim.md)
- [Wire-contract Secret data fields](./secret-fields.md)

### S3 proxy

- [Supported S3 operations](./s3-proxy/operations.md)
- [SigV4 signature handling](./s3-proxy/sigv4.md)
- [Proxy error responses](./s3-proxy/errors.md)
