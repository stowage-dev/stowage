---
type: how-to
---

# Anonymous read endpoints

Some buckets are configured for anonymous read through the proxy.
You don't need credentials to read them; you do need a network
route.

## Identifying an anonymous bucket

Your operator tells you. There's no auto-discovery — anonymous
bindings aren't listed in `ListBuckets` (which is synthesised from a
specific credential's scope, and anonymous requests have no
credential).

## Fetch an object anonymously

```sh
curl -fsSL https://s3.stowage.example.com/public-bucket/path/to/file.bin -o file.bin
```

Or with `aws-cli` configured with `--no-sign-request`:

```sh
aws --endpoint-url https://s3.stowage.example.com \
  --no-sign-request \
  s3 cp s3://public-bucket/path/to/file.bin -
```

## What's allowed

Hard-coded read-only operation allowlist:

- `GetObject`
- `HeadObject`
- `ListObjectsV2`

Everything else returns 401.

## Per-source-IP rate limiting

Anonymous bindings carry a per-client-IP RPS cap (configured by the
operator, default 20). If you exceed it, the proxy returns 429
`SlowDown` with a `Retry-After` header.

The cap is per source IP, so a hostile client behind one IP can't
saturate the bucket for everyone. If your workload needs sustained
high RPS from one host, ask the operator for either a higher cap or
an authenticated credential.

## Pre-signed URLs vs anonymous

Two related-but-different patterns:

- **Anonymous binding** — the bucket is open to the public. Anyone
  with the URL can read.
- **Pre-signed URL** — a cryptographic token that grants short-lived
  access to one specific object. The bucket itself stays closed.

If you only want to share *one* file, ask your operator for a Stowage
share link instead of an anonymous binding. Share links carry
expiry, optional password, and a download cap.

## Behaviour through the dashboard share URL

`/s/<code>` is a Stowage share, not an anonymous proxy binding. The
URL is unrelated to the upstream bucket name; the recipient
experience is described at
[Self-host → Sharing → recipient page](../self-host/sharing/recipient.md).

If you've been given a `/s/<code>` URL, you don't need this page —
just open it in a browser.
