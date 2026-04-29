---
type: reference
---

# `stowage create-admin`

Create the first local admin user. Idempotent (creating a user that
already exists returns an error).

## Usage

```
stowage create-admin --username <name> --password <pw> [flags]
```

## Flags

| Flag | Default | Description |
|---|---|---|
| `--config` | "" | Path to YAML config (used to find the SQLite path). |
| `--username` | (required) | Username for the new admin. |
| `--password` | (required) | Password for the new admin. Must satisfy `auth.local.password.min_length`. |
| `--email` | "" | Optional email. |
| `--must-change-password` | false | Force the user to rotate the password on first login. |

## Behaviour

1. Loads the config to find the SQLite path. Defaults if `--config`
   is empty.
2. Opens the SQLite store, applying any pending migrations.
3. Creates a new user row with role `admin`, password hashed with
   argon2id, and `must_change_pw` per the flag.
4. Prints the new user's ID to stdout.

## Errors

| Output | Meaning |
|---|---|
| `username already taken` | Pick a different username. |
| `weak_password: ...` | Password didn't satisfy `auth.local.password.*` policy. |
| `read /etc/...: permission denied` | The CLI can't read the config file. |

## Notes

- This command runs **without** starting the HTTP server. It's safe
  to run alongside a running `stowage serve` if both point at the
  same SQLite file (SQLite handles single-writer concurrency).
- For non-admin users, use the dashboard or the API after the first
  admin exists. There is no `create-user` subcommand.
