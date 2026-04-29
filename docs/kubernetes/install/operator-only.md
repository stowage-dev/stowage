---
type: how-to
---

# Operator only (against external Stowage)

If Stowage already runs somewhere else (a VM, a different cluster)
and you want the operator alone on Kubernetes for the CRD-driven
provisioning flow.

## Caveat

This mode is **only useful if external Stowage's S3 proxy can read
the operator's Secret data via the in-cluster informer**. Today,
Stowage's
[`source_kubernetes.go`](https://github.com/stowage-dev/stowage/blob/main/internal/s3proxy/source_kubernetes.go)
expects to run inside the same cluster the operator writes Secrets
to. Pointing it at a remote cluster from an external Stowage
deployment requires a kubeconfig whose service account has list / watch
on Secrets in the operator namespace — supported by the config but
not common.

If the external Stowage cannot read the cluster's Secrets, you can
still use the operator to create buckets on the upstream and write
consumer Secrets for tenant Pods to mount, but the proxy won't
recognise the credentials. In that scenario, mint dashboard-side
credentials separately for the proxy path, and use the operator-
written credentials only for direct upstream access (which usually
defeats the point of using Stowage).

## Install

```sh
helm install stowage ./deploy/chart \
  --namespace stowage-system \
  --create-namespace \
  --set stowage.enabled=false
```

## What's deployed

- Operator Deployment + RBAC.
- Admission webhook + cert (unless `webhook.enabled=false`).
- The two CRDs.

No Stowage Deployment, no Stowage PVC, no Service, no Ingress.

## Pointing external Stowage at the operator's Secrets

In external Stowage's config:

```yaml
s3_proxy:
  enabled: true
  kubernetes:
    enabled: true
    namespace: stowage-system
    kubeconfig: /etc/stowage/kubeconfig
```

The kubeconfig should authenticate as a service account in the
cluster with `list`/`watch` permissions on Secrets in
`stowage-system`. The chart's RBAC creates such an account; you'll
need to extract its token and build the kubeconfig from it.
