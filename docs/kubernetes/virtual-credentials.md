---
type: how-to
---

# Virtual credentials in Kubernetes

When the operator reconciles a `BucketClaim`, it writes two Secrets:

1. **Consumer Secret** — in the claim's namespace, the AWS_* env
   vars tenants mount into Pods.
2. **Internal Secret** — in the operator namespace, the authoritative
   record from `access_key_id` to `secret_access_key`, bucket scope,
   backend, quota. The Stowage proxy's Kubernetes informer reads
   these.

## How tenants consume the consumer Secret

Mount it via `envFrom`:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-app
  namespace: my-app
spec:
  template:
    spec:
      containers:
        - name: app
          image: my-app:1.2.3
          envFrom:
            - secretRef:
                name: uploads-creds
```

Or per-env:

```yaml
env:
  - name: BUCKET
    valueFrom:
      secretKeyRef:
        name: uploads-creds
        key: BUCKET_NAME
  - name: AWS_ENDPOINT_URL
    valueFrom:
      secretKeyRef:
        name: uploads-creds
        key: AWS_ENDPOINT_URL
```

## Endpoint URL routing

`AWS_ENDPOINT_URL` and `AWS_ENDPOINT_URL_S3` point at the Stowage
proxy Service:

```
http://stowage.stowage-system.svc.cluster.local:8090
```

Tenants don't reach the upstream directly. Every request is verified
and re-signed by the proxy.

## What the proxy does at request time

1. Parses the SigV4 signature.
2. Looks up the `access_key_id` in its merged limit / credential
   cache (Kubernetes Secrets + SQLite). Kubernetes wins on access-key
   collision.
3. Verifies the signature against the secret stored on the cache
   entry.
4. Enforces bucket scope from `bucket_scopes` (or `bucket_name` for
   single-bucket credentials).
5. Pre-checks the quota.
6. Re-signs with the upstream admin credentials and forwards.

## Internal vs consumer Secret labels

Both Secrets carry these labels (from
[`internal/operator/vcstore/labels.go`](https://github.com/stowage-dev/stowage/blob/main/internal/operator/vcstore/labels.go)):

```
broker.stowage.io/role            = virtual-credential | consumer-secret
broker.stowage.io/claim-namespace = <claim namespace>
broker.stowage.io/claim-name      = <claim name>
broker.stowage.io/claim-uid       = <claim UID>
broker.stowage.io/access-key-id   = AKIA…
broker.stowage.io/backend         = <backend name>
broker.stowage.io/bucket          = <bucket name>
broker.stowage.io/rotation-generation = <int>
```

This means you can find every Secret backing a claim with one
selector:

```sh
kubectl get secrets --all-namespaces \
  -l broker.stowage.io/claim-name=uploads,broker.stowage.io/claim-namespace=my-app
```

## Listing virtual credentials from the dashboard

`/admin/s3-proxy` shows the merged view across SQLite and Kubernetes
sources. Operator-managed entries are read-only in the dashboard —
you manage them by editing the `BucketClaim`.

## Why the contract lives in two files

The operator and the proxy live in the same Go module but compile
into separate binaries. The Secret data fields are the wire contract
between them:

- [`internal/operator/vcstore/labels.go`](https://github.com/stowage-dev/stowage/blob/main/internal/operator/vcstore/labels.go)
  — what the operator writes.
- [`internal/s3proxy/source_kubernetes.go`](https://github.com/stowage-dev/stowage/blob/main/internal/s3proxy/source_kubernetes.go)
  — what the proxy reads.

Changing one without the other silently breaks the integration.
Reviewers must look at both whenever a field name moves.
