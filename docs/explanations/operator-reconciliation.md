---
type: explanation
---

# The operator's reconciliation model

The Stowage operator is a controller-runtime-based reconciler with
two controllers: one for `S3Backend`, one for `BucketClaim`. This
page walks the lifecycle of each.

## `S3Backend` reconciliation

The `S3Backend` controller is mostly a credentials and connectivity
checker. It doesn't create or destroy resources beyond updating the
status subresource.

### On create

1. Read the admin credentials from
   `spec.adminCredentialsSecretRef.name` in the named namespace.
2. Render the `bucketNameTemplate` with dummy variables to validate
   it parses.
3. Probe the endpoint: a `ListBuckets` (or equivalent service-level
   call) with the admin credentials.
4. Update the status:
   - `Ready=True, Reason=EndpointReachable` on success.
   - `Ready=False` with a specific reason
     (`EndpointUnreachable`, `CredentialsInvalid`,
     `TemplateInvalid`) on failure.

### On update

The reconciler picks up changes via the standard
controller-runtime watch loop. Edits to `spec.endpoint`,
`spec.adminCredentialsSecretRef`, or `spec.bucketNameTemplate`
trigger a re-probe.

### On delete

Cluster-scoped, no finalizer. Kubernetes deletes the object and the
status disappears. `BucketClaim`s that referenced it now point at a
non-existent backend; their reconciliation will set their status
condition to `BackendNotReady`.

## `BucketClaim` reconciliation

This is where the real work lives.

### Phases

```
Pending → Creating → Bound      (steady state)
Pending → Creating → Failed     (terminal until you fix the spec)
Bound   → Deleting → (deleted)  (after kubectl delete)
```

### On create

1. **Attach finalizer**
   `broker.stowage.io/bucketclaim-protection`. Subsequent deletes
   become "marked for deletion" until the finalizer is removed.
2. **Resolve the `S3Backend`.** If not found or not `Ready=True`,
   set status `Phase=Pending, BackendNotReady` and requeue.
3. **Compute the bucket name.** From `spec.bucketName` if set,
   otherwise the template.
4. **Create the bucket on the upstream** via the
   `internal/operator/backend/` shim (this is a separate package
   from the dashboard's `internal/backend/` because it talks the
   admin API for creation, not the user-facing operations).
5. **Mint a virtual credential.** Generate `(access_key_id,
   secret_access_key)`, persist into the *internal* Secret in the
   operator namespace.
6. **Write the *consumer* Secret** in the claim's namespace, with
   the AWS_* env vars and the connection metadata.
7. **Apply the quota** by writing `quota_soft_bytes` /
   `quota_hard_bytes` into the internal Secret. The proxy's
   informer picks this up.
8. **Update status:** `Phase=Bound`, `Ready=True`,
   `BoundSecretName=...`, `AccessKeyId=...`.

### On update

- **Name change** → rejected by the webhook (immutable field).
- **Backend change** → currently triggers a re-resolve of the
  backend; the bucket itself is not migrated. Don't repoint a claim
  at a different backend casually.
- **Anonymous mode change** → updates the corresponding Secret's
  `anonymous_mode` data field. The proxy informer picks it up.
- **Quota change** → updates the consumer Secret's
  `quota_soft_bytes` / `quota_hard_bytes`. Effective on the next
  proxy request after the informer notices.
- **Rotation policy change** → the next reconciliation either
  schedules a rotation (TimeBased mode) or doesn't (Manual).

### On delete

Reconciler runs the `deletionPolicy`:

- **`Retain`** (default) — leave the bucket on the upstream alone.
  Just delete the Secrets and remove the finalizer.
- **`Delete`** — empty the bucket on the upstream, delete it,
  delete the Secrets, remove the finalizer.

If `forceDelete: false` and the bucket isn't empty, the reconciler
sets status `Reason=BucketNotEmpty` and refuses to proceed. Edit the
claim with `forceDelete: true` to override.

### Rotation flow

When a rotation triggers (manual annotation or scheduled):

1. Mint a new `(access_key_id, secret_access_key)` pair.
2. Update the internal Secret with the new credential.
3. Update the consumer Secret with the new `AWS_*` env vars.
4. Mark the old credential as "in overlap" — the proxy still
   accepts it.
5. Wait `overlapSeconds`.
6. Revoke the old credential (delete its row in the internal
   Secret).
7. Update status `RotatedAt`, bump
   `metadata.labels.broker.stowage.io/rotation-generation`.

The overlap window is what gives tenants time to roll Pods that
mount the consumer Secret.

## Concurrency model

- **Single replica.** The chart deploys 1 operator Pod. Leader
  election is supported by controller-runtime but defaults to off
  because a single replica is the supported topology today.
- **Per-claim ordering.** The work-queue serialises reconciles per
  object. Two changes to the same claim are processed in order.
- **Inter-claim parallelism.** Different claims reconcile in
  parallel up to the configured worker count.

## Idempotency

Every step in `BucketClaim` reconciliation is idempotent:

- `CreateBucket` is OK if the bucket already exists.
- `MintCredential` checks for an existing internal Secret with the
  correct labels and reuses it.
- `WriteConsumerSecret` is server-side-apply where supported, so
  field ownership is clear.

If a reconcile is interrupted mid-way and re-runs, it does the right
thing. The `CreationInconsistent` reason exists for the rare case
where the upstream has a bucket but the operator's record of the
matching credential is missing or corrupt — that one needs operator
attention.

## Source

- Reconcilers: [`internal/operator/controller/`](https://github.com/stowage-dev/stowage/tree/main/internal/operator/controller).
- Credential generation: [`internal/operator/credentials/`](https://github.com/stowage-dev/stowage/tree/main/internal/operator/credentials).
- Secret writing: [`internal/operator/vcstore/`](https://github.com/stowage-dev/stowage/tree/main/internal/operator/vcstore).
- Upstream bucket lifecycle: [`internal/operator/backend/`](https://github.com/stowage-dev/stowage/tree/main/internal/operator/backend).
- Webhook: [`internal/operator/webhook/`](https://github.com/stowage-dev/stowage/tree/main/internal/operator/webhook).
