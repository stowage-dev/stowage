---
type: how-to
---

# Multi-tenant install (chart + operator)

The default Helm install. Deploys Stowage with the operator manager
and admission webhook embedded in the same Pod. Recommended for new
clusters.

## Prerequisites

- Kubernetes 1.28 or newer.
- A default `StorageClass` that supports `RWO` PVCs (or set
  `storage.storageClassName` explicitly).
- An upstream S3-compatible backend reachable from the cluster, with
  admin credentials.

## Install

```sh
helm install stowage ./deploy/chart \
  --namespace stowage-system \
  --create-namespace \
  --set ingress.enabled=true \
  --set ingress.host=stowage.example.com
```

## What gets deployed

```
namespace/stowage-system
├── deployment/stowage             # 1 replica: dashboard + S3 proxy + operator manager
├── service/stowage                # 80 → http, 8090 → s3, 443 → webhook
├── pvc/stowage-data               # RWO, holds stowage.db + secret key
├── ingress/stowage                # if ingress.enabled=true
├── secret/stowage-secret-key      # AES-256 root key
├── configmap/stowage-config       # rendered config.yaml
├── secret/stowage-webhook-cert    # if webhook.enabled=true
├── validatingwebhookconfiguration/stowage-validating-webhook
├── role + rolebinding/stowage     # namespaced Secret read for proxy informer
└── clusterrole + clusterrolebinding/stowage  # operator manager
```

Cluster-scoped:

```
crd/s3backends.broker.stowage.io
crd/bucketclaims.broker.stowage.io
```

## Verify

```sh
kubectl -n stowage-system get pods
kubectl -n stowage-system get svc,pvc,ingress
kubectl get crd | grep stowage.io
kubectl -n stowage-system logs deploy/stowage | tail -40
```

The startup log lists the HTTP listener, the embedded S3 proxy, and
(when `operator.enabled`) the operator manager booting its
reconcilers and webhook.

## Bootstrap the first admin

```sh
kubectl -n stowage-system exec deploy/stowage -- \
  stowage create-admin --username admin --password 'S3cur3-P@ssw0rd!'
```

## Where the AES-256 root key lives

`Secret/stowage` in the install namespace. The chart generates a
fresh key on first install and uses `helm lookup` on subsequent
upgrades to preserve it. To override:

```sh
helm install ... --set secretKey=$(openssl rand -hex 32)
```

Once installed, do not change `secretKey` without going through the
[key-rotation procedure](../../self-host/operations/key-rotation.md).

## Override the chart's stowage `config.yaml`

The chart renders a `config.yaml` into `Secret/stowage-config`. To
override any field, pass YAML through `config:`:

```sh
helm upgrade stowage ./deploy/chart \
  --namespace stowage-system \
  --reuse-values \
  -f - <<'YAML'
config:
  auth:
    modes: [oidc]
    oidc:
      issuer: https://idp.example.com/realms/main
      client_id: stowage
      client_secret_env: OIDC_CLIENT_SECRET
      role_claim: groups
      role_mapping:
        admin: [stowage-admins]
        user:  [stowage-users]
        readonly: [stowage-readonly]
  s3_proxy:
    enabled: true
    host_suffixes: [s3.stowage.example.com]
YAML
```

The merged YAML is what Stowage actually reads. Don't try to mutate
the rendered Secret directly; the next `helm upgrade` will overwrite
it.

## Install minimal (no Ingress, no webhook)

```sh
helm install stowage ./deploy/chart \
  --namespace stowage-system \
  --create-namespace \
  --set ingress.enabled=false \
  --set webhook.enabled=false
```

You can still reach the dashboard via `kubectl port-forward
svc/stowage 8080:8080`. The webhook off means CRD validation
relaxes to whatever the OpenAPI schema in the CRD itself enforces;
losing it is fine for kicking the tyres but not for production.

## Install with cert-manager for the webhook

```sh
helm install stowage ./deploy/chart \
  --namespace stowage-system --create-namespace \
  --set webhook.selfSigned.enabled=false \
  --set webhook.certManager.enabled=true \
  --set webhook.certManager.issuerRef.kind=ClusterIssuer \
  --set webhook.certManager.issuerRef.name=letsencrypt-prod
```

cert-manager must already be installed. See
[Webhook & cert-manager](../webhook.md) for the full options matrix.
