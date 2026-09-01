# Implementation Plan: Structured Logging (Slice 8)

## Overview

This plan implements `internal/logging` per `design.md`, then wires it into
`cmd/server/main.go`/`cmd/runner/main.go` and rewrites every existing
`log.Printf` call site in `internal/orchestrator` (24 sites across 6 files:
`comment.go`, `comments.go`, `execute.go`, `pullrequest.go`, `result.go`,
`sweep.go`) plus the two startup-error `fmt.Fprintf` sites in the `cmd/*`
`main.go` files. The 6 orchestrator files have no dependency on each other
and can be rewritten in parallel once `internal/logging` exists; unit tests
for `internal/logging` come last since nothing else in this slice depends
on them.

## Tasks

- [x] 1. Implement the `internal/logging` package
  - [x] 1.1 Create `internal/logging/logging.go`
    - Implement `func ParseLevel(raw string) slog.Level`: case-insensitive
      match on `"debug"`/`"info"`/`"warn"`/`"error"`, defaulting to
      `slog.LevelInfo` for an empty or unrecognized value — never returns
      an error, per Decision 1's "a typo'd log level should degrade to a
      sane default, not stop the process from starting"
    - Implement `func New(w io.Writer, level slog.Level) *slog.Logger`:
      returns `slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level}))`
    - Verify compilation with `go build ./internal/logging/...`
    - _Requirements: 1.1, 1.3_
  - [x] 1.2 Create `internal/logging/doc.go`
    - A short package doc comment describing `internal/logging`'s purpose
      (JSON-structured `log/slog` setup: level parsing from
      `TURNIP_LOG_LEVEL` and a JSON-handler constructor), matching the
      one-paragraph style of `internal/lock/doc.go`
    - _Requirements: (documentation, no direct requirement)_

- [x] 2. Checkpoint - Verify `internal/logging` compiles
  - Ensure `go build ./internal/logging/...` succeeds. Ask the user if questions arise.

- [x] 3. Wire `TURNIP_LOG_LEVEL` and the default logger into both commands
  - [x] 3.1 Amend `cmd/server/main.go`
    - At the top of `main()`, before `orchestrator.ConfigFromEnv` is
      called: `slog.SetDefault(logging.New(os.Stderr, logging.ParseLevel(os.Getenv("TURNIP_LOG_LEVEL"))))`
      — read directly via `os.Getenv`, not threaded through
      `orchestrator.Config`, per design.md's "`TURNIP_LOG_LEVEL` is read
      once, in each `cmd/*/main.go`, before any other setup"
    - Replace both `fmt.Fprintf(os.Stderr, "server: %v\n", err)` startup-error
      sites (the `ConfigFromEnv` error and the `run(cfg)` error) with
      `slog.Error("...", "error", err)` (no context available this early,
      so the non-`Context` variant); drop the `"server: "` prefix (redundant
      once every record already carries structured fields/JSON)
    - Remove the `"fmt"` import if nothing else in the file still uses it
    - Verify compilation with `go build ./cmd/server/...`
    - _Requirements: 1.1, 1.3_
  - [x] 3.2 Amend `cmd/runner/main.go`
    - Same pattern: `slog.SetDefault(logging.New(os.Stderr, logging.ParseLevel(os.Getenv("TURNIP_LOG_LEVEL"))))`
      before `runner.ConfigFromEnv`, and replace the one
      `fmt.Fprintf(os.Stderr, "runner: %v\n", err)` with `slog.Error("...", "error", err)`
    - Remove the `"fmt"` import if nothing else in the file still uses it
    - Verify compilation with `go build ./cmd/runner/...`
    - _Requirements: 1.1, 1.3_

- [x] 4. Checkpoint - Verify both commands build with the new default logger wired
  - Ensure `go build ./cmd/...` succeeds. Ask the user if questions arise.

