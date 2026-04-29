---
type: how-to
---

# Cross-backend transfers

Copy an object from one backend to another in one click. Stowage
streams the bytes through the dashboard process — there is no direct
backend-to-backend channel.

## From the UI

1. Open the source object's row in the object browser.
2. Click **Copy**.
3. In the destination picker, pick a **different backend** (the same
   destination picker works for cross-prefix copies within a backend
   too).
4. Optionally edit the destination key.
5. Click **Copy**.

Stowage:

1. Pre-checks the destination bucket's quota.
2. Streams the object via `GET` source → `PUT` destination. No disk
   intermediate, no full buffering — the body flows through.
3. Records `object.transfer` in the audit log with the source and
   destination identifiers in `detail`.

## From the API

```sh
curl -sS -b jar -H "X-CSRF-Token: $CSRF" \
  -H 'Content-Type: application/json' \
  -d '{
    "src_bucket":"backups",
    "src_key":"2026/04/db.sql.gz",
    "dst_backend":"archive",
    "dst_bucket":"backups-cold",
    "dst_key":"2026/04/db.sql.gz"
  }' \
  https://stowage.example.com/api/backends/prod/object/copy
```

The path-element `prod` is the *source* backend. `dst_backend`
selects the destination — omitting it is a same-backend copy.

## Bandwidth and egress

The transfer streams through Stowage. That means:

- Egress is paid on the **source** side (e.g. AWS → Wasabi pays AWS
  egress).
- Ingress on the **destination** side may also bill (rarely; most
  S3-compatible vendors don't charge ingress).
- The transfer rate is bounded by Stowage's CPU + the network between
  Stowage and both backends. There's no parallel-part optimisation
  for cross-backend single-object copies — they go end-to-end as one
  stream.

For large datasets, the recommended pattern is to use the upstream's
own bulk-replication tools (e.g. `mc mirror`, AWS S3 Replication) and
use Stowage for the unified observability + audit view, not for the
bulk move itself.

## Recursive folder transfers

`POST /api/backends/{id}/buckets/{bucket}/objects/copy-prefix`
copies a prefix to another prefix on the same backend, or to a
different backend. The audit row is `object.copy_prefix` for
same-backend, `object.transfer_prefix` for cross-backend.

The current implementation fans out per-key via the same single-
object copy path. There is no server-side bulk copy. For very large
prefixes, prefer the upstream's native sync tooling.

## Quota interaction

Cross-backend copies are quota-checked on the destination. If the
destination bucket is over its hard cap, the transfer fails with
`quota_exceeded`. The source bucket's quota is unaffected.
