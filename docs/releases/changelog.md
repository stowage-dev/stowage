---
type: reference
---

# Changelog

The authoritative list of releases is on
[GitHub Releases](https://github.com/stowage-dev/stowage/releases).
This page tracks the high-level shape of each version.

The project uses [conventional commits](https://www.conventionalcommits.org/)
(`feat:`, `fix:`, `docs:`, `chore:`, `refactor:`, `test:`) so the
release notes are largely auto-generated.

## v1.0.0

The first stable release. Phases 0–8 done plus the post-v1
endpoint manager, embedded S3 SigV4 proxy, and Kubernetes operator.

Headline capabilities:

- OIDC + local + static authentication.
- Backend registry with `s3v4` driver and per-backend probe
  history.
- Object browser with multi-select, multipart upload (16 MiB parts,
  pause/resume), preview, version history, tags + metadata,
  cross-bucket / cross-prefix move + copy, streamed bulk
  download-as-zip.
- Share links with argon2id passwords, expiry, atomic download cap.
- Admin dashboard with 24h request histogram + per-backend storage +
  top-10 buckets + recent 5xx events.
- Admin audit log with filtered list and CSV export.
- Per-bucket quotas (soft + hard) with proxy-enforced 507.
- Embedded SigV4 proxy on `:8090` with virtual credentials, bucket
  scope, anonymous bindings, audit, quota.
- Kubernetes operator reconciling `S3Backend` + `BucketClaim` CRDs
  with consumer Secrets, time-based credential rotation,
  per-anonymous-binding RPS.
- Helm chart with admission webhook (self-signed or cert-manager),
  Ingress, NetworkPolicy, image pull secrets.
- Prometheus metrics + sample Grafana dashboard.

## Pre-v1

Pre-v1 development happened in named phases (Phase 0–8). The phase
log lives in `README.md` for historical context. The phase model is
no longer used for v1.x onward — tagged minor releases are.

## Future

See [Roadmap](../explanations/roadmap.md) for what's planned and
what's deferred.
