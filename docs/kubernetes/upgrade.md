---
type: how-to
---

# Upgrade

Stowage on Kubernetes upgrades via `helm upgrade`. The chart
preserves the AES-256 root key across upgrades using `helm lookup`,
so `secretKey` does not need to be re-supplied.

## Standard upgrade

```sh
helm upgrade stowage ./deploy/chart \
  --namespace stowage-system \
  --reuse-values
```

If the chart has new templates, they apply on the next reconcile. If
it has new CRDs, they have to be applied manually — Helm intentionally
does not re-apply CRDs on upgrade. Apply them with `kubectl apply`:

```sh
kubectl apply -f deploy/chart/crds/
helm upgrade stowage ./deploy/chart --namespace stowage-system --reuse-values
```

## Image tag

Override the image tag explicitly to pin to a specific release:

```sh
helm upgrade stowage ./deploy/chart \
  --namespace stowage-system \
  --reuse-values \
  --set image.tag=v1.0.1
```

The chart's `values.yaml` default is `sha-<short>` — fine for testing
against a tip-of-main image, not great for reproducible production
installs. Pin to a tagged release.

## Database migrations

Stowage applies SQLite migrations during startup of the new image
tag. The PVC is shared across the rolling update because Stowage uses
`Recreate` strategy (a single replica with an RWO PVC can't roll).
Expect a few seconds of downtime per upgrade.

## Rolling back

```sh
helm rollback stowage <revision> --namespace stowage-system
```

Helm doesn't roll back the database. If the new image applied a
migration the old image doesn't understand, the rollback won't be
clean — restore from the [backup](../self-host/operations/backup.md)
you took before the upgrade.

## Operator upgrades

The operator and Stowage are versioned together (one image tag, two
binaries). Upgrading the chart upgrades both. CRD schema changes
land via the Helm upgrade flow plus the manual `kubectl apply -f
crds/` step.

## What to verify after upgrade

- `kubectl -n stowage-system get pods` — both Pods Running.
- `kubectl -n stowage-system logs deploy/stowage` — no migration
  failures.
- `kubectl get s3backends` — `Ready=True` on every backend.
- A test `BucketClaim` reconciles to `Phase=Bound` within a minute.
- Click around the dashboard, do a small upload, check
  `/admin/audit`.
