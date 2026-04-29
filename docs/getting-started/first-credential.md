---
type: tutorial
---

# Your first virtual S3 credential

Five minutes from logging in to a tenant credential that works against
the embedded SigV4 proxy. Assumes you've enabled the proxy
(`s3_proxy.enabled: true` in your config) and have admin rights.

## 1. Open the proxy admin page

Navigate to `/admin/s3-proxy`. You'll see a tabbed view:

- **Credentials** — virtual access keys and the buckets they're scoped
  to.
- **Anonymous bindings** — buckets opened to unauthenticated reads.

## 2. Mint a credential

On the Credentials tab, click **Create credential**. Fill in:

- **Description** — human-readable label (`alice-uploader`).
- **Bucket scopes** — pick one or more `(backend, bucket)` pairs. The
  credential will receive 403s on anything else.
- **Quota inheritance** — leave default (the credential inherits the
  bucket's quota).
- **Expiry** — optional.

Click **Create**. The dashboard shows the new credential's
`access_key_id` and `secret_access_key` *once* — copy both, the
secret is not retrievable later.

## 3. Configure `aws-cli`

```sh
export AWS_ACCESS_KEY_ID=AKIA…
export AWS_SECRET_ACCESS_KEY=…
export AWS_REGION=us-east-1
```

## 4. Hit the proxy

```sh
aws --endpoint-url http://localhost:8090 s3 ls
```

You'll see only the buckets you scoped the credential to —
`ListBuckets` is synthesised by the proxy from the credential's scope
list and never reaches the upstream.

Upload a file:

```sh
echo 'hello' > hello.txt
aws --endpoint-url http://localhost:8090 \
  s3 cp ./hello.txt s3://<one-of-your-scoped-buckets>/hello.txt
```

And try a denied bucket — you should get a fast 403:

```sh
aws --endpoint-url http://localhost:8090 \
  s3 ls s3://some-other-bucket
```

## 5. See the audit trail

Back in the dashboard, go to `/admin/audit` and filter by action
`s3.proxy.`. Successful reads default to *not* being audited (see
[Explanations → Audit sampling](../explanations/audit-sampling.md))
but writes, deletes, and any rejected request are recorded. The
credential's `access_key_id` is in the `detail` JSON column.

## 6. Revoke

Back at `/admin/s3-proxy`, find your credential and click **Revoke**.
The next SDK call gets a 401 immediately — the proxy reads from a
small in-memory cache backed by the SQLite store, with cache
invalidation on every mutation.

## What you've learned

- How tenants get scoped, audit-able, quota-checked credentials
  without ever seeing the upstream admin keys.
- `ListBuckets` is synthesised — a tenant only sees what they were
  granted.
- Revocation is immediate, and the audit log records every action with
  the access key ID attached.

## Next step

- [Use as an S3 endpoint →](../s3-endpoint/) — the tenant-developer
  documentation set: cookbook, error semantics, supported operations.
- [Self-host → Set bucket quotas](../self-host/buckets/quotas.md) —
  the soft / hard cap mechanics.
