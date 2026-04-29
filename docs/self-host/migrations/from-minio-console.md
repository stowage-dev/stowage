---
type: how-to
---

# Migrating from the MinIO Console

The single most common reason people land on Stowage is dissatisfaction
with the MinIO Console after the May 2025 administrative-features
removal. This page maps Console features to Stowage equivalents and
calls out the edges.

## What's preserved

You **do not have to migrate object data** to switch to Stowage. The
data already lives on the upstream backend. Stowage proxies access to
it.

That means the migration is a control-plane swap, not a data move:

1. Keep MinIO running.
2. Stand up Stowage in front of it.
3. Point users at Stowage's URL instead of the MinIO Console.
4. Stop using MinIO Console for day-to-day administration.

## Feature mapping

| MinIO Console | Stowage equivalent | Notes |
|---|---|---|
| Object browser | `/b/<backend>/<bucket>/` | Multi-select, preview, drag-drop multipart upload, version history, tags + metadata. |
| Public share with expiry | Stowage shares (`/api/shares`, `/s/<code>`) | Password (argon2id), atomic download cap, revocable. |
| Bucket settings (versioning, lifecycle, policy, CORS) | Bucket → Settings | Same JSON format on the wire. |
| Bucket quotas | Bucket → Settings → Quota | Stowage enforces the cap at the proxy; MinIO's own quota stays disabled. |
| Access keys | `/admin/s3-proxy` (virtual credentials) | Stowage mints credentials that route through its proxy with bucket scope. **The upstream MinIO admin keys never reach tenants.** |
| Identity provider integration | `auth.modes: [oidc]` | Stowage handles the OIDC flow; MinIO no longer needs IdP integration. |
| Audit log | `/admin/audit` | SQLite-backed; CSV export. |
| Prometheus metrics | `/metrics` | Stowage's own metrics, not MinIO's. Run both Prometheus targets if you want both views. |

## Feature mapping for things that aren't equivalent

| MinIO Console | Stowage answer |
|---|---|
| MinIO user / group / policy editor | Not implemented yet (`Capabilities.AdminAPI=""` for the MinIO driver). For now, manage MinIO users with `mc` and Stowage virtual credentials with the dashboard. See [Roadmap](../../explanations/roadmap.md). |
| Tier / batch / replication | Not implemented in Stowage. Continue using `mc` for these. |
| Direct upstream PUT via the Console | Tenants point AWS SDKs at Stowage's `:8090` instead, with virtual credentials. |
| Console "registered cluster" telemetry | None. Stowage doesn't phone home. |

## Step-by-step migration

### 1. Stand up Stowage

Use any of the [quickstarts](../../getting-started/). Configure your
existing MinIO as a YAML backend:

```yaml
backends:
  - id: prod
    name: "Production MinIO"
    type: s3v4
    endpoint: https://minio.example.com
    region: us-east-1
    access_key_env: MINIO_ROOT_USER
    secret_key_env: MINIO_ROOT_PASSWORD
    path_style: true
```

The credentials Stowage uses are MinIO admin credentials (or
near-admin scope). Stowage uses them to perform bucket operations and
to re-sign tenant requests through the proxy.

### 2. Mirror the auth setup

If you used Console with OIDC, set up
[OIDC](../auth/oidc.md) on Stowage with the same provider. Map the
groups you used in Console's policy attachments to Stowage's three
roles (`admin`, `user`, `readonly`).

If you used Console with local users, create them in Stowage's
`/admin/users`. Stowage doesn't import MinIO users.

### 3. Migrate tenants from upstream creds to virtual creds

For each tenant that previously held a MinIO `mc admin user add`
credential:

1. Mint a Stowage virtual credential (`/admin/s3-proxy`) scoped to
   the buckets that tenant should reach.
2. Hand the new `access_key` / `secret_key` pair to the tenant.
3. Have them swap the credentials in their SDK config and point the
   `--endpoint-url` at Stowage's `:8090`.
4. Once they confirm everything works, delete the upstream MinIO
   user.

### 4. Configure quotas in Stowage

If you relied on MinIO's bucket quota, configure the equivalent in
Stowage (Bucket → Settings → Quota). Stowage's quota is independent
and proxy-enforced; you can leave MinIO's own quota disabled.

### 5. Mirror lifecycle / policy / CORS

These are 1:1 — the JSON shapes Stowage sends are identical to what
the Console sent. You can copy-paste from `mc admin bucket info`.

### 6. Point users at the new URL

Update DNS or a redirect from `minio.example.com` to
`stowage.example.com`. Tenants stop visiting the Console.

### 7. Decide what to do with the Console

You can leave it disabled, or leave it accessible only from inside
your management network as an emergency fallback. Stowage doesn't
require the Console to be off; the two can coexist as long as
tenants stop using it for day-to-day operations.

## What you give up

- The Console's "managed" features that depend on MinIO Enterprise.
- The Console's user / policy editor (until the Stowage `minio`
  driver lands).
- Anything that depends on touching the upstream's admin API directly
  — `mc` is still the answer for those.

## What you gain

- A vendor-neutral dashboard that works against any S3-compatible
  backend.
- A structural license commitment (AGPL-3.0-or-later, no community
  edition) that the dashboard will not be quietly stripped down later.
  See [Why AGPL](../../explanations/why-agpl.md).
- Per-tenant credentials that route through a proxy with audit and
  quota enforcement, not raw MinIO admin keys.
- Share links with passwords and download caps.
- One audit log across multiple backends, instead of one per
  upstream.
