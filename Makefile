BINARY          := stowage
CMD             := ./cmd/stowage
BIN_DIR         := bin

.PHONY: help build run test envtest envtest-assets vet lint tidy clean docker frontend setup-hooks

help:
	@echo "Targets:"
	@echo "  build           build the stowage binary into $(BIN_DIR)/"
	@echo "  run             build and run 'stowage serve'"
	@echo "  test            go test ./..."
	@echo "  envtest         go test -tags envtest ./... (operator + s3proxy)"
	@echo "  envtest-assets  download kube-apiserver/etcd via setup-envtest"
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

# envtest spins up a real kube-apiserver+etcd via sigs.k8s.io/controller-runtime/tools/setup-envtest
# and exercises the operator controllers, admission webhooks, vcstore Reader/Writer, and the
# s3proxy KubernetesSource against the real informer.
ENVTEST_K8S_VERSION ?= 1.32.0

envtest-assets:
	@command -v setup-envtest >/dev/null 2>&1 || \
		go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest
	setup-envtest use $(ENVTEST_K8S_VERSION) -p path

envtest: envtest-assets
	KUBEBUILDER_ASSETS="$$(setup-envtest use $(ENVTEST_K8S_VERSION) -p path)" \
		go test -tags envtest -timeout 10m \
		./internal/operator/... ./internal/s3proxy/...

vet:
	go vet ./...

lint: vet
	go run honnef.co/go/tools/cmd/staticcheck@latest ./...

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
