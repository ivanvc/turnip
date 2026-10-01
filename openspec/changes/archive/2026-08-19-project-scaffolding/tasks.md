# Implementation Plan: Project Scaffolding (Slice 0)

## Overview

This plan implements the structural foundation for the turnip platform. Each task creates files that compile and integrate with the rest of the project. Tasks are ordered so that foundational pieces (module, directories, entry points) come first, followed by tooling (protobuf, Makefile, linting), containerization (Dockerfiles), and CI/CD configuration.

## Tasks

- [x] 1. Initialize Go module and create package placeholders
  - [x] 1.1 Configure go.mod with module path and dependencies
    - Set module path to `github.com/ivanvc/turnip` with Go 1.26+ directive
    - Add direct dependencies: google.golang.org/grpc, google.golang.org/protobuf, k8s.io/client-go, k8s.io/api, k8s.io/apimachinery
    - Run `go mod tidy` to populate go.sum with verified checksums
    - Verify `go mod tidy` produces no further changes (exit 0, no diff)
    - _Requirements: 1.1, 1.2, 1.3, 1.4, 1.5_

  - [x] 1.2 Create internal package placeholder files
    - Create `internal/plugin/doc.go` with `package plugin` and doc comment about Plugin interface (Slice 2)
    - Create `internal/lock/doc.go` with `package lock` and doc comment about Redis lock management (Slice 3)
    - Create `internal/github/doc.go` with `package github` and doc comment about GitHub client/webhook handling (Slice 4)
    - Create `internal/config/doc.go` with `package config` and doc comment about configuration parsing (Slice 1)
    - Remove any existing `.go.orig` files from `internal/` that are not part of the scaffold
    - Verify all packages compile with `go build ./...`
    - _Requirements: 2.3, 2.4, 2.5, 2.6, 2.9, 2.10_

- [x] 2. Implement entry points
  - [x] 2.1 Create Server entry point at cmd/server/main.go
    - Create `cmd/server/main.go` in package main
    - Log startup message to stdout containing "turnip-server" and a version string
    - Exit with code 0 immediately (no listeners)
    - Verify compilation with `go build ./cmd/server`
    - _Requirements: 2.1, 3.1, 3.2, 3.3, 3.4, 3.5_

  - [x] 2.2 Create Runner entry point at cmd/runner/main.go
    - Create `cmd/runner/main.go` in package main
    - Log startup message to stdout containing "runner" and a version string
    - Exit with code 0 after logging
    - Verify compilation with `go build ./cmd/runner`
    - _Requirements: 2.2, 4.1, 4.2, 4.3, 4.4, 4.5_

- [x] 3. Checkpoint - Verify module and entry points
  - Ensure `go build ./...` succeeds, `go mod tidy` produces no changes, and both binaries print their startup messages. Ask the user if questions arise.

- [x] 4. Set up Protobuf with buf
  - [x] 4.1 Create proto directory structure and buf configuration
    - Create `proto/buf.yaml` with v2 config, module name `buf.build/ivanvc/turnip`, STANDARD lint rules, FILE breaking rules
    - Create `proto/buf.gen.yaml` with v2 config, remote plugins for protocolbuffers/go and grpc/go, output to `../internal/grpc`, `paths=source_relative` option
    - Create `proto/turnip/v1/operation.proto` with proto3 syntax, package `turnip.v1`, go_package `github.com/ivanvc/turnip/internal/grpc/turnip/v1`
    - Define `OperationService` with one RPC: `ExecuteOperation(OperationRequest) returns (stream OperationResponse)`
    - Define empty `OperationRequest` and `OperationResponse` messages
    - _Requirements: 7.1, 7.2, 7.3, 7.6_

  - [x] 4.2 Generate Go code from proto definitions
    - Run `cd proto && buf generate` to produce Go source files in `internal/grpc/`
    - Verify generated files exist in `internal/grpc/turnip/v1/`
    - Verify compilation with `go build ./internal/grpc/...`
    - _Requirements: 7.4, 7.5_

- [x] 5. Create Makefile
  - [x] 5.1 Create Makefile with all required targets
    - Create `Makefile` at repository root
    - Implement `build` target that compiles both server and runner binaries to `bin/` directory
    - Implement `test` target that runs `go test -race ./...`
    - Implement `lint` target that runs `golangci-lint run ./...`
    - Implement `proto-gen` target that runs `cd proto && buf generate`
    - Implement `fmt` target that runs `gofmt -w .`
    - Implement `clean` target that removes `bin/` directory
    - Mark all targets as `.PHONY`
    - Verify `make build` produces binaries in `bin/`
    - _Requirements: 8.1, 8.2, 8.3, 8.4, 8.5, 8.6, 8.7_

