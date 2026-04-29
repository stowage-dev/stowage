---
type: how-to
---

# Install from source

Use this when you're contributing, debugging an issue against a
specific commit, or running on an architecture without published
binaries.

## Prerequisites

- Go 1.26 or newer.
- [Bun](https://bun.sh) (only needed if you want to rebuild the
  embedded SvelteKit frontend).
- A C toolchain is **not** required — Stowage uses the pure-Go
  `modernc.org/sqlite` driver.

## Clone

```sh
git clone https://github.com/stowage-dev/stowage.git
cd stowage
```

## Build the frontend (optional)

The Go build embeds `web/dist/` via `//go:embed all:dist`. If you want
the dashboard, build the bundle first:

```sh
cd web
bun install
bun run build
cd ..
```

If you only need a backend-only binary for local testing, scaffold a
stub instead:

```sh
mkdir -p web/dist
[ -f web/dist/index.html ] || echo '<!doctype html><title>stowage</title>' > web/dist/index.html
```

This is what CI does when running Go-only jobs.

## Build the binary

```sh
make build
# → bin/stowage
```

For the operator binary as well:

```sh
make build-all
# → bin/stowage and bin/stowage-operator
```

## Test and lint

```sh
make test         # go test ./...
make vet          # go vet ./...
make lint         # vet + staticcheck
```

CI runs `go test -race`. Don't introduce data races, even in helpers.

## Run

```sh
./bin/stowage create-admin \
  --config config.example.yaml \
  --username admin \
  --password 'S3cur3-P@ssw0rd!'

./bin/stowage serve --config config.example.yaml
```

## Cross-compiling

Go's standard `GOOS`/`GOARCH` env vars work:

```sh
GOOS=linux   GOARCH=arm64 go build -trimpath -o bin/stowage-linux-arm64   ./cmd/stowage
GOOS=darwin  GOARCH=arm64 go build -trimpath -o bin/stowage-darwin-arm64  ./cmd/stowage
GOOS=windows GOARCH=amd64 go build -trimpath -o bin/stowage-windows.exe   ./cmd/stowage
```

The release pipeline does the same with goreleaser. See
[`Makefile`](https://github.com/stowage-dev/stowage/blob/main/Makefile)
for the canonical recipe.
