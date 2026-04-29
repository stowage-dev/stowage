---
type: reference
---

# Audit event catalogue

Every audit action emitted by the codebase. Sourced from the
literals in `internal/api/*.go` and the proxy server. Each row in the
audit table carries:

- `at` — UTC timestamp.
- `action` — string from the table below.
- `user_id` — empty for anonymous proxy events.
- `backend`, `bucket`, `key` — when applicable.
- `status` — `ok`, `error`, or a vendor-specific status string.
- `detail` — JSON blob with action-specific fields.

## Authentication

| Action | Emitted by | Notes |
|---|---|---|
| `auth.login` | `POST /auth/login/local`, OIDC callback | Status `ok` on success, `error` for any failure. |
| `auth.logout` | `POST /auth/logout` | Always `ok`. |

## Backends (UI-managed endpoints)

| Action | Emitted by | Notes |
|---|---|---|
| `backend.create` | `POST /api/admin/backends` | New endpoint saved. |
| `backend.update` | `PATCH /api/admin/backends/{bid}` | Endpoint edited. |
| `backend.delete` | `DELETE /api/admin/backends/{bid}` | Endpoint removed. |
| `backend.test` | `POST /api/admin/backends/test` | Test button clicked. |

## Bucket settings

| Action | Emitted by | Notes |
|---|---|---|
| `bucket.versioning.set` | `PUT .../versioning` | |
| `bucket.cors.set` | `PUT .../cors` | |
| `bucket.policy.set` | `PUT .../policy` | |
| `bucket.policy.delete` | `DELETE .../policy` | |
| `bucket.lifecycle.set` | `PUT .../lifecycle` | |
| `bucket.size_tracking.set` | `PUT .../size-tracking` | |

## Quotas

| Action | Emitted by | Notes |
|---|---|---|
| `quota.set` | `PUT .../quota` | |
| `quota.delete` | `DELETE .../quota` | |

## Object operations

| Action | Emitted by | Notes |
|---|---|---|
| `object.upload` | `POST .../object` | |
| `object.delete` | `DELETE .../object` | |
| `object.bulk_delete` | `POST .../objects/delete` | One row per request, with the keys in `detail`. |
| `object.delete_prefix` | `POST .../objects/delete-prefix` | Recursive. |
| `object.copy` | `POST .../object/copy` (same backend) | |
| `object.transfer` | `POST .../object/copy` (cross-backend) | |
| `object.copy_prefix` | `POST .../objects/copy-prefix` (same backend) | |
| `object.transfer_prefix` | `POST .../objects/copy-prefix` (cross-backend) | |

Note: tag and metadata writes are intentionally not audited per-call;
they're surfaced in the dashboard's request logs.

## Shares

| Action | Emitted by | Notes |
|---|---|---|
| `share.create` | `POST /api/shares` | |
| `share.revoke` | `DELETE /api/shares/{id}` | |
| `share.access` | `GET /s/{code}/info`, `POST /s/{code}/unlock`, `GET /s/{code}/raw` | One row per request. Detail carries the result code. |

## Embedded S3 proxy

| Action pattern | Source |
|---|---|
| `s3.proxy.<operation>` | Every proxy request. `<operation>` is lower-cased: `s3.proxy.getobject`, `s3.proxy.putobject`, `s3.proxy.headbucket`, `s3.proxy.listbuckets`, `s3.proxy.listobjects`, `s3.proxy.deleteobject`, `s3.proxy.deleteobjects`, `s3.proxy.copyobject`, `s3.proxy.createmultipartupload`, `s3.proxy.uploadpart`, `s3.proxy.completemultipartupload`, `s3.proxy.abortmultipart`, `s3.proxy.listmultipartuploads`, `s3.proxy.listparts`, `s3.proxy.headobject`, `s3.proxy.unknown`. |

The `detail` JSON includes `access_key_id`, `auth_mode` (`signed` /
`anonymous`), `result` (`ok`, `auth_failure`, `scope_violation`,
`quota_exceeded`, etc.).

## Sampling

Successful read-shaped proxy events (GET / HEAD with 2xx / 3xx) are
recorded only at rate `audit.sampling.proxy_success_read_rate`
(default 0.0). Writes, deletes, and any non-2xx response are always
recorded.

## Source

- API handlers: `internal/api/*.go` (every `audit.Event{Action: ...}`).
- Proxy: `internal/s3proxy/server.go::255` and `logging.go`.
- Recorder: `internal/audit/`.
