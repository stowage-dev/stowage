---
type: how-to
---

# Static config-defined account

A single emergency-access account whose credentials live in
configuration, not the database. Useful for break-glass scenarios and
for environments where you genuinely don't want a writable user store.

## When to use it

- You want a recovery path that works even if the SQLite database is
  corrupted or empty.
- You're running Stowage in a strictly read-only file system and don't
  want a database write path at all.
- You're scripting tests where local user creation is overhead.

For day-to-day use, prefer `local` or `oidc`. Static accounts can't
rotate their own password, can't be locked out, and have no per-user
audit attribution beyond the username.

## Enable it

```yaml
auth:
  modes: [local, static]
  static:
    enabled: true
    username: emergency
    password_hash_env: STOWAGE_STATIC_PASSWORD_HASH
```

Then provide the hash via env:

```sh
STOWAGE_STATIC_PASSWORD_HASH=$(stowage hash-password --password 'S3cur3-P@ssw0rd!') \
  stowage serve --config /etc/stowage/config.yaml
```

The hash format is the standard argon2id encoded string Stowage's
`hash-password` subcommand emits.

## Behaviour

- The static account always has the `admin` role.
- It cannot be deleted, renamed, or have its password rotated through
  the API.
- Its login emits an `auth.login` audit row with the configured
  username; subsequent actions attribute to that username.
- Lockout (`local.lockout`) does not apply.
- The per-IP login rate limit on `/auth/login/local` still applies.

## Disabling it

Remove `static` from `auth.modes`, or set `static.enabled: false`. The
env var doesn't have to be unset.