- [x] 5. Rewrite `internal/orchestrator`'s `log.Printf` call sites
  - [x] 5.1 Rewrite `comments.go` (4 sites)
    - `log.Printf("orchestrator: reading plan comment record for %s/%s#%d: %v", ...)` → `slog.ErrorContext(ctx, "reading plan comment record", "owner", repo.Owner, "repo", repo.Name, "pr_number", prNumber, "error", err)`
    - `log.Printf("orchestrator: minimizing comment %s: %v", ...)` → `slog.ErrorContext(ctx, "minimizing comment", "node_id", nodeID, "error", err)`
    - `log.Printf("orchestrator: writing plan comment record for %s/%s#%d: %v", ...)` → `slog.ErrorContext(ctx, "writing plan comment record", "owner", repo.Owner, "repo", repo.Name, "pr_number", prNumber, "error", err)`
    - `log.Printf("orchestrator: posting comment on %s/%s#%d: %v", ...)` (in package-level `postBodies`, not a method — `ctx` is still its first parameter) → `slog.ErrorContext(ctx, "posting comment", "owner", repo.Owner, "repo", repo.Name, "pr_number", prNumber, "error", err)`
    - Remove the `"log"` import
    - _Requirements: 1.1, 1.2, 1.4, 1.5_
  - [x] 5.2 Rewrite `result.go` (5 sites)
    - `updating check run for operation %q` → `slog.ErrorContext(ctx, "updating check run", "operation_id", operationID, "error", err)`
    - `storing plan data for operation %q` → `slog.ErrorContext(ctx, "storing plan data", "operation_id", operationID, "error", err)`
    - `releasing lock for operation %q` → `slog.ErrorContext(ctx, "releasing lock", "operation_id", operationID, "error", err)`
    - `deleting operation record %q` → `slog.ErrorContext(ctx, "deleting operation record", "operation_id", operationID, "error", err)`
    - `publishing done notification for %q` → `slog.ErrorContext(ctx, "publishing done notification", "operation_id", operationID, "error", err)`
    - Remove the `"log"` import; keep `"fmt"` if `result.go` still uses it elsewhere (verify before removing)
    - _Requirements: 1.1, 1.2, 1.4, 1.5_
  - [x] 5.3 Rewrite `pullrequest.go` (4 sites)
    - `fetching config at PR close for %s/%s#%d` → `slog.ErrorContext(ctx, "fetching config at PR close", "owner", owner, "repo", repoName, "pr_number", prNumber, "error", err)`
    - `releasing lock %q on PR close` → `slog.ErrorContext(ctx, "releasing lock on PR close", "lock_key", key, "error", err)`
    - `posting unlock comment for %s/%s#%d` → `slog.ErrorContext(ctx, "posting unlock comment", "owner", owner, "repo", repoName, "pr_number", prNumber, "error", err)`
    - `deleting plan comment record for %s/%s#%d` → `slog.ErrorContext(ctx, "deleting plan comment record", "owner", owner, "repo", repoName, "pr_number", prNumber, "error", err)`
    - Remove the `"log"` import; keep `"fmt"` if `pullrequest.go` still uses it elsewhere (verify before removing)
    - _Requirements: 1.1, 1.2, 1.4, 1.5_
  - [x] 5.4 Rewrite `sweep.go` (6 sites)
    - `sweep scan: %v` → `slog.ErrorContext(ctx, "sweep scan", "error", err)`
    - `sweep claim for %q` → `slog.ErrorContext(ctx, "sweep claim", "operation_id", operationID, "error", err)`
    - `diagnosing timeout for operation %q` → `slog.ErrorContext(ctx, "diagnosing timeout", "operation_id", operationID, "error", err)`
    - `updating check run for timed-out operation %q` → `slog.ErrorContext(ctx, "updating check run for timed-out operation", "operation_id", operationID, "error", err)`
    - `deleting timed-out operation record %q` → `slog.ErrorContext(ctx, "deleting timed-out operation record", "operation_id", operationID, "error", err)`
    - `publishing timeout notification for %q` → `slog.ErrorContext(ctx, "publishing timeout notification", "operation_id", operationID, "error", err)`
    - Remove the `"log"` import; keep `"fmt"` if `sweep.go` still uses it elsewhere (verify before removing)
    - _Requirements: 1.1, 1.2, 1.4, 1.5_
  - [x] 5.5 Rewrite `comment.go` (2 sites)
    - `posting reply comment on %s/%s#%d` → `slog.ErrorContext(ctx, "posting reply comment", "owner", owner, "repo", repoName, "pr_number", event.PullRequest.Number, "error", err)`
    - `releasing lock %q via unlock command` → `slog.ErrorContext(ctx, "releasing lock via unlock command", "lock_key", key, "error", err)`
    - Leave the unrelated `fmt.Fprintf(&b, "- line %d: `%s`\n", ...)` at line 22 untouched — it builds Markdown comment body text, not a log line
    - Remove the `"log"` import; keep `"fmt"` (still used for the comment-body builder)
    - _Requirements: 1.1, 1.2, 1.4, 1.5_
  - [x] 5.6 Rewrite `execute.go` (3 sites)
    - `creating check run for %s/%s#%d %s` → `slog.ErrorContext(ctx, "creating check run", "owner", repo.Owner, "repo", repo.Name, "pr_number", pr.Number, "project", t.Project.Name, "error", err)`
    - `recording job name for operation %q` → `slog.ErrorContext(ctx, "recording job name for operation", "operation_id", operationID, "error", err)`
    - `deleting operation record %q` → `slog.ErrorContext(ctx, "deleting operation record", "operation_id", operationID, "error", err)`
    - Remove the `"log"` import; keep `"fmt"` if `execute.go` still uses it elsewhere (verify before removing)
    - _Requirements: 1.1, 1.2, 1.4, 1.5_

