# Implementation Plan: Runner Workspace & Project Environment (Slice 12)

## Overview

This plan implements the three changes in `design.md`, ordered so that the
schema change lands first (it touches fixtures every later test relies on),
then the Job spec and Runner changes that depend on each other through
`TURNIP_WORKSPACE_DIR`, then documentation.

The schema migration is deliberately its own early group: renaming
`version` to `schemaVersion` breaks every existing test fixture, and doing
that while also changing the Job spec would make a failing test ambiguous
between the two causes.

Unlike the completed slices, every box here is unchecked — this is the
plan, not a record.

## Tasks

- [x] 1. Schema version: data model and validation
  - [x] 1.1 Update `internal/config/config.go`
    - Replace `Version int` (yaml:"version") with `SchemaVersion string` (yaml:"schemaVersion")
    - Add an exported constant for the single accepted value, `v1alpha1`
    - Do **not** retain a field bound to `yaml:"version"`: the old key is ignored like any other unrecognised field (Decision 4)
    - Verify compilation with `go build ./internal/config/...`
    - _Requirements: 4.1, 4.2, 4.3, 4.7_
  - [x] 1.2 Validate it in `internal/config/validate.go`
    - Reject a `SchemaVersion` other than the accepted constant, naming both what was found and what is supported
    - Reject an absent `SchemaVersion`; apply no default
    - Report both as `*ValidationError` with `ProjectRef` set to `turnip.yaml` and `Field` set to `schemaVersion` (Decision 5), appended to the same `ValidationErrors` as project problems rather than returned early
    - _Requirements: 4.4, 4.6, 4.8_

- [x] 2. Migrate every existing `version: 1` occurrence
  - [x] 2.1 Update test fixtures
    - Wider than first planned — found by grepping rather than by listing: `internal/config/config_test.go`, `parse_test.go`, `property_test.go`, `validate_test.go`, `internal/github/client_test.go` (where `version: 1` is file *content* for a GetFile test), and `internal/orchestrator/ha_test.go`, `configfetch_test.go`, `pullrequest_test.go`
    - In each, `version: 1` becomes `schemaVersion: v1alpha1`. In `validate_test.go` this matters beyond compilation: several tests assert an exact error *count*, and an unmigrated fixture adds a "schemaVersion: required" error to every one of them
    - Two deliberate exceptions: `parse_test.go`'s legacy-field test keeps `version: 1`, since it asserts the old key gets no special treatment; and `Project.Env` needs `omitempty` on its yaml tag, or a nil map marshals to `env: {}` and parses back non-nil, breaking the round-trip property
    - _Requirements: 4.10_
  - [x] 2.2 Update documentation and spec examples
    - `docs/configuration.md`'s example, and the illustrative snippet at `multi-iac-automation-platform/design.md:335`
    - _Requirements: 4.10_

- [x] 3. Checkpoint - Schema change is self-consistent
  - `go build ./...` and `go test ./internal/config/... ./internal/github/...` pass. A failure here is about the schema alone; nothing else has changed yet.

- [x] 4. Project environment: data model and validation
  - [x] 4.1 Add `Env map[string]string` (yaml:"env") to `Project` in `internal/config/config.go`
    - Keep it distinct from `Config`: `Config` is tool-specific and read by Plugins, `Env` is passed to the process and never interpreted
    - _Requirements: 2.1_
  - [x] 4.2 Validate reserved names in `internal/config/validate.go`
    - Reject any name beginning with `TURNIP_`, and reject `PATH`
    - Report every offending name rather than the first, as `*ValidationError` with `Field` rendered as `env["NAME"]`
    - _Requirements: 2.3, 2.4, 2.5_

- [x] 5. Job spec: carry a Project's environment
  - [x] 5.1 Append `project.Env` to the Runner container's `env` in `internal/jobs/build.go`
    - Appended after turnip's own variables; no Runner-side code, transport encoding or parsing is added (Decision 3)
    - Escape each value by doubling every `$`, so Kubernetes' `$(VAR)` expansion cannot alter what the tool receives (Requirement 2.6). `$$` is the documented escape, and escaped references are never expanded whether or not the referenced variable exists
    - Iterate the map in sorted key order so the generated Job spec is deterministic and diffable
    - _Requirements: 2.2, 2.6_

- [x] 6. Checkpoint - Environment reaches the subprocess
  - `go build ./...` passes and a Project's `env` appears on the Runner container in the generated Job spec, escaped.

- [x] 7. Job spec: the `/turnip` layout
  - [x] 7.1 Update constants and volumes in `internal/jobs/build.go`
    - `toolsMountPath` becomes `/turnip/tools`; add a workspace constant `/turnip/src`
    - Add a second `emptyDir` volume for the workspace, mounted **only** on the Runner container — the initContainer keeps its tools mount and gains nothing (Decision 2)
    - Set `TURNIP_WORKSPACE_DIR` alongside the existing `TURNIP_TOOLS_DIR`
    - _Requirements: 1.1, 1.2, 1.3, 1.4_

