---
type: reference
---

# `S3Backend` CRD reference

API: `broker.stowage.io/v1alpha1`. Cluster-scoped. Short name
`s3b`.

Source:
[`internal/operator/api/v1alpha1/s3backend_types.go`](https://github.com/stowage-dev/stowage/blob/main/internal/operator/api/v1alpha1/s3backend_types.go).

## Spec

```yaml
spec:
  endpoint: string                       # required, ^https?://.+
  region: string                         # default: us-east-1
  addressingStyle: path|virtual          # default: path
  adminCredentialsSecretRef:
    name: string                         # required
    namespace: string                    # required
    accessKeyField: string               # default: AWS_ACCESS_KEY_ID
    secretKeyField: string               # default: AWS_SECRET_ACCESS_KEY
  tls:
    insecureSkipVerify: bool             # default: false
    caBundleSecretRef:
      name: string
      namespace: string
      key: string                        # default: ca.crt
  bucketNameTemplate: string             # default: "{{ .Namespace }}-{{ .Name }}"
```

## Status

```yaml
status:
  conditions: []metav1.Condition
  observedGeneration: int64
  bucketCount: int32
```

## Conditions

The reconciler emits `Ready` with one of these reasons:

- `EndpointReachable` — admin probe succeeded.
- `EndpointUnreachable` — couldn't reach the endpoint.
- `CredentialsInvalid` — admin probe got 401 / 403.
- `TemplateInvalid` — `bucketNameTemplate` doesn't render.
- `BackendError` — generic upstream error.

## kubebuilder validation

Encoded as `+kubebuilder:validation:*` tags on the type:

- `endpoint` matches `^https?://.+`.
- `addressingStyle` ∈ `{path, virtual}`.
- `region` is unconstrained but rendered to defaults.
- Admin Secret name / namespace must be non-empty.
- `bucketNameTemplate` is a Go text/template with vars `.Namespace`,
  `.Name`, `.Hash`.

## Printer columns

```
NAME    ENDPOINT                 READY   BUCKETS   AGE
prod    http://minio:9000        True    7         3h
```

## Relationship with `BucketClaim`

A `BucketClaim` references this backend by `spec.backendRef.name`.
The reconciler will not create buckets on a backend that isn't
`Ready=True`.
