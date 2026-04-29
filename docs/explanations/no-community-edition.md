---
type: explanation
---

# No community edition

There is one Stowage. There is no separately-gated "free" tier and
"enterprise" tier. Every feature in the codebase ships in the AGPL
build:

- The dashboard.
- The share-link system.
- The audit log + CSV export.
- The embedded SigV4 proxy.
- The Kubernetes operator with `S3Backend` and `BucketClaim` CRDs.
- The starter Grafana dashboard.
- Cross-backend transfers.
- Per-bucket quotas.
- Endpoint management at `/admin/endpoints`.
- All current and future drivers under `internal/backend/`.

## What this means structurally

- The maintainer holds copyright on original code.
- Contributions arrive under DCO without copyright transfer
  (see [`CONTRIBUTING.md`](https://github.com/stowage-dev/stowage/blob/main/CONTRIBUTING.md)).
- The license is AGPL-3.0-or-later, OSI-approved.

These three together make a "carve out a paid edition later"
strategy hard. The maintainer would have to relicense future code
(legal, but a one-way door for community trust) and contributors
would have to re-sign new agreements (which most won't).

## What this means practically

- The AGPL build will never be feature-stripped to push you toward a
  paid edition. The features you use today will remain available
  under the same license forever.
- If a commercial license is ever offered, it will exist *alongside*
  the AGPL build, not *instead of* it.
- Forks made today against the AGPL build will keep working under
  the AGPL even if the upstream changes its mind later.

## Why be explicit about this

A surprising number of projects declare themselves "open source"
while structurally setting themselves up to pull a MinIO-Console
move later. The pattern is well-known: start with a permissive or
copyleft license, build a community, then relicense after the user
base is captive.

Stowage's specific origin story (see
[Why AGPL](./why-agpl.md)) means the project has to be especially
clear about what it won't do.

## What you should still verify

This document is a project commitment, not a contract. To verify:

- Read `LICENSE` (the AGPL-3.0 text) and
  [`docs/explanations/why-agpl.md`](./why-agpl.md) (the project's own
  rationale).
- Check `CONTRIBUTING.md` to see how contributions flow (DCO, no
  copyright transfer).
- Look at the `git log` — there's no relicensing event in the
  history.

If any of these change in the future, the precedent is the change
itself, not this page.

## Future commercial licensing — what's documented

The
[why-agpl explanation](./why-agpl.md)
says:

> The maintainer holds copyright on all original Stowage code, and
> contributions are accepted under DCO without copyright transfer.
> This preserves the option to offer commercial licenses to
> organisations that cannot adopt AGPL for policy reasons, without
> affecting the freely available AGPL-licensed version. **No
> commercial license is offered today; this paragraph is here so the
> option is documented.**

If a commercial license is offered later, it will be advertised
publicly and will not feature-strip the AGPL build. Both license
options would carry the same code; the commercial option would only
relax the AGPL's "publish modifications when you offer the modified
version over a network" clause for organisations that can't comply
with it for policy reasons.
