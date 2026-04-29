---
type: tutorial
---

# What is Stowage

Stowage is a single Go binary that puts a modern web dashboard, an
embedded AWS-SigV4-compatible S3 proxy, and (optionally) a Kubernetes
operator in front of any S3-compatible storage backend. It is licensed
[AGPL-3.0-or-later](../explanations/why-agpl.md) and is not source-
available, not BSL, not SSPL.

## What it does

- Lets users sign in via OIDC, local username + password, or a static
  account, and browse buckets and objects in a polished UI.
- Issues password-protected, expiry-bound, download-capped share links
  so you can hand off a file or a folder without inventing presigned-URL
  plumbing.
- Mints per-tenant **virtual S3 credentials** so tenant developers can
  point `aws-cli` (or any AWS SDK) at Stowage instead of the upstream,
  with bucket-scope enforcement, per-bucket quotas, and a full audit
  trail.
- Records every authentication event, share access, object mutation,
  bucket-settings change, and proxy request to a SQLite audit log,
  filterable in the dashboard or exportable as CSV.
- Exposes Prometheus metrics, ships a starter Grafana dashboard, and
  works behind any reverse proxy that can speak HTTP.

## What it does not do

- It does not store your bytes. Object data lives on the upstream S3
  backend; Stowage proxies access to it.
- It does not implement an S3 server. The embedded proxy verifies SigV4,
  enforces scope and quota, and re-signs to the upstream — it doesn't
  hold objects on disk.
- It does not implement TLS. Stowage listens on plaintext HTTP and
  expects to sit behind a TLS-terminating reverse proxy you already
  operate.
- It does not run multi-replica today. SQLite + an in-process limiter
  pin Stowage to a single instance per deployment.

## Who Stowage is for

- **Self-hosters and homelab operators** who want one dashboard across
  the MinIO / Garage / SeaweedFS instances they already run.
- **Small platform teams** who want to give developers SDK-compatible
  credentials without exposing the upstream admin keys.
- **Anyone burned by the May 2025 MinIO Console change** who wants a
  vendor-neutral dashboard with a structural license commitment that
  it won't be quietly stripped down later. (See
  [Why AGPL](../explanations/why-agpl.md).)

## Who Stowage is not for

- **Petabyte-scale single-tenant operators** who need their dashboard
  horizontally scaled. Stowage is single-replica by design.
- **Object lifecycle automation that needs to live close to the data**
  — that belongs at the storage backend or in your application.
- **Public-facing CDN-shaped traffic.** The proxy is for SDK access and
  share-link delivery, not for serving 50k rps to anonymous browsers.

## What ships in v1.0

The capabilities above are all in v1.0 of the AGPL build. There is no
"community edition" and no "enterprise edition" — see
[No community edition](../explanations/no-community-edition.md). The
roadmap of deferred items lives at
[Explanations → Roadmap](../explanations/roadmap.md).

## Next step

- [Quickstart: one-liner](./quickstart-oneliner.md) — install and run
  in two minutes.
