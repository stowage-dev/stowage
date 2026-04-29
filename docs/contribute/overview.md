---
type: how-to
---

# Contributing

The canonical version of this page is
[`CONTRIBUTING.md`](https://github.com/stowage-dev/stowage/blob/main/CONTRIBUTING.md).
This page is the docs-site mirror with the same rules.

## License

By submitting a pull request, you agree your contribution is
licensed under [AGPL-3.0-or-later](../explanations/why-agpl.md). You
retain copyright in your contribution; you license it to the
project. There is no CLA.

## DCO sign-off

Every commit must carry a `Signed-off-by` line. See
[DCO sign-off](./dco.md).

## Before opening a pull request

1. **Open or claim an issue first** for anything bigger than a typo.
   This avoids wasted work on a direction the project isn't going.
2. **One logical change per PR.** Multi-purpose PRs are hard to
   review and risky to revert.
3. **Tests and `go vet ./...` pass locally.** CI will run them too,
   but it's faster to find problems on your own machine.
4. **Update docs in the same PR.** If the change is user-visible,
   `README.md` and `/docs` should reflect it.
5. **Conventional commit messages** (`feat:`, `fix:`, `docs:`,
   `chore:`, `refactor:`, `test:`) — these feed the changelog
   generator.

## What's in scope

- Bug fixes.
- New features that fit the project's positioning (vendor-neutral
  S3 dashboard with audit, sharing, and a SigV4 proxy).
- Documentation, examples, and tests.
- Backend drivers (`internal/backend/<vendor>/`).
- Performance work backed by pprof / benchmark numbers.

## What's out of scope

- Anything that breaks the [single-binary](../explanations/single-binary.md)
  story. New external services need a strong justification.
- Backwards-incompatible changes without a deprecation path.
- Features the [Why AGPL](../explanations/why-agpl.md) page rules
  out (paid edition gating, source-available license stuff).
- Changes that lock Stowage to one upstream backend — the project
  is explicitly vendor-neutral.

## Code of conduct

- Be patient.
- Be specific.
- Disagree without making it personal.
- Reviewers have a right to say no, and contributors have a right to
  know why.

## Reviewing other people's PRs

Reviewers from outside the maintainer team are welcome. Sign off
with the same DCO line on review comments to make it explicit when
you've genuinely tested vs eyeballed.
