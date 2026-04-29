---
type: how-to
---

# Managing endpoints from the dashboard

The `/admin/endpoints` page lets admins add, edit, disable, delete,
and test S3-compatible backends without restarting Stowage. Each
endpoint's secret key is sealed at rest with AES-256-GCM under the
master key.

## Prerequisites

Stowage must have an AES-256 root key configured:

- `STOWAGE_SECRET_KEY` env var (64 hex chars or 44 base64 chars), or
- `server.secret_key_file` pointing at a file containing the same.

Without a key, every UI-managed endpoint handler returns 503
`secret_key_unset`. YAML-defined backends still work — they don't
carry sealed secrets.

## Add an endpoint

1. Go to `/admin/endpoints`.
2. Click **Add endpoint**.
3. Fill in the same fields as the YAML form (`id`, `name`, `type`,
   `endpoint`, `region`, `access_key`, `secret_key`, `path_style`).
4. Click **Test** before saving. The dashboard runs `ListBuckets`
   against the new credentials and surfaces the result.
5. Click **Save**.

The new backend is registered in the live registry immediately. No
restart.

## Edit an endpoint

UI-managed endpoints have an edit button on every row. YAML-managed
endpoints render with a `config` badge and are read-only.

When you change credentials, Stowage:

1. Validates the new credentials with a `ListBuckets` call.
2. Re-seals the secret key with the master key.
3. Replaces the entry in the registry. The probe history is reset
   because the new client may point at a different endpoint.

## Disable / delete

- **Disable** keeps the row but skips it from the live registry. Useful
  for short-term outages without losing the configuration.
- **Delete** drops the row entirely. Sealed secrets are removed from
  SQLite.

## ID collisions with YAML

If a YAML-defined backend and a UI row share an `id`, the YAML one
wins. The UI surfaces this with a 409 `yaml_managed` error if you try
to create a colliding row.

## Audit trail

Every action emits an audit row:

| Action | When |
|---|---|
| `backend.create` | New endpoint saved. |
| `backend.update` | Endpoint edited. |
| `backend.delete` | Endpoint removed. |
| `backend.test` | Test button clicked. |

Filter `/admin/audit` by `action=backend.` to find them.

## Source

- Handler: [`internal/api/admin_backends.go`](https://github.com/stowage-dev/stowage/blob/main/internal/api/admin_backends.go)
- Sealing: [`internal/secrets/`](https://github.com/stowage-dev/stowage/tree/main/internal/secrets)
- Storage: backed by the same SQLite store as the rest of Stowage's
  state.
