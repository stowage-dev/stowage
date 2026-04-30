---
type: how-to
---

# `S3Backend`

Cluster-scoped CRD. Declares an upstream S3-compatible backend the
operator can provision buckets on. One `S3Backend` per upstream
cluster / account.

## Minimal example

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

## Fields

| Field | Required | Default | Notes |
|---|---|---|---|
| `spec.endpoint` | yes | — | `http://...` or `https://...`. Reachable from the operator and from Stowage's proxy. |
| `spec.region` | no | `us-east-1` | AWS region or backend's region label. |
| `spec.addressingStyle` | no | `path` | `path` or `virtual`. Use `path` for MinIO/Garage/SeaweedFS, `virtual` for AWS/B2/R2/Wasabi. |
| `spec.adminCredentialsSecretRef.name` | yes | — | Secret holding the admin access key + secret. |
| `spec.adminCredentialsSecretRef.namespace` | yes | — | Namespace of the Secret. |
| `spec.adminCredentialsSecretRef.accessKeyField` | no | `AWS_ACCESS_KEY_ID` | Key in the Secret data. |
| `spec.adminCredentialsSecretRef.secretKeyField` | no | `AWS_SECRET_ACCESS_KEY` | Key in the Secret data. |
| `spec.tls.insecureSkipVerify` | no | false | Skip TLS verify. Don't enable in production. |
| `spec.tls.caBundleSecretRef` | no | — | Custom CA bundle Secret. Defaults to `key=ca.crt`. |
| `spec.bucketNameTemplate` | no | `{{ .Namespace }}-{{ .Name }}` | Go text/template applied to `BucketClaim` to compute the real bucket name. Vars: `.Namespace`, `.Name`, `.Hash`. |

## Status

Reported by the operator:

| Field | Notes |
|---|---|
| `status.conditions[type=Ready]` | `True` once the operator probed the endpoint with the admin credentials and got a successful `ListBuckets`. |
| `status.observedGeneration` | The `metadata.generation` the operator last reconciled. |
| `status.bucketCount` | Number of `BucketClaim`s pointing at this backend. |

The condition's `reason` field uses one of: `EndpointReachable`,
`EndpointUnreachable`, `CredentialsInvalid`, `TemplateInvalid`,
`BackendNotReady`, `BackendError`. See
[`internal/operator/api/v1alpha1/s3backend_types.go`](https://github.com/stowage-dev/stowage/blob/main/internal/operator/api/v1alpha1/s3backend_types.go).

## Interaction with `BucketClaim`

A `BucketClaim` references an `S3Backend` by name (cluster-scoped):

```yaml
spec:
  backendRef:
    name: prod-minio
```

If the backend isn't `Ready`, the claim's reconciliation is requeued.
The operator does not create a bucket on an upstream it can't reach.

## kubectl printer columns

```sh
kubectl get s3backends
# NAME         ENDPOINT                        READY   BUCKETS   AGE
# prod-minio   http://minio...:9000            True    7         3h
```

## Source

- Type: [`internal/operator/api/v1alpha1/s3backend_types.go`](https://github.com/stowage-dev/stowage/blob/main/internal/operator/api/v1alpha1/s3backend_types.go)
- CRD manifest: [`deploy/chart/crds/broker.stowage.io_s3backends.yaml`](https://github.com/stowage-dev/stowage/blob/main/deploy/chart/crds/broker.stowage.io_s3backends.yaml)
- Reconciler: [`internal/operator/controller/`](https://github.com/stowage-dev/stowage/tree/main/internal/operator/controller)