- [x] 6. Checkpoint - Verify `internal/orchestrator` compiles and existing tests still pass
  - Ensure `go build ./...` succeeds and `go test ./internal/orchestrator/...` passes unchanged (per Requirement 1.5 / design.md's Testing Strategy: no existing assertion touches log output, so no test file itself should need edits). Ask the user if questions arise.

- [x] 7. Write unit tests for `internal/logging`
  - [x] 7.1 Create `internal/logging/logging_test.go`
    - Table test over `ParseLevel`: `"debug"`, `"info"`, `"warn"`, `"error"`
      (all lowercase), at least one mixed-case input (e.g. `"WARN"` or
      `"Error"`) mapping the same as its lowercase form, an empty string,
      and an unrecognized value (e.g. `"verbose"`) — the latter two both
      expected to yield `slog.LevelInfo`
    - Smoke test that `New(w, level)` writes well-formed JSON to a
      `bytes.Buffer`: at `slog.LevelInfo`, a `Debug`-level message is
      absent from the buffer while an `Info`-level message is present and
      unmarshals as valid JSON with the expected `msg`/`level` fields
    - Use `github.com/stretchr/testify`'s `assert`/`require` per this
      repo's testing convention (`require` for the JSON-unmarshal
      precondition, `assert` for the individual field checks)
    - Verify with `go test ./internal/logging/...`
    - _Requirements: 1.1, 1.3_

- [x] 8. Final checkpoint - Full verification
  - Ensure `go build ./...` succeeds, `go test -race ./...` passes
    repo-wide, `gofmt -l .` is empty, `go mod tidy` produces no changes
    (no new dependency — Decision 0), and the real golangci-lint v2 passes
    (`go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest run ./...`,
    or `go vet ./...` + `gofmt -l .` as a fallback if that download is
    unavailable — see `CLAUDE.md`). Confirm `internal/logging`'s coverage
    meets the 90% target from design.md's Testing Strategy
    (`go test ./internal/logging/... -coverprofile=/tmp/cover.out && go tool cover -func=/tmp/cover.out`).
    Ask the user if questions arise.

## Notes

- No new dependencies — `log/slog` is standard library (see Decision 0 in
  design.md, and Requirement 1.1's "no new third-party logging
  dependency").
- This slice is a pure format/structure change to existing log call
  sites (Requirement 1.5): no new events are logged, no call site moves,
  and no other package's behavior changes. `internal/orchestrator`'s
  exported API is untouched.
- `comment.go`'s `fmt.Fprintf(&b, ...)` (building Markdown comment body
  text) and any other non-logging `fmt.Fprintf`/`fmt.Errorf` call in these
  files are explicitly out of scope — only the `log.Printf` call sites
  listed in requirements.md and the two `cmd/*/main.go` startup-error
  `fmt.Fprintf`s are in scope.
- Tasks 5.1-5.6 (the six orchestrator files) have no dependency on each
  other — only on `internal/logging` existing (task 1) — and can be done
  in parallel, as reflected in the dependency graph below.

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1.1", "1.2"] },
    { "id": 1, "tasks": ["3.1", "3.2", "5.1", "5.2", "5.3", "5.4", "5.5", "5.6"] },
    { "id": 2, "tasks": ["7.1"] }
  ]
}
```
