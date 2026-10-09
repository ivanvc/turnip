.PHONY: build docker-build docker-push test docs-check test-load test-kind lint manifests-check fmt proto-gen clean

BIN_DIR := bin
SERVER_BIN := $(BIN_DIR)/server
RUNNER_BIN := $(BIN_DIR)/runner

# DOCKER_REGISTRY is a single repo path (not a namespace) -- docker-build/
# docker-push distinguish server vs. runner by tag prefix, not by repo
# name, so a non-default registry should follow that same convention.
# Override per-invocation, e.g. `make docker-push DOCKER_REGISTRY=ghcr.io/ivanvc/turnip`.
DOCKER_REGISTRY ?= docker.io/ivan/turnip
TAG ?= $(shell git rev-parse --short=7 HEAD)

build: $(SERVER_BIN) $(RUNNER_BIN)

$(SERVER_BIN):
	go build -o $(SERVER_BIN) ./cmd/server

$(RUNNER_BIN):
	go build -o $(RUNNER_BIN) ./cmd/runner

docker-build:
	docker build -f build/server/Dockerfile -t $(DOCKER_REGISTRY):server-$(TAG) .
	docker build -f build/runner/Dockerfile -t $(DOCKER_REGISTRY):runner-$(TAG) .

docker-push: docker-build
	docker push $(DOCKER_REGISTRY):server-$(TAG)
	docker push $(DOCKER_REGISTRY):runner-$(TAG)

test: docs-check
	go test -race ./...

# Tools that are not Go modules of their own are downloaded into
# $(BIN_DIR) on first use, as their release binaries, and verified against
# the release's published checksums. Each one's version is the image tag
# in build/tools/<tool>/Dockerfile, a file never built that exists only
# so Dependabot opens a pull request for each new release. The binary's
# name carries its version, so a bumped tag downloads the new one (and
# removes the old). Nothing is installed outside this repository.
UNAME_S := $(shell uname -s)
UNAME_M := $(shell uname -m)
ARCH_ARM := $(filter arm64 aarch64,$(UNAME_M))

# docs-check verifies every relative link and #anchor in the repository's
# Markdown resolves, with lychee (https://github.com/lycheeverse/lychee),
# offline: external links are not fetched. It reads only the files git
# tracks, so untracked scratch directories are never checked, and skips
# .kiro/ and openspec/ (specs cite paths as history, not as links to
# follow).
LYCHEE_VERSION := $(shell sed -n 's|^FROM docker.io/lycheeverse/lychee:||p' build/tools/lychee/Dockerfile)
LYCHEE := $(BIN_DIR)/lychee-$(LYCHEE_VERSION)
ifeq ($(UNAME_S),Darwin)
  LYCHEE_TARGET := $(if $(ARCH_ARM),aarch64,x86_64)-apple-darwin
else
  LYCHEE_TARGET := $(if $(ARCH_ARM),aarch64,x86_64)-unknown-linux-gnu
endif
LYCHEE_URL := https://github.com/lycheeverse/lychee/releases/download/lychee-v$(LYCHEE_VERSION)/lychee-$(LYCHEE_TARGET).tar.gz

$(LYCHEE):
	@mkdir -p $(BIN_DIR)
	@tmp=$$(mktemp -d); trap 'rm -rf "$$tmp"' EXIT; \
	curl -sSfL -o "$$tmp/lychee-$(LYCHEE_TARGET).tar.gz" "$(LYCHEE_URL)" && \
	curl -sSfL -o "$$tmp/sha256" "$(LYCHEE_URL).sha256" && \
	(cd "$$tmp" && $(SHA256_CHECK) sha256) && \
	tar -xzf "$$tmp/lychee-$(LYCHEE_TARGET).tar.gz" -C "$$tmp" && \
	rm -f $(BIN_DIR)/lychee-* && \
	mv "$$tmp/lychee-$(LYCHEE_TARGET)/lychee" $@ && \
	echo "downloaded lychee $(LYCHEE_VERSION) ($(LYCHEE_TARGET)) to $@"

docs-check: $(LYCHEE)
	$(LYCHEE) --offline --include-fragments --no-progress $$(git ls-files '*.md' ':!:.kiro/**' ':!:openspec/**')

# lint runs golangci-lint from its release binary, which its maintainers
# recommend over building it from source.
GOLANGCI_LINT_VERSION := $(shell sed -n 's|^FROM docker.io/golangci/golangci-lint:v||p' build/tools/golangci-lint/Dockerfile)
GOLANGCI_LINT := $(BIN_DIR)/golangci-lint-$(GOLANGCI_LINT_VERSION)
GOLANGCI_LINT_TARGET := $(if $(filter Darwin,$(UNAME_S)),darwin,linux)-$(if $(ARCH_ARM),arm64,amd64)
GOLANGCI_LINT_BASE := https://github.com/golangci/golangci-lint/releases/download/v$(GOLANGCI_LINT_VERSION)
GOLANGCI_LINT_ARCHIVE := golangci-lint-$(GOLANGCI_LINT_VERSION)-$(GOLANGCI_LINT_TARGET)

