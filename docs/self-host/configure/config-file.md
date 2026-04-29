---
type: how-to
---

# The config file and env-var overrides

Stowage reads a single YAML file at the path you pass with `--config`.
Every key has a sane default, so a real config can stay short. A
small set of operationally-important values can also be overridden by
environment variables — useful for container deployments and for
keeping secrets out of files on disk.

## Minimum viable config

```yaml
server:
  listen: ":8080"
  secret_key_file: /var/lib/stowage/secret.key

db:
  driver: sqlite
  sqlite:
    path: /var/lib/stowage/stowage.db

auth:
  modes: [local]

backends:
  - id: prod
    name: "Prod MinIO"
    type: s3v4
    endpoint: https://minio.example.com
    region: us-east-1
    access_key_env: PROD_ACCESS_KEY
    secret_key_env: PROD_SECRET_KEY
    path_style: true
```

That's it. The remaining defaults (session lifetime, log format,
session rate-limit ceiling, audit sampling) come from
[`internal/config/config.go`](https://github.com/stowage-dev/stowage/blob/main/internal/config/config.go).

For the exhaustive list of every key with type and default, see
**[Reference → Configuration](../../reference/config.md)**.

## How the loader merges values

Stowage applies values in this order, last writer wins:

1. Defaults from `config.Defaults()`.
2. Values in the YAML file you passed via `--config`.
3. Environment-variable overrides.
4. Built-in validation runs at the end and refuses invalid combinations
   (for example, `s3_proxy.listen` must differ from `server.listen` if
   `s3_proxy.enabled` is true).

If validation fails, `stowage serve` exits with a non-zero status and
a message naming the offending key.

## Environment variables that override the file

| Variable | Overrides | Notes |
|---|---|---|
| `STOWAGE_LISTEN` | `server.listen` | Useful in containers. |
| `STOWAGE_PUBLIC_URL` | `server.public_url` | Used to build absolute share URLs. |
| `STOWAGE_LOG_LEVEL` | `log.level` | `debug`, `info`, `warn`, `error`. |
| `STOWAGE_LOG_FORMAT` | `log.format` | `json` or `text`. |
| `STOWAGE_SQLITE_PATH` | `db.sqlite.path` | |
| `STOWAGE_SECRET_KEY_FILE` | `server.secret_key_file` | |
| `STOWAGE_SECRET_KEY` | (no key) | The AES-256 root key directly: 64 hex chars or 44 base64 chars. Takes precedence over the file. |
| `STOWAGE_PPROF_LISTEN` | (no key) | If set, exposes `/debug/pprof/*` on the given address. Off by default. |

Backend access keys and OIDC client secrets always come from env vars
referenced by `*_env` config fields — Stowage never reads them from
the YAML directly. That keeps secrets out of files on disk and out of
your version control.

## Where to put it

Convention is `/etc/stowage/config.yaml`, mode 0640, owned by the
service user. The Helm chart renders it into a ConfigMap; the Docker
image expects it at `/etc/stowage/config.yaml`.

## Editing live

There is no SIGHUP. Restart `stowage` to pick up config changes, or
use the dashboard (`/admin/endpoints`) for backend changes that don't
require a restart.

## Validation cheatsheet

The loader rejects any of these:

- `server.listen` is empty.
- `log.format` is anything other than `json`, `text`, or empty.
- `log.level` is anything other than `debug`, `info`, `warn`, `error`,
  or empty.
- `db.driver` is `postgres` (not yet implemented) or anything other
  than `sqlite` / empty.
- `db.driver` is `sqlite` and `db.sqlite.path` is empty.
- `auth.modes` is empty, or contains anything other than `local`,
  `oidc`, `static`.
- Two backends share the same `id`.
- A backend has an empty `id` or an empty `type`.
- `s3_proxy.enabled` is true and `s3_proxy.listen` is empty or equal
  to `server.listen`.

## Next

- [Logging](./logging.md)
- [SQLite path and lifecycle](./sqlite.md)
- [Reference → Configuration](../../reference/config.md) — every key.
