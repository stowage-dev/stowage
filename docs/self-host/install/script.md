---
type: how-to
---

# Install with the script

The install scripts at `https://stowage.dev/install.sh` and
`https://stowage.dev/install.ps1` download a release binary, verify
its SHA256 checksum, and either run it or leave it next to your shell.

## Linux, macOS, WSL

```sh
curl -fsSL https://stowage.dev/install.sh | sh
```

## Windows (PowerShell)

```powershell
irm https://stowage.dev/install.ps1 | iex
```

## Environment overrides

| Variable | Default | Effect |
|---|---|---|
| `STOWAGE_VERSION` | `latest` | Tag to fetch (e.g. `v1.0.0`). |
| `STOWAGE_REPO` | `stowage-dev/stowage` | GitHub `owner/name`. |
| `STOWAGE_RELEASE_BASE` | (computed) | Full base URL for downloads. Overrides repo + version. |
| `STOWAGE_NO_RUN` | `0` | If `1`, download and verify but don't exec. |

## Architectures

The installers ship `linux-amd64`, `linux-arm64`, `darwin-amd64`,
`darwin-arm64`, and `windows-amd64`. `windows-arm64` binaries are not
yet published; the PowerShell installer fails with a clear error on
that target.

The `install.sh` script aborts on `MINGW*`/`MSYS*`/`CYGWIN*` shells —
on those systems use `install.ps1` directly.

## Checksum verification

The script downloads `SHA256SUMS` from the same release directory and
compares it against the local hash. A mismatch aborts before the
binary is moved into place.

## What the script does not do

- Add anything to `PATH`. The binary lands in your current directory;
  move it where you want.
- Create a system service. See [systemd](./systemd.md) for that.
- Install a config file. The default `stowage` invocation runs
  `stowage quickstart`; for a real install supply your own config and
  pass `serve --config /path/to/config.yaml`.

## Passing arguments to `stowage`

```sh
curl -fsSL https://stowage.dev/install.sh | sh -s -- serve --config my.yaml
```

```powershell
& ([scriptblock]::Create((irm https://stowage.dev/install.ps1))) serve --config my.yaml
```

## Source for the scripts

- [`deploy/install/install.sh`](https://github.com/stowage-dev/stowage/blob/main/deploy/install/install.sh)
- [`deploy/install/install.ps1`](https://github.com/stowage-dev/stowage/blob/main/deploy/install/install.ps1)
- [`deploy/install/install.cmd`](https://github.com/stowage-dev/stowage/blob/main/deploy/install/install.cmd)
  — minimal shim that calls `install.ps1` for users who land in `cmd.exe`.

If your operations team won't pipe a remote script to `sh`, see
[Install from a release binary](./release-binary.md) for the manual
flow.
