---
type: explanation
---

# Stowage vs MinIO Console

The headline comparison. Stowage exists in part because of the May
2025 MinIO Console change (see
[Why AGPL](../explanations/why-agpl.md)) and is positioned
explicitly as a vendor-neutral alternative.

## What each tool is

- **MinIO Console** is the dashboard bundled with MinIO. Tightly
  integrated with the upstream MinIO server. Some administrative
  features were moved behind a commercial product in May 2025
  (see [why-agpl](../explanations/why-agpl.md)).
- **Stowage** is a vendor-neutral dashboard + SigV4 proxy + optional
  Kubernetes operator. Works in front of any S3-compatible backend
  (MinIO, Garage, SeaweedFS, AWS S3, B2, R2, Wasabi).

## Feature comparison

| | Stowage | MinIO Console |
|---|---|---|
| **License (today)** | AGPL-3.0-or-later (OSI) | Mixed — open Console with admin features moved commercial. Verify against MinIO's current license. |
| **Backend coverage** | Any S3-compatible | MinIO only |
| **Object browser** | ✅ Multi-select, multipart, preview, tags, version history | ✅ |
| **Public share links** | ✅ argon2id passwords, atomic download cap, expiry | Verify against MinIO docs |
| **Cross-backend copy** | ✅ Stream through proxy host | ❌ (single backend) |
| **Embedded SigV4 proxy with per-tenant scope** | ✅ Bucket scope, per-cred RPS, audit | ❌ |
| **Audit log** | ✅ SQLite-backed, CSV export | Verify against MinIO docs |
| **Prometheus + sample Grafana** | ✅ | Verify against MinIO docs |
| **Kubernetes operator with `BucketClaim` CRD** | ✅ | MinIO Operator for cluster lifecycle, but the `BucketClaim` model differs |
| **Single binary** | ✅ Pure-Go, no CGo, no external services | ✅ |
| **OIDC** | ✅ | Verify against MinIO docs |
| **Multi-replica** | ❌ Single-replica only today | Yes |
| **Native admin-API screens (users / policies)** | ❌ Today (gated on `Capabilities.AdminAPI != ""`; no MinIO driver yet) | ✅ |

The right column has a few "verify against MinIO docs" entries — the
project's own codebase doesn't make claims about MinIO Console's
current shape, and we don't want to put unverified claims here.

## When to pick Stowage

- You want to manage **multiple** S3-compatible backends from one
  pane (some MinIO clusters, some AWS S3, some Backblaze).
- You want a **structural commitment** (AGPL-3.0-or-later, no
  community edition) that the dashboard won't be quietly stripped
  down later. See [Why AGPL](../explanations/why-agpl.md).
- You want **audit + quotas + per-tenant credentials** layered on
  top of *whatever* upstream you happen to run.
- You want **share links** with passwords, expiry, and download
  caps without inventing presigned-URL plumbing.
- You're already burnt on the May 2025 Console change and want a
  clean break.

## When to stick with MinIO Console

- You exclusively run MinIO and don't need vendor neutrality.
- You need MinIO's native admin features (user / policy editor,
  replication / tier configuration) immediately, and Stowage's
  `minio` driver hasn't shipped yet.
- You need horizontal scaling on the dashboard side. MinIO Console
  scales with the MinIO server; Stowage today is single-replica.

## Coexistence

You can run both. Stowage proxies a MinIO upstream just fine; the
MinIO Console keeps working on the upstream's `:9001` port. Run
both for a transition period if your team is migrating gradually.
The
[Migrating from the MinIO Console](../self-host/migrations/from-minio-console.md)
how-to walks through the transition.

## What Stowage doesn't replace

- **MinIO itself** — the storage server. Stowage proxies in front
  of it, doesn't replace it.
- **`mc` (the MinIO client)** — Stowage doesn't proxy the native
  admin API. Use `mc admin` for things that need it until the
  Stowage `minio` driver lands.
