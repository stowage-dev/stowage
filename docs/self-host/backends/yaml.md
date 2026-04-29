---
type: how-to
---

# Connecting an S3-compatible backend in YAML

YAML-defined backends are loaded at startup and are read-only in the
admin UI (they show with a `config` badge). For backends you'll add
and remove without restarting, see
[UI-managed backends](./ui-managed.md). YAML wins on `id` collisions.

## Schema

```yaml
backends:
  - id: prod
    name: "Prod MinIO"
    type: s3v4
    endpoint: https://minio.example.com
    region: us-east-1
    access_key_env: PROD_ACCESS_KEY
    secret_key_env: PROD_SECRET_KEY
    path_style: true
```

| Field | Required | Notes |
|---|---|---|
| `id` | yes | Stable identifier. Used in URLs and audit rows. Must be unique. |
| `name` | recommended | Human-readable label shown in the UI. Falls back to `id` if empty. |
| `type` | yes | The driver. Today the only implemented driver is `s3v4`. |
| `endpoint` | yes | Base URL of the upstream API. Include scheme and port if non-standard. |
| `region` | yes | AWS region. Use `us-east-1` if the upstream doesn't care. |
| `access_key_env` | yes | Name of an env var that holds the access key. |
| `secret_key_env` | yes | Name of an env var that holds the secret key. |
| `path_style` | depends | `true` for MinIO / Garage / SeaweedFS. `false` for AWS S3. See per-vendor pages. |

The credentials themselves never appear in YAML. Putting
`PROD_ACCESS_KEY=AKIA…` in your shell or systemd unit means the
secret stays out of files-on-disk and out of `git`.

## Multi-backend

Just add more entries:

```yaml
backends:
  - id: prod
    name: "Prod MinIO"
    type: s3v4
    endpoint: https://minio.prod.example.com
    region: us-east-1
    access_key_env: PROD_ACCESS_KEY
    secret_key_env: PROD_SECRET_KEY
    path_style: true

  - id: archive
    name: "Backblaze B2 Archive"
    type: s3v4
    endpoint: https://s3.us-west-002.backblazeb2.com
    region: us-west-002
    access_key_env: B2_KEY_ID
    secret_key_env: B2_APPLICATION_KEY
    path_style: false
```

Stowage probes every configured backend at startup and shows the
result on the `/admin/backends/health` page (a 20-tile rolling
history strip per backend, plus last error and last latency).

## Why each backend needs admin-grade credentials

Stowage proxies dashboard actions and SDK actions through these
credentials:

- The dashboard's bucket settings calls (CORS, policy, lifecycle,
  versioning) need backend-level access.
- The embedded SigV4 proxy re-signs every tenant request under the
  upstream admin credentials; without admin scope, tenant uploads
  may fail when the backend's policy denies cross-account writes.
- The startup probe runs `ListBuckets`, which on most backends needs
  admin scope.

If you can't give Stowage admin credentials, scope the IAM user as
narrowly as your backend allows but at minimum cover the bucket-
admin operations Stowage uses (see the
[Backend interface](https://github.com/stowage-dev/stowage/blob/main/internal/backend/backend.go)
for the full list).

## Per-vendor specifics

- [MinIO](./minio.md)
- [Garage](./garage.md)
- [SeaweedFS](./seaweedfs.md)
- [AWS S3](./aws-s3.md)
- [Backblaze B2](./b2.md)
- [Cloudflare R2](./r2.md)
- [Wasabi](./wasabi.md)
