---
type: how-to
---

# Quickstart for SDK users

You've been handed a Stowage virtual credential by an admin. This
page gets you talking to the proxy in 60 seconds.

## What you have

- An access key ID (looks like `AKIA…`).
- A secret access key.
- An endpoint URL (e.g. `https://s3.stowage.example.com`).
- One or more bucket names you've been granted access to.

If any of those are missing, ask your admin. The proxy doesn't
auto-discover.

## Test it with `aws-cli`

```sh
export AWS_ACCESS_KEY_ID='AKIA...'
export AWS_SECRET_ACCESS_KEY='...'
export AWS_REGION='us-east-1'
ENDPOINT='https://s3.stowage.example.com'

aws --endpoint-url $ENDPOINT s3 ls
```

You should see only the buckets you were granted. `ListBuckets` is
synthesised by the proxy from your credential's bucket-scope list —
it doesn't reach the upstream and only shows what you can actually
use.

Upload a small file:

```sh
echo 'hello' > hello.txt
aws --endpoint-url $ENDPOINT s3 cp hello.txt s3://my-bucket/hello.txt
```

Get it back:

```sh
aws --endpoint-url $ENDPOINT s3 cp s3://my-bucket/hello.txt -
```

## What works

- `GetObject`, `PutObject`, `HeadObject`, `DeleteObject`,
  `ListObjects`/`ListObjectsV2`, multipart uploads (init / part /
  complete / abort / list).
- `HeadBucket`, `ListBuckets`, `GetBucketLocation`.
- `CopyObject` within the same backend.
- Presigned URLs.
- Path-style addressing
  (`https://endpoint/<bucket>/<key>`) and virtual-hosted-style
  addressing (`https://<bucket>.endpoint/<key>`) — the latter
  requires `host_suffixes` to be configured on the proxy.

For the full matrix, see
[Operations matrix](./operations-matrix.md).

## What doesn't work

- Cross-account / cross-bucket operations on a credential that's
  scoped to a single bucket — they get 403 `forbidden`.
- Bucket creation and deletion — those are admin-only and don't go
  through tenant credentials.
- Anything you'd hit upstream's admin API for (IAM, replication
  config, etc.) — that's not what the proxy is for.

## What you don't need to know

- The upstream's identity. The proxy re-signs to the upstream with
  its own admin credentials. Your access key never touches the
  upstream.
- What the upstream backend even is. From the SDK's perspective, the
  proxy is the S3 endpoint.

## Next step

- [`aws-cli` cookbook](./cookbook/aws-cli.md) for more invocations.
- [Endpoint URL convention](./endpoint-url.md) — pick path vs
  virtual-hosted style.
- [Authentication and bucket scope](./auth-and-scope.md) — the rules
  the proxy enforces.
