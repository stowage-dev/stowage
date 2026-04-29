---
type: reference
---

# Deprecation policy

What happens to features the project decides to remove.

## The rule

A user-visible thing (config field, API endpoint, CRD field, CLI
flag, behaviour) gets one minor-version-deprecation window before
removal. That window is at least one minor release; longer for
features in wide use.

```
v1.4   announce deprecation in release notes; emit a runtime warning
       at startup or per-request. Old behaviour still works.
v1.5   removed.
```

## What constitutes "user-visible"

In scope:

- Config keys and their semantics.
- HTTP API paths, request shapes, response shapes.
- CRD spec and status fields.
- CLI subcommands and flags.
- Audit action names.
- Prometheus metric names and labels.
- Helm chart values.
- Wire-contract Secret data fields (which break the operator/proxy
  cross-binary contract).

Out of scope (free to change without warning):

- Internal Go package layout under `internal/`.
- SQLite schema details that aren't reflected on the wire.
- Build / test infrastructure.
- Internal log line formats.

## How deprecation is communicated

- **Release notes** have a "Deprecations" section listing what's
  going away and when.
- **Runtime warnings** are emitted at startup or per-request when a
  deprecated thing is used. The warnings are explicit about what to
  switch to.
- **Documentation** moves the deprecated section to a "Deprecated"
  banner at the top of the page; it's not deleted until removal.

## Security exceptions

Security fixes may remove a behaviour without the deprecation
window. The release notes are explicit about this when it happens.

## What the user can rely on

- A `v1.x.y` config will keep working on every later `v1.x.z`.
- A `v1.x.y` config will keep working on `v1.(x+1).0` unless that
  release explicitly calls out a deprecation that's been pending.
- A `v1.x.y` config is **not** guaranteed to work on `v2.0.0`.
  Migration notes will accompany the major bump.

## What about pre-v1.0

Anything tagged before v1.0 is effectively unsupported. v1.0 is the
first release with this policy.
