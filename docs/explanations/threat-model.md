---
type: explanation
---

# Threat model

What Stowage trusts, what it doesn't, and the per-defence list.

## Trust boundaries

| Trusted | Notes |
|---|---|
| The reverse proxy in front of Stowage | Sets `X-Forwarded-*`. Stowage's gate honours these only from peers in `server.trusted_proxies`. |
| The host's filesystem | The SQLite DB and the AES-256 key file live there. Compromise of the host = compromise of Stowage. |
| The OIDC provider | If you trust an external IdP for auth, Stowage trusts the ID tokens it signs. |
| The Kubernetes API server | The proxy's informer reads Secrets via the API; the operator writes them via the API. RBAC must be tight. |

| NOT trusted | Why |
|---|---|
| The network between client and reverse proxy | TLS terminates at the proxy. |
| The client's browser | XSS, CSRF, malicious tabs — defended via CSP, HttpOnly + Secure cookies, double-submit CSRF. |
| The tenant's SDK | Could be malicious or buggy. Verifier + scope check + quota run before the request reaches the upstream. |
| The upstream backend | A buggy upstream can return malformed responses. The proxy and dashboard surface upstream errors directly rather than mask them. |

## Defence list

### Authentication

- **Sessions** are HttpOnly + Secure cookies, opaque IDs, server-side
  state in SQLite. Lifetime + idle timeout per
  [`auth.session`](../reference/config.md).
- **Local passwords** hashed with argon2id (`m=65536`). Per-account
  lockout (default 5 attempts / 15 min) and per-IP rate limit on
  `/auth/login/local` (10 attempts / 15 min, hard-coded).
- **OIDC ID tokens** verified with the issuer's JWKS. PKCE on the
  authorization-code flow.
- **Static accounts** authenticated against an env-supplied
  argon2id hash. No state file write.

### Authorization

- **RBAC roles** `admin`, `user`, `readonly` enforced at the chi
  middleware layer (`requireAdmin`, `requireWriter`).
- **CSRF**: double-submit cookie + `X-CSRF-Token` header on every
  mutation. Reads are exempt.
- **Per-session API rate limit** (default 600 req/min). 429 +
  `Retry-After`.

### S3 proxy

- **SigV4 verification** with a derived-signing-key cache and
  secret-fingerprint binding so old keys can't be used after a
  rotation.
- **Bucket scope** enforced before forwarding upstream.
- **Per-credential RPS** and **global RPS** limits.
- **Anonymous endpoints** restricted to a hard-coded read-only
  operation allowlist + per-source-IP RPS cap.
- **Cluster-wide anonymous kill switch** (`s3_proxy.anonymous_enabled:
  false`).

### Sharing

- **Public share gate** uses argon2id passwords (`m=65536`).
- **Per-IP rate limit** on `/s/<code>/*` (default 10 req/min) so
  leaked codes can't be brute-forced.
- **Atomic download cap** — the underlying SQL is `UPDATE … SET used
  = used + 1 WHERE used < cap`, so racing parallel downloads can't
  both squeeze through.
- **Unlock cookie** is a 30-minute HMAC-signed token. Restart
  rotates the HMAC key.

### Secret handling

- **`STOWAGE_SECRET_KEY`** seals every UI-managed endpoint secret and
  every virtual credential at rest with AES-256-GCM. Without the
  key, those handlers return 503 `secret_key_unset`.
- **No secrets in YAML.** Backend access keys, OIDC client secrets,
  static-auth password hashes are read from env vars referenced by
  the config.
- **`stowage.key`** auto-generated mode 0600 if missing.

### HTTP

- **Strict Content-Security-Policy** by default
  (`default-src 'self'`; `frame-ancestors 'none'`; `base-uri 'self'`;
  `form-action 'self'`).
- **`X-Content-Type-Options: nosniff`** on every response.
- **`X-Frame-Options: DENY`**.
- **`Referrer-Policy: same-origin`**.
- **`Permissions-Policy`** disabling geolocation, microphone, camera,
  payment, USB.
- **HSTS** on every TLS-bearing request (handled by the reverse
  proxy in production).

### Audit

- Every authentication event, share-lifecycle event, mutation, and
  proxy request (per the sampling rule) is recorded.
- Audit rows include `remote_addr` (post `trusted_proxies`),
  `user_id`, `action`, `status`, action-specific `detail` JSON.
- CSV export at `/admin/audit.csv` is admin-only.

## What's not in the model

- **DDoS protection** at the application layer is beyond Stowage's
  scope. The proxy has rate limits; absorbing flood traffic is the
  job of your edge (Cloudflare, etc.).
- **Persistent process integrity.** If an attacker can write to the
  Stowage filesystem, they can replace the binary. Sign the binary
  out of band if your threat model demands it.
- **Side-channel attacks.** Argon2id verification times leak whether
  a hash exists; the per-IP login rate limit caps the parallelism
  this gives an attacker, but the absolute timing is observable.
- **Compromised AES key.** If `STOWAGE_SECRET_KEY` is exposed,
  rotate it (see [Operations → Key rotation](../self-host/operations/key-rotation.md))
  and treat every sealed credential as compromised.

## Reporting a vulnerability

See [Security → Reporting a vulnerability](../security/report-vulnerability.md).
