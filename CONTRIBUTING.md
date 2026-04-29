# Contributing to Stowage

Thanks for your interest in contributing. This document covers the
practical mechanics. For project direction and what's in scope, see
the [docs/](./docs/) tree and the [issue
tracker](https://github.com/stowage-dev/stowage/issues).

## License of contributions

By submitting a pull request, you agree that your contribution is
licensed under the same license as the project,
[AGPL-3.0-or-later](./LICENSE). You retain copyright in your
contribution; you license it to the Stowage project. There is no
contributor license agreement to sign.

We use the [Developer Certificate of Origin
(DCO)](https://developercertificate.org/) to record this. The DCO is a
short statement that you are entitled to submit the work you're
contributing. To certify it, every commit must carry a `Signed-off-by`
line in its message:

```
Signed-off-by: Your Name <[email protected]>
```

The easiest way to add this is `git commit -s` (or set
`git config commit.gpgsign true` and `git config user.signingkey ...`
if you also want to sign cryptographically — that's optional, the DCO
sign-off is required).

If you forget, the bot will tell you, and you can amend with:

```
git commit --amend --signoff
git push --force-with-lease
```

## Before opening a pull request

1. **Open or claim an issue first** for anything bigger than a typo.
   This avoids wasted work on a direction the project isn't going.
2. **One logical change per PR.** Multi-purpose PRs are hard to review
   and risky to revert.
3. **Tests and `go vet ./...` pass locally.** CI will run them too, but
   it's faster to find problems on your own machine.
4. **Update docs in the same PR.** If the change is user-visible, the
   `README.md` or `/docs` content should reflect it.
5. **Conventional commit messages** (`feat:`, `fix:`, `docs:`, `chore:`,
   `refactor:`, `test:`) — these feed the changelog generator.

## Local development

See [`README.md`](./README.md) for the build-from-source flow. Short
version: Go 1.22+, Bun for the frontend, `make build` for the binary.

## Code of conduct

Be patient. Be specific. Disagree without making it personal. Reviewers
have a right to say no, and contributors have a right to know why.
