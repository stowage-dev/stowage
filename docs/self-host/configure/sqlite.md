---
type: how-to
---

# SQLite path and lifecycle

Stowage's SQLite database holds users, sessions, audit rows, share
metadata, virtual credentials, sealed endpoint secrets, and anonymous
bindings. It does **not** hold object payloads — those stay on the
upstream S3 backend.

The driver is the pure-Go
[`modernc.org/sqlite`](https://gitlab.com/cznic/sqlite). No CGo
dependency, no system libsqlite required.

## Configuration

```yaml
db:
  driver: sqlite
  sqlite:
    path: /var/lib/stowage/stowage.db
```

The default in `config.Defaults()` is `./stowage.db` (relative to the
working directory). For real installs, point this at a stable
absolute path that the service user can write to.

`STOWAGE_SQLITE_PATH` overrides the YAML value at runtime.

## What lives on disk

For a database at `/var/lib/stowage/stowage.db`, you'll see:

```
stowage.db        # main database file
stowage.db-shm    # WAL shared memory
stowage.db-wal    # write-ahead log
```

All three are part of one logical database. **Backup all three at
once**, or use the dump-via-CLI approach below to take a single
consistent snapshot.

## Migrations

The schema lives at
[`internal/store/sqlite/migrations.go`](https://github.com/stowage-dev/stowage/blob/main/internal/store/sqlite/migrations.go)
as an append-only ordered list. Stowage applies pending migrations on
every startup and records progress in a `schema_migrations` table.

You do not run migrations manually. Stowage owns its schema; never
modify tables out-of-band.

## Sizing

A real-world Stowage deployment with thousands of audit rows per day
sits in the low tens of megabytes. The
[`stowage_sqlite_db_bytes`](../../reference/metrics-catalogue.md)
gauge tracks size on every Prometheus scrape — alert on growth, not
absolute size.

If you're seeing the database grow much faster than expected, the
likely culprit is `audit.sampling.proxy_success_read_rate=1.0` in a
high-RPS deployment. See
[Explanations → Audit sampling](../../explanations/audit-sampling.md).

## Backup

The simple, locked, low-risk way:

```sh
sudo systemctl stop stowage
sudo cp /var/lib/stowage/stowage.db   /var/backups/stowage-$(date +%F).db
sudo cp /var/lib/stowage/stowage.db-* /var/backups/
sudo systemctl start stowage
```

The hot, online way using SQLite's CLI (only works if the service
user has read access to the WAL):

```sh
sudo -u stowage sqlite3 /var/lib/stowage/stowage.db ".backup '/var/backups/stowage-$(date +%F).db'"
```

See [Operations → Backup and restore](../operations/backup.md) for the
full procedure including the secret-key file.

## Concurrency limits

SQLite supports concurrent readers and a single writer. Stowage's
audit recorder has an async wrapper that batches writes; the rest of
the API serialises through the same writer.

This is why Stowage runs single-replica today. The Helm chart
explicitly sets `replicas: 1` and uses an `RWO` PVC.

## Postgres?

`db.driver: postgres` is reserved in the config schema but
**not yet implemented** — `Load()` returns an explicit "not yet
implemented" error if you try it. Track this in
[Roadmap](../../explanations/roadmap.md).
