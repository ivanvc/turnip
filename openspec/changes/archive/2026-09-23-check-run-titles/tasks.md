# Implementation Plan: What the Checks List Says (Slice 35)

## Overview

Built so the part that must not move is pinned first: the shared
renderers are extracted from the comment code before anything new calls
them, and the comment's existing tests prove it still renders byte for
byte (Requirement 7.1). Then the formatter, then the Failure_Category from
the wire inward, then the sites.

**The checkpoint that matters is task 2.** Every later Title reuses
`ChangeText` and `ScopeText`; if extracting them changed the comment, the
slice would have altered the one output it promised to leave alone, and
nothing downstream would notice.

**The generated protobuf code is regenerated during implementation** —
the Runner and Server cannot compile against the new field otherwise — and
**committed afterwards by the repository owner** (task 10).

## Tasks

- [x] 1. Plain renderers for counts and scope
  - [x] 1.1 `github.ChangeText(ChangeCounts) string` — `+1 ~4 -2`, or
    `no changes` when all are zero
  - [x] 1.2 `github.ScopeText([]string) string` — the arguments joined and
    truncated to `scopeMarkerWidth`, as plain text
  - [x] 1.3 Rebuild `summaryLine` and `scopeMarker` on them;
    `scopeMarker` keeps its `<code>` wrapping and HTML escaping, applied to
    `ScopeText`'s result
  - [x] 1.4 Tests for the plain forms, including that `ScopeText` does not
    HTML-escape
  - _Requirements: 1.2, 1.3, 7.1_

- [x] 2. Checkpoint — the comment is unchanged
  - The existing comment tests pass without edits
  - `go build ./...`, `go test -race ./...`, golangci-lint (v2) pass

- [x] 3. The Title formatter
  - [x] 3.1 `internal/orchestrator/titles.go` with the design's functions:
    `completedTitle`, `runningTitle`, `runningRecordedPlanTitle`,
    `failedTitle`, `jobNotCreatedTitle`, `timeoutTitle`, `aggregateTitle`,
    `unsupportedTitle`, plus the two unchanged aggregate Titles moved in
    from `verdict.go`
  - [x] 3.2 Table-driven tests asserting exact strings for every row of
    Requirements 2, 3, 4 and 6 — including a scoped and a truncated scope,
    `failed` for an unspecified category, and no Title containing `-1`
    or `·`
  - _Requirements: 1.1, 1.4, 1.5, 2, 3, 4.1–4.3, 4.5, 6.1–6.4_
  - Done after task 4, so the formatter used the real `rpc.FailureCategory`
    rather than a stand-in

- [x] 4. The Failure_Category on the wire
  - [x] 4.1 `proto/turnip/v1/operation.proto`: the `FailureCategory` enum
    and `failure_category = 7` on `OperationResult`, as the design gives them
  - [x] 4.2 `cd proto && buf lint`, then `make proto-gen` to regenerate
    `internal/grpc/turnip/v1/`
  - [x] 4.3 `rpc.FailureCategory`, with translations to and from the proto
    enum; `rpc.OperationResult` gains the field and `rpc.Server` fills it
  - [x] 4.4 A server test round-tripping each category into `HandleResult`,
    plus one asserting every proto value has a Go value, so nothing the
    proto defines is silently read as unspecified
  - _Requirements: 5.1, 5.3_

- [x] 5. The Runner reports it
  - [x] 5.1 `runner.OperationResult` gains `FailureCategory`;
    `reporter.go` translates it
  - [x] 5.2 Set at each failure point in `run.go`: the two clone failures,
    `resolveWorkspace`, the Plugin's `Execute` error, and a non-zero exit
    code. The exit code and error message are otherwise unchanged
  - [x] 5.3 One test per failure point asserting the reported category,
    extending `run_test.go`'s failure tests, and a reporter test that the
    category reaches the wire
  - _Requirements: 5.2, 5.4_

- [x] 6. The sites
  - [x] 6.1 `execute.go` create: `runningTitle` for a plan;
    `runningRecordedPlanTitle` for a Mutating_Operation, keeping
    `plan.Summary` from the `PlanRecord` alongside `plan.Data` and
    `plan.Args`
  - [x] 6.2 `execute.go` Job-creation failure: `jobNotCreatedTitle`
  - [x] 6.3 `result.go` completion: `completedTitle` on success,
    `failedTitle` on failure; remove `checkRunResultTitle`. A small
    `resultTitle` in `result.go` chooses between them; it holds no wording
  - [x] 6.4 `sweep.go` timeout: `timeoutTitle` of the existing diagnostic,
    now held in its own variable rather than read back from `pr.Output`
  - [x] 6.5 `verdict.go`: every Title from `titles.go`; the unsupported
    Title names the first Project by name
  - [x] 6.6 Update the site tests that assert created and updated check
    runs to the new Titles
  - _Requirements: 1.1, 2, 3, 4, 6_

- [x] 7. Checkpoint — every Title from one place
  - `go build ./...`, `go test -race ./...`, golangci-lint (v2) pass
  - No `Title:` literal remains outside `titles.go` except those passing a
    formatter's result
  - The comment tests still pass unedited
  - **Observed, not explained**: `TestProperty_RunnerCreationPerTriggeredProject`
    failed once in a full `go test -race ./...` run and did not reproduce in
    nine further runs; replaying rapid's recorded input passes, so it is
    timing-dependent. Its path (`executeTargets`, no refusals) changed here
    only in which title string is passed. Written up, with the earlier
    Slice 11 sighting and a hypothesis, in the roadmap Backlog: "An
    intermittent failure in the orchestrator's property tests"

- [x] 8. Documentation
  - [x] 8.1 `docs/usage.md`: example Titles for Project_Check states and
    the `turnip` check, replacing Slice 37's `projects applied` wording
  - [x] 8.2 `docs/troubleshooting.md`: each failure Title, and that the
    full error is in the check's details
  - _Requirements: 8_

- [x] 9. Final checkpoint
  - Build, tests and lint pass; every acceptance criterion maps to a task
  - Roadmap status updated

- [x] 10. After implementation — the repository owner's step
  - Review and commit the regenerated `internal/grpc/turnip/v1/` files
    together with `operation.proto`. CI's proto-freshness check (`buf
    generate` + `git diff --exit-code`) fails on any push where they
    differ
