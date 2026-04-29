---
type: how-to
---

# Kubernetes overview

The Stowage Helm chart deploys two components that are coupled by a
contract on Kubernetes Secret data fields. Anything else in the
cluster is consumer-only — your application Pods consume the Secrets
the operator writes, but they don't speak directly to either Stowage
process.

## Components

| Resource | Purpose | Replicas |
|---|---|---|
| Stowage Deployment | The dashboard process and the embedded SigV4 proxy. | 1 |
| Operator Deployment | Reconciles `S3Backend` and `BucketClaim`. | 1 |
| Admission webhook | Validates CRD changes. | 0–1 (depends on `webhook.enabled`) |
| Stowage Service | ClusterIP, ports 8080 + 8090 | — |
| Stowage Ingress | optional | — |
| Stowage PVC | RWO, holds the SQLite database and the AES-256 root key | — |
| `S3Backend` CRD | Cluster-scoped. | — |
| `BucketClaim` CRD | Namespaced. | — |

Stowage runs single-replica because of SQLite + the in-process
limiter. The chart sets `replicas: 1` and uses an RWO PVC. Multi-
replica Stowage is on the roadmap; today it is not supported.

## Data flow at install time

```
helm install
   ├─ creates the namespace
   ├─ generates the AES-256 root key (helm lookup preserves on upgrade)
   ├─ deploys Stowage (PVC + Service + Deployment)
   ├─ deploys the operator (Deployment + RBAC)
   ├─ installs the admission webhook (Service + cert)
   └─ installs the CRDs
```

## Data flow at runtime

```
developer ─ kubectl apply ──▶ S3Backend / BucketClaim
                                     │
                                     ▼
                       ┌──────────────────────────┐
                       │ stowage-operator         │
                       │  • verifies upstream     │
                       │  • creates the bucket    │
                       │  • mints virtual creds   │
                       │  • writes Secret         │
                       └──────────────────────────┘
                                     │
                                     ▼
                       ┌──────────────────────────┐
                       │ Kubernetes Secret        │
                       │  (consumer namespace)    │
                       │  data:                   │
                       │   AWS_ACCESS_KEY_ID      │
                       │   AWS_SECRET_ACCESS_KEY  │
                       │   AWS_REGION             │
                       │   AWS_ENDPOINT_URL       │
                       │   AWS_ENDPOINT_URL_S3    │
                       │   BUCKET_NAME            │
                       │   S3_ADDRESSING_STYLE    │
                       └──────────────────────────┘
                                     │
                       ┌─────────────┘─────────────┐
                       ▼                           ▼
              consumer Pod (mounts          stowage proxy
              Secret as env)                informer (reads
                                            internal Secrets)
                       │                           │
                       └──────── SigV4 ────────────▶
                                                 upstream S3
```

## Wire-contract Secret data fields

Both sides agree on the keys. They live in
[`internal/operator/vcstore/labels.go`](https://github.com/stowage-dev/stowage/blob/main/internal/operator/vcstore/labels.go)
and
[`internal/s3proxy/source_kubernetes.go`](https://github.com/stowage-dev/stowage/blob/main/internal/s3proxy/source_kubernetes.go).
See [Reference → Secret data fields](../reference/secret-fields.md)
for the exhaustive list with field semantics.

## Multi-tenancy model

- One `S3Backend` per upstream cluster (cluster-scoped). Admin keys
  for the upstream live in a Secret in the operator namespace.
- One `BucketClaim` per logical bucket (namespaced). The claim's
  namespace becomes the tenancy boundary — the consumer Secret is
  written in the same namespace.
- Tenants point AWS SDKs at the `stowage` Service on port 8090.
  Their credentials are the operator-minted virtual ones. They never
  see the upstream admin keys.

## Source-of-truth conflicts

- **YAML-defined backends and `S3Backend` CRDs are independent.** YAML
  backends live inside the Stowage config. CRDs live in the cluster.
  Pick one model per upstream — running both for the same upstream
  works but doubles the bookkeeping.
- **Kubernetes-managed virtual credentials and SQLite-managed virtual
  credentials are merged.** Kubernetes wins on access-key collision.
  The merged view is at `/admin/s3-proxy` in the dashboard.
- **`BucketClaim.spec.quota` shadows dashboard-managed quotas.** The
  in-cluster informer reads the claim's quota and feeds it into the
  proxy's limit cache, taking precedence over a dashboard-set quota
  for the same bucket.
