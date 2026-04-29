---
type: how-to
---

# Upgrading between releases

Stowage applies SQLite migrations automatically on startup. The
upgrade procedure is "stop the old binary, swap, start the new
binary" plus the usual due diligence around backups.

## General procedure

1. **Read the release notes** for the version you're upgrading to.
   See [Releases](../../releases/). Breaking changes are flagged at
   the top.
2. **Back up** the database. See
   [Backup and restore](./backup.md). The migration is forward-only —
   if you need to roll back you'll restore the backup.
3. **Stop the old binary**:
   ```sh
   sudo systemctl stop stowage
   ```
4. **Replace the binary** (or the Docker image tag).
5. **Start the new binary**. It applies pending migrations during
   startup; watch the logs for `migration applied` lines.
   ```sh
   sudo systemctl start stowage
   journalctl -u stowage -f
   ```
6. **Verify**: hit `/healthz`, log in, click around. Audit a recent
   row in `/admin/audit` to confirm the schema migration didn't break
   row decoding.

## Rolling back

The database schema is append-only — newer Stowage adds tables and
columns but doesn't remove or rename existing ones in the same
migration. Despite that, **downgrading the binary is not supported**:
the new schema may have columns the old code doesn't know how to
populate.

If you need to roll back, restore the pre-upgrade backup. Sessions
created during the upgrade window will be lost; users log in again.

## Docker image upgrade

```sh
docker compose -f deploy/compose/docker-compose.yml pull
docker compose -f deploy/compose/docker-compose.yml up -d
```

Compose handles graceful shutdown via `docker stop` (SIGTERM, then
SIGKILL after `stop_grace_period`). Stowage's HTTP server respects
SIGTERM and drains in-flight requests up to `server.shutdown_timeout`
(default 10s).

## Helm upgrade

See [Kubernetes → Upgrade](../../kubernetes/upgrade.md).

## Release cadence

Stowage ships a tagged release per phase, then minor releases for
features and patch releases for fixes. The
[changelog](../../releases/changelog.md) lists every release with the
date and the headline change.

Conventional commit prefixes (`feat:`, `fix:`, `docs:`, `chore:`,
`refactor:`, `test:`) drive the changelog generator. PRs that don't
follow them are rebased before merge.
