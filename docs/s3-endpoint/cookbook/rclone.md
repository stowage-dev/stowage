---
type: how-to
---

# Cookbook: `rclone`

`rclone` works against Stowage as a generic S3 remote.

## Configure interactively

```sh
rclone config
```

When prompted:

- **Storage:** `s3`
- **Provider:** `Other`
- **AWS Access Key ID** / **Secret**: paste your virtual credential.
- **Region:** `us-east-1` (or whatever the operator told you).
- **Endpoint:** `https://s3.stowage.example.com`
- **Force path-style:** `true`

## Configure via file

`~/.config/rclone/rclone.conf`:

```ini
[stowage]
type = s3
provider = Other
env_auth = false
access_key_id = AKIA...
secret_access_key = ...
region = us-east-1
endpoint = https://s3.stowage.example.com
force_path_style = true
```

## Use it

```sh
rclone ls    stowage:my-bucket/
rclone copy  ./local-dir/ stowage:my-bucket/remote-dir/ --progress
rclone sync  ./local-dir/ stowage:my-bucket/remote-dir/ --dry-run
rclone serve http stowage:my-bucket/ --addr :8081
```

## Tuning

- `--s3-chunk-size 16M` and `--transfers 8` are reasonable defaults
  matching Stowage's 16 MiB multipart part size.
- `--s3-upload-cutoff 64M` triggers multipart for files over 64 MiB.
- `--checksum` and `--immutable` are recommended for `sync` to a
  versioned bucket.

## Mount

`rclone mount` is supported but slower than direct SDK access. Useful
for ad-hoc inspection, not for production data paths.

```sh
rclone mount stowage:my-bucket/ /mnt/stowage --vfs-cache-mode writes
```
