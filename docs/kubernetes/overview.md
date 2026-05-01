---
type: how-to
---

# Kubernetes overview

The Stowage Helm chart deploys a single Pod that runs the dashboard,
the embedded SigV4 proxy, and the operator manager together. Anything
else in the cluster is consumer-only — your application Pods consume
the Secrets the operator writes, but they don't speak directly to
the stowage process.

## Components

| Resource | Purpose |
|---|---|
| Stowage Deployment (1 replica) | Dashboard, embedded SigV4 proxy, and the operator manager (S3Backend / BucketClaim reconcilers + admission webhook), all in one container. |
| Stowage Service | ClusterIP. Ports 80 → http, 8090 → s3 proxy, 443 → webhook (when enabled). |
| Stowage Ingress | Optional. |
| Stowage PVC | RWO. Holds the SQLite database and the AES-256 root key. |
| Validating webhook | Validates `S3Backend` / `BucketClaim` writes. Targets the stowage Service. |
| `S3Backend` CRD | Cluster-scoped. |
| `BucketClaim` CRD | Namespaced. |

## Single-replica deployment is the only supported topology

Stowage is backed by SQLite on an RWO volume and an in-process
rate-limiter / audit batcher / quota counter. Running a second pod
would race on the database, double-execute reconciles, and split
the rate-limit + audit state. The chart hardcodes `replicas: 1`
with `strategy: Recreate` and the operator manager runs without
leader election. **Multi-replica is not supported and is not on
the roadmap** — if you need HA, deploy a second stowage cluster
in a separate namespace or cluster and point at a different
upstream.

The merged-pod deployment also means dashboard and operator share
a fate: a pod restart drops admission availability for the
restart window. The chart's webhook `failurePolicy` defaults to
`Fail` (writes to CRs are blocked while the pod is down); flip it
to `Ignore` in `values.yaml` if you'd rather accept unvalidated
writes during restarts.

## Data flow at install time

```
helm install
   ├─ creates the namespace
   ├─ generates the AES-256 root key (helm lookup preserves on upgrade)
   ├─ deploys Stowage (PVC + Service + Deployment)
   ├─ installs the cluster + namespaced RBAC for the operator manager
   ├─ installs the admission webhook (cert + ValidatingWebhookConfiguration)
   └─ installs the CRDs
```

## Data flow at runtime

```
developer ─ kubectl apply ──▶ S3Backend / BucketClaim
                                     │
                                     ▼
                       ┌──────────────────────────────────┐
                       │ stowage Pod                      │
                       │  ├─ operator manager             │
                       │  │   • verifies upstream         │
                       │  │   • creates the bucket        │
                       │  │   • mints virtual creds       │
                       │  │   • writes Secret             │
                       │  ├─ embedded S3 SigV4 proxy      │
                       │  └─ admin dashboard              │
                       └──────────────────────────────────┘
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
                                     ▼
              consumer Pod (mounts Secret as env)
                                     │
                                     ▼
                            stowage Service :8090
                                     │
                                     ▼
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
