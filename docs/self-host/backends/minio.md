---
type: how-to
---

# MinIO

MinIO is the upstream Stowage is most often paired with — the project
exists in part because of the May 2025 MinIO Console change (see
[Why AGPL](../../explanations/why-agpl.md)).

## YAML

```yaml
backends:
  - id: minio
    name: "MinIO"
    type: s3v4
    endpoint: http://minio:9000
    region: us-east-1
    access_key_env: MINIO_ROOT_USER
    secret_key_env: MINIO_ROOT_PASSWORD
    path_style: true
```

## What works

- All bucket operations: list, create, delete, head.
- Versioning, CORS, bucket policy, lifecycle.
- Object operations including multipart, tagging, user metadata,
  per-object version history.
- The embedded SigV4 proxy re-signs cleanly to MinIO; tenants get
  per-credential `ListBuckets` synthesis.

## What doesn't work yet

- The optional **native admin API** (creating MinIO users / policies
  from inside the Stowage admin UI) needs a `minio` driver that
  hasn't been written. `Capabilities.AdminAPI` returns `""` today —
  the UI hides the `Users` / `Policies` screens that would consume it.
  Tracked in [Roadmap](../../explanations/roadmap.md).

## Notes

- Always use `path_style: true`. MinIO's virtual-hosted addressing
  needs DNS wildcards that you usually don't have on internal
  installs.
- Set `MINIO_REGION` on the MinIO side to match your `region`. Default
  `us-east-1` works for most homelab setups.
- For the `quickstart` subcommand's bundled MinIO download, see
  [`internal/quickstart/quickstart.go`](https://github.com/stowage-dev/stowage/blob/main/internal/quickstart/quickstart.go).
