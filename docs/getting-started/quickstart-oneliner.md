---
type: tutorial
---

# Quickstart: one-liner

Two minutes from "I have a terminal" to "I am logged into the Stowage
dashboard". The installer downloads a SHA256-verified release binary
into the current directory and exec's it. Nothing is installed system-
wide.

## Linux, macOS, WSL

```sh
curl -fsSL https://stowage.dev/install.sh | sh
```

## Windows (PowerShell)

```powershell
irm https://stowage.dev/install.ps1 | iex
```

The script:

1. Detects your OS + architecture.
2. Downloads the matching binary and the release `SHA256SUMS` from
   `github.com/stowage-dev/stowage/releases/latest/download`.
3. Verifies the checksum.
4. Drops `./stowage` (or `stowage.exe`) into the current directory.
5. Exec's it with no arguments — which runs `stowage quickstart`.

`stowage quickstart` then:

1. Downloads a matching MinIO release into `./data/` (only on first run).
2. Starts MinIO as a child process with random credentials.
3. Creates a fresh SQLite database at `./data/stowage.db`.
4. Creates an `admin` user with a random password (printed to stdout).
5. Starts the dashboard on `http://localhost:8080`.

When the printed admin password scrolls past, copy it. Open
[http://localhost:8080](http://localhost:8080) and log in.

## Skipping the auto-run

Set `STOWAGE_NO_RUN=1` to download and verify but not exec:

```sh
STOWAGE_NO_RUN=1 curl -fsSL https://stowage.dev/install.sh | sh
./stowage --help
```

## Pinning a version

```sh
STOWAGE_VERSION=v1.0.0 curl -fsSL https://stowage.dev/install.sh | sh
```

`STOWAGE_VERSION` defaults to `latest` and accepts any tagged release.

## Passing arguments to `stowage`

After the `--`, arguments are forwarded to the binary:

```sh
curl -fsSL https://stowage.dev/install.sh | sh -s -- serve --config my.yaml
```

The PowerShell variant uses a script-block:

```powershell
& ([scriptblock]::Create((irm https://stowage.dev/install.ps1))) serve --config my.yaml
```

## What you have now

- A `./stowage` binary in your working directory.
- A `./data/` directory with MinIO, MinIO's data, the Stowage SQLite
  database, and the auto-generated AES-256 root key (mode 0600).
- A running dashboard on `:8080` with an admin user.

## Next step

- [Your first share link](./first-share.md) — try the dashboard.
- [Self-host → Configure](../self-host/configure/config-file.md) when
  you're ready to switch to a hand-written config and a real backend.
