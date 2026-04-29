---
type: explanation
---

# Security model

This page summarises Stowage's defence stance for operators. For the
full discussion of trust boundaries, see
[Threat model](../explanations/threat-model.md). For the per-defence
references, see the per-page links below.

## Authentication

| Defence | Where it lives |
|---|---|
| HttpOnly + Secure session cookies | [`internal/auth/session.go`](https://github.com/stowage-dev/stowage/blob/main/internal/auth/session.go) |
| argon2id password hashing (`m=65536`) | [`internal/auth/password.go`](https://github.com/stowage-dev/stowage/blob/main/internal/auth/password.go) |
| Per-account lockout (default 5 attempts / 15 min) | `auth.local.lockout.*` config |
| Per-IP login rate limit (10 attempts / 15 min, hard-coded) | [`internal/server/server.go`](https://github.com/stowage-dev/stowage/blob/main/internal/server/server.go) |
| OIDC PKCE + ID-token verification | [`internal/auth/oidc/`](https://github.com/stowage-dev/stowage/tree/main/internal/auth/oidc) |
| Static-account hash from env (no plaintext at rest) | [`internal/auth/service.go`](https://github.com/stowage-dev/stowage/blob/main/internal/auth/service.go) |

## Authorization

| Defence | Where it lives |
|---|---|
| RBAC (`admin` / `user` / `readonly`) | [`internal/auth/middleware.go`](https://github.com/stowage-dev/stowage/blob/main/internal/auth/middleware.go) |
| CSRF double-submit cookie | [`internal/auth/csrf.go`](https://github.com/stowage-dev/stowage/blob/main/internal/auth/csrf.go) |
| Per-session API rate limit (default 600/min) | `ratelimit.api_per_minute` |
| Public share rate limit (default 10/min per IP) | [`internal/api/shares.go`](https://github.com/stowage-dev/stowage/blob/main/internal/api/shares.go) |

## Embedded SigV4 proxy

| Defence | Where it lives |
|---|---|
| SigV4 verification | [`internal/sigv4verifier/`](https://github.com/stowage-dev/stowage/tree/main/internal/sigv4verifier) |
| Bucket scope enforcement | [`internal/s3proxy/scope.go`](https://github.com/stowage-dev/stowage/blob/main/internal/s3proxy/scope.go) |
| Per-credential and global RPS limits | `s3_proxy.{global_rps,per_key_rps}` |
| Anonymous read-only allowlist + per-IP RPS | [`internal/s3proxy/anonymous.go`](https://github.com/stowage-dev/stowage/blob/main/internal/s3proxy/anonymous.go) |
| Cluster-wide anonymous kill switch | `s3_proxy.anonymous_enabled` |
| Pre-checked bucket quotas | [`internal/quotas/`](https://github.com/stowage-dev/stowage/tree/main/internal/quotas) |

## Sharing

| Defence | Where it lives |
|---|---|
| argon2id share passwords | [`internal/shares/`](https://github.com/stowage-dev/stowage/tree/main/internal/shares) |
| Atomic download cap (`UPDATE … SET used = used+1 WHERE used < cap`) | [`internal/api/shares.go`](https://github.com/stowage-dev/stowage/blob/main/internal/api/shares.go) |
| 30-min HMAC-signed unlock cookie | [`internal/api/shares.go`](https://github.com/stowage-dev/stowage/blob/main/internal/api/shares.go) |

## Secret handling

| Defence | Where it lives |
|---|---|
| AES-256-GCM sealing of endpoint secrets and virtual credentials | [`internal/secrets/`](https://github.com/stowage-dev/stowage/tree/main/internal/secrets) |
| `STOWAGE_SECRET_KEY` from env (or auto-generated key file mode 0600) | [`internal/server/server.go`](https://github.com/stowage-dev/stowage/blob/main/internal/server/server.go) |
| Backend / OIDC / static-auth secrets read from env vars, never from YAML | [`internal/config/config.go`](https://github.com/stowage-dev/stowage/blob/main/internal/config/config.go) |

## HTTP

| Defence | Where it lives |
|---|---|
| Strict CSP | [`internal/api/security_headers.go`](https://github.com/stowage-dev/stowage/blob/main/internal/api/security_headers.go) |
| `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`, `Referrer-Policy: same-origin` | same |
| `Permissions-Policy` (geolocation/microphone/camera/payment/USB off) | same |
| HSTS on TLS-bearing requests | same |
| `server.trusted_proxies` CIDR gate for `X-Forwarded-*` | [`internal/auth/proxy.go`](https://github.com/stowage-dev/stowage/blob/main/internal/auth/proxy.go) |

## Audit

Every authentication event, share-lifecycle event, mutation, and
proxy request (subject to sampling) is recorded with `remote_addr`
(post `trusted_proxies`), `user_id`, action, status, and a
per-action `detail` JSON. See
[Reference → Audit catalogue](../reference/audit-catalogue.md).

## See also

- [Threat model](../explanations/threat-model.md) — what's in scope
  and what isn't.
- [Hardening checklist](./hardening-checklist.md) — the operational
  checklist to run before exposing Stowage to anything beyond
  localhost.
- [Known limitations](./known-limitations.md) — current gaps.
