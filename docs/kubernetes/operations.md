---
type: how-to
---

# Operations on Kubernetes

Day-2 tasks specific to the Kubernetes deployment shape.

## Inspecting a stuck `BucketClaim`

```sh
kubectl -n my-app describe bucketclaim uploads
```

The `Conditions:` section names the failing condition (`BucketCreated`,
`CredentialsProvisioned`) and the reason (`EndpointUnreachable`,
`CredentialsInvalid`, `BackendNotReady`). Cross-reference the operator
logs:

```sh
kubectl -n stowage-system logs deploy/stowage-operator --tail=100 \
  | grep my-app/uploads
```

## Common reconcile failures

| Reason | Likely fix |
|---|---|
| `EndpointUnreachable` on the `S3Backend` | Stowage operator can't reach the upstream. Check egress policy, DNS, and the endpoint URL. |
| `CredentialsInvalid` on the `S3Backend` | The admin credentials Secret is wrong or the upstream rejected them. |
| `TemplateInvalid` on the `S3Backend` | The `bucketNameTemplate` doesn't render. Check the Go template syntax. |
| `BackendNotReady` on the `BucketClaim` | The referenced `S3Backend` isn't `Ready=True` yet. Fix the backend first. |
| `BucketNotEmpty` on `BucketClaim` deletion | `deletionPolicy: Delete` but the bucket has objects. Set `forceDelete: true`. |
| `CreationInconsistent` | The bucket exists on the upstream but the operator's Secret doesn't reflect the right credentials. Usually means a previous reconcile crashed mid-flight. Annotate the claim with a rotate to retry. |

## Force a reconcile

Annotate the claim to bump generation:

```sh
kubectl -n my-app annotate bucketclaim uploads \
  broker.stowage.io/reconcile="$(date +%s)" --overwrite
```

The operator uses standard controller-runtime requeueing — this is a
nudge, not a magic word. Persistent failures need to be diagnosed
from the conditions and logs.

## Inspecting the merged credential cache

In the dashboard, `/admin/s3-proxy` shows credentials from both
SQLite and the Kubernetes informer. Each row carries a source label.
Operator-managed entries can't be edited from the UI — that's
intentional, since they're owned by the claim.

The proxy logs the cache size on Prometheus:
`stowage_s3_credential_cache_size`. Watch for a flat line at zero
when `s3_proxy.kubernetes.enabled` is supposed to be on — that means
the informer never started.

## Operator logs to watch

| Log line | Meaning |
|---|---|
| `Reconciler started` | Controller booted. One per CRD kind. |
| `creating bucket on upstream` | First reconcile of a `BucketClaim`. |
| `provisioning virtual credential` | The operator is minting a new credential pair. |
| `rotating credential` | Either time-based or annotation-triggered rotation. |
| `bucket retained` | `deletionPolicy: Retain` saved the bucket on claim deletion. |
| `informer disconnected, reconnecting` | Transient API server issue. Self-heals. |

## Forcing immediate credential revocation

See [Rotation → forcing immediate revocation](./rotation.md#forcing-immediate-revocation).

## Replacing the AES-256 root key

The root key is in `Secret/stowage` in the install namespace. Don't
change it casually — see
[Self-host → Key rotation](../self-host/operations/key-rotation.md)
for the procedure. The same procedure applies on Kubernetes; replace
the Secret value with `kubectl create secret generic stowage --from-
literal=secret-key=$(openssl rand -hex 32) --dry-run=client -oyaml |
kubectl apply -f -`.

## Resizing the PVC

The chart uses an RWO PVC sized at `storage.size` (default 1Gi). To
resize:

1. The underlying StorageClass must support volume expansion.
2. Edit the chart values and `helm upgrade`. Helm patches the PVC.
3. The Pod must be restarted for the file system to grow (some
   StorageClass implementations require `persistentVolumeReclaimPolicy:
   Retain` and a manual filesystem resize).

For most installs, 1Gi is plenty until you have millions of audit
rows.
