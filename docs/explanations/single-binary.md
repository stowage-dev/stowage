---
type: explanation
---

# Why a single binary

Stowage is one Go binary with the SvelteKit frontend embedded. There
is no separate frontend container, no separate database container,
no required Redis or NATS or Kafka. It runs from a single
`./stowage serve --config config.yaml` invocation.

This page explains the deliberate tradeoffs.

## What's embedded

- The SvelteKit dashboard at `web/dist/` is embedded via
  `//go:embed all:dist` in
  [`web/embed.go`](https://github.com/stowage-dev/stowage/blob/main/web/embed.go).
- The SQLite driver is `modernc.org/sqlite` — pure Go, no CGo.
- The argon2id implementation is `golang.org/x/crypto/argon2`.
- The OIDC client is `coreos/go-oidc`.
- The chi router is `go-chi/chi`.

The build needs Go and Bun. The runtime needs nothing except the
binary.

## What we give up

- **Horizontal scale.** SQLite has one writer; in-process caches
  can't be shared. Multi-replica is on the long-term roadmap, not
  v1.0.
- **Postgres.** The `db.driver: postgres` config field exists and
  validates, but the implementation is intentionally absent. SQLite
  is the production database for now.
- **Hot config reload.** No SIGHUP; restart to pick up changes. The
  exception is UI-managed endpoints, which apply live.

## What we gain

- **Trivial install.** `curl … | sh` and you have a working
  dashboard.
- **Trivial upgrade.** Replace the binary, restart.
- **Trivial backup.** Three SQLite files plus an AES key.
- **Predictable resource usage.** One process, one PVC, one set of
  metrics.
- **No dependency rot.** Nothing to keep patched except the Stowage
  binary itself.

## Why no CGo

`modernc.org/sqlite` is a Go translation of the SQLite C source. It's
slower than a CGo binding to libsqlite3 — usually by 10-30 % on
microbenchmarks — but the win is huge:

- Cross-compiling for `windows/amd64` and `darwin/arm64` from a Linux
  build host works without setting up a cross C toolchain.
- The release binaries are statically linked; no glibc-version
  drama.
- The Docker image is distroless with no `libsqlite3` to keep
  patched.

The performance loss versus libsqlite3 is well below the
upstream-S3 round-trip for any operation that touches storage.
Stowage's SQLite usage is mostly auth + audit + shares — small,
indexed queries — where the gap doesn't matter.

## Why no Redis / NATS / Kafka

The proxy + dashboard hot paths fit in-process for a single replica:

- Session attach: one indexed SQLite read.
- CSRF check: header vs cookie comparison.
- Rate limit: in-memory leaky bucket.
- Audit: batched SQLite writer.

Adding an external broker would buy nothing the in-process path
doesn't already deliver, while requiring operators to run, monitor,
and back up another moving part.

## What this means for the future

If multi-replica becomes a hard requirement, the design has to grow
an external session/cache store and either move SQLite to a network
filesystem or add a Postgres backend. The shape of those changes is
visible in the codebase (interfaces over implementations) but the
project hasn't done that work yet, and won't until there's a real
demand it can't meet by scaling vertically.
