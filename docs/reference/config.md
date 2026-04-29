---
type: reference
---

# Configuration

Every YAML key with type, default, and effect. Source of truth:
[`internal/config/config.go`](https://github.com/stowage-dev/stowage/blob/main/internal/config/config.go).

## Top-level structure

```yaml
server:    {...}
log:       {...}
db:        {...}
auth:      {...}
backends:  [{...}, ...]
quotas:    {...}
ratelimit: {...}
s3_proxy:  {...}
audit:     {...}
```

## `server`

| Key | Type | Default | Notes |
|---|---|---|---|
| `server.listen` | string | `:8080` | HTTP listen address for the dashboard. Required. |
| `server.shutdown_timeout` | duration | `10s` | Drain window after SIGTERM/SIGINT. |
| `server.public_url` | string | "" | Used to render absolute share URLs. Optional. |
| `server.trusted_proxies` | []CIDR | `[]` | CIDRs whose `X-Forwarded-*` headers are honoured. Empty = trust every immediate peer. |
| `server.secret_key_file` | string | "" | Path to a file holding the AES-256 root key. Auto-generated mode 0600 if missing. |

## `log`

| Key | Type | Default | Notes |
|---|---|---|---|
| `log.level` | string | `info` | `debug`, `info`, `warn`, `error`. |
| `log.format` | string | `json` | `json` or `text`. |

## `db`

| Key | Type | Default | Notes |
|---|---|---|---|
| `db.driver` | string | `sqlite` | Only `sqlite` is implemented today. |
| `db.sqlite.path` | string | `./stowage.db` | Path to the SQLite database file. |

## `auth`

```yaml
auth:
  modes: [local]
  session:
    lifetime: 8h
    idle_timeout: 1h
  local:
    allow_self_registration: false
    require_admin_approval: false
    password:
      min_length: 12
      prevent_reuse: true
    lockout:
      max_attempts: 5
      window: 15m
    reset_email:
      enabled: false
  oidc:
    issuer: ""
    client_id: ""
    client_secret_env: ""
    scopes: [openid, profile, email]
    role_claim: ""
    role_mapping: {}
  static:
    enabled: false
    username: ""
    password_hash_env: ""
```

| Key | Type | Default | Notes |
|---|---|---|---|
| `auth.modes` | []string | `[local]` | At least one of `local`, `oidc`, `static`. |
| `auth.session.lifetime` | duration | `8h` | Hard ceiling. |
| `auth.session.idle_timeout` | duration | `1h` | Idle-out window. |
| `auth.local.password.min_length` | int | `12` | Minimum password length. |
| `auth.local.password.prevent_reuse` | bool | `true` | Reject changes that match the current password. |
| `auth.local.lockout.max_attempts` | int | `5` | Per-user failed-login limit before lockout. |
| `auth.local.lockout.window` | duration | `15m` | Lockout window. |
| `auth.oidc.issuer` | string | "" | OIDC issuer URL. Required when mode includes `oidc`. |
| `auth.oidc.client_id` | string | "" | OIDC client ID. |
| `auth.oidc.client_secret_env` | string | "" | Name of env var holding the client secret. |
| `auth.oidc.scopes` | []string | `[openid, profile, email]` | |
| `auth.oidc.role_claim` | string | "" | ID-token claim listing groups. |
| `auth.oidc.role_mapping` | map[role]→[]group | `{}` | Stowage role → group strings. |
| `auth.static.enabled` | bool | false | |
| `auth.static.username` | string | "" | The static account's username. |
| `auth.static.password_hash_env` | string | "" | Env var holding the argon2id hash. |

## `backends`

A list. Each entry:

| Key | Type | Notes |
|---|---|---|
| `id` | string | Unique stable identifier. Required. |
| `name` | string | Human-readable label. |
| `type` | string | Driver. Today only `s3v4`. |
| `endpoint` | string | Base URL of the upstream API. |
| `region` | string | AWS region or backend's region label. |
| `access_key_env` | string | Env var holding the access key. |
| `secret_key_env` | string | Env var holding the secret key. |
| `path_style` | bool | Use path-style addressing. |

## `quotas`

| Key | Type | Default | Notes |
|---|---|---|---|
| `quotas.scan_interval` | duration | `30m` | Scheduled re-count cadence. Negative = disabled. |

## `ratelimit`

| Key | Type | Default | Notes |
|---|---|---|---|
| `ratelimit.api_per_minute` | int | `600` | Per-session req/min on `/api/*`. 0 disables. |

## `s3_proxy`

| Key | Type | Default | Notes |
|---|---|---|---|
| `s3_proxy.enabled` | bool | false | Master switch for the proxy. |
| `s3_proxy.listen` | string | `:8090` | Bind address for the proxy. Must differ from `server.listen`. |
| `s3_proxy.host_suffixes` | []string | `[]` | Virtual-hosted-style host suffixes (e.g. `s3.example.com`). |
| `s3_proxy.global_rps` | float | 0 | Total RPS ceiling across all credentials. 0 = unlimited. |
| `s3_proxy.per_key_rps` | float | 0 | Per-credential RPS ceiling. 0 = unlimited. |
| `s3_proxy.anonymous_enabled` | bool | false | Cluster-wide kill switch for anonymous reads. |
| `s3_proxy.anonymous_rps` | float | 20 | Per-source-IP RPS default for anonymous reads. |
| `s3_proxy.kubernetes.enabled` | bool | false | Read virtual creds from a Kubernetes Secret informer. |
| `s3_proxy.kubernetes.namespace` | string | `stowage-system` | Namespace holding the operator-written Secrets. |
| `s3_proxy.kubernetes.kubeconfig` | string | "" | Optional kubeconfig path. Empty = in-cluster. |

## `audit`

| Key | Type | Default | Notes |
|---|---|---|---|
| `audit.sampling.proxy_success_read_rate` | float (0..1) | `0.0` | Fraction of successful proxy reads to record. Writes/deletes/errors always recorded. |

## Environment variables

The following env vars override config-file values at runtime:

| Variable | Overrides |
|---|---|
| `STOWAGE_LISTEN` | `server.listen` |
| `STOWAGE_PUBLIC_URL` | `server.public_url` |
| `STOWAGE_LOG_LEVEL` | `log.level` |
| `STOWAGE_LOG_FORMAT` | `log.format` |
| `STOWAGE_SQLITE_PATH` | `db.sqlite.path` |
| `STOWAGE_SECRET_KEY_FILE` | `server.secret_key_file` |

The following env vars are read directly (no config-file equivalent):

| Variable | Effect |
|---|---|
| `STOWAGE_SECRET_KEY` | AES-256 root key. 64 hex chars or 44 base64 chars. Takes precedence over `secret_key_file`. |
| `STOWAGE_PPROF_LISTEN` | If set, exposes `/debug/pprof/*` on the given address. |

Backend access keys, OIDC client secrets, and the static-auth password
hash are read from env vars *named* by the config-file fields
(`access_key_env`, `client_secret_env`, `password_hash_env`) — the
secrets themselves never appear in the YAML.

## Validation rules

Loader rejects any of these:

- `server.listen` empty.
- `log.format` ∉ {`json`, `text`, ""}.
- `log.level` ∉ {`debug`, `info`, `warn`, `error`, ""}.
- `db.driver` ∉ {`sqlite`, ""}.
- `db.driver=sqlite` and `db.sqlite.path` empty.
- `auth.modes` empty or contains anything outside `local`, `oidc`,
  `static`.
- Two backends sharing the same `id`.
- A backend with empty `id` or `type`.
- `s3_proxy.enabled=true` and `s3_proxy.listen` empty or equal to
  `server.listen`.
