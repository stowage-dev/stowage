---
type: how-to
---

# Backup and restore

Stowage's state is the SQLite database plus the AES-256 root key.
Lose the key and the sealed endpoint secrets and virtual credentials
are unrecoverable; lose the database and you lose users, sessions,
audit, shares, and credentials but **not the bucket data** (that's on
the upstream backend).

## What to back up

| File | Why | Frequency |
|---|---|---|
| `stowage.db`, `stowage.db-shm`, `stowage.db-wal` | All Stowage state. Back up all three or use `.backup`. | Hourly to daily, depending on churn. |
| The secret key (env var or file) | Required to decrypt sealed endpoint secrets and virtual credentials. | Once, store offline. |
| `config.yaml` | Optional — usually in version control already. | On change. |

## Cold backup (simplest)

```sh
sudo systemctl stop stowage
sudo cp /var/lib/stowage/stowage.db   /backup/stowage-$(date +%F).db
sudo cp /var/lib/stowage/stowage.db-* /backup/
sudo systemctl start stowage
```

Downtime: a few seconds.

## Hot backup (no downtime)

```sh
sudo -u stowage sqlite3 /var/lib/stowage/stowage.db \
  ".backup '/backup/stowage-$(date +%F).db'"
```

This uses SQLite's `.backup` command, which produces a single
consistent file regardless of in-flight writes. The output is a
plain `.db` file with no `-shm` / `-wal` companions.

Schedule from cron with a deduplicating archiver (restic, rsync) for
durability.

## Restore

1. Stop Stowage.
2. Restore the `.db` file (and `-shm` / `-wal` if you have them) to
   `db.sqlite.path`.
3. Make sure the AES-256 root key is the **same** as the one that
   originally encrypted the sealed secrets. If not, sealed endpoint
   credentials and virtual credentials will fail to decrypt and you'll
   need to re-create them.
4. Start Stowage.

A new admin user can always be created with `stowage create-admin`
even after restoring from a corrupt or empty database, so you don't
get locked out.

## Disaster recovery checklist

- [ ] Off-host backups (S3 / Backblaze / wherever — *not* on the
      same host as Stowage).
- [ ] AES-256 root key stored offline (printed, in a password manager,
      in a different cloud account).
- [ ] Backup restore tested at least once. Practice it before you
      need it.
- [ ] Documented per-runbook recovery time objective (RTO) and
      recovery point objective (RPO).