$(GOLANGCI_LINT):
	@mkdir -p $(BIN_DIR)
	@tmp=$$(mktemp -d); trap 'rm -rf "$$tmp"' EXIT; \
	curl -sSfL -o "$$tmp/$(GOLANGCI_LINT_ARCHIVE).tar.gz" "$(GOLANGCI_LINT_BASE)/$(GOLANGCI_LINT_ARCHIVE).tar.gz" && \
	curl -sSfL "$(GOLANGCI_LINT_BASE)/golangci-lint-$(GOLANGCI_LINT_VERSION)-checksums.txt" \
		| grep " $(GOLANGCI_LINT_ARCHIVE).tar.gz$$" > "$$tmp/sha256" && \
	(cd "$$tmp" && $(SHA256_CHECK) sha256) && \
	tar -xzf "$$tmp/$(GOLANGCI_LINT_ARCHIVE).tar.gz" -C "$$tmp" && \
	rm -f $(BIN_DIR)/golangci-lint-* && \
	mv "$$tmp/$(GOLANGCI_LINT_ARCHIVE)/golangci-lint" $@ && \
	echo "downloaded golangci-lint $(GOLANGCI_LINT_VERSION) ($(GOLANGCI_LINT_TARGET)) to $@"

# SHA256_CHECK verifies a sha256sum-format file: sha256sum on Linux,
# shasum on macOS.
SHA256_CHECK := $$(command -v sha256sum >/dev/null && echo "sha256sum -c --quiet" || echo "shasum -a 256 -c --quiet")

# test-load runs the ha-validation Load Test (Requirement 1.4): 100
# concurrent webhook events against a real Redis. Needs
# TURNIP_TEST_REDIS_ADDR set to a real Redis instance; not part of the
# default `test` target (Requirement 1.7).
test-load:
	go test -tags load -race ./internal/orchestrator/... -run TestLoad -timeout 60s

# test-kind stands up a real kind cluster, deploys deployment-kustomize's
# kind overlay (Requirement 1.5), and runs the RBAC/networking checks in
# test/load/kind_test.go against it — always tearing the cluster down
# afterward, success or failure. Needs kind, docker, and kubectl on PATH;
# not part of the default `test` target (Requirement 1.7).
#
# If kube-proxy/coredns fail with "too many open files" and the Deployment
# never goes Available, your host's fs.inotify.max_user_instances is too
# low (kind's own documented minimum is 512; many minimal Linux setups
# default to 128) — see https://kind.sigs.k8s.io/docs/user/known-issues/#pod-errors-due-to-too-many-open-files.
# Fix: `sudo sysctl fs.inotify.max_user_watches=524288 fs.inotify.max_user_instances=8192`.
KIND_CLUSTER := turnip-ha
test-kind:
	set -e; \
	trap 'kind delete cluster --name $(KIND_CLUSTER)' EXIT; \
	kind create cluster --name $(KIND_CLUSTER); \
	docker build -f build/server/Dockerfile -t ghcr.io/ivanvc/turnip-server:dev .; \
	docker build -f build/runner/Dockerfile -t ghcr.io/ivanvc/turnip-runner:dev .; \
	kind load docker-image ghcr.io/ivanvc/turnip-server:dev --name $(KIND_CLUSTER); \
	kind load docker-image ghcr.io/ivanvc/turnip-runner:dev --name $(KIND_CLUSTER); \
	KEY=$$(mktemp); \
	openssl genrsa -out $$KEY 2048 2>/dev/null; \
	kubectl create secret generic turnip-github-app \
		--from-literal=webhook-secret=test \
		--from-file=private-key=$$KEY; \
	rm -f $$KEY; \
	kubectl apply -f test/load/redis.yaml; \
	kubectl apply -k deploy/overlays/kind; \
	kubectl wait --for=condition=available deployment/turnip-server --timeout=120s; \
	go test -tags kind -race ./test/load/... -timeout 360s

lint: $(GOLANGCI_LINT)
	$(GOLANGCI_LINT) run ./...

# manifests-check validates deploy/'s rendered kind overlay with
# kubeconform, run from tools/go.mod (a module of its own, so the tools
# never touch turnip's go.mod). Needs kubectl on PATH.
manifests-check:
	kubectl kustomize deploy/overlays/kind | go tool -modfile=tools/go.mod kubeconform -strict -summary

fmt:
	gofmt -w .

proto-gen:
	cd proto && buf generate

clean:
	rm -rf $(BIN_DIR)
