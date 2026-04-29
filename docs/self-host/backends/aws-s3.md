---
type: how-to
---

# AWS S3

```yaml
backends:
  - id: aws
    name: "AWS S3 (us-east-1)"
    type: s3v4
    endpoint: https://s3.us-east-1.amazonaws.com
    region: us-east-1
    access_key_env: AWS_ACCESS_KEY_ID
    secret_key_env: AWS_SECRET_ACCESS_KEY
    path_style: false
```

## What works

- Everything in `Capabilities`: versioning, lifecycle, bucket policy,
  CORS, tagging, server-side encryption.
- The embedded SigV4 proxy.

## Notes

- Use `path_style: false` for AWS — virtual-hosted addressing is the
  default and avoids the path-style deprecation timeline.
- Set `region` to the bucket's region. Cross-region access works but
  pays the latency cost.
- IAM policy on the access key must cover everything Stowage uses:
  list / get / put / delete on objects and buckets, plus the bucket
  configuration verbs (`s3:GetBucketCORS`, `s3:PutBucketLifecycle`,
  etc.) for the dashboard's bucket-settings UI.
- The dashboard surfaces upstream `AccessDenied` directly; you'll see
  it in the audit log if a particular operation isn't covered by your
  IAM policy.
- AWS bills egress. Cross-backend transfers via Stowage's
  `POST /object/copy` flow stream through the proxy host, so an
  AWS→other-vendor transfer pays AWS egress.
