.PHONY: build docker-build docker-push test test-load test-kind lint fmt proto-gen clean

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

test:
	go test -race ./...

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
	go test -tags kind -race ./test/load/... -timeout 180s

lint:
	golangci-lint run ./...

fmt:
	gofmt -w .

proto-gen:
	cd proto && buf generate

clean:
	rm -rf $(BIN_DIR)
