---
type: how-to
---

# Install as a systemd unit

A minimal unit file for running Stowage on a Linux box that you don't
want to babysit.

## 1. Drop the binary in place

Pick a stable path. `/usr/local/bin/stowage` is conventional:

```sh
sudo install -m 0755 stowage /usr/local/bin/stowage
```

## 2. Create a service user

```sh
sudo useradd --system --home /var/lib/stowage --shell /usr/sbin/nologin stowage
sudo install -d -o stowage -g stowage -m 0750 /var/lib/stowage
sudo install -d -o stowage -g stowage -m 0750 /etc/stowage
```

## 3. Drop in your config and key

```sh
sudo install -o stowage -g stowage -m 0640 config.yaml /etc/stowage/config.yaml
sudo install -o stowage -g stowage -m 0600 stowage.key  /etc/stowage/secret.key
```

The key file format is hex (no whitespace, 64 hex chars for 32 bytes).
If `server.secret_key_file` points at a missing path, Stowage will
generate the key on first boot and write it itself, mode 0600 — that
also works.

## 4. Write the unit

`/etc/systemd/system/stowage.service`:

```ini
[Unit]
Description=Stowage S3 dashboard
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=stowage
Group=stowage
ExecStart=/usr/local/bin/stowage serve --config /etc/stowage/config.yaml
Restart=on-failure
RestartSec=5s

# Hardening
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/stowage
ReadOnlyPaths=/etc/stowage
CapabilityBoundingSet=
AmbientCapabilities=
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6
RestrictNamespaces=true
LockPersonality=true
MemoryDenyWriteExecute=true
SystemCallArchitectures=native
SystemCallFilter=@system-service
SystemCallFilter=~@privileged @resources

[Install]
WantedBy=multi-user.target
```

The `ReadWritePaths` line covers the SQLite database and the
secret-key file if you've placed them under `/var/lib/stowage`. Adjust
to wherever your config points.

## 5. Bootstrap and enable

```sh
sudo systemctl daemon-reload

# First admin:
sudo -u stowage /usr/local/bin/stowage create-admin \
  --config /etc/stowage/config.yaml \
  --username admin --password 'S3cur3-P@ssw0rd!'

sudo systemctl enable --now stowage.service
sudo systemctl status stowage.service
```

Logs go to the journal:

```sh
journalctl -u stowage.service -f
```

## Reverse proxy

The unit binds Stowage to `0.0.0.0:8080` (or whatever
`server.listen` says). Terminate TLS in nginx / Caddy / Traefik in
front of it. See [Reverse proxy → overview](../reverse-proxy/overview.md).
