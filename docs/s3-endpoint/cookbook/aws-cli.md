---
type: how-to
---

# Cookbook: `aws-cli`

```sh
export AWS_ACCESS_KEY_ID='AKIA...'
export AWS_SECRET_ACCESS_KEY='...'
export AWS_REGION='us-east-1'
ENDPOINT='https://s3.stowage.example.com'
```

## Path-style config

`aws-cli` defaults to virtual-hosted style. Force path style globally:

```ini
# ~/.aws/config
[default]
s3 =
  addressing_style = path
```

Or per-command: every example below works with whichever style is
active.

## List

```sh
aws --endpoint-url $ENDPOINT s3 ls
aws --endpoint-url $ENDPOINT s3 ls s3://my-bucket/
aws --endpoint-url $ENDPOINT s3 ls s3://my-bucket/prefix/ --recursive
```

## Upload

```sh
# Single file:
aws --endpoint-url $ENDPOINT s3 cp ./file.bin s3://my-bucket/path/file.bin

# Recursive directory:
aws --endpoint-url $ENDPOINT s3 cp ./dir/ s3://my-bucket/dir/ --recursive

# Streaming from stdin:
some-pipeline | aws --endpoint-url $ENDPOINT s3 cp - s3://my-bucket/log.gz
```

## Download

```sh
aws --endpoint-url $ENDPOINT s3 cp s3://my-bucket/file.bin ./file.bin
aws --endpoint-url $ENDPOINT s3 cp s3://my-bucket/file.bin -   # to stdout
```

## Sync

```sh
aws --endpoint-url $ENDPOINT s3 sync ./local/ s3://my-bucket/remote/
```

## Multipart manually

`aws-cli` chunks large uploads automatically (default threshold 8 MiB,
chunk size 8 MiB). Tune with:

```ini
# ~/.aws/config
[default]
s3 =
  multipart_threshold = 64MB
  multipart_chunksize = 16MB
```

## Pre-signed URLs

```sh
aws --endpoint-url $ENDPOINT s3 presign s3://my-bucket/file.bin --expires-in 3600
```

The URL works against the proxy without further authentication. It's
scope-limited to the same credential's scope — a presign over a
bucket you can't access fails.

## Useful checks

```sh
# What buckets do I have access to?
aws --endpoint-url $ENDPOINT s3api list-buckets

# Bucket exists & I can reach it?
aws --endpoint-url $ENDPOINT s3api head-bucket --bucket my-bucket

# What's in this object?
aws --endpoint-url $ENDPOINT s3api head-object --bucket my-bucket --key file.bin
```

## Troubleshooting

- `An error occurred (SignatureDoesNotMatch)` → wrong secret key, or
  clock skew between client and proxy. Check NTP.
- `An error occurred (AccessDenied)` → bucket not in your credential's
  scope. Get a credential from your admin that includes the bucket.
- `An error occurred (EntityTooLarge)` → bucket hit its hard quota.
  See [Quota errors](../quota-errors.md).
- Slow uploads on tiny objects → that's normal. The per-request
  overhead is ~5 ms; small files don't amortise.
