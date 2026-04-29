---
type: how-to
---

# Cookbook: MinIO `mc`

The MinIO client speaks plain S3 SigV4 and works against any
S3-compatible endpoint, including Stowage.

## Configure an alias

```sh
mc alias set stowage \
  https://s3.stowage.example.com \
  AKIA... \
  ...
```

`mc` defaults to virtual-hosted style. If your operator hasn't
configured `host_suffixes` on the proxy, force path-style:

```sh
mc alias set stowage \
  https://s3.stowage.example.com \
  AKIA... ... \
  --path
```

## Use it

```sh
mc ls    stowage/my-bucket/
mc cp    ./file.bin stowage/my-bucket/file.bin
mc rm    stowage/my-bucket/file.bin
mc mirror ./local/ stowage/my-bucket/remote/
```

## What won't work

`mc admin` commands talk to MinIO's native admin API, which Stowage
doesn't proxy. They'll error against the Stowage endpoint. For
admin-style operations on Stowage:

- Adding endpoints / minting credentials → Stowage dashboard
  `/admin/endpoints` and `/admin/s3-proxy`.
- Quotas → Stowage dashboard bucket settings.
- User management → Stowage dashboard `/admin/users`.

## Multipart and concurrency

`mc cp --multipart-chunk-size 16MB --parallel 4 ...` matches
Stowage's defaults reasonably well.
