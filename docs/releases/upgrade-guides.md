---
type: how-to
---

# Upgrade guides

Per-version notes on what changes between adjacent releases, with
the upgrade path for any breaking changes.

The general procedure is documented at:

- [Self-host → Upgrade](../self-host/operations/upgrade.md) for
  binary / Docker installs.
- [Kubernetes → Upgrade](../kubernetes/upgrade.md) for Helm.

## v1.0.0 → next

(Pending — populated when the first post-v1.0 release ships.)

## Breaking changes policy

Stowage v1.x follows semver-ish behaviour:

- **Patch (`v1.0.x`)** — bug fixes and security fixes. No breaking
  changes.
- **Minor (`v1.x.0`)** — new features. Backwards-compatible config
  and API.
- **Major (`vX.0.0`)** — may break config, API, or DB schema.
  Migration tooling and notes will accompany the release.

Until v2 ships, that's the contract.

## What "breaking" means in practice

| Change | Breaking? |
|---|---|
| New config field with a sensible default | No. |
| Removed config field | Yes. Will be deprecated for one minor before removal. |
| New API endpoint | No. |
| Removed API endpoint | Yes. Same deprecation window. |
| New optional CRD field | No. |
| Removed CRD field | Yes. Requires a CRD bump. |
| Schema migration that adds a column | No. |
| Schema migration that drops a column | Yes. Will be batched into a major version. |
| Wire-contract Secret data field rename | Yes — operator + proxy must move in lockstep. Documented as a breaking change. |

## Reading the release notes

Every release on GitHub Releases has:

- A summary line.
- A "Breaking changes" section if applicable.
- "Features", "Fixes", "Other" sections derived from
  conventional commits.
- An "Upgrade notes" section if the release needs operator action
  beyond replacing the binary.

When you upgrade, read the "Breaking changes" and "Upgrade notes"
sections of every intermediate release between your current version
and the target.
