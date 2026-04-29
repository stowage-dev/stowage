---
type: reference
---

# `stowage hash-password`

Print an argon2id hash for a password. Useful for seeding a
`static` auth account, or for offline tooling that needs to store a
hash without a running Stowage.

## Usage

```
stowage hash-password --password <pw>
```

## Flags

| Flag | Default | Description |
|---|---|---|
| `--password` | (required) | The cleartext password to hash. |

## Output

A single line on stdout, the encoded argon2id hash:

```
$argon2id$v=19$m=65536,t=...,p=...$<salt>$<hash>
```

## Use with `auth.static`

```sh
HASH=$(stowage hash-password --password 'S3cur3-P@ssw0rd!')
STOWAGE_STATIC_PASSWORD_HASH="$HASH" stowage serve --config config.yaml
```

With the config:

```yaml
auth:
  modes: [local, static]
  static:
    enabled: true
    username: emergency
    password_hash_env: STOWAGE_STATIC_PASSWORD_HASH
```

## Cost parameters

The hash uses `m=65536` (~64 MiB per verification), `t=3`,
parallelism 1. These match the verifier in
[`internal/auth/password.go`](https://github.com/stowage-dev/stowage/blob/main/internal/auth/password.go).
