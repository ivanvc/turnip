# Requirements Document

## Introduction

This document specifies requirements for Slice 0 (Project Scaffolding) of the turnip multi-IaC automation platform. This slice establishes the Go project skeleton so all subsequent slices have a foundation to build on. It delivers directory structure, build tooling, containerization, protobuf code generation, CI pipeline, and placeholder entry points — with no business logic.

**Context**: The turnip platform automates infrastructure-as-code operations (Terraform, Pulumi, Helmfile) via GitHub webhooks, orchestrating ephemeral Kubernetes runner pods. This slice provides the structural foundation referenced by Slices 1–8 in the roadmap.

**Implementation Constraints** (from global spec):
- Language: Go
- Communication: gRPC with Protocol Buffers
- Orchestration: Kubernetes Jobs
- GitHub Integration: GitHub App
- Locking: Redis/Valkey

## Glossary

- **Platform**: The complete turnip multi-IaC automation system
- **Server**: The HTTP/gRPC service component that receives webhooks and orchestrates runners
- **Runner**: The ephemeral Kubernetes pod component that executes IaC operations
- **Go_Module**: The Go module defined by go.mod at the repository root
- **Protobuf**: Protocol Buffers, the interface definition language used for gRPC service contracts
- **Code_Generator**: The tool (buf or protoc) that produces Go source from .proto files
- **Makefile**: The task runner file defining build, test, lint, and code generation targets
- **CI_Pipeline**: The GitHub Actions workflow that validates code on push and pull request events
- **Linter**: The golangci-lint tool that enforces code style and catches common errors
- **Dockerfile**: Container image build specification for Server or Runner

## Requirements

### Requirement 1: Go Module Initialization

**User Story:** As a platform developer, I want a properly configured Go module, so that dependency management works correctly for all subsequent slices.

#### Acceptance Criteria

1. THE Go_Module SHALL use "github.com/ivanvc/turnip" as the module path
2. THE Go_Module SHALL target Go 1.26 or later via the go directive in go.mod
3. THE Go_Module SHALL declare dependencies for google.golang.org/grpc, google.golang.org/protobuf, k8s.io/client-go, k8s.io/api, and k8s.io/apimachinery as direct dependencies in go.mod
4. THE Go_Module SHALL include a go.sum file with verified dependency checksums
5. WHEN a developer runs "go mod tidy", THE Go_Module SHALL exit with code 0 and produce no changes to go.mod or go.sum

### Requirement 2: Directory Structure

**User Story:** As a platform developer, I want a standardized directory layout, so that all subsequent slices have clear locations for their code.

#### Acceptance Criteria

1. THE Platform SHALL provide a cmd/server/ directory containing the Server entry point (main.go)
2. THE Platform SHALL provide a cmd/runner/ directory containing the Runner entry point (main.go)
3. THE Platform SHALL provide an internal/plugin/ directory containing at least one .go file with a valid package declaration for Plugin interface and implementations
4. THE Platform SHALL provide an internal/lock/ directory containing at least one .go file with a valid package declaration for Redis lock management code
5. THE Platform SHALL provide an internal/github/ directory containing at least one .go file with a valid package declaration for GitHub client and webhook handling code
6. THE Platform SHALL provide an internal/config/ directory containing at least one .go file with a valid package declaration for configuration parsing code
7. THE Platform SHALL provide an internal/grpc/ directory containing at least one .go file with a valid package declaration for gRPC service definitions and implementations
8. THE Platform SHALL provide a proto/ directory for Protocol Buffer definition files
9. WHEN a developer runs "go build ./...", THE Platform SHALL compile all packages in the directory structure without errors
10. THE Platform SHALL locate all directories relative to the repository root where go.mod resides

### Requirement 3: Server Entry Point

**User Story:** As a platform developer, I want a Server main.go that compiles and runs, so that subsequent slices can incrementally add functionality to it.

#### Acceptance Criteria

1. THE Server entry point SHALL be located at cmd/server/main.go
2. WHEN the Server binary is executed, THE Server SHALL log a startup message to standard output containing the service name "turnip-server" and a version string
3. WHEN the Server binary is executed with no HTTP or gRPC listeners configured, THE Server SHALL exit with exit code 0 within 1 second of startup
4. THE Server entry point SHALL compile without errors using "go build ./cmd/server"
5. WHEN the Server binary is executed, THE Server SHALL write log output to standard error or standard output in plain text format

### Requirement 4: Runner Entry Point

**User Story:** As a platform developer, I want a Runner main.go that compiles and runs, so that subsequent slices can incrementally add functionality to it.

#### Acceptance Criteria

1. THE Runner entry point SHALL be located at cmd/runner/main.go within package main
2. WHEN the Runner binary is executed, THE Runner SHALL log a startup message to standard output containing the service name "runner" and a version string
3. IF no additional work is configured, THEN THE Runner SHALL exit with exit code 0 after logging the startup message
4. THE Runner entry point SHALL compile without errors using "go build ./cmd/runner"
5. WHEN the Runner binary is executed, THE Runner SHALL complete its startup-and-exit sequence within 5 seconds

### Requirement 5: Server Dockerfile

**User Story:** As a platform operator, I want a Dockerfile for the Server, so that the Server can be built and deployed as a container image.

#### Acceptance Criteria

1. THE Platform SHALL provide a Dockerfile at build/server/Dockerfile
2. THE Server Dockerfile SHALL use a multi-stage build with a Go builder stage using a Go version compatible with the Go_Module (1.22 or later) and a runtime stage based on a distroless or scratch base image
3. THE Server Dockerfile SHALL produce a statically linked binary by building with CGO_ENABLED=0
4. WHEN the Server Dockerfile is built with "docker build", THE build process SHALL complete without errors and produce a container image
5. WHEN the Server container image is inspected, THE image SHALL contain only the Server binary and CA root certificates for TLS verification
6. WHEN the Server container is started, THE Server binary SHALL be the container's entrypoint

