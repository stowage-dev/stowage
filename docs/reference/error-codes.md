---
type: reference
---

# Error codes

The dashboard API returns errors with this shape:

```json
{
  "error": {
    "code": "rate_limited",
    "message": "...",
    "detail": ""
  }
}
```

`code` is a stable string. `message` is human-readable. `detail` is
optional and may carry validation specifics.

## Catalogue

Sourced from `internal/api/*.go` (every `writeError(...)` call).

| Code | Typical status | Meaning |
|---|---|---|
| `account_locked` | 429 | Too many failed logins; user is locked for `auth.local.lockout.window`. |
| `backend_error` | 500 / 502 | Upstream backend returned an error or was unreachable. `detail` may carry the upstream message. |
| `bad_range` | 400 | Range request was malformed or unsatisfiable. |
| `bad_request` | 400 | Generic validation failure. `detail` describes what. |
| `conflict` | 409 | Generic conflict. |
| `csrf_invalid` | 403 | Missing or mismatched `X-CSRF-Token`. |
| `email_taken` | 409 | Another user already has this email. |
| `exhausted` | 410 | Share's download limit has been reached. |
| `expired` | 410 | Share has passed its `expires_at`. |
| `forbidden` | 403 | RBAC denied the operation. |
| `id_taken` | 409 | A backend with this `id` already exists. |
| `internal` | 500 | Unrecoverable handler failure. Check logs. |
| `invalid_bucket` | 400 | Bucket name failed validation. |
| `invalid_bucket_name` | 400 | Bucket name failed AWS-style naming rules. |
| `invalid_cors` | 400 | CORS config rejected (e.g. malformed JSON). |
| `invalid_credentials` | 401 | Wrong password / unknown username. |
| `invalid_key` | 400 | Object key failed validation (control chars, leading `/`, etc.). |
| `invalid_lifecycle` | 400 | Lifecycle rules rejected. |
| `invalid_metadata` | 400 | User metadata rejected (size or character constraints). |
| `invalid_policy` | 400 | Bucket policy rejected by upstream or pre-validator. |
| `invalid_prefix` | 400 | Prefix parameter failed validation. |
| `invalid_quota` | 400 | Quota values rejected (e.g. soft > hard). |
| `invalid_request` | 400 | Generic invalid-request. |
| `invalid_tag` | 400 | Object tag rejected. |
| `last_admin` | 409 | Refusing to remove or downgrade the last remaining admin. |
| `length_required` | 411 | Request needed a `Content-Length`. |
| `mode_disabled` | 403 | Auth mode disabled in config but invoked anyway. |
| `not_found` | 404 | Resource not found. |
| `not_local` | 400 | Operation requires a local user (e.g. password change for an OIDC user). |
| `not_supported` | 400 | Operation valid but the backend's `Capabilities` doesn't list it. |
| `object_exists` | 409 | Refused to overwrite an existing object. |
| `oidc_failed` | 400 / 500 | OIDC flow failed (token verification, role mapping, discovery). |
| `password_change_required` | 403 | User has `must_change_pw=true`; rotate before continuing. |
| `password_mismatch` | 401 | Old password didn't match in a self-service rotate. |
| `password_required` | 401 | The share has a password and the unlock cookie is missing. |
| `quota_exceeded` | 507 | Bucket would exceed its hard quota. |
| `rate_limited` | 429 | Per-session, per-IP, or per-key rate limit hit. `Retry-After` header set. |
| `register_failed` | 409 | Backend created in DB but registry registration failed. |
| `revoked` | 410 | Share has been revoked. |
| `secret_key_unset` | 503 | A handler that needs the AES-256 root key was called and the key isn't set. |
| `self_delete` | 403 | Refusing to let a user delete their own account. |
| `self_role_change` | 403 | Refusing to let a user change their own role. |
| `session_error` | 500 | Session attach / mint failed. |
| `size_tracking_disabled` | 400 | Per-bucket size tracking is off; the requested operation needs it on. |
| `static_user` | 403 | Refusing to mutate the static-config user via the API. |
| `store_unavailable` | 503 | Endpoint store not configured (no `STOWAGE_SECRET_KEY`). |
| `too_large` | 413 | Body exceeded a per-route size limit. |
| `too_many_keys` | 400 | Bulk-delete request listed too many keys. |
| `too_many_tags` | 400 | More than the per-object tag limit. |
| `unauthorized` | 401 | No session, expired session, or session revoked. |
| `username_taken` | 409 | Another user already has this username. |
| `weak_password` | 400 | Password didn't satisfy the policy. |
| `yaml_managed` | 409 | Refusing to mutate a YAML-defined backend through the UI. |

## S3 proxy errors

The proxy returns AWS-shaped XML errors, not the JSON shape above.
See [S3 proxy → errors](./s3-proxy/errors.md).