- [x] 6. Configure linting
  - [x] 6.1 Create .golangci.yml configuration
    - Create `.golangci.yml` at repository root
    - Set `run.timeout` to 5m
    - Enable linters: gofmt, govet, errcheck, staticcheck, ineffassign, unused, misspell, gosimple
    - Exclude generated protobuf files via regex patterns: `.*\.pb\.go$` and `.*_grpc\.pb\.go$`
    - Verify golangci-lint parses the config without errors
    - _Requirements: 11.1, 11.2, 11.3, 11.4, 11.5, 11.6, 11.7_

- [x] 7. Checkpoint - Verify tooling
  - Ensure `make build`, `make test`, `make lint`, `make proto-gen`, and `make fmt` all succeed. Ask the user if questions arise.

- [x] 8. Create Dockerfiles
  - [x] 8.1 Create Server Dockerfile at build/server/Dockerfile
    - Create `build/server/Dockerfile` with multi-stage build
    - Builder stage: `golang:1.26` base, copy go.mod/go.sum, `go mod download`, copy source, build with `CGO_ENABLED=0 GOOS=linux` and `-ldflags="-s -w"`
    - Runtime stage: `gcr.io/distroless/static-debian12:nonroot` base
    - Copy binary from builder, set ENTRYPOINT to `/server`
    - _Requirements: 5.1, 5.2, 5.3, 5.4, 5.5, 5.6_

  - [x] 8.2 Create Runner Dockerfile at build/runner/Dockerfile
    - Create `build/runner/Dockerfile` with multi-stage build
    - Builder stage: `golang:1.26` base, same build pattern as server
    - Runtime stage: `alpine:3.23` base
    - Install git and ca-certificates via apk
    - Copy binary from builder, set ENTRYPOINT to `/runner`
    - _Requirements: 6.1, 6.2, 6.3, 6.4, 6.5_

- [x] 9. Configure CI and Dependabot
  - [x] 9.1 Create GitHub Actions CI workflow
    - Create `.github/workflows/ci.yml`
    - Trigger on push to main and pull_request to main (opened, synchronize, reopened)
    - Use `actions/checkout` pinned by commit SHA with version comment
    - Use `actions/setup-go` pinned by commit SHA with `go-version-file: go.mod`
    - Use `bufbuild/buf-setup-action` pinned by commit SHA
    - Add Build step: `go build ./...`
    - Add Test step: `go test -race ./...`
    - Add Lint step: `golangci/golangci-lint-action` pinned by commit SHA
    - Add Proto freshness step: `cd proto && buf generate` then `git diff --exit-code`
    - _Requirements: 9.1, 9.2, 9.3, 9.4, 9.5, 9.6, 9.7, 9.8, 9.9_

  - [x] 9.2 Create Dependabot configuration
    - Create `.github/dependabot.yml` with version 2
    - Add `gomod` ecosystem entry: directory `/`, target-branch main, weekly schedule, cooldown default-days 3, group minor+patch updates
    - Add `docker` ecosystem entry for `/build/server`: target-branch main, weekly, cooldown default-days 3
    - Add `docker` ecosystem entry for `/build/runner`: target-branch main, weekly, cooldown default-days 3
    - Add `github-actions` ecosystem entry: directory `/`, target-branch main, weekly, cooldown default-days 3
    - _Requirements: 10.1, 10.2, 10.3, 10.4, 10.5, 10.6, 10.7_

- [x] 10. Final checkpoint - Full verification
  - Ensure `go build ./...` compiles all packages, `make lint` passes, `make test` passes, proto generation produces no diff, and all configuration files are syntactically valid. Ask the user if questions arise.

## Notes

- No property-based tests for this slice — it is purely structural with no business logic
- Verification is done via compilation (`go build ./...`), linting (`golangci-lint`), and smoke tests (running binaries)
- All GitHub Actions are pinned by commit SHA with version comments for supply-chain security
- Dependabot will automatically propose SHA bumps when new action versions are released
- Package placeholders use `doc.go` files with package declarations only — no interfaces or types
- The `internal/grpc/` directory is populated by buf code generation, not manually

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1.1"] },
    { "id": 1, "tasks": ["1.2", "2.1", "2.2"] },
    { "id": 2, "tasks": ["4.1"] },
    { "id": 3, "tasks": ["4.2", "5.1", "6.1"] },
    { "id": 4, "tasks": ["8.1", "8.2", "9.1", "9.2"] }
  ]
}
```
