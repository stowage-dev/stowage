---
type: how-to
---

# Use as an S3 endpoint

Documentation for tenant developers — the people pointing AWS SDKs
at the embedded SigV4 proxy. If you're an operator running Stowage,
see [Self-host](../self-host/) or [Run on Kubernetes](../kubernetes/)
instead.

## Pages

- [Quickstart for SDK users](./quickstart.md)
- [Endpoint URL convention](./endpoint-url.md)
- [Authentication and bucket scope](./auth-and-scope.md)
- [Quota errors and retry semantics](./quota-errors.md)
- [Anonymous read endpoints](./anonymous.md)
- [Supported S3 operations matrix](./operations-matrix.md)
- Cookbook:
  - [`aws-cli`](./cookbook/aws-cli.md)
  - [Go (AWS SDK v2)](./cookbook/go.md)
  - [Python (boto3)](./cookbook/python.md)
  - [JavaScript (AWS SDK for JS)](./cookbook/js.md)
  - [`rclone`](./cookbook/rclone.md)
  - [`s3fs-fuse`](./cookbook/s3fs.md)
  - [MinIO `mc`](./cookbook/mc.md)