- [x] 8. Runner: resolve the workspace
  - [x] 8.1 Add `WorkspaceDir` to `internal/runner/config.go`
    - Read from `TURNIP_WORKSPACE_DIR`; **optional**, unlike `TURNIP_TOOLS_DIR`, since an empty value is the documented fallback
    - _Requirements: 1.4, 1.5_
  - [x] 8.2 Add `resolveWorkspace(dir string) (path string, cleanup func(), err error)` and use it in `execute`
    - A given directory is used as-is and **not** removed — it is a volume mount point
    - An empty value creates a temporary directory, removed when the Operation finishes
    - `stripSandboxPath` keeps working unchanged — it takes the directory as a parameter, so a fixed path behaves exactly as a temporary one did
    - Rename it to `stripWorkspacePath` (and `sandboxpath.go`/`sandboxpath_test.go` to `workspacepath.go`/`workspacepath_test.go`, plus the test names): every document now says *Workspace*, and "sandbox" described a randomly-named directory that no longer exists. Behaviour is identical; this is vocabulary only
    - _Requirements: 1.5, 1.6, 1.7_

- [x] 9. Checkpoint - Layout change is complete end to end
  - `go build ./...` and `go test -race ./...` pass. The Runner's own tests still create temporary directories, because `testRunConfig()` sets no `WorkspaceDir` — if they now fail, the fallback in 8.2 is wrong.

- [x] 10. Tests
  - [x] 10.1 `internal/config`: schema version
    - Accepted value parses; an unsupported value is rejected naming both values; an absent value is rejected; a file carrying only the old `version: 1` fails on the *absent* `schemaVersion`, confirming Decision 4's behaviour is what ships
    - _Requirements: 4.3, 4.4, 4.6, 4.7_
  - [x] 10.2 `internal/config`: project environment
    - `env` survives a parse round-trip; `TURNIP_`-prefixed names and `PATH` are rejected; several offending names in one file are reported together
    - Extend the existing property test: any generated name beginning with `TURNIP_` is rejected regardless of suffix
    - _Requirements: 2.1, 2.3, 2.4, 2.5_
  - [x] 10.3 `internal/jobs`: Job spec
    - Two volumes exist with the expected mount paths; the initContainer mounts only `tools`; both `TURNIP_TOOLS_DIR` and `TURNIP_WORKSPACE_DIR` are set on the Runner container
    - A Project's `env` appears on the Runner container; a value containing `$(` is escaped to `$$(`; a value without `$` is byte-identical; entries are in sorted key order
    - _Requirements: 1.1, 1.2, 1.3, 1.4, 2.2, 2.6_
  - [x] 10.4 `internal/runner`: workspace
    - `resolveWorkspace` with a given directory (used, not removed) and without one (created, then removed)
    - _Requirements: 1.5, 1.6_

- [x] 11. Documentation
  - [x] 11.1 Update `docs/configuration.md`
    - Document `env`, including the reserved `TURNIP_` prefix and `PATH`, alongside the existing `config` map
    - Document `schemaVersion` as the schema version of the *file*, distinguishing it from turnip's own version and from the per-Project `config.version` that pins an IaC_Tool release — three unrelated meanings of "version" the current page leaves the reader to disentangle
    - State the graduation path: alpha advances within alpha, and becomes `v1` at turnip 1.0
    - Document `/turnip/src` and `/turnip/tools`, noting that `/turnip/src` may be referenced from committed configuration
    - _Requirements: 3.1, 3.2, 4.9_

- [x] 12. Final checkpoint - Full verification
  - `go build ./...`, `go test -race ./...`, `go mod tidy` produces no changes, `gofmt -l .` is clean, and the real golangci-lint v2 passes — run via `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest run ./...`, since the asdf-pinned binary is v1 and cannot parse this repo's v2 config (see `CLAUDE.md`). Ask the user if questions arise.

## Notes

- **No new dependencies.** Everything here uses the standard library plus
  packages already in `go.mod`.
- **The migration in task 2 is breaking and deliberate.** `v1alpha1`
  exists to make that acceptable; see `requirements.md`'s Requirement 4.5
  for the graduation path, and Decision 4 for why no compatibility shim is
  written. Rolling it out has an order, though it is a deployment concern
  rather than work in this repository: a migrated `turnip.yaml` is accepted
  by both the currently-deployed Server (which ignores unrecognised keys
  and never validated `version`) and the new one, so the files move first
  and the Server second.
- **A Project's environment is carried by the Job spec**, not applied by
  the Runner (Decision 3). That deletes a transport, a parser and a loop —
  the Runner gains no code for this feature at all. The cost is one
  subtlety: Kubernetes expands `$(VAR)` inside env values, so values are
  escaped by doubling every `$`, which is why task 10.3 tests escaping
  rather than task 10.4 testing the subprocess.
- **Waves pair tasks that touch different files.** Tasks 5.1 and 7.1 both
  edit `internal/jobs/build.go`, so they sit in consecutive waves rather
  than the same one; 8.1 and 8.2 are sequenced because the second reads
  the field the first adds.
- **No test requires a real cluster**, matching every prior slice:
  `k8s.io/client-go/kubernetes/fake` and the existing fakes in
  `internal/runner` are sufficient.
- **Coverage target**: 80% for the touched packages, consistent with
  `grpc-runner` and `server-orchestration`.
- `/tools` → `/turnip/tools` is invisible outside `internal/jobs`: the
  Runner has always learned that path from `TURNIP_TOOLS_DIR`, and neither
  `docs/` nor `deploy/` names it.

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1.1"] },
    { "id": 1, "tasks": ["1.2", "4.1"] },
    { "id": 2, "tasks": ["2.1", "2.2", "4.2"] },
    { "id": 3, "tasks": ["5.1", "8.1"] },
    { "id": 4, "tasks": ["7.1", "8.2"] },
    { "id": 5, "tasks": ["10.1", "10.2", "10.3", "10.4"] },
    { "id": 6, "tasks": ["11.1"] }
  ]
}
```
