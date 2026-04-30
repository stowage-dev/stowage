# stowage

[![Website](https://img.shields.io/badge/website-stowage.dev-0a7cff.svg)](https://stowage.dev)
[![License: AGPL-3.0-or-later](https://img.shields.io/badge/License-AGPL%203.0--or--later-blue.svg)](./LICENSE)
[![CI](https://github.com/stowage-dev/stowage/actions/workflows/ci.yml/badge.svg)](https://github.com/stowage-dev/stowage/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/stowage-dev/stowage?include_prereleases&sort=semver)](https://github.com/stowage-dev/stowage/releases/latest)
[![Go reference](https://pkg.go.dev/badge/github.com/stowage-dev/stowage.svg)](https://pkg.go.dev/github.com/stowage-dev/stowage)

**Official site: [stowage.dev](https://stowage.dev) · Documentation: [stowage.dev/docs](https://stowage.dev/docs) · Downloads: [stowage.dev/download](https://stowage.dev/download)**

A single Go binary that puts a modern web dashboard, an embedded
AWS-SigV4 S3 proxy, and an optional Kubernetes operator in front of any
S3-compatible backend — MinIO, Garage, SeaweedFS, AWS S3, Cloudflare R2,
Backblaze B2, Wasabi. One pane of glass for the storage you already
run, with audit, quotas, share links, and per-tenant SDK credentials,
without locking you to a vendor.

AGPL-3.0-or-later. No community edition, no enterprise tier — see
[Why AGPL](https://stowage.dev/docs/explanations/why-agpl) and
[No community edition](https://stowage.dev/docs/explanations/no-community-edition)
for the rationale.

## Quickstart

Three paths. Pick the one that matches where you want Stowage to live.
Each path is documented end-to-end at
[stowage.dev/docs/getting-started](https://stowage.dev/docs/getting-started).

### One-liner (Linux, macOS, WSL)

```sh
curl -fsSL https://stowage.dev/install.sh | sh
```

### Windows (PowerShell)

```powershell
irm https://stowage.dev/install.ps1 | iex
```

The installer downloads a SHA256-verified release binary into the
current directory and execs `stowage quickstart`, which spawns a managed
MinIO, creates a SQLite DB, prints a random admin password, and opens
the dashboard on `http://localhost:8080`. Nothing is installed
system-wide. Checksums and signatures for every release are published at
[stowage.dev/releases](https://stowage.dev/releases).

### Docker Compose

```sh
docker compose -f deploy/compose/docker-compose.yml up -d
docker compose -f deploy/compose/docker-compose.yml exec stowage \
  stowage create-admin --username admin --password 'S3cur3-P@ssw0rd!'
```

### Kubernetes (Helm)

```sh
helm install stowage ./deploy/chart \
  --namespace stowage-system --create-namespace \
  --set ingress.enabled=true --set ingress.host=stowage.example.com

kubectl -n stowage-system exec deploy/stowage -- \
  stowage create-admin --username admin --password 'S3cur3-P@ssw0rd!'
```

Full walkthroughs:
[one-liner](https://stowage.dev/docs/getting-started/quickstart-oneliner) ·
[Docker Compose](https://stowage.dev/docs/getting-started/quickstart-compose) ·
[Kubernetes](https://stowage.dev/docs/getting-started/quickstart-kubernetes).

## What ships in v1.0

A complete feature matrix lives at
[stowage.dev/features](https://stowage.dev/features); the summary
below mirrors it.

**Dashboard.** OIDC, local accounts with argon2id hashing, or a static
admin from env. Object browser with multi-select, drag-and-drop
multipart upload (16 MiB parts, pause/resume), preview (text / image /
PDF / video), version history, tags + metadata, single-object rename,
cross-bucket / cross-prefix move and copy, and streamed
download-as-zip.

**Public sharing.** Share links with argon2id passwords, expiry presets,
atomic download caps (`UPDATE … WHERE used < cap` — no race), and
per-IP rate limiting. No presigned-URL plumbing.

**Per-tenant SDK access.** A second listener (default `:8090`) accepts
AWS SigV4 requests with per-tenant virtual credentials, verifies the
signature, enforces bucket scope, and re-signs to the upstream with the
backend's admin credentials. Standard AWS SDKs work unmodified:

```sh
AWS_ACCESS_KEY_ID=AKIA... AWS_SECRET_ACCESS_KEY=... \
  aws --endpoint-url http://stowage:8090 \
  s3 cp ./hello.txt s3://uploads/hello.txt
```

`ListBuckets` is synthesised per-credential — tenants only see what they
were granted.

**Multi-backend workflows.** Cross-backend object copy streams through
the proxy host. Per-user pinned buckets across backends. Unified search
fans out across every configured endpoint with per-backend cancellation.

**Quotas.** Soft and hard caps per bucket, scheduled scanner, `507
Insufficient Storage` on hard-cap, in-browser warning banner at the
soft cap. Quotas apply equally to dashboard uploads and SDK uploads.

**Audit + observability.** SQLite-backed audit log with per-event detail
JSON, CSV export, and a filtered list at `/admin/audit`. Prometheus
`/metrics` with bounded label cardinality, plus a starter Grafana
dashboard at
[`deploy/grafana/stowage.json`](./deploy/grafana/stowage.json).

**Kubernetes-native (optional).** A `BucketClaim` CRD provisions a
bucket on the upstream and writes an `aws-sdk`-shaped Secret into the
requesting namespace. An `S3Backend` CRD declares the upstream. The
operator and the dashboard ship from the same Helm chart.

## Architecture

One Go binary. SvelteKit frontend embedded via `//go:embed`. SQLite
(pure-Go, no CGo) holds users, sessions, shares, audit events, sealed
backend secrets, and virtual credentials. Endpoint secrets and tenant
secret keys are sealed with AES-256-GCM under a master key from
`STOWAGE_SECRET_KEY` (or an auto-generated `stowage.key` file, mode
0600).

The runtime needs nothing except the binary — no Redis, no NATS, no
external auth service, no separate frontend container. The build needs
Go and Bun. See
[Why one binary](https://stowage.dev/docs/explanations/single-binary)
for the tradeoffs (single-replica, SQLite-only, no hot config reload —
all deliberate). The architecture overview, including diagrams, is at
[stowage.dev/docs/explanations/architecture](https://stowage.dev/docs/explanations/architecture).

Stowage does **not** store object bytes. Data lives on the upstream;
Stowage proxies access to it.

## Documentation

The canonical, versioned documentation is at
[**stowage.dev/docs**](https://stowage.dev/docs). The sources also live
in [`./docs/`](./docs/) in this repo for offline reading. Both follow
[Diátaxis](https://diataxis.fr/):

| Section | When to read |
|---|---|
| **[Getting started →](https://stowage.dev/docs/getting-started)** | "I'm new — teach me by doing." |
| **[Self-host →](https://stowage.dev/docs/self-host)** | "I want this on a single host. Show me the recipes." |
| **[Run on Kubernetes →](https://stowage.dev/docs/kubernetes)** | Helm chart, operator, CRDs, virtual credentials. |
| **[Use as an S3 endpoint →](https://stowage.dev/docs/s3-endpoint)** | For tenant developers pointing AWS SDKs at the proxy. |
| **[Reference →](https://stowage.dev/docs/reference)** | Every CLI flag, config key, API endpoint, CRD field, metric. |
| **[Explanations →](https://stowage.dev/docs/explanations)** | Architecture, threat model, design tradeoffs. |
| **[Security →](https://stowage.dev/docs/security)** | Threat model, hardening checklist, vulnerability reporting. |
| **[Comparisons →](https://stowage.dev/docs/comparisons)** | Stowage vs [MinIO Console](https://stowage.dev/docs/comparisons/minio), [Cyberduck](https://stowage.dev/docs/comparisons/cyberduck), [raw S3 + presigned URLs](https://stowage.dev/docs/comparisons/raw-s3). |

## Building from source

Requires Go 1.26+ and [Bun](https://bun.sh) for the frontend.

```sh
make frontend    # bun install + bun run build → web/dist/
make build       # bin/stowage
make test
make docker      # multi-stage distroless image
```

Tagged releases publish multi-arch (`linux/amd64`, `linux/arm64`,
`darwin/amd64`, `darwin/arm64`, `windows/amd64`) binaries — listed on
[stowage.dev/download](https://stowage.dev/download) and mirrored to
[GitHub Releases](https://github.com/stowage-dev/stowage/releases) — and
multi-arch (`linux/amd64`, `linux/arm64`) container images on
`ghcr.io/stowage-dev/stowage`. Images are signed with cosign (keyless),
ship with SBOMs, and carry SLSA provenance attestations. Verification
recipes are at
[stowage.dev/docs/security/verify-releases](https://stowage.dev/docs/security/verify-releases).

## Deploying

Stowage listens on plaintext HTTP and expects TLS to be terminated by a
reverse proxy. See
[Self-host → Reverse proxy](https://stowage.dev/docs/self-host/reverse-proxy)
for the required headers, the `server.trusted_proxies` config, and
worked examples for nginx, Caddy, and Traefik. The
[hardening checklist](https://stowage.dev/docs/security/hardening-checklist)
is the pre-production gate.

## Security

Report vulnerabilities privately via
[GitHub Security Advisories](https://github.com/stowage-dev/stowage/security/advisories/new)
or by following the disclosure process at
[stowage.dev/security](https://stowage.dev/security). Do not open
public issues for security reports. The full policy, response SLAs, and
safe-harbour terms are in [`SECURITY.md`](./SECURITY.md) and
[stowage.dev/docs/security/report-vulnerability](https://stowage.dev/docs/security/report-vulnerability).

For operators, the
[security model](https://stowage.dev/docs/security/model) summarises
every defence (authentication, authorization, sigv4 proxy, sharing,
secret handling, HTTP, audit) and links each to the source file that
implements it.

## Community

- **Website:** [stowage.dev](https://stowage.dev)
- **Blog & release notes:** [stowage.dev/blog](https://stowage.dev/blog)
- **Source:** [github.com/stowage-dev/stowage](https://github.com/stowage-dev/stowage)
- **Discussions:** [github.com/stowage-dev/stowage/discussions](https://github.com/stowage-dev/stowage/discussions)
- **Issues:** [github.com/stowage-dev/stowage/issues](https://github.com/stowage-dev/stowage/issues)

## Contributing

PRs welcome. All commits must be signed off (`git commit -s`) under the
[Developer Certificate of Origin](https://developercertificate.org/) —
the DCO check in CI will tell you if you forget. There is no CLA. See
[`CONTRIBUTING.md`](./CONTRIBUTING.md) and
[stowage.dev/docs/contributing](https://stowage.dev/docs/contributing)
for the workflow, and [`CODE_OF_CONDUCT.md`](./CODE_OF_CONDUCT.md) for
community standards.

## License

[AGPL-3.0-or-later](./LICENSE). Running unmodified Stowage — including
inside a company, inside a homelab, or as part of a SaaS — imposes no
publication obligation. If you modify Stowage and expose those
modifications to other users over a network, you must publish your
changes under the same license. The rationale is in
[Why AGPL](https://stowage.dev/docs/explanations/why-agpl).

## Maintainer

Built and maintained by
[Damian van der Merwe](https://damianvandermerwe.com), an
infrastructure & DevOps engineer based in Hamilton, New Zealand. Project
home: [stowage.dev](https://stowage.dev).