### Requirement 6: Runner Dockerfile

**User Story:** As a platform operator, I want a Dockerfile for the Runner, so that the Runner can be built and deployed as a container image.

#### Acceptance Criteria

1. THE Platform SHALL provide a Dockerfile at build/runner/Dockerfile
2. THE Runner Dockerfile SHALL use a multi-stage build with a Go builder stage (version matching go.mod) and an alpine-based runtime stage
3. THE Runner Dockerfile SHALL produce a statically linked binary by disabling CGO
4. THE Runner Dockerfile SHALL include git in the runtime image for repository cloning
5. WHEN the Runner Dockerfile is built, THE resulting image SHALL contain the Runner binary as the ENTRYPOINT, git, and CA certificates for TLS connections

### Requirement 7: Protobuf Setup and Code Generation

**User Story:** As a platform developer, I want protobuf tooling configured, so that gRPC service contracts can be defined and Go code generated from them.

#### Acceptance Criteria

1. THE Platform SHALL provide a proto/ directory containing at least one .proto file defining the OperationService
2. THE .proto file SHALL use proto3 syntax and specify a Go package option mapping to the internal/grpc output directory
3. THE Platform SHALL provide configuration for buf (buf.yaml and buf.gen.yaml) or a protoc invocation script that generates both Protocol Buffer serialization code and gRPC service stubs
4. WHEN the code generation command (make proto-gen or buf generate) is executed, THE Code_Generator SHALL produce Go source files in the internal/grpc directory
5. THE generated Go source files SHALL compile without errors when verified via "go build ./internal/grpc/..."
6. THE .proto file SHALL define an OperationService containing at least one unary RPC method with defined request and response message types (messages may have no fields)

### Requirement 8: Makefile Task Runner

**User Story:** As a platform developer, I want a Makefile with common targets, so that build, test, lint, and code generation operations are standardized.

#### Acceptance Criteria

1. THE Platform SHALL provide a Makefile at the repository root
2. THE Makefile SHALL include a "build" target that compiles both Server and Runner binaries
3. THE Makefile SHALL include a "test" target that runs all Go tests with race detection enabled
4. THE Makefile SHALL include a "lint" target that executes golangci-lint
5. THE Makefile SHALL include a "proto-gen" target that runs protobuf code generation
6. THE Makefile SHALL include a "fmt" target that formats all Go source files
7. WHEN a developer runs "make build", THE Makefile SHALL produce binaries in a bin/ directory or the default Go output location

### Requirement 9: CI Pipeline Configuration

**User Story:** As a platform developer, I want a GitHub Actions CI pipeline, so that code quality is validated automatically on every push and pull request.

#### Acceptance Criteria

1. THE Platform SHALL provide a GitHub Actions workflow file at .github/workflows/ci.yml
2. THE CI_Pipeline SHALL trigger on push to the main branch and on pull request events targeting the main branch (opened, synchronize, and reopened)
3. THE CI_Pipeline SHALL run "go build ./..." to verify compilation
4. THE CI_Pipeline SHALL run "go test -race ./..." to execute all tests with race detection enabled
5. THE CI_Pipeline SHALL run golangci-lint to check code quality
6. THE CI_Pipeline SHALL verify that generated protobuf code is up to date by running the code generation command and then checking that no files in the working tree have been modified (git diff --exit-code)
7. IF any CI step fails, THEN THE CI_Pipeline SHALL report failure status on the commit
8. THE CI_Pipeline SHALL use a Go version consistent with the minimum version specified in go.mod
9. THE CI_Pipeline SHALL pin all GitHub Actions dependencies by commit SHA with a version comment (e.g., actions/checkout@abc123 # v4.1.0) to prevent supply-chain attacks from tag mutation

### Requirement 10: Dependabot Configuration

**User Story:** As a platform developer, I want automated dependency update PRs, so that Go modules, Docker base images, and GitHub Actions dependencies stay current without manual tracking.

#### Acceptance Criteria

1. THE Platform SHALL provide a Dependabot configuration file at .github/dependabot.yml
2. THE Dependabot configuration SHALL monitor "gomod" ecosystem for Go module dependency updates
3. THE Dependabot configuration SHALL monitor "docker" ecosystem for Dockerfile base image updates
4. THE Dependabot configuration SHALL monitor "github-actions" ecosystem for GitHub Actions dependency updates (pinned SHA bumps)
5. THE Dependabot configuration SHALL schedule update checks on a weekly interval
6. THE Dependabot configuration SHALL set a minimum cooldown period of 3 days between update PRs for each ecosystem
7. THE Dependabot configuration SHALL target the main branch for update PRs

### Requirement 11: Linting and Formatting Configuration

**User Story:** As a platform developer, I want linting and formatting rules configured, so that code style is consistent across all contributors and slices.

#### Acceptance Criteria

1. THE Platform SHALL provide a .golangci.yml configuration file at the repository root that is parseable by golangci-lint without errors
2. THE Linter configuration SHALL enable gofmt, govet, errcheck, and staticcheck linters at minimum
3. THE Linter configuration SHALL set a timeout of at least 5 minutes for CI environments
4. WHEN a developer runs the lint target, THE Linter SHALL report violations without modifying source files
5. THE Linter configuration SHALL exclude generated protobuf files (files matching the *.pb.go and *_grpc.pb.go suffixes) from analysis
6. IF the Linter detects one or more violations, THEN THE Linter SHALL exit with a non-zero exit code
7. IF the Linter detects no violations, THEN THE Linter SHALL exit with exit code 0
