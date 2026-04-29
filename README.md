# stowage

[![License: AGPL-3.0-or-later](https://img.shields.io/badge/License-AGPL%203.0--or--later-blue.svg)](./LICENSE)
[![CI](https://github.com/stowage-dev/stowage/actions/workflows/ci.yml/badge.svg)](https://github.com/stowage-dev/stowage/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/stowage-dev/stowage?include_prereleases&sort=semver)](https://github.com/stowage-dev/stowage/releases/latest)
[![Go reference](https://pkg.go.dev/badge/github.com/stowage-dev/stowage.svg)](https://pkg.go.dev/github.com/stowage-dev/stowage)

A single Go binary that puts a modern web dashboard, an embedded
AWS-SigV4 S3 proxy, and an optional Kubernetes operator in front of any
S3-compatible backend — MinIO, Garage, SeaweedFS, AWS S3, Cloudflare R2,
Backblaze B2, Wasabi. One pane of glass for the storage you already
run, with audit, quotas, share links, and per-tenant SDK credentials,
without locking you to a vendor.

AGPL-3.0-or-later. No community edition, no enterprise tier — see
[Why AGPL](./docs/explanations/why-agpl.md) and
[No community edition](./docs/explanations/no-community-edition.md) for
the rationale.

## Quickstart

Three paths. Pick the one that matches where you want Stowage to live.

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
system-wide.

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
[one-liner](./docs/getting-started/quickstart-oneliner.md) ·
[Docker Compose](./docs/getting-started/quickstart-compose.md) ·
[Kubernetes](./docs/getting-started/quickstart-kubernetes.md).

## What ships in v1.0

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
Go and Bun. See [Why one binary](./docs/explanations/single-binary.md)
for the tradeoffs (single-replica, SQLite-only, no hot config reload —
all deliberate).

Stowage does **not** store object bytes. Data lives on the upstream;
Stowage proxies access to it.

## Documentation

The docs follow [Diátaxis](https://diataxis.fr/):

| Section | When to read |
|---|---|
| **[Getting started →](./docs/getting-started/)** | "I'm new — teach me by doing." |
| **[Self-host →](./docs/self-host/)** | "I want this on a single host. Show me the recipes." |
| **[Run on Kubernetes →](./docs/kubernetes/)** | Helm chart, operator, CRDs, virtual credentials. |
| **[Use as an S3 endpoint →](./docs/s3-endpoint/)** | For tenant developers pointing AWS SDKs at the proxy. |
| **[Reference →](./docs/reference/)** | Every CLI flag, config key, API endpoint, CRD field, metric. |
| **[Explanations →](./docs/explanations/)** | Architecture, threat model, design tradeoffs. |
| **[Security →](./docs/security/)** | Threat model, hardening checklist, vulnerability reporting. |
| **[Comparisons →](./docs/comparisons/)** | Stowage vs [MinIO Console](./docs/comparisons/minio.md), [Cyberduck](./docs/comparisons/cyberduck.md), [raw S3 + presigned URLs](./docs/comparisons/raw-s3.md). |

## Building from source

Requires Go 1.26+ and [Bun](https://bun.sh) for the frontend.

```sh
make frontend    # bun install + bun run build → web/dist/
make build       # bin/stowage
make test
make docker      # multi-stage distroless image
```

Tagged releases publish multi-arch (`linux/amd64`, `linux/arm64`,
`darwin/amd64`, `darwin/arm64`, `windows/amd64`) binaries on
[GitHub Releases](https://github.com/stowage-dev/stowage/releases) and
multi-arch (`linux/amd64`, `linux/arm64`) container images on
`ghcr.io/stowage-dev/stowage`. Images are signed with cosign (keyless),
ship with SBOMs, and carry SLSA provenance attestations.

## Deploying

Stowage listens on plaintext HTTP and expects TLS to be terminated by a
reverse proxy. See
[Self-host → Reverse proxy](./docs/self-host/reverse-proxy/) for the
required headers, the `server.trusted_proxies` config, and worked
examples for nginx, Caddy, and Traefik. The
[hardening checklist](./docs/security/hardening-checklist.md) is the
pre-production gate.

## Security

Report vulnerabilities privately via
[GitHub Security Advisories](https://github.com/stowage-dev/stowage/security/advisories/new).
Do not open public issues for security reports. The full policy,
response SLAs, and safe-harbour terms are in
[`SECURITY.md`](./SECURITY.md) and
[`docs/security/report-vulnerability.md`](./docs/security/report-vulnerability.md).

For operators, the [security model](./docs/security/model.md)
summarises every defence (authentication, authorization, sigv4 proxy,
sharing, secret handling, HTTP, audit) and links each to the source
file that implements it.

## Contributing

PRs welcome. All commits must be signed off (`git commit -s`) under the
[Developer Certificate of Origin](https://developercertificate.org/) —
the DCO check in CI will tell you if you forget. There is no CLA. See
[`CONTRIBUTING.md`](./CONTRIBUTING.md) for the workflow and
[`CODE_OF_CONDUCT.md`](./CODE_OF_CONDUCT.md) for community standards.

## License

[AGPL-3.0-or-later](./LICENSE). Running unmodified Stowage — including
inside a company, inside a homelab, or as part of a SaaS — imposes no
publication obligation. If you modify Stowage and expose those
modifications to other users over a network, you must publish your
changes under the same license. The rationale is in
[Why AGPL](./docs/explanations/why-agpl.md).

## Maintainer

Built and maintained by
[Damian van der Merwe](https://damianvandermerwe.com), an
infrastructure & DevOps engineer based in Hamilton, New Zealand.
