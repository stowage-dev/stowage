---
type: reference
---

# `stowage quickstart`

Download a matching MinIO release, start it, and run stowage against
it. Used as the container's default `CMD` and as the developer-
friendly bootstrap.

## Usage

```
stowage quickstart [flags]
```

## Flags

| Flag | Default | Description |
|---|---|---|
| `--data-dir` | `./data` | Directory for MinIO binary, MinIO data, Stowage state. |
| `--listen` | `:8080` | Stowage dashboard listen address. |
| `--minio-addr` | `:9000` | MinIO S3 API listen address. |
| `--minio-console-addr` | `:9001` | MinIO console listen address. |
| `--admin-username` | `admin` | Username for the bootstrapped admin. |
| `--admin-password` | (random) | Password for the bootstrapped admin. Empty = random 24-char. |
| `--minio-base-url` | `https://github.com/stowage-dev/stowage-minio/releases/latest/download` | Where to fetch MinIO from. |

## Behaviour

1. Resolves `--data-dir` to an absolute path; creates it if missing.
2. Downloads the matching MinIO binary (skips if already present).
3. Starts MinIO as a child process with random root credentials.
4. Waits for MinIO's health endpoint to come up.
5. Bootstraps the admin user (if not already present in the embedded
   SQLite database).
6. Starts the Stowage dashboard pointing at the local MinIO.
7. Prints the admin credentials to stdout once.

The state under `--data-dir`:

```
data/
  minio                       # downloaded binary
  minio-data/                 # MinIO's own data directory
  stowage.db                  # Stowage SQLite
  secret.key                  # AES-256 root key (mode 0600)
```

## Source

[`internal/quickstart/quickstart.go`](https://github.com/stowage-dev/stowage/blob/main/internal/quickstart/quickstart.go).
