---
type: reference
---

# `stowage serve`

Run the dashboard HTTP server (and the embedded SigV4 proxy if
enabled in config).

## Usage

```
stowage serve [--config path/to/config.yaml]
```

## Flags

| Flag | Default | Description |
|---|---|---|
| `--config` | "" | Path to a YAML config file. Env vars override values in the file. |

If `--config` is empty, Stowage uses the built-in defaults.

## Behaviour

1. Loads the config file (if any).
2. Applies env-var overrides (`STOWAGE_LISTEN`, etc.).
3. Validates the config; exits non-zero on validation failure.
4. Initialises the SQLite store, applying any pending migrations.
5. Initialises the backend registry from the config + DB-managed
   endpoints, runs an initial probe per backend.
6. Starts the dashboard HTTP server on `server.listen`.
7. If `s3_proxy.enabled: true`, starts the proxy listener on
   `s3_proxy.listen`.
8. Runs until SIGTERM / SIGINT, then drains in-flight requests up to
   `server.shutdown_timeout` (default 10s) before exiting.

## Environment-variable overrides

See [Configuration → environment variables](../config.md#environment-variables).

## Exit codes

| Code | Meaning |
|---|---|
| 0 | Clean shutdown after SIGTERM/SIGINT. |
| 1 | Config load or validation failure, or unrecoverable startup error. |
