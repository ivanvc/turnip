# Implementation Plan: Plugin System & Helmfile Plugin (Slice 2)

## Overview

This plan implements `internal/plugin` per `design.md`. Tasks are ordered
so the four independent leaf files (interface/types, errors, command
execution seam, diff-output parser) land first — none of them depend on
each other — followed by `HelmfilePlugin`, which is the only file that
depends on all four, then the package doc update, then tests (unit, then
property).

## Tasks

- [x] 1. Implement core Plugin types
  - [x] 1.1 Create `internal/plugin/plugin.go`
    - Define `Plugin` interface: `Name() string`, `GetOperations() []string`, `GetPlanOperation() string`, `GetApplyOperation() string`, `Execute(ctx context.Context, operation string, opts ExecuteOptions) (*ExecuteResult, error)`
    - Define `ExecuteOptions` struct: `WorkingDir string`, `Config map[string]string`, `ExtraArgs []string`, `PlanData []byte`
    - Define `ExecuteResult` struct: `Output string`, `ChangeSummary ChangeSummary`, `PlanData []byte`, `ExitCode int`, `Error error`
    - Define `ChangeSummary` struct: `Add int`, `Change int`, `Destroy int`
    - Verify compilation with `go build ./internal/plugin/...`
    - _Requirements: 1.1, 1.2, 1.3, 1.4, 1.5, 1.6, 2.1, 2.2, 2.3_

- [x] 2. Implement plugin errors
  - [x] 2.1 Create `internal/plugin/errors.go`
    - Define `UnsupportedOperationError` struct: `Plugin string`, `Operation string`, `Supported []string`
    - Implement `Error() string` including the plugin name, the invalid operation, and the supported operations list
    - Verify compilation with `go build ./internal/plugin/...`
    - _Requirements: 1.7_

- [x] 3. Checkpoint - Verify types and errors compile
  - Ensure `go build ./internal/plugin/...` succeeds. Ask the user if questions arise.

- [x] 4. Implement command execution seam
  - [x] 4.1 Create `internal/plugin/command.go`
    - Define `commandRunner` type: `func(ctx context.Context, dir, name string, args []string) (stdout, stderr []byte, exitCode int, err error)`
    - Implement `execCommand`, the default `commandRunner`, backed by `os/exec.CommandContext`
    - `execCommand` returns `(stdout, stderr, exitCode, nil)` when the subprocess starts and runs (including non-zero exit), extracting the real exit code via `exec.ExitError`
    - `execCommand` returns `(nil, nil, -1, err)` when the subprocess cannot be started at all (binary not found, invalid working directory)
    - Verify compilation with `go build ./internal/plugin/...`
    - _Requirements: 2.4, 2.6, 2.7_

- [x] 5. Implement Helmfile diff output parsing
  - [x] 5.1 Create `internal/plugin/helmfile_parse.go`
    - Implement `parseChangedReleases(output string) int` per design.md's algorithm: count `Comparing release=<name>, chart=<chart>` headers that are followed by at least one non-blank line before the next header (or EOF)
    - A header immediately followed by another header (or EOF) with no body counts as unchanged, not changed
    - Verify compilation with `go build ./internal/plugin/...`
    - _Requirements: 3.9, 3.10_

- [x] 6. Checkpoint - Verify command seam and parser compile
  - Ensure `go build ./internal/plugin/...` succeeds. Ask the user if questions arise.

- [x] 7. Implement Helmfile plugin
  - [x] 7.1 Create `internal/plugin/helmfile.go`
    - Define `HelmfilePlugin` struct with an unexported `run commandRunner` field
    - Implement `NewHelmfilePlugin() *HelmfilePlugin`, defaulting `run` to `execCommand`
    - Implement `Name() string` returning `"helmfile"`
    - Implement `GetOperations() []string` returning `["diff", "apply", "sync", "destroy"]`
    - Implement `GetPlanOperation() string` returning `"diff"`
    - Implement `GetApplyOperation() string` returning `"apply"`
    - Implement `Execute`: validate `operation` is in `GetOperations()`, else return `(nil, *UnsupportedOperationError)` without invoking `run`
    - Build args as `[operation]`; prepend `--environment <value>` when `opts.Config["environment"]` is non-empty (global flag, must precede the subcommand); append `opts.ExtraArgs`
    - Invoke `p.run(ctx, opts.WorkingDir, "helmfile", args)`
    - Populate `ExecuteResult.Output` from stdout with stderr appended when non-empty; `ExitCode` from the runner's result; `Error` nil unless `run` itself errored (couldn't start the process)
    - Populate `ChangeSummary` via `parseChangedReleases` only when `operation == "diff"`; zero-valued otherwise
    - Always return `PlanData: nil`
    - Verify compilation with `go build ./internal/plugin/...`
    - _Requirements: 1.6, 1.7, 2.4, 2.5, 2.6, 2.7, 3.1, 3.2, 3.3, 3.4, 3.5, 3.6, 3.7, 3.8, 3.9, 3.10, 4.1, 4.2, 4.3_

