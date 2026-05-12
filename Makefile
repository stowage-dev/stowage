BINARY          := stowage
CMD             := ./cmd/stowage
BIN_DIR         := bin

.PHONY: help build run test e2e e2e-bootstrap e2e-teardown chart-test vet lint tidy clean docker frontend setup-hooks

help:
	@echo "Targets:"
	@echo "  build           build the stowage binary into $(BIN_DIR)/"
	@echo "  run             build and run 'stowage serve'"
	@echo "  test            go test ./..."
	@echo "  e2e             bring up a kind cluster and run the e2e suite"
	@echo "  e2e-bootstrap   create the kind cluster + install CRDs (idempotent)"
	@echo "  e2e-teardown    delete the kind cluster"
	@echo "  chart-test      run offline Helm chart tests (lint + template)"
	@echo "  vet             go vet ./..."
	@echo "  lint            vet + staticcheck"
	@echo "  tidy            go mod tidy"
	@echo "  frontend        bun install + bun run build in web/"
	@echo "  docker          build the production stowage Docker image"
	@echo "  clean           remove $(BIN_DIR)/"
	@echo "  setup-hooks     point this clone at .githooks/ (DCO auto-signoff)"

build:
	@mkdir -p $(BIN_DIR)
	go build -trimpath -o $(BIN_DIR)/$(BINARY) $(CMD)

run: build
	$(BIN_DIR)/$(BINARY) serve --config config.demo.yaml

test:
	go test ./...

# Offline Helm chart tests (lint + template snapshots). Needs `helm` on PATH;
# does not need a cluster.
chart-test:
	go test ./test/chart/...

# e2e runs the Kubernetes integration suite against a kind cluster. The
# bootstrap script creates the cluster (if absent), installs the broker
# CRDs from deploy/chart/crds, patches kind nodes so host.docker.internal
# resolves to the docker bridge gateway, and emits the env exports that
# the Go test process needs. We `eval` those exports for this invocation
# only — re-running `make e2e` is idempotent.
e2e-bootstrap:
	./scripts/e2e-bootstrap.sh >/dev/null

e2e-teardown:
	./scripts/e2e-teardown.sh

e2e:
	eval "$$(./scripts/e2e-bootstrap.sh)" && \
		go test -tags e2e -timeout 15m -count=1 ./test/e2e/...

vet:
	go vet ./...

lint: vet
	@# `go run @latest` downgrades the toolchain to staticcheck's go.mod
	@# minimum (go1.25), and the resulting binary then can't analyze our
	@# go1.26 source. Pinning GOTOOLCHAIN matches CI's behaviour — bump
	@# in lockstep with the `go` directive in go.mod.
	GOTOOLCHAIN=go1.26.0 go install honnef.co/go/tools/cmd/staticcheck@latest
	"$$(go env GOPATH)/bin/staticcheck" ./...

tidy:
	go mod tidy

frontend:
	cd web && bun install && bun run build

docker:
	docker build -f deploy/docker/Dockerfile -t stowage:dev .

clean:
	rm -rf $(BIN_DIR)

setup-hooks:
	git config core.hooksPath .githooks
	@echo "Hooks installed. Future commits will auto-append a DCO Signed-off-by."
