---
type: how-to
---

# Credential rotation

The operator can rotate a `BucketClaim`'s virtual credential
manually or on a time schedule.

## Manual rotation (default)

```yaml
spec:
  rotationPolicy:
    mode: Manual
```

To trigger a rotation, edit the claim and bump
`metadata.annotations.broker.stowage.io/rotate` to a new value (any
string change triggers reconciliation). Or `kubectl annotate` it:

```sh
kubectl -n my-app annotate bucketclaim uploads \
  broker.stowage.io/rotate="$(date +%s)" --overwrite
```

The operator mints a new credential, writes the consumer Secret,
keeps the old credential active for `overlapSeconds`, then revokes
it.

`status.rotatedAt` is updated when the new credential takes effect.
`status.accessKeyId` updates with the new key ID.

## Time-based rotation

```yaml
spec:
  rotationPolicy:
    mode: TimeBased
    intervalDays: 90
    overlapSeconds: 300
```

| Field | Min | Notes |
|---|---|---|
| `intervalDays` | 7 | How often the operator schedules a rotation. |
| `overlapSeconds` | 0 | Time both old and new credentials are accepted simultaneously. |

The operator schedules rotations from `status.rotatedAt`. The
admission webhook rejects `mode: TimeBased` with `intervalDays < 7`.

## What happens during the overlap window

For `overlapSeconds`:

- Both the old and the new access keys verify successfully.
- The consumer Secret already carries the new key.
- Tenant Pods that haven't yet picked up the rotated Secret keep
  working with the old key.

After `overlapSeconds`, the old key is revoked. Tenant Pods running
with stale credentials get 401s until they pick up the new Secret.

## Pod side: making rotation invisible

Two patterns:

1. **Restart on Secret change.** Use a tool like
   [Reloader](https://github.com/stakater/Reloader) to roll the
   Deployment when its Secret changes. The overlap window covers the
   rolling restart.
2. **Re-read at request time.** Use the AWS SDK's credential
   provider chain or set `AWS_SHARED_CREDENTIALS_FILE` to a path on a
   `projected` volume that picks up the Secret changes without a
   restart. The SDK will reload on its own cadence.

`overlapSeconds: 300` is a sensible starting default — long enough for
a rolling restart of typical workloads, short enough that a leaked
credential is short-lived.

## Forcing immediate revocation

To revoke a credential immediately (e.g. suspected compromise),
delete the internal Secret in the operator namespace:

```sh
kubectl -n stowage-system delete secret \
  -l broker.stowage.io/role=virtual-credential,broker.stowage.io/access-key-id=AKIA...
```

The operator will reconcile and mint a fresh credential within seconds.
Tenants get 401s until they pick up the new Secret.

This is the closest thing to a "panic button" — the manual annotation
flow above is preferable for routine rotation because it preserves
the overlap window.
