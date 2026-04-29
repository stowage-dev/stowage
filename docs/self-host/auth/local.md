---
type: how-to
---

# Local username + password

The simplest auth mode. Users sit in Stowage's SQLite, passwords are
argon2id-hashed, sessions are HttpOnly secure cookies.

## Enable it

```yaml
auth:
  modes: [local]
  local:
    password:
      min_length: 12
      prevent_reuse: true
    lockout:
      max_attempts: 5
      window: 15m
```

`auth.modes` is a list — you can combine `local` with `oidc` or
`static`. The login screen shows whichever methods are enabled.

## Defaults

| Field | Default |
|---|---|
| `local.password.min_length` | 12 |
| `local.password.prevent_reuse` | true |
| `local.lockout.max_attempts` | 5 |
| `local.lockout.window` | 15m |

`prevent_reuse=true` makes Stowage reject any password change that
matches the user's current password. There is no longer-history reuse
prevention; argon2id makes per-attempt brute force expensive enough
that the marginal value of a longer history is small.

## Lockout

After `max_attempts` failed login attempts, the account is locked for
`window`. The lockout is per-user; legitimate users on shared NAT
aren't penalised.

A separate per-IP rate limit on `/auth/login/local` (10 attempts / 15
min, hard-coded in `internal/server/server.go`) defends against
brute-force of unknown usernames.

To unlock from the dashboard, an admin opens
`/admin/users/<id>` and clicks **Unlock**. The audit log records the
unlock as part of `auth.*` actions.

## Creating the first admin

```sh
stowage create-admin \
  --config /etc/stowage/config.yaml \
  --username admin \
  --password 'S3cur3-P@ssw0rd!'
```

Or with `--must-change-password` to force a rotation on first login:

```sh
stowage create-admin --must-change-password ...
```

## Creating subsequent users

In the dashboard, an admin opens `/admin/users` and clicks
**Create user**. Required fields are username, role, password (or
"send password reset email" if `auth.local.reset_email.enabled` is
set, which today is a stub — see Roadmap).

The role is one of:

- `admin` — full access including `/admin/*` and bucket settings.
- `user` — can read and write objects, manage their own pins and
  shares, mint their own virtual S3 credentials.
- `readonly` — can read everything they're authorised for, but every
  mutating API call returns 403 `forbidden`.

## Self-service password change

Any user can rotate their password at `/me/password`. The flow checks
the old password and applies the policy to the new one.

If a user's record carries `must_change_pw=true`, every API call
except `/api/me`, `/api/me/password`, and the auth endpoints returns
403 `password_change_required` until they rotate.

## Password hashing

Passwords are hashed with argon2id (`m=65536` ≈ 64 MiB per
verification). Verify cost is intentional: it makes credential-stuffing
expensive, and the per-IP login limiter caps the parallelism a single
attacker can apply.

This is why `POST /auth/login/local` has a low concurrency ceiling in
the [benchmarks](../../explanations/benchmarks.md) — the cost is by
design.

## Hashing a password without a database

For one-off scripting (seeding a static account, manually building a
user record offline), the `hash-password` subcommand emits a hash to
stdout:

```sh
stowage hash-password --password 'S3cur3-P@ssw0rd!'
```

## Disabling local auth

Set `auth.modes` to a list that doesn't include `local` — for
example `[oidc]`. Existing local user rows stay in the database but
can't log in until you re-enable the mode.
