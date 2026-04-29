---
type: reference
---

# CLI

The `stowage` binary has four subcommands.

```
Usage: stowage <command> [flags]

Commands:
  serve           Run the dashboard server
  quickstart      Download MinIO into ./data and run stowage against it
  create-admin    Create the first local admin user
  hash-password   Print an argon2id hash for a password
  help            Show this message
```

- [`stowage serve`](./serve.md)
- [`stowage quickstart`](./quickstart.md)
- [`stowage create-admin`](./create-admin.md)
- [`stowage hash-password`](./hash-password.md)

The CLI is defined in
[`cmd/stowage/main.go`](https://github.com/stowage-dev/stowage/blob/main/cmd/stowage/main.go).
