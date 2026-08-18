.PHONY: build test lint fmt proto-gen clean

BIN_DIR := bin
SERVER_BIN := $(BIN_DIR)/server
RUNNER_BIN := $(BIN_DIR)/runner

build: $(SERVER_BIN) $(RUNNER_BIN)

$(SERVER_BIN):
	go build -o $(SERVER_BIN) ./cmd/server

$(RUNNER_BIN):
	go build -o $(RUNNER_BIN) ./cmd/runner

test:
	go test -race ./...

lint:
	golangci-lint run ./...

fmt:
	gofmt -w .

proto-gen:
	cd proto && buf generate

clean:
	rm -rf $(BIN_DIR)
