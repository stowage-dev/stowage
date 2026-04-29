---
type: how-to
---

# Sessions, idle timeout, lockout

How long a session stays valid, when it expires, and what the per-IP
and per-session rate limits do.

## Session lifetime

```yaml
auth:
  session:
    lifetime: 8h
    idle_timeout: 1h
```

| Field | Default | Effect |
|---|---|---|
| `lifetime` | 8h | Hard ceiling on a single session. After this, the user must log in again. |
| `idle_timeout` | 1h | If the session sees no API activity for this long, it expires. |

Sessions live in SQLite. The cookie carries an opaque session ID;
nothing else.

## Cookie attributes

- `HttpOnly` — JavaScript can't read it.
- `Secure` — set when the request rode over TLS, including via
  `X-Forwarded-Proto: https` from a trusted proxy.
- `SameSite=Lax` — works with the OIDC redirect flow, blocks cross-
  site mutation attempts.
- `Path=/` — applied to every route.

## CSRF

Mutations require a `X-CSRF-Token` header whose value matches the
`stowage_csrf` cookie. The cookie is set on every authenticated
response so single-page-app code can read it from `document.cookie`
and replay it.

Reads (`GET`, `HEAD`) do not require the header.

If a mutation arrives without a valid token, the response is 403
`csrf_invalid`.

## Per-session API rate limit

```yaml
ratelimit:
  api_per_minute: 600
```

Default is 600. Counts requests against `/api/*` per session. On
overflow, the response is 429 `rate_limited` with a `Retry-After`
header set to the bucket window in seconds.

`0` disables the limiter entirely (not recommended in shared
deployments).

Multipart uploads with high parallelism may need this raised; raise
it in steps and watch `stowage_request_duration_seconds`.

## Per-IP login rate limit

`POST /auth/login/local` is limited to 10 attempts per 15 minutes per
client IP. The limit is hard-coded — it's a defence against
brute-force of unknown usernames, distinct from the per-account
lockout policy.

The limit applies to OIDC start as well, so a hostile client can't
spin the IdP redirect dance at high RPS.

## Per-IP share rate limit

`/s/<code>/info`, `/s/<code>/unlock`, and `/s/<code>/raw` are limited
per client IP (default 10 req/min) so a leaked code can't be used to
brute-force the share password at scale. The limiter is configured
separately from the API limiter.

## Logging out

`POST /auth/logout` deletes the server-side session row and clears
the cookie. Subsequent calls with the old cookie return 401
`unauthorized`.

Closing the browser does not automatically log out — the session
cookie may persist for the full `lifetime`. Closing all browser
windows then reopening will not auto-end the session unless the OS
or browser policy clears session cookies.

## Source files

- [`internal/auth/session.go`](https://github.com/stowage-dev/stowage/blob/main/internal/auth/session.go)
- [`internal/auth/csrf.go`](https://github.com/stowage-dev/stowage/blob/main/internal/auth/csrf.go)
- [`internal/auth/ratelimit.go`](https://github.com/stowage-dev/stowage/blob/main/internal/auth/ratelimit.go)