- [x] 8. Update package documentation
  - [x] 8.1 Replace the Slice 0 placeholder in `internal/plugin/doc.go`
    - Replace the "Slice 2" placeholder doc comment with one describing the package's actual purpose (the unified Plugin interface and its Helmfile implementation)
    - Verify `go build ./...` still succeeds for the whole module
    - _Requirements: (documentation, no direct requirement)_

- [x] 9. Checkpoint - Verify library compiles end-to-end
  - Ensure `go build ./...` succeeds and `go vet ./internal/plugin/...` reports no issues. Ask the user if questions arise.

- [x] 10. Write unit tests
  - [x] 10.1 Write `internal/plugin/plugin_test.go`
    - Cover `UnsupportedOperationError.Error()` message formatting, including the plugin name, invalid operation, and supported list
    - _Requirements: 1.7_

  - [x] 10.2 Write `internal/plugin/command_test.go`
    - Cover `execCommand` against a real trivial command (e.g. `sh -c "echo ...; exit 0"`) confirming stdout capture and `exitCode == 0`
    - Cover a command that exits non-zero, confirming stdout/stderr are still captured, `exitCode` matches, and `err == nil`
    - Cover a nonexistent binary, confirming `exitCode == -1` and a non-nil `err`
    - Cover that `dir` is respected (command observes the given working directory)
    - _Requirements: 2.4, 2.6, 2.7_

  - [x] 10.3 Write `internal/plugin/helmfile_parse_test.go`
    - Cover zero changed releases (headers present, no body before next header/EOF)
    - Cover one changed release (header followed by non-blank body)
    - Cover multiple changed releases, and a mix of changed/unchanged releases in one output
    - Cover empty input returning `0`
    - _Requirements: 3.9, 3.10_

  - [x] 10.4 Write `internal/plugin/helmfile_test.go`
    - Construct `HelmfilePlugin` with a fake `commandRunner` (unexported field, same package) that records the `dir`/`name`/`args` it was called with
    - Cover each of `"diff"`, `"apply"`, `"sync"`, `"destroy"` invoking `helmfile <operation>` with `opts.WorkingDir` as `dir`
    - Cover `Execute` called with an unsupported operation returns `(nil, *UnsupportedOperationError)` without invoking the fake runner
    - Cover `opts.Config["environment"]` present prepends `--environment <value>` before the operation argument; absent adds no environment flag
    - Cover `opts.ExtraArgs` are appended after the operation (and after `--environment` when present)
    - Cover `ChangeSummary` is populated (non-zero-capable) only for `"diff"`; zero-valued for `"apply"`/`"sync"`/`"destroy"` even when the fake runner returns diff-shaped stdout
    - Cover `ExecuteResult.PlanData` is always `nil`
    - Cover a fake runner returning a non-nil `err` (simulating "binary not found") propagates as `Execute`'s returned error with a `nil` `*ExecuteResult`
    - _Requirements: 1.6, 1.7, 2.4, 2.5, 2.6, 2.7, 3.1, 3.2, 3.3, 3.4, 3.5, 3.6, 3.7, 3.8, 3.9, 4.1, 4.2, 4.3_

