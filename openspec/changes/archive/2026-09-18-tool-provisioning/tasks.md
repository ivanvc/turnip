# Implementation Plan: Per-Tool Provisioning (Slice 14)

## Overview

Ordered so the clone mode lands before the Job spec changes that depend on
it: the new Job shapes reference a `clone` entrypoint, so building them
first would mean asserting against a container command that does not yet
work.

The two strategies are implemented together rather than one at a time.
Splitting them would leave `BuildJob` briefly asserting one shape while the
tool table declares another, and a failing test would be ambiguous between
the two.

## Tasks

- [x] 1. Clone as a mode of the runner binary
  - [x] 1.1 Select the mode in `cmd/runner/main.go`
    - An argument chooses clone mode; absent it, today's behavior is
      unchanged. `ConfigFromEnv` is reused as-is, since the Job sets the
      same variables on both containers
    - _Requirements: 3.1, 3.2_
  - [x] 1.2 Add the clone entrypoint in `internal/runner`
    - Calls the existing `Clone` — unchanged, along with its merge
      semantics, `MergeConflictError`, and token redaction
    - On failure it reports an `OperationResult` over the existing
      reporter **before** exiting non-zero, so the pull request gets git's
      own message immediately rather than a start-timeout five minutes
      later (Decision 4)
    - _Requirements: 3.3_
  - [x] 1.3 Remove the clone from `execute`
    - `resolveWorkspace` stays: the workspace is now populated by an
      initContainer, but the Runner still resolves the path, and the
      temporary-directory fallback still serves tests and hand-runs
    - _Requirements: 3.1, 3.2_

- [x] 2. Checkpoint - the runner still works unchanged
  - `go build ./...` and `go test -race ./internal/runner/...` pass. The
    Job spec has not changed yet, so a failure here is about the clone mode
    alone.

- [x] 3. Provisioning strategy
  - [x] 3.1 Declare it per tool in `internal/jobs/versions.go`
    - `toolImage` gains a strategy; helmfile becomes run-in-image, and
      `binaryPath` is left empty there because it means nothing
    - Terraform and Pulumi keep copy-out, unchanged
    - _Requirements: 1.1, 1.3, 1.4_
  - [x] 3.2 Branch on it in `internal/jobs/build.go`
    - Copy-out: today's shape, plus the clone initContainer
    - Run-in-image: main container is the vendor image with
      `command: ["/turnip/bin/runner"]`; an initContainer from turnip's own
      image supplies that binary
    - `/turnip/bin` is its own volume, not `tools` — under run-in-image the
      copied binary is turnip's, not the tool's, and naming it `tools`
      would make the Pod lie about what it holds
    - _Requirements: 1.2, 1.5, 2.1, 2.2, 2.3_
  - [x] 3.3 Set `TURNIP_TOOLS_DIR` only under copy-out
    - No Runner change is needed: Slice 12 made it optional, so
      `pathWithToolsDir` leaves `PATH` untouched when it is absent and the
      tool is found on the vendor image's own PATH
    - _Requirements: 3.5_
  - [x] 3.4 Set `TURNIP_GITHUB_TOKEN` only on the clone initContainer
    - The tool's process no longer inherits an installation token it has no
      use for — which matters most under run-in-image, where that
      environment belongs to a vendor image running arbitrary plugins
    - _Requirements: 2.3 (Decision 5)_
  - [x] 3.5 Stop requiring the variables 3.3 and 3.4 removed
    - Found during implementation, not planned: `ConfigFromEnv` still listed
      `TURNIP_GITHUB_TOKEN` and `TURNIP_TOOLS_DIR` as required, and it runs
      before the mode branch — so every Runner turnip builds would have
      refused to start with `missing required environment variable(s)`
    - The whole suite was blind to it: every config test starts from an
      environment carrying all variables at once, and `testRunConfig()`
      bypasses `ConfigFromEnv` entirely. Added a test that uses the
      environment the tool container actually receives
    - _Requirements: 2.3, 3.5_

- [x] 4. Checkpoint - both shapes build
  - `go build ./...` and `go test -race ./...` pass. Existing `internal/jobs`
    tests assert the copy-out shape, so they are the regression guard for
    the strategy that did not change.

- [x] 5. Tests
  - [x] 5.1 `internal/jobs`: both Job shapes
    - Copy-out produces today's containers, volumes and mounts, plus the
      clone initContainer; run-in-image produces the vendor image as the
      main container with the runner binary as its command
    - `TURNIP_TOOLS_DIR` is set for one and absent for the other
    - `TURNIP_GITHUB_TOKEN` appears on the clone initContainer and **not**
      on the main container — the assertion that pins Decision 5
    - _Requirements: 1.5, 2.1, 2.2, 3.5_
  - [x] 5.2 `internal/runner`: the clone mode
    - Reports before exiting on failure, using the existing fake reporter
      rather than a real gRPC server; a merge conflict stays
      distinguishable
    - _Requirements: 3.3_
  - [x] 5.3 `clone.go` itself needs no new tests — it is unchanged, and its
    existing coverage moves with it

- [x] 6. Documentation
  - [x] 6.1 `docs/configuration.md` and `docs/troubleshooting.md`
    - State per tool how it is provisioned, and that a run-in-image tool's
      plugins and helpers come from the vendor image — so adding one means
      choosing an image that has it, not configuring turnip
    - Cover a missing helper binary or plugin in troubleshooting: it is the
      failure this slice exists to eliminate, and the next adopter will
      meet its neighbors
    - _Requirements: 4.1, 4.2, 4.3_

- [x] 7. Final checkpoint - full verification
  - `go build ./...`, `go test -race ./...`, `go mod tidy` clean,
    `gofmt -l .` clean, and the real golangci-lint v2 via
    `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest run ./...`

## Notes

- **No new dependencies.** The clone mode reuses `Clone` and the existing
  reporter; the Job shapes are `corev1` types already in use.
- **The pilot's actual failure is task 3.1.** `exec: "helm": executable
  file not found in $PATH` is fixed the moment helmfile's strategy flips,
  because the tool then runs in an image that already contains helm, its
  plugins, and the environment that lets helm find them.
- **One fail-fast regression, accepted.** Under run-in-image an unpullable
  tool version is detected after the clone rather than before it, because
  the vendor image is now the main container. The user-visible timing is
  unchanged — both paths report when the sweep claims the record at the
  start deadline — so the cost is a wasted clone, not slower feedback.
  Slice 13 also narrowed the case considerably by rejecting malformed
  versions at parse time; what remains is a well-formed version the vendor
  does not publish.
- **Two limits recorded in design.md rather than solved**: a non-root
  vendor image may not be able to read a workspace written by the root
  clone initContainer (no supported tool uses one today), and a `scratch`
  vendor image works for run-in-image but is irrelevant since the only one
  in this space belongs to a copy-out tool.
- **Coverage target**: 80% for touched packages, consistent with prior
  slices.

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1.1", "1.2"] },
    { "id": 1, "tasks": ["1.3", "3.1"] },
    { "id": 2, "tasks": ["3.2"] },
    { "id": 3, "tasks": ["3.3", "3.4"] },
    { "id": 4, "tasks": ["5.1", "5.2"] },
    { "id": 5, "tasks": ["6.1"] }
  ]
}
```
