---
type: how-to
---

# Endpoint URL convention

Two valid addressing styles. Pick whichever your SDK supports.

## Path style

```
https://<endpoint>/<bucket>/<key>
```

Example:

```
https://s3.stowage.example.com/my-bucket/path/to/object.bin
```

This works regardless of DNS configuration and is the easiest mode.
Most CLIs default to it; `aws-cli` switches to virtual-hosted style
unless you set `--addressing-style path` (or `s3.addressing_style =
path` in `~/.aws/config`).

Set this in `aws-cli`:

```ini
# ~/.aws/config
[default]
s3 =
  addressing_style = path
```

## Virtual-hosted style

```
https://<bucket>.<endpoint>/<key>
```

Example:

```
https://my-bucket.s3.stowage.example.com/path/to/object.bin
```

This requires the operator to have configured `host_suffixes` so the
proxy can extract the bucket from the Host header. If you call
virtual-hosted-style without that config, the proxy treats your URL
as path-style and fails the lookup.

Most modern AWS SDKs default to this style. If your operator has set
`host_suffixes`, just use the SDK's defaults.

## Wildcard DNS

Virtual-hosted style needs DNS for the wildcard domain. The
operator's responsibility, not yours, but it affects whether your
SDK's defaults work without extra config:

```
*.s3.stowage.example.com → 198.51.100.10
```

If the wildcard isn't set up, the SDK's default virtual-hosted
style fails before reaching the proxy. Fall back to path style.

## Port

The proxy listens on `:8090` by default. The reverse proxy in front
of it (nginx, the Ingress controller) usually publishes it on `:443`
under a different hostname (`s3.stowage.example.com`). Use the
hostname your admin gave you; you don't need to specify a port unless
they did.

## TLS

Behind a TLS-terminating reverse proxy, use `https://`. Stowage
itself only serves `http://` — but the proxy in front handles TLS
termination. Don't try to talk plaintext to a TLS endpoint.

## Picking a style with each SDK

| SDK | Path style flag |
|---|---|
| `aws-cli` | `s3.addressing_style = path` in config; or `--endpoint-url-mode path` (newer versions) |
| AWS SDK for Go v2 | `o.UsePathStyle = true` on the `s3.Options` |
| boto3 (Python) | `s3 = {'addressing_style': 'path'}` in client config |
| AWS SDK for JS v3 | `forcePathStyle: true` on the `S3Client` |
| MinIO `mc` | always virtual-hosted; configure host_suffixes on the proxy |
| `rclone` | `force_path_style = true` in the remote config |