- [x] 11. Checkpoint - Verify unit tests pass with full coverage
  - Ensure `go test ./internal/plugin/...` passes and coverage meets the 90% target ("Plugin implementations" per the global design doc's Testing Strategy). Ask the user if questions arise.

- [x] 12. Write property tests
  - [x] 12.1 Write `internal/plugin/property_test.go`
    - `// Feature: multi-iac-automation-platform, Property 4: Plugin Result Structure Completeness` — for a random operation among `GetOperations()` and random `ExecuteOptions`, using a fake `commandRunner` returning randomized stdout/stderr/exitCode, assert the returned `*ExecuteResult` is non-nil with `Output`/`ExitCode` always populated; ≥100 iterations
    - `// Feature: multi-iac-automation-platform, Property 22: Helmfile Plugin Command Execution` — for each of `"diff"`, `"sync"`, `"apply"`, `"destroy"`, assert `Execute` invokes `helmfile <operation>` (with `--environment` correctly placed when configured), using a fake `commandRunner` to capture the call; ≥100 iterations
    - Reuse the `testParameters()` helper pattern established in `internal/config/property_test.go` (MinSuccessfulTests = 100)
    - _Requirements: 1.6, 2.1, 2.2, 3.5, 3.6, 3.7, 3.8, 4.1_

- [x] 13. Final checkpoint - Full verification
  - Ensure `go build ./...` compiles, `go test -race ./internal/plugin/...` passes including property tests, `go mod tidy` produces no changes, and `golangci-lint run ./internal/plugin/...` passes (or `go vet`/`gofmt -l` if golangci-lint is unavailable locally — see CLAUDE.md). Ask the user if questions arise.

- [x] 14. Migrate property tests from `gopter` to `pgregory.net/rapid` (2026-08 amendment)
  - [x] 14.1 Rewrite `internal/plugin/property_test.go` against `rapid`
    - `gopter`'s last release (`v0.2.11`) is from April 2024 with no newer tag; `pgregory.net/rapid` is actively maintained and became this repo's property-testing convention for all slices — see the global design doc's Testing Strategy and `CLAUDE.md`
    - Replace `testParameters()`/`gopter.NewProperties`/`prop.ForAll` with `rapid.Check(t, func(t *rapid.T) {...})` per property function; `rapid.Check`'s default `checks` count (100) already satisfies the ≥100-iterations convention
    - Replace `gen.IntRange`/`gen.AlphaString`/`gen.OneConstOf` with `rapid.IntRange`/`rapid.String`/`rapid.SampledFrom`
    - Property assertions moved from returning `bool` to calling `t.Fatalf` with a descriptive message on failure
    - Verify `go test ./internal/plugin/... -run TestProperty` passes both properties
    - _Requirements: (maintenance amendment, no behavioral change — see the "Testing Strategy" section of design.md, which now names `rapid`)_

  - [x] 14.2 Update dependencies
    - Add `pgregory.net/rapid` as a direct dependency; `go mod tidy` removes `github.com/leanovate/gopter` once no package in the module imports it anymore (verified repo-wide, not just `internal/plugin`, since `internal/config` and `internal/lock` were migrated in the same amendment) — note that this supersedes this file's own "Notes" section below, which predates the migration and is left as the historical record of Slice 2's original state
    - _Requirements: (dependency infrastructure, no direct requirement)_

  - [x] 14.3 Checkpoint - Full re-verification
    - Ensure `go build ./...`, `go vet ./internal/plugin/...`, `gofmt -l internal/plugin/`, and `go test -race ./internal/plugin/...` all pass, coverage remains at 90%+, and `go mod tidy` is stable

- [x] 15. Adopt `testify` for unit test assertions (2026-08 amendment)
  - [x] 15.1 Rewrite `internal/plugin`'s unit test files against `github.com/stretchr/testify`
    - Adopted repo-wide (see `CLAUDE.md`) to replace hand-rolled `if ... { t.Fatalf(...) }`/`t.Errorf(...)` checks with `require`/`assert`
    - `command_test.go`, `helmfile_parse_test.go`, `helmfile_test.go`, `plugin_test.go`: `require` where the original check was fatal, `assert` where it was non-fatal
    - `property_test.go`: `*rapid.T` satisfies testify's `TestingT` interface directly, so `rapid.Check` bodies use `require` the same way
    - Verify `go test ./internal/plugin/...` passes with unchanged behavior and 100% coverage
    - _Requirements: (maintenance amendment, no behavioral change)_

  - [x] 15.2 Update dependencies
    - Add `github.com/stretchr/testify` as a direct dependency
    - _Requirements: (dependency infrastructure, no direct requirement)_

  - [x] 15.3 Checkpoint - Full re-verification
    - Ensure `go build ./...`, `go vet ./internal/plugin/...`, `gofmt -l internal/plugin/`, `go test -race ./internal/plugin/...` (coverage 90%+), and the real `golangci-lint` v2 (`go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest run ./...`) all pass

## Notes

- No new external dependencies — `gopter` is already a direct dependency from Slice 1; the command seam uses only `os/exec` and `context` from the standard library.
- No integration tests against a real `helmfile` binary or Kubernetes cluster are part of this slice, per design.md's Testing Strategy — that belongs to the global roadmap's Slice 11 (Integration Testing).
- `command.go`'s unit tests (10.2) exercise `execCommand` against real shell commands (`sh`, a nonexistent binary) rather than `helmfile` itself, since the seam's contract is generic subprocess handling, not Helmfile-specific behavior.
- Coverage target is 90%, per the global design doc's "Plugin implementations" line item, applied here to the whole `internal/plugin` package.

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1.1", "2.1", "4.1", "5.1"] },
    { "id": 1, "tasks": ["7.1"] },
    { "id": 2, "tasks": ["8.1"] },
    { "id": 3, "tasks": ["10.1", "10.2", "10.3", "10.4"] },
    { "id": 4, "tasks": ["12.1"] }
  ]
}
```
