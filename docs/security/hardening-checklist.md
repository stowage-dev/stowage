---
type: how-to
---

# Hardening checklist

Run this before exposing Stowage outside localhost.

## Network

- [ ] Stowage is behind a TLS-terminating reverse proxy (nginx,
      Caddy, Traefik, or cloud LB). Stowage's listener is plaintext
      HTTP and is **not** reachable directly from the internet.
- [ ] `server.trusted_proxies` is set to a CIDR list that includes
      only your reverse proxy's source range. The default trusts
      every immediate peer; this is fine when the listener is bound
      to localhost or a private network, dangerous if it's bound to
      `0.0.0.0` and reachable elsewhere.
- [ ] `/metrics` is restricted at the reverse-proxy or
      NetworkPolicy layer. Stowage exposes it without authentication
      by design.
- [ ] If you've enabled the embedded SigV4 proxy, it has its own
      reverse-proxy entry with its own host name and TLS.

## Secrets

- [ ] `STOWAGE_SECRET_KEY` is set (env or `server.secret_key_file`).
      Without it, UI-managed endpoints and virtual credentials are
      unavailable.
- [ ] The key is stored offline as well, in case the host is lost.
- [ ] Backend admin keys, OIDC client secrets, and static-auth
      password hashes are supplied via env vars referenced by the
      config file, never inline in the YAML.
- [ ] The config file's permissions are `0640` and owned by the
      service user. The key file is `0600`.

## Authentication

- [ ] If using OIDC, role mapping covers everyone in your
      organisation who should have access. No-match logins are
      rejected.
- [ ] Local `auth.local.password.min_length` is at least 12.
- [ ] `auth.local.lockout` is on (default).
- [ ] Static account is enabled only if you genuinely need a
      break-glass; it's not the day-to-day login path.
- [ ] `must_change_password` is set on any seeded admin account
      whose initial password is shared with multiple humans.

## Authorization

- [ ] `readonly` users are the default for new joiners; promote to
      `user` or `admin` only when needed.
- [ ] `ratelimit.api_per_minute` is set (default 600). Tune higher
      only after observing real workload patterns.
- [ ] Bucket settings (versioning, lifecycle, policy) are owned by
      admins; non-admins can't reach those handlers.

## SigV4 proxy

- [ ] Every virtual credential is scoped to specific buckets, not a
      catchall.
- [ ] `s3_proxy.global_rps` and `s3_proxy.per_key_rps` are set if
      multi-tenant traffic is expected.
- [ ] Anonymous bindings exist only on buckets that genuinely should
      be public; defaults are `mode: None`.
- [ ] If anonymous reads are off, `s3_proxy.anonymous_enabled:
      false` is set as a defence in depth.

## Audit

- [ ] `/admin/audit` is monitored or its CSV export is shipped
      somewhere (cron + restic, SIEM, etc.).
- [ ] You've decided on `audit.sampling.proxy_success_read_rate`.
      Default 0.0 is right for most deployments. 1.0 only if
      compliance demands it. See
      [Audit sampling](../explanations/audit-sampling.md).

## Backup

- [ ] SQLite database (`stowage.db`, `-shm`, `-wal`) is backed up
      off-host on a schedule.
- [ ] AES-256 root key is stored offline.
- [ ] Restore has been tested at least once.

## Updates

- [ ] You're tracking the project's release feed (GitHub Releases or
      Watch → Releases).
- [ ] Security advisories from
      `github.com/stowage-dev/stowage/security/advisories` are
      subscribed to.

## Kubernetes-specific

- [ ] `webhook.enabled: true` (CRD validation enforces invariants
      the OpenAPI schema can't).
- [ ] `networkPolicy.enabled: true` if your cluster supports it.
- [ ] Image pull secrets configured if you're using a private
      registry.
- [ ] PVC backups handled by your cluster's backup solution.
- [ ] `BucketClaim.spec.deletionPolicy` is `Retain` for any claim
      where data loss would be expensive.
- [ ] Any `forceDelete: true` claim is reviewed before merging.
