---
type: tutorial
---

# Quickstart: Kubernetes

Fifteen minutes to a Helm-installed Stowage + operator stack on a
cluster you already control, ending with one `BucketClaim` resolved
and a tenant-ready Secret in your namespace.

## Prerequisites

- A Kubernetes cluster (1.28+).
- `kubectl` configured against it.
- `helm` 3.13+.
- An upstream S3-compatible backend reachable from the cluster (a
  MinIO Tenant, a Garage cluster, AWS S3, etc.) and a Secret in the
  cluster containing its admin access + secret key.

## Install the chart

```sh
helm install stowage ./deploy/chart \
  --namespace stowage-system \
  --create-namespace \
  --set ingress.enabled=true \
  --set ingress.host=stowage.example.com
```

This deploys:

- The Stowage dashboard (Deployment + Service + PVC) on port 8080.
- The embedded SigV4 proxy on port 8090.
- The Stowage operator (Deployment + RBAC).
- An admission webhook (self-signed cert, valid 10 years by default).
- An optional Ingress on the host you set.
- The `S3Backend` and `BucketClaim` CRDs.

The chart auto-generates the AES-256 root key on first install and
preserves it across upgrades via `helm lookup`. Override with
`--set secretKey=<64 hex chars>` if you'd rather supply your own.

To skip components: `--set operator.enabled=false` (dashboard only)
or `--set stowage.enabled=false` (operator only).

## Bootstrap an admin

```sh
kubectl -n stowage-system exec deploy/stowage -- \
  stowage create-admin \
    --username admin \
    --password 'S3cur3-P@ssw0rd!'
```

## Declare an S3Backend

Create a Secret with the backend admin credentials:

```sh
kubectl -n stowage-system create secret generic minio-admin \
  --from-literal=AWS_ACCESS_KEY_ID=minioadmin \
  --from-literal=AWS_SECRET_ACCESS_KEY=minioadmin
```

Then declare the backend:

```yaml
apiVersion: broker.stowage.io/v1alpha1
kind: S3Backend
metadata:
  name: prod-minio
spec:
  endpoint: http://minio.minio.svc.cluster.local:9000
  region: us-east-1
  addressingStyle: path
  adminCredentialsSecretRef:
    name: minio-admin
    namespace: stowage-system
```

Apply it:

```sh
kubectl apply -f s3backend.yaml
kubectl get s3backends -w
```

Wait until `Ready=True` (the operator probes the endpoint with the
admin credentials before flipping the condition).

## Create your first BucketClaim

```yaml
apiVersion: broker.stowage.io/v1alpha1
kind: BucketClaim
metadata:
  name: uploads
  namespace: my-app
spec:
  backendRef:
    name: prod-minio
  deletionPolicy: Retain
  writeConnectionSecretToRef:
    name: uploads-creds
  quota:
    soft: 8Gi
    hard: 10Gi
```

Apply it and watch:

```sh
kubectl apply -f bucketclaim.yaml
kubectl -n my-app get bucketclaims -w
```

When the claim shows `Phase=Bound`, the operator has created the bucket
on the upstream, minted a virtual credential, and written
`Secret/uploads-creds` in the `my-app` namespace.

## Use the credential

The Secret carries `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`,
`AWS_REGION`, `AWS_ENDPOINT_URL`, `AWS_ENDPOINT_URL_S3`,
`BUCKET_NAME`, and `S3_ADDRESSING_STYLE`. Mount it into a Pod, or
extract a value directly:

```sh
kubectl -n my-app get secret uploads-creds -o jsonpath='{.data.access_key_id}' | base64 -d
```

Point any AWS SDK at the Stowage proxy Service
(`stowage.stowage-system.svc.cluster.local:8090`) using those
credentials. See [Use as an S3 endpoint](../s3-endpoint/) for the
client cookbook.

## What's running

```sh
kubectl -n stowage-system get pods,svc,ingress
kubectl get crd | grep stowage.io
```

You should see the Stowage Pod, the operator Pod, the dashboard +
proxy Services, the Ingress, and the two CRDs.

## Next step

- [Run on Kubernetes →](../kubernetes/) — the full operator and CRD
  reference.
- [Use as an S3 endpoint →](../s3-endpoint/) — client-side guides.
