---
type: how-to
---

# Install from a release binary

For users who don't want to pipe a remote shell script. Identical end
state to the [install script](./script.md), just done by hand.

## 1. Pick a release

Releases live at
`https://github.com/stowage-dev/stowage/releases`. Each tagged release
publishes:

- `stowage-linux-amd64`
- `stowage-linux-arm64`
- `stowage-darwin-amd64`
- `stowage-darwin-arm64`
- `stowage-windows-amd64.exe`
- `SHA256SUMS`

## 2. Download the binary and the checksums

```sh
VER=v1.0.0
ARCH=stowage-linux-amd64

curl -fsSLO "https://github.com/stowage-dev/stowage/releases/download/$VER/$ARCH"
curl -fsSLO "https://github.com/stowage-dev/stowage/releases/download/$VER/SHA256SUMS"
```

## 3. Verify the checksum

```sh
sha256sum -c --ignore-missing SHA256SUMS
```

On macOS use `shasum -a 256` and compare manually:

```sh
shasum -a 256 "$ARCH"
grep "$ARCH" SHA256SUMS
```

On Windows:

```powershell
$expected = (Select-String -Path SHA256SUMS -Pattern 'stowage-windows-amd64.exe').Line.Split()[0]
$actual   = (Get-FileHash -Algorithm SHA256 stowage-windows-amd64.exe).Hash.ToLower()
if ($expected -ne $actual) { throw 'checksum mismatch' }
```

## 4. Move it into place

```sh
chmod +x "$ARCH"
sudo mv "$ARCH" /usr/local/bin/stowage
```

## 5. Verify it runs

```sh
stowage --help
```

You should see the four subcommands: `serve`, `quickstart`,
`create-admin`, `hash-password`.

## 6. Next step

- [Configure the config file](../configure/config-file.md) to point
  Stowage at your backends.
- [Set up a systemd unit](./systemd.md) so Stowage restarts on reboot.
