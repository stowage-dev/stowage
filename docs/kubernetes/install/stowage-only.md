---
type: how-to
---

# Stowage only (no operator)

For when you want the Stowage dashboard + SigV4 proxy on Kubernetes
but don't want the operator. Useful if you'll manage backends and
credentials by hand from the dashboard, or if your cluster doesn't
allow CRDs.

## Install

```sh
helm install stowage ./deploy/chart \
  --namespace stowage-system \
  --create-namespace \
  --set operator.enabled=false \
  --set webhook.enabled=false
```

Disabling the operator implies disabling the webhook (which only
exists to validate CRDs the operator owns).

## What's deployed

Same as the multi-tenant install minus:

- The operator Deployment + RBAC.
- The admission webhook + cert.
- The CRDs are still installed by default — Helm renders them
  unconditionally. They sit unused.

If you'd rather not install the CRDs at all, remove the chart's `crds/`
manifests before packaging or use the `--skip-crds` flag at install
time:

```sh
helm install stowage ./deploy/chart \
  --skip-crds \
  --namespace stowage-system --create-namespace \
  --set operator.enabled=false \
  --set webhook.enabled=false
```

`--skip-crds` only applies on first install; Helm deliberately leaves
CRDs alone on subsequent upgrades.

## Configuring backends without the operator

Use one of:

- **YAML in the rendered config** — pass through `config:` (see
  [Multi-tenant install](./multi-tenant.md)).
- **`/admin/endpoints` in the dashboard** — UI-managed backends
  sealed with the AES-256 root key.

Both paths work without the operator.

## Configuring virtual credentials without the operator

Mint them in the dashboard at `/admin/s3-proxy`. Without the
operator, the only source of virtual credentials is the SQLite store.
The Kubernetes informer in
[`internal/s3proxy/source_kubernetes.go`](https://github.com/stowage-dev/stowage/blob/main/internal/s3proxy/source_kubernetes.go)
is only enabled when `s3_proxy.kubernetes.enabled: true` — leave it
off in this install mode.
