---
type: how-to
---

# Build, test, lint

Mechanics for working on the Stowage codebase.

## Prerequisites

- Go 1.26 or newer.
- [Bun](https://bun.sh) for the frontend.
- A C toolchain is **not** needed (`modernc.org/sqlite` is pure Go).

## Build

```sh
make build           # → bin/stowage
make build-operator  # → bin/stowage-operator
make build-all       # both
```

The Go module path is `stowage` (no domain prefix). The operator
imports the same internal packages as the dashboard, so they always
build from the same source tree.

## Test

```sh
make test            # go test ./...
```

CI runs with `-race`:

```sh
go test -race ./...
```

Don't introduce data races even in helpers — CI catches them but
catching them locally is faster.

## envtest (operator + s3proxy)

The operator's controllers and the proxy's Kubernetes informer have
end-to-end tests against a real `kube-apiserver`+`etcd`:

```sh
make envtest-assets  # downloads kube-apiserver/etcd via setup-envtest
make envtest         # runs operator + s3proxy envtest
```

`ENVTEST_K8S_VERSION` defaults to `1.32.0`; override to test against
a specific Kubernetes version.

## Vet

```sh
make vet             # go vet ./...
```

## Lint

```sh
make lint            # vet + staticcheck
```

Staticcheck is invoked via `go run` so contributors don't need it
installed locally.

## Tidy

```sh
make tidy            # go mod tidy
```

## Frontend

```sh
make frontend        # cd web && bun install && bun run build
```

Or directly:

```sh
cd web
bun install --frozen-lockfile
bun run lint         # prettier + eslint
bun run check        # svelte-check
bun run build        # production bundle to web/dist
```

## Frontend conventions

See [Frontend conventions](./frontend.md).

## CI

Lives in `.github/workflows/`:

| Workflow | What it runs |
|---|---|
| `ci.yml` | Go: vet → build → `go test -race` → staticcheck. Frontend: `bun install --frozen-lockfile` → `bun run lint` → `bun run build`. |
| `dco.yml` | Verifies `Signed-off-by` on every commit in the PR. |
| `oci-release.yml` | Builds and pushes Docker images on tag. |
| `release.yml` | Builds and uploads release binaries on tag. |
| `benchmark.yml` | Runs the bench harness on demand and on schedule. |

## Stub frontend for backend-only work

The Go build needs `web/dist/index.html` because of the
`//go:embed all:dist` directive. CI scaffolds a stub when the bundle
is absent. Locally, do the same when you don't want to wait for Bun:

```sh
mkdir -p web/dist
[ -f web/dist/index.html ] || echo '<!doctype html><title>stowage</title>' > web/dist/index.html
```

## Docker images

```sh
make docker          # multi-stage stowage image
make docker-operator # operator image
```

Both use `deploy/docker/Dockerfile` and
`deploy/operator/Dockerfile` respectively.

## SPDX header

Every Go file starts with:

```go
// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later
```

Add this to any new file. Other languages follow their own
conventional comment syntax for the same two lines.
