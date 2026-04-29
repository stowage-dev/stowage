---
type: how-to
---

# Querying and exporting the audit log

Every authenticated mutation, every share access, every proxy request
(per the sampling rule), and every authentication event lands in the
SQLite audit table. The dashboard exposes it at `/admin/audit` and a
CSV export at `/admin/audit.csv`.

## The dashboard view

`/admin/audit` is admin-only. The filter bar accepts:

- **Action** — exact match or prefix (`object.`, `share.`,
  `s3.proxy.`, `auth.`, `bucket.`, `quota.`, `backend.`).
- **User** — by user ID or username.
- **Backend** — backend ID.
- **Bucket** — bucket name.
- **Status** — `ok`, `error`, or any vendor-specific status the row
  carries.
- **Date range** — typically the last 24h / 7d / 30d preset.

The table shows time, action, user, target, status, and a "view
detail" button that opens the row's JSON `detail` payload.

## CSV export

`/admin/audit.csv` accepts the same filters as query parameters and
streams the matching rows as CSV. Useful for ad-hoc analysis in a
spreadsheet or feeding to a SIEM by cron.

```sh
curl -sS -b jar \
  "https://stowage.example.com/admin/audit.csv?action=share.&from=2026-04-01&to=2026-04-30" \
  > shares-april.csv
```

Note: the current implementation does NOT stream-paginate very large
exports. For multi-million-row datasets, prefer querying the
underlying SQLite directly (taking a backup snapshot first) until
streaming pagination ships — see
[Roadmap](../../explanations/roadmap.md).

## What's recorded

| Action prefix | Examples | Source |
|---|---|---|
| `auth.` | `auth.login`, `auth.logout` | login + logout endpoints |
| `share.` | `share.create`, `share.revoke`, `share.access` | share APIs and recipient endpoints |
| `object.` | `object.upload`, `object.delete`, `object.bulk_delete`, `object.copy`, `object.copy_prefix`, `object.transfer`, `object.transfer_prefix`, `object.delete_prefix` | object handlers |
| `bucket.` | `bucket.versioning.set`, `bucket.cors.set`, `bucket.policy.set`, `bucket.policy.delete`, `bucket.lifecycle.set`, `bucket.size_tracking.set` | bucket settings handlers |
| `quota.` | `quota.set`, `quota.delete` | quota handlers |
| `backend.` | `backend.create`, `backend.update`, `backend.delete`, `backend.test` | UI-managed endpoint admin |
| `s3.proxy.` | `s3.proxy.getobject`, `s3.proxy.putobject`, `s3.proxy.deleteobject`, … | embedded SigV4 proxy (one event per request, sampled per `audit.sampling.proxy_success_read_rate`) |

For the exhaustive list with field schemas, see
[Reference → Audit catalogue](../../reference/audit-catalogue.md).

## Sampling

Successful proxy reads (GET / HEAD with a 2xx / 3xx response) are
**not** recorded by default. See
[Explanations → Audit sampling](../../explanations/audit-sampling.md)
for the rationale and how to flip it.

Writes, deletes, and any non-2xx response are always recorded
regardless of the sampling knob.

## Forwarding to a SIEM

Stowage does not forward audit events to an external system. Two
recommended patterns:

1. **Periodic CSV export** with a cron job hitting `/admin/audit.csv`
   and shipping it.
2. **JSON log scraping** from stdout, since the request log lines
   already include user / method / path / status. Audit's `detail`
   JSON column is richer; for forensic-grade requirements pair logs
   with periodic CSV exports.

Native syslog / OTLP export is on the wishlist; see the project
issue tracker.
