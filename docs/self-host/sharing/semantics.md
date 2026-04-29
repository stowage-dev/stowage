---
type: how-to
---

# Share semantics

The four share knobs and what each one actually does at the protocol
level.

## Expiry

A share is valid until the configured timestamp, after which:

- `/s/<code>/info` returns 410 `expired`.
- `/s/<code>/unlock` returns 410 `expired`.
- `/s/<code>/raw` returns 410 `expired`.

The check is `now() > expires_at`, evaluated per request — there's no
caching layer that could keep an expired share alive.

There is no auto-deletion of the share row. Expired rows stay in
SQLite for audit purposes; clean them up with a periodic SQL job if
you care.

## Password

If set, the recipient must enter the password before fetching the
file. Hashing is argon2id with `m=65536` (~64 MiB per verification),
so brute-force is expensive.

The password is also rate-limited per client IP at the HTTP layer
(default 10 req/min on `/s/<code>/*`), so even with a long
verification time, a leaked code can't be used to mass-guess.

On successful unlock, Stowage issues a short-lived (30 minute)
HMAC-signed cookie (`stowage_unlock_<code>`). That cookie is required
on subsequent `/info` and `/raw` calls.

The HMAC signature covers both the share code and the cookie
expiration timestamp, so a cookie minted for share A can't be
replayed under share B.

Restarting Stowage rotates the HMAC key, invalidating outstanding
unlock cookies. That's an acceptable tradeoff for a 30-minute TTL.

## Download limit

Each successful download increments a counter atomically (a single
SQL `UPDATE … SET downloads_used = downloads_used + 1 WHERE
downloads_used < download_limit`). Two parallel downloads can't both
squeeze through the last allowed slot — the second one races and
loses, returning `exhausted`.

If the recipient's connection drops mid-download, the count is still
incremented. There's no replay refund.

## Disposition

| Value | Effect |
|---|---|
| `inline` | Browser previews if the content type allows. PDFs render in-tab; images and videos play in tab. |
| `attachment` | Browser downloads with the original filename. |

The header on the `/raw` response is
`Content-Disposition: attachment; filename="<key>"` or `inline` per
the share row.

## Public surface

`/s/<code>` itself falls through to the SvelteKit SPA, which renders
the password gate and preview. Only the JSON+bytes plumbing is
server-rendered:

| Path | Method | Body | Description |
|---|---|---|---|
| `/s/<code>/info` | GET | JSON | Metadata: file name, size, content type, expires_at, password_required, downloads_remaining |
| `/s/<code>/unlock` | POST | `{password: "..."}` | Sets the unlock cookie if the password matches |
| `/s/<code>/raw` | GET | bytes | The file. Requires the unlock cookie if the share has a password. |

Each emits an audit row of `share.access`.

## Admin override

A share is created by a user; only its creator and admins can revoke
it. Admins see all shares system-wide; users see only their own.

There is no admin override that lets one user view another user's
share *contents* without unlocking it. The password gate applies to
admins too.
