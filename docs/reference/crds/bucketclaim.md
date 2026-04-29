---
type: reference
---

# `BucketClaim` CRD reference

API: `broker.stowage.io/v1alpha1`. Namespaced. Short name `bc`.

Source:
[`internal/operator/api/v1alpha1/bucketclaim_types.go`](https://github.com/stowage-dev/stowage/blob/main/internal/operator/api/v1alpha1/bucketclaim_types.go).

## Spec

```yaml
spec:
  backendRef:
    name: string                         # required
  bucketName: string                     # optional, immutable, ^[a-z0-9][a-z0-9.-]{2,62}$
  deletionPolicy: Retain|Delete          # default: Retain
  forceDelete: bool                      # default: false; requires deletionPolicy=Delete
  writeConnectionSecretToRef:
    name: string                         # name of the consumer Secret
  rotationPolicy:
    mode: Manual|TimeBased               # default: Manual
    intervalDays: int                    # default 90, min 1; min 7 when TimeBased
    overlapSeconds: int                  # default 300
  anonymousAccess:
    mode: None|ReadOnly                  # default: None
    perSourceIPRPS: int                  # default 0 (inherit s3_proxy.anonymous_rps)
  quota:
    soft: Quantity                       # optional
    hard: Quantity                       # optional, must be >= soft when both set
```

## Status

```yaml
status:
  phase: Pending|Creating|Bound|Failed|Deleting
  bucketName: string
  proxyEndpoint: string
  boundSecretName: string
  accessKeyId: string
  rotatedAt: metav1.Time
  observedGeneration: int64
  conditions: []metav1.Condition
```

## Conditions

| Type | Reasons |
|---|---|
| `Ready` | `Bound`, `BackendNotReady`, `BucketNotEmpty`, `CreationInconsistent` |
| `BucketCreated` | `CreatedOnBackend`, `BackendError` |
| `CredentialsProvisioned` | `SecretWritten`, `CreationInconsistent` |

## Webhook validations (CEL)

Encoded as `+kubebuilder:validation:XValidation`:

- `quota.soft <= quota.hard` when both set.
- `forceDelete: true` requires `deletionPolicy: Delete`.
- `rotationPolicy.mode: TimeBased` requires `intervalDays >= 7`.
- `bucketName` is immutable once set.

## Finalizer

`broker.stowage.io/bucketclaim-protection` (constant
`Finalizer` in the types file). The reconciler attaches it on first
reconcile and removes it after the deletion policy is satisfied.

## Phase transitions

```
Pending  → Creating → Bound      (steady state)
Pending  → Creating → Failed     (terminal until you fix the spec)
Bound    → Deleting → (deleted)  (after kubectl delete)
```

## Printer columns

```
NAME      BUCKET           PHASE     READY   AGE
uploads   my-app-uploads   Bound     True    3h
```

## Bucket name resolution

`spec.bucketName` empty (default) renders the
`S3Backend.spec.bucketNameTemplate` with:

| Var | Value |
|---|---|
| `.Namespace` | The claim's namespace. |
| `.Name` | The claim's name. |
| `.Hash` | A deterministic 8-character hash of the claim's UID. Useful when names collide on the upstream. |

Default template: `{{ .Namespace }}-{{ .Name }}`.

`spec.bucketName` non-empty overrides the template entirely. Once
set, it can't change — the immutability is enforced by the webhook.

## Quota Quantity values

The `quota.soft` and `quota.hard` fields accept the standard
Kubernetes resource Quantity strings: `8Gi`, `500Mi`, `2T`, etc.

The operator copies the values into the consumer Secret as decimal
byte counts (`quota_soft_bytes`, `quota_hard_bytes`) so the proxy
doesn't link in `apimachinery` just to parse a number.
