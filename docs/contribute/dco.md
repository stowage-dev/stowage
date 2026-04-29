---
type: how-to
---

# DCO sign-off

Stowage uses the
[Developer Certificate of Origin](https://developercertificate.org/)
to record that contributors have the right to submit the work
they're contributing. Every commit must carry a `Signed-off-by`
trailer.

## What the DCO says

A short statement that you, the contributor, are entitled to submit
the work. By signing off, you affirm that:

- You wrote the work yourself, or
- The work is based on previous work made available under an
  appropriate open-source license, or
- The work was provided to you by someone who certified the same.

The full text is at developercertificate.org. It's intentionally
short — read it once.

## Signing off

The mechanical bit: every commit needs this trailer in the message:

```
Signed-off-by: Your Name <[email protected]>
```

The easiest way:

```sh
git commit --signoff -m "fix: explain the thing"
```

`--signoff` (or `-s`) appends the trailer using the email from your
local git config. Set it once:

```sh
git config user.name 'Your Name'
git config user.email '[email protected]'
```

If you also want to GPG-sign (separate from DCO sign-off):

```sh
git config commit.gpgsign true
git config user.signingkey <key-id>
```

## Fixing a missed sign-off

For a single tip commit:

```sh
git commit --amend --reset-author --signoff --no-edit
git push --force-with-lease
```

For several commits, rebase with `--exec`:

```sh
git rebase -x 'git commit --amend --reset-author --signoff --no-edit' main
git push --force-with-lease
```

## CI enforcement

The
[DCO workflow](https://github.com/stowage-dev/stowage/blob/main/.github/workflows/dco.yml)
runs on every PR and blocks merges if any commit is missing the
sign-off trailer. The bot tells you which commit is missing it; fix
and force-push.

## Ghost-writing on someone else's behalf

If you're committing someone else's work (pair programming, applying
a patch from a mailing list), use both `--author` and
`Co-Authored-By:`:

```sh
GIT_AUTHOR_NAME='Original Author' \
GIT_AUTHOR_EMAIL='[email protected]' \
git commit --signoff -m "feat: ...

Co-Authored-By: You <[email protected]>"
```

The `Signed-off-by` trailer goes on every commit regardless. The
author of the work is responsible for being entitled to submit it.
