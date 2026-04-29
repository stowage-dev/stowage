---
type: how-to
---

# Logging

Stowage uses the standard library's `log/slog` and writes to stdout.
There is no log file rotation built in — let the platform around
Stowage (systemd journal, Docker logging driver, sidecar) handle that.

## Format

```yaml
log:
  level: info
  format: json
```

| Format | Use when |
|---|---|
| `json` | Production. Parsers like Loki / Vector / Splunk eat it directly. |
| `text` | Local development, where you're tailing on a terminal. |

The default in `config.Defaults()` is `json`.
[`config.demo.yaml`](https://github.com/stowage-dev/stowage/blob/main/config.demo.yaml)
uses `text` because it's friendlier when kicking the tyres.

## Levels

| Level | What's emitted |
|---|---|
| `debug` | Per-request handler decisions, retry decisions, source-of-truth merges. Verbose. |
| `info` | Default. Startup banner, config summary, audit-recorder lifecycle, scheduled scans. |
| `warn` | Recoverable problems: backend probe failure, audit-queue overflow, secret-key auto-regenerated on first boot. |
| `error` | Unrecoverable per-request errors. The handler still returns a response; this just records why. |

## Useful structured fields

Every request log line includes:

- `req_id` — chi's RequestID middleware (UUID).
- `remote_addr` — after `server.trusted_proxies` resolution.
- `method`, `path`, `status`, `duration_ms`, `bytes_written`.
- `user_id` (if a session was attached).
- `backend` (for routes scoped to a backend).

Audit events have their own table in SQLite (see
[Audit](../audit.md)) — the structured logs are best-effort runtime
breadcrumbs, not a forensic trail.

## Tuning verbosity at runtime

There is no signal-driven log-level toggle. Restart Stowage with
`STOWAGE_LOG_LEVEL=debug` to bump verbosity briefly:

```sh
STOWAGE_LOG_LEVEL=debug ./stowage serve --config /etc/stowage/config.yaml
```

For production, prefer leaving the level at `info` and querying the
audit log when you need to reconstruct user actions.

## What is NOT logged

- Passwords (plaintext or hashed).
- AES key material.
- Session tokens or CSRF cookie values.
- Share password attempts (the audit row records `share.access` with
  status `error` when the password is wrong; the password itself never
  hits stdout).

## See also

- [Audit](../audit.md) — the persistent record.
- [Reverse proxy → overview](../reverse-proxy/overview.md) — how proxy
  trust affects the `remote_addr` you see in log lines.
