# Stowage documentation

An OIDC-authenticated dashboard, embedded S3 SigV4 proxy, and optional
Kubernetes operator that sit in front of any S3-compatible backend (MinIO,
Garage, SeaweedFS, AWS S3, Backblaze B2, Cloudflare R2, Wasabi). One Go
binary, embedded SvelteKit frontend, SQLite by default, AGPL-3.0-or-later.

This site is organised after the [Diátaxis](https://diataxis.fr/)
framework. Pick the entry point that matches what you're trying to do:

- **[Get started](./getting-started/)** — guided walkthroughs that end
  with you having something running. Start here if you've never run
  Stowage before.
- **[Self-host](./self-host/)** — task-oriented recipes for running
  Stowage on a single host or VM (homelab, internal team server,
  small-org deployment).
- **[Run on Kubernetes](./kubernetes/)** — Helm chart, the optional
  operator, `S3Backend` and `BucketClaim` CRDs, virtual credentials
  brokered through Secrets.
- **[Use as an S3 endpoint](./s3-endpoint/)** — for tenant developers
  pointing AWS SDKs at the embedded SigV4 proxy.
- **[Reference](./reference/)** — exhaustive specs: every CLI flag, every
  config key, every API endpoint, every CRD field, every metric.
- **[Explanations](./explanations/)** — how it's built and why.
  Architecture, threat model, the rationale for AGPL, design tradeoffs.

Top-level utility links:

- **[Security](./security/)** — security model, hardening checklist,
  reporting a vulnerability.
- **[Contributing](./contribute/)** — how to send patches, DCO sign-off,
  repo layout, build instructions.
- **[Releases](./releases/)** — changelog and upgrade guides.
- **[Comparisons](./comparisons/)** — Stowage vs MinIO and friends.

## How this documentation is organised

| Quadrant | Folder | When to read |
|---|---|---|
| Tutorial | `getting-started/` | "I'm new — teach me by doing." |
| How-to guide | `self-host/`, `kubernetes/`, `s3-endpoint/` | "I have a specific job — show me the steps." |
| Reference | `reference/` | "I know what I want — where's the spec?" |
| Explanation | `explanations/` | "Why does it work this way?" |

Pages declare their type in the frontmatter. Reviewers reject PRs that
mix two types on one page — that's how documentation rots.

## Conventions

- Code blocks are runnable as written. No `…` ellipses, no implied
  context.
- Examples assume a Bash-like shell unless a heading says otherwise.
- Every config key, CLI flag, and CRD field links to the source file
  that defines it so readers can verify rather than trust.
- "Last updated" timestamps live at the bottom of each page; the
  dates reflect content review, not casual edits.

## Getting unstuck

- Search this site (top-right).
- Ask in the
  [GitHub Discussions](https://github.com/stowage-dev/stowage/discussions).
- File an issue on
  [GitHub](https://github.com/stowage-dev/stowage/issues).
- Security reports: see [Security → Reporting a vulnerability](./security/report-vulnerability.md).
