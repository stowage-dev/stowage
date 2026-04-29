---
type: reference
---

# HTTP API

Generated from
[`internal/api/router.go`](https://github.com/stowage-dev/stowage/blob/main/internal/api/router.go).
Every authenticated mutation requires both a session cookie and a
`X-CSRF-Token` header that matches the `stowage_csrf` cookie.

Errors are JSON-shaped:

```json
{ "error": { "code": "rate_limited", "message": "...", "detail": "" } }
```

For the full code list, see [Error codes](./error-codes.md).

## Public endpoints

| Method | Path | Purpose |
|---|---|---|
| GET | `/healthz` | Liveness probe. |
| GET | `/readyz` | Readiness probe. |
| GET | `/metrics` | Prometheus scrape (no auth — gate at the proxy). |

## Auth

| Method | Path | Purpose |
|---|---|---|
| POST | `/auth/login/local` | Local username + password. Per-IP rate-limited. |
| GET | `/auth/login/oidc` | Begin the OIDC redirect dance. |
| GET | `/auth/callback` | OIDC callback. |
| POST | `/auth/logout` | Clear the session. |

## `/api` root

| Method | Path | Auth | Purpose |
|---|---|---|---|
| GET | `/api/auth/config` | none | Lists enabled auth modes and OIDC start URL. |
| GET | `/api/me` | session | Current user. |
| POST | `/api/me/password` | session + CSRF | Change own password. |
| GET | `/api/search` | session | Cross-backend bucket+prefix search. |

## Pinned buckets

| Method | Path | Role | Purpose |
|---|---|---|---|
| GET | `/api/me/pins` | any | List your pinned buckets. |
| POST | `/api/me/pins` | user/admin | Pin a `(backend, bucket)`. |
| DELETE | `/api/me/pins/{bid}/{bucket}` | user/admin | Unpin. |

## Self-service S3 virtual credentials

Available when `s3_proxy.enabled: true`.

| Method | Path | Role | Purpose |
|---|---|---|---|
| GET | `/api/me/s3-credentials` | any | List your own virtual credentials. |
| POST | `/api/me/s3-credentials` | user/admin | Mint one. |
| PATCH | `/api/me/s3-credentials/{akid}` | user/admin | Update description / scope / disable. |
| DELETE | `/api/me/s3-credentials/{akid}` | user/admin | Revoke. |

## Backends and bucket data

| Method | Path | Role | Purpose |
|---|---|---|---|
| GET | `/api/backends` | any | List configured backends + status. |
| GET | `/api/backends/{bid}` | any | One backend's status. |
| GET | `/api/backends/{bid}/health` | any | Force a probe and return the result. |
| GET | `/api/backends/{bid}/buckets` | any | List buckets on a backend. |
| POST | `/api/backends/{bid}/buckets` | admin | Create a bucket. |
| DELETE | `/api/backends/{bid}/buckets/{bucket}` | admin | Delete a bucket. |

## Bucket settings (admin-only)

| Method | Path | Purpose |
|---|---|---|
| GET / PUT | `/api/backends/{bid}/buckets/{bucket}/versioning` | Get / set versioning. |
| GET / PUT | `/api/backends/{bid}/buckets/{bucket}/cors` | Get / set CORS. |
| GET / PUT / DELETE | `/api/backends/{bid}/buckets/{bucket}/policy` | Bucket policy. |
| GET / PUT | `/api/backends/{bid}/buckets/{bucket}/lifecycle` | Lifecycle rules. |
| GET / PUT / DELETE | `/api/backends/{bid}/buckets/{bucket}/quota` | Quota. |
| POST | `/api/backends/{bid}/buckets/{bucket}/quota/recompute` | Force a quota scan. |
| PUT | `/api/backends/{bid}/buckets/{bucket}/size-tracking` | Per-bucket size-tracking toggle. |

## Bucket size endpoints

| Method | Path | Role | Purpose |
|---|---|---|---|
| GET | `/api/backends/{bid}/buckets/{bucket}/size-tracking` | any | Read current size-tracking state. |
| GET | `/api/backends/{bid}/buckets/{bucket}/prefix-size` | any | Sum bytes under a prefix. |

## Object operations

All paths under `/api/backends/{bid}/buckets/{bucket}/`.

| Method | Path | Role | Purpose |
|---|---|---|---|
| GET | `/objects` | any | List objects. |
| POST | `/objects/delete` | user/admin | Bulk delete. |
| POST | `/objects/delete-prefix` | user/admin | Recursive delete. |
| POST | `/objects/folder` | user/admin | Create a "folder" (zero-byte placeholder). |
| POST | `/objects/copy-prefix` | user/admin | Recursive copy (intra- or cross-backend). |
| GET | `/objects/zip` | any | Stream a zip of selected keys. |
| GET / HEAD | `/object` | any | Get / head one object. |
| GET | `/object/info` | any | Equivalent to HEAD. |
| POST | `/object` | user/admin | Upload (small file). |
| DELETE | `/object` | user/admin | Delete. |
| POST | `/object/copy` | user/admin | Single-object copy (intra- or cross-backend). |
| GET / PUT | `/object/tags` | varies | Get / set tags. |
| PUT | `/object/metadata` | user/admin | Update user metadata in place. |
| GET | `/object/versions` | any | List versions. |

## Multipart

All under `/api/backends/{bid}/buckets/{bucket}/multipart/`.

| Method | Path | Role | Purpose |
|---|---|---|---|
| GET | `/` | any | List in-progress uploads. |
| POST | `/` | user/admin | Initiate. |
| DELETE | `/` | user/admin | Abort. |
| POST | `/complete` | user/admin | Complete. |
| PUT | `/parts/{part}` | user/admin | Upload one part. |

## Shares

| Method | Path | Role | Purpose |
|---|---|---|---|
| GET | `/api/shares` | any | List shares (your own; admins can switch to "all"). |
| POST | `/api/shares` | user/admin | Create. |
| DELETE | `/api/shares/{id}` | user/admin | Revoke. |

## Public share endpoints

Per-IP rate-limited (default 10 req/min).

| Method | Path | Purpose |
|---|---|---|
| GET | `/s/{code}/info` | Share metadata. |
| POST | `/s/{code}/unlock` | Submit password. |
| GET | `/s/{code}/raw` | Stream the file. |

The bare `/s/{code}` falls through to the SvelteKit SPA, which
renders the recipient page.

## Admin: dashboard / backends / users

All under `/api/admin/`. Admin role required.

| Method | Path | Purpose |
|---|---|---|
| GET | `/dashboard` | The admin dashboard data. |
| GET | `/backends/health` | All backends with probe history. |
| GET | `/audit` | Audit log query. |
| GET | `/audit.csv` | Audit log CSV export. |
| GET | `/users` | List users. |
| POST | `/users` | Create user. |
| GET | `/users/{id}` | One user. |
| PATCH | `/users/{id}` | Update. |
| POST | `/users/{id}/reset-password` | Admin password reset. |
| POST | `/users/{id}/unlock` | Unlock a locked-out account. |
| DELETE | `/users/{id}` | Delete. |
| GET | `/backends` | List UI-managed endpoints. |
| POST | `/backends` | Create one. |
| POST | `/backends/test` | Test new credentials. |
| GET | `/backends/{bid}` | Detail. |
| PATCH | `/backends/{bid}` | Update. |
| DELETE | `/backends/{bid}` | Delete. |

## Admin: S3 proxy management

Available when `s3_proxy.enabled: true`. All admin role.

| Method | Path | Purpose |
|---|---|---|
| GET | `/api/admin/s3-credentials` | List virtual credentials. |
| POST | `/api/admin/s3-credentials` | Mint. |
| PATCH | `/api/admin/s3-credentials/{akid}` | Update. |
| DELETE | `/api/admin/s3-credentials/{akid}` | Revoke. |
| GET | `/api/admin/s3-anonymous` | List anonymous bindings. |
| POST | `/api/admin/s3-anonymous` | Add or update. |
| DELETE | `/api/admin/s3-anonymous/{bid}/{bucket}` | Remove. |
| GET | `/api/admin/s3-proxy/credentials` | Read-only merged view (SQLite + Kubernetes). |
| GET | `/api/admin/s3-proxy/anonymous` | Read-only merged anonymous view. |

## CSRF & session details

See [Self-host → Sessions](../self-host/auth/sessions.md).

## Status codes summary

| Code | Meaning |
|---|---|
| 200 | OK |
| 204 | No content |
| 400 | Validation failure (`bad_request`, `invalid_*`) |
| 401 | No / invalid session (`unauthorized`) |
| 403 | RBAC / CSRF / scope (`forbidden`, `csrf_invalid`, `self_role_change`) |
| 404 | Object / bucket / user / backend not found |
| 409 | Conflict (`username_taken`, `id_taken`, `last_admin`, `yaml_managed`) |
| 410 | Share expired or revoked |
| 429 | Rate-limited |
| 500 | Internal error |
| 503 | `secret_key_unset` or `store_unavailable` |
| 507 | Bucket quota exceeded (`quota_exceeded`) |
