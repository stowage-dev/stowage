---
type: how-to
---

# Authoring docs

How to contribute to *this* documentation site.

## Where docs live

`docs/` in the repo root. The docs site is rendered from these
Markdown files. Each top-level folder corresponds to one of the
[Diátaxis](https://diataxis.fr/) quadrants or a utility section.

## Pick the right quadrant

Every page declares its type in YAML frontmatter:

```markdown
---
type: tutorial | how-to | reference | explanation
---
```

| Type | When | Tone |
|---|---|---|
| `tutorial` | Learning by doing. End state: reader has something working. | Hand-holding, step-numbered, complete. |
| `how-to` | Recipe for a specific task. | Goal-oriented, assumes context. |
| `reference` | Spec lookup. | Dry, exhaustive, alphabetised where applicable. |
| `explanation` | Why it works the way it does. | Discursive, opinionated, contextual. |

**Don't mix two quadrants on one page.** A tutorial that breaks into
reference loses the reader; a reference page that drifts into
explanation buries the spec. Reviewers reject PRs that mix types.

## File naming and links

- Use kebab-case file names: `from-minio-console.md`,
  `key-rotation.md`.
- Internal links are relative: `[Audit](../self-host/audit.md)`.
- Source-code links are GitHub URLs:
  `[backend.go](https://github.com/stowage-dev/stowage/blob/main/internal/backend/backend.go)`.

## Code blocks

- Runnable as written. No `…` ellipses, no implied prior context.
- Default to a Bash-like shell unless the page heading says
  otherwise.
- For Windows-only commands, use a separate code block with a
  PowerShell header.

```sh
# This is a Bash example.
echo 'hi'
```

```powershell
# This is a PowerShell example.
Write-Host 'hi'
```

## Cross-linking between sections

Lean into it. The four quadrants are designed to cross-link:

- A how-to should link to relevant reference pages for the spec
  details it depends on.
- An explanation should link to the how-to that puts the concept
  into practice.
- A tutorial should link to "next steps" in either how-to or
  reference.

Don't duplicate content. If something is documented in
`reference/config.md`, link to it; don't paste a copy.

## Style

- Direct, declarative sentences. "Stowage logs to stdout." not
  "Stowage's logging mechanism outputs to standard output."
- Short paragraphs. A wall of text is a wall of text.
- Concrete numbers over hand-waves. "p99 of 78 ms at 16 concurrency"
  beats "fast under typical load."
- Scope claims to the source. "From `internal/api/router.go`:
  …" beats invented examples.

## When the source disagrees with the docs

The source wins. If you find a documented behaviour that doesn't
match the code, file a bug or send a PR fixing the docs — preferably
both, and tell the maintainer which one you meant.

## Adding a new page

1. Drop the file under the right quadrant folder.
2. Add a link from that folder's `README.md` (the section index).
3. Add the frontmatter `type:` line.
4. Submit a PR with a `docs:` conventional-commit prefix.

## Review

PRs touching docs follow the same review rules as code:

- One logical change per PR.
- Conventional commit message.
- DCO sign-off.
- Reviewers may push back if the page mixes Diátaxis types or
  duplicates content elsewhere on the site.
