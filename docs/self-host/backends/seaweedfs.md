---
type: how-to
---

# SeaweedFS

[SeaweedFS](https://github.com/seaweedfs/seaweedfs) exposes an
S3 endpoint via its `s3` server.

## YAML

```yaml
backends:
  - id: seaweedfs
    name: "SeaweedFS"
    type: s3v4
    endpoint: http://seaweedfs:8333
    region: us-east-1
    access_key_env: SEAWEED_ACCESS_KEY
    secret_key_env: SEAWEED_SECRET_KEY
    path_style: true
```

## What works

- Bucket and object CRUD.
- Multipart uploads.
- The embedded SigV4 proxy.

## Notes

- Path-style only.
- SeaweedFS supports a subset of S3 features. Versioning, lifecycle,
  and bucket policy support depends on the SeaweedFS release —
  Stowage surfaces upstream errors directly rather than pre-flighting.
- The native admin API for SeaweedFS is not yet implemented
  (`Capabilities.AdminAPI=""`).
