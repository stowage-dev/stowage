---
type: how-to
---

# CORS

CORS rules let browser-based applications fetch objects directly from
the bucket. Stowage presents the upstream's CORS configuration as a
list of rules.

## Edit

Open the bucket, then **Settings → CORS**. Stowage shows the
current rule list. Add a rule with:

- **Allowed origins** — list. Use `*` for any.
- **Allowed methods** — `GET`, `PUT`, `POST`, `DELETE`, `HEAD`.
- **Allowed headers** — list. Often `*` or `Authorization`,
  `Content-Type`, `Range`.
- **Exposed headers** — list. Browser code can only read these from
  the response; usually `ETag`, `Content-Range`.
- **Max age (seconds)** — preflight cache TTL.

Save runs `SetBucketCORS`. Audit action: `bucket.cors.set`.

## When you need this

- A web app fetching objects directly from the bucket via the AWS SDK
  for JavaScript.
- A static site generator pulling assets from S3.
- The Stowage object browser itself **does not** require CORS — it
  fetches via the dashboard's `/api/*` proxy on the same origin.

## When you don't need this

If you only access the bucket via Stowage's UI, server-side, or via
SDKs running outside browsers, you don't need CORS at all.

## Backend support

`Capabilities.CORS=true` gates the UI. Most backends support CORS;
CloudFlare R2 supports it too with the AWS-shaped API.
