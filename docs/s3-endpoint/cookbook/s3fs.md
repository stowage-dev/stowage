---
type: how-to
---

# Cookbook: `s3fs-fuse`

[`s3fs-fuse`](https://github.com/s3fs-fuse/s3fs-fuse) mounts an S3
bucket as a FUSE filesystem.

## Recommended use

Ad-hoc inspection only. FUSE-mounted S3 has quirks (rename costs an
upload, no atomic directory operations, sub-optimal random access)
that make it a bad fit for any data path that wasn't designed
around them. Prefer the SDK or `rclone copy` for real workloads.

## Setup

Put the credentials in a file with mode 0600:

```sh
echo "AKIA...:..." > ~/.passwd-s3fs
chmod 0600 ~/.passwd-s3fs
```

Mount:

```sh
s3fs my-bucket /mnt/stowage \
  -o passwd_file=$HOME/.passwd-s3fs \
  -o url=https://s3.stowage.example.com \
  -o use_path_request_style \
  -o sigv4 \
  -o region=us-east-1
```

## Unmount

```sh
fusermount -u /mnt/stowage
```

## Caveats

- Append/random-write to existing objects rewrites the entire
  object, which counts against your bucket quota each time. A `dd
  conv=notrunc` to update a 1 GiB file's middle byte costs you a
  1 GiB write.
- File listing latency is dominated by `ListObjectsV2`, which can
  paginate. Big buckets feel slow.
- Permissions, ownership, and timestamps are best-effort. Don't rely
  on them for security.
- The proxy's per-credential RPS limits apply. A FUSE mount can
  generate surprising request volume; if you see 429s, lower the
  `parallel_count` and `multipart_size` options.
