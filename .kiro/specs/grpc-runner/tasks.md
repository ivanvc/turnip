# Implementation Plan: gRPC & Runner (Slice 5)

## Overview

This plan implements the proto contract, `internal/jobs`, `internal/rpc`,
`internal/runner`, and one additive amendment to the already-complete
`internal/plugin` (Slice 2), per `design.md`. Tasks are ordered so the
independent leaves (the `internal/plugin` amendment, the proto fill-in,
`internal/jobs`) land first, since nothing else in this slice depends on
them landing in any particular order relative to each other, followed by
`internal/rpc` (needs the generated proto code), then `internal/runner`
(the biggest package — needs the amended `internal/plugin`, the generated
proto code, and its own config/clone/reporter files), then `cmd/runner`
wiring, then tests (unit, then property).

## Tasks

- [x] 1. Amend `internal/plugin` for incremental output (Slice 2 amendment)
  - [x] 1.1 Add `ExecuteOptions.OnOutput` and rewrite `execCommand`
    - Add `OnOutput func(stream, line string)` to `ExecuteOptions` in `internal/plugin/plugin.go`, documented as optional (nil means no incremental callback, today's behavior unchanged); `stream` is `"stdout"` or `"stderr"` — carrying which pipe a line came from is what lets a caller both mirror it to the matching local stream and pick a `LogLine.level` without guessing (design.md's Decision 2 note)
    - Rewrite `execCommand` in `internal/plugin/command.go`: instead of `cmd.Output()`/whole-buffer capture, obtain `cmd.StdoutPipe()`/`cmd.StderrPipe()`, read each with a `bufio.Scanner` on its own goroutine, write every scanned line into a shared, mutex-guarded `bytes.Buffer` per stream (preserving today's `(stdout, stderr []byte, exitCode int, err error)` return contract exactly), and — when a non-nil `OnOutput` is threaded through — invoke it once per line as it's scanned, tagged with whichever pipe that goroutine is reading
    - `commandRunner`'s signature does not change; `execCommand`'s new line-scanning behavior is purely internal. `HelmfilePlugin.Execute` gains a small change: pass `opts.OnOutput` through to wherever it invokes `p.run` (the `commandRunner`) — this requires `commandRunner`'s type to grow an `OnOutput` parameter, or for `Execute` to close over `opts.OnOutput` when constructing the call; pick whichever keeps `commandRunner`'s existing test fakes (`fakeRunner` in `helmfile_test.go`) working with minimal change
    - Verify `go build ./internal/plugin/...` and `go test ./internal/plugin/...` (existing tests, unmodified expectations) still pass
    - _Requirements: (Slice 5's Requirement 4.1, implemented as a Slice 2 amendment)_

  - [x] 1.2 Add tests for the new incremental-output path
    - `command_test.go`: cover `OnOutput` being invoked once per line, in order, tagged with the correct `stream` ("stdout" vs "stderr"), for a command producing multiple lines on both; cover `OnOutput` never being invoked when nil (today's behavior, unchanged); cover the returned whole-buffer `stdout`/`stderr` still matching exactly what `OnOutput` was called with per stream, concatenated
    - `helmfile_test.go`: cover `ExecuteOptions.OnOutput` reaching the underlying `commandRunner`/`execCommand` unchanged through `HelmfilePlugin.Execute`
    - Verify `go test -race ./internal/plugin/...` passes and coverage remains at 90%+ (the existing target for this package)
    - _Requirements: (test coverage for 1.1)_

  - [x] 1.3 Record the amendment in `plugin-helmfile`'s `tasks.md`
    - Append a new numbered task there (this repo's audit-trail convention — `tasks.md` for a `Complete` slice is never edited in place) summarizing 1.1/1.2 and linking back to this slice's Requirement 4.1 as the reason
    - _Requirements: (process, no direct requirement)_

- [x] 2. Fill in the protobuf schema and regenerate
  - [x] 2.1 Write `proto/turnip/v1/operation.proto` per design.md's "Requirement 1" section
    - Change `ExecuteOperation`'s request to `stream ExecuteOperationRequest` (client-streaming); keep the RPC name, and both message names, unchanged from Slice 0's placeholder
    - Define `ExecuteOperationRequest` as a `oneof payload { OperationStart start = 1; LogLine log = 2; OperationResult result = 3; }`
    - Define `OperationStart`, `LogLine`, `OperationResult`, `ChangeSummary` exactly per design.md's field lists (including `OperationStart.resumed` and `.operation_id`)
    - Leave `ExecuteOperationResponse` empty (design.md's "deliberately empty" note)
    - Run `make proto-gen` (`cd proto && buf generate`) and verify it regenerates `internal/grpc/turnip/v1/*.pb.go` cleanly with no manual edits needed
    - Verify `go build ./internal/grpc/...`
    - _Requirements: 1.1, 1.2, 1.3_

- [x] 3. Checkpoint - Verify the amended Plugin and generated proto code compile
  - Ensure `go build ./...` succeeds. Ask the user if questions arise.

- [x] 4. Implement the Kubernetes Job builder
  - [x] 4.1 Create `internal/jobs/versions.go`
    - Define the per-tool vendor-image table from design.md (`terraform`/`pulumi`/`helmfile` → vendor image template, binary path, default version) as an unexported map or switch
    - Implement `resolveVersion(tool, requestedVersion string) (version string, err error)`: returns the requested version if it's in that tool's known-good list, the documented default if `requestedVersion` is empty, or an error identifying the unrecognized version otherwise (Requirement 8.3-8.5)
    - Verify compilation with `go build ./internal/jobs/...`
    - _Requirements: 8.3, 8.4, 8.5_

  - [x] 4.2 Create `internal/jobs/build.go`
    - Define `OperationParams` and `BuildJob(project config.Project, op OperationParams) (*batchv1.Job, error)` per design.md's sketch
    - Call `resolveVersion` first; return its error immediately without constructing any part of the Job spec on failure (Requirement 8.4)
    - Build one initContainer per Requirement 8.1 using the resolved vendor image, `command: ["sh", "-c", "cp <binary path> /tools/<tool>"]`, mounting a shared `emptyDir` volume at `/tools`
    - Build the Runner main container: image from a package-level constant (the Runner's own image, not tool-specific), `/tools` prepended to `PATH` via an `PATH=/tools:$PATH` env entry or an explicit shell wrapper, and every environment variable Requirement 7.2 lists plus `OPERATION_ID` and `SERVER_ADDR` (design.md's note on the two names Requirement 7.2's literal list doesn't separately spell out)
    - Set `Spec.RestartPolicy: corev1.RestartPolicyNever` and `Spec.TTLSecondsAfterFinished` to a package-level `*int32` constant of 15 minutes (design.md, matching the result-delivery retry budget)
    - Verify compilation with `go build ./internal/jobs/...`
    - _Requirements: 7.1, 7.2, 7.3, 7.4, 8.1, 8.2, 9.4_

  - [x] 4.3 Create `internal/jobs/client.go`
    - Define a thin wrapper (e.g. `type Client struct { jobs batchv1client.JobInterface }`, constructed from a `*kubernetes.Clientset` and namespace) with `Create(ctx, job *batchv1.Job) (*batchv1.Job, error)` and `Delete(ctx, name string) error`
    - `Delete` SHALL pass `metav1.DeleteOptions{PropagationPolicy: ptr.To(metav1.DeletePropagationBackground)}` explicitly (Requirement 9.3, design.md's note on not relying on implicit API server defaults)
    - Verify compilation with `go build ./internal/jobs/...`
    - _Requirements: 7.1, 9.2, 9.3_

- [x] 5. Checkpoint - Verify `internal/jobs` compiles
  - Ensure `go build ./internal/jobs/...` succeeds. Ask the user if questions arise.

- [x] 6. Implement Server-side gRPC hosting
  - [x] 6.1 Create `internal/rpc/server.go`
    - Define `OperationHandler` interface (`HandleLog`, `HandleResult`) and `NewServer(handler OperationHandler) *grpc.Server` per design.md
    - Implement the generated `OperationServiceServer`'s `ExecuteOperation(stream) error` method: loop `stream.Recv()` until `io.EOF`, switching on each `ExecuteOperationRequest`'s oneof — `start` is a no-op at this layer (design.md's note: nothing in this slice's scope reacts to an operation merely starting), `log` calls `handler.HandleLog`, `result` calls `handler.HandleResult`; on `io.EOF`, call `stream.SendAndClose(&pb.ExecuteOperationResponse{})`
    - A `HandleLog`/`HandleResult` error SHALL abort the loop and return that error as the RPC's status (so the Runner's reporter sees the call fail and retries, rather than silently swallowing a handler failure)
    - Verify compilation with `go build ./internal/rpc/...`
    - _Requirements: 3.1, 3.2, 4.1, 4.2_

- [x] 7. Checkpoint - Verify `internal/rpc` compiles
  - Ensure `go build ./internal/rpc/...` succeeds. Ask the user if questions arise.

- [x] 8. Implement Runner startup configuration
  - [x] 8.1 Create `internal/runner/config.go`
    - Define `Config` per design.md's sketch
    - Implement `ConfigFromEnv(env func(string) string) (Config, error)` (an injectable lookup function, not a direct `os.Getenv` call, so it's testable without mutating process-wide env — mirroring `internal/plugin`'s `commandRunner` testability-seam convention) reading `SERVER_ADDR`, `OPERATION_ID`, `PROJECT_NAME`, `PROJECT_DIR`, `TOOL`, `OPERATION`, `REPO_URL`, `COMMIT_SHA`, `GITHUB_TOKEN`, and a JSON-encoded `TOOL_CONFIG`/`EXTRA_ARGS`/`PLAN_DATA` (base64) as needed
    - Return an error naming every missing required variable at once (mirroring `internal/config`'s accumulate-everything validation convention), not just the first
    - Verify compilation with `go build ./internal/runner/...`
    - _Requirements: 2.1_

- [x] 9. Implement repository cloning
  - [x] 9.1 Create `internal/runner/clone.go`
    - Implement `Clone(ctx, dir, repoURL, commitSHA, token string) error` shelling out to `git` via a local `commandRunner`-equivalent seam (a small unexported function type, same testability pattern as `internal/plugin/command.go`, deliberately not shared/imported across packages per this codebase's preference for a few duplicated lines over a premature cross-package abstraction for two call sites)
    - Sequence: `git init <dir>`; `git -C <dir> remote add origin <repoURL with token embedded as `x-access-token:<token>@`>`; `git -C <dir> fetch --depth 1 origin <commitSHA>`; `git -C <dir> checkout FETCH_HEAD` — a shallow, single-commit fetch rather than a full clone, since IaC repos can be large and only one commit's tree is ever needed
    - Never include the token in any error message or log line `Clone` produces (redact the remote URL before it can reach `OnOutput` or an error string)
    - Verify compilation with `go build ./internal/runner/...`
    - _Requirements: 5.1, 5.2_

- [x] 10. Checkpoint - Verify config and clone compile
  - Ensure `go build ./internal/runner/...` succeeds. Ask the user if questions arise.

- [x] 11. Implement the retrying reporter
  - [x] 11.1 Create `internal/runner/backoff.go`
    - Implement a small exponential-backoff-with-full-jitter helper per design.md's table: `func nextDelay(attempt int, base, max time.Duration, rnd func() float64) time.Duration`, and a loop-driving type/function that repeats a given `func() error` with that delay, stopping on success or when a total elapsed-time budget is exceeded, taking `now func() time.Time` and `sleep func(time.Duration)` seams (never a real `time.Sleep` in this file directly, so tests never wait on wall-clock time)
    - Verify compilation with `go build ./internal/runner/...`
    - _Requirements: 2.6_

  - [x] 11.2 Create `internal/runner/reporter.go`
    - Define a `reporter` type wrapping an `OperationServiceClient`, the `Config` (for `OperationID` and everything `OperationStart` needs), a bounded (256 KiB) log ring buffer guarded by a mutex, and the backoff helper from 11.1
    - Implement `Connect(ctx) error`: dial `Config.ServerAddr`, retrying per the "initial connection" row of design.md's backoff table (base 1s, max 30s, 2-minute budget) — Requirement 2.2/2.3
    - Implement `LogLine(stream, line string)`: map `stream` to a `LogLine.level` (`"stdout"` → `"info"`, `"stderr"` → `"error"`), append the resulting `LogLine` to the ring buffer (dropping the oldest entries and inserting a `"N lines dropped"` marker on overflow, Requirement 4.3), and, if a stream is currently open, attempt to send it immediately — a send failure here does not itself trigger reconnection logic; it just leaves the line in the buffer for the next successful stream to carry. This method may block briefly on the gRPC send; callers that also need an unconditional, non-blocking local echo (task 13.1) must do that *before* calling this, not rely on this method to do it
    - Implement `Report(ctx, result OperationResult) error`: the top-level driver — opens a stream (if not already open) and sends `OperationStart{resumed: false}` on the very first attempt or `{resumed: true}` on every subsequent one, resends the *entire current buffer's* contents (design.md's note on why: no per-message ack exists to tell the Runner what already arrived), sends `result`, closes the send side, and waits for `ExecuteOperationResponse`; on any failure at any point in that sequence, applies the "reconnection"/"final result delivery" backoff row (base 1s, max 60s, 15-minute total budget) and retries the whole sequence from a fresh stream (Requirement 2.4, 2.5, 4.4)
    - `Report`'s local subprocess dependency: `Report` is only ever called once `Plugin.Execute` has already returned, so nothing in this file ever needs to touch or cancel the subprocess — Requirement 2.4's "don't abort the subprocess" guarantee is satisfied structurally, by `Report` and subprocess execution never sharing a cancellation path (design.md's note)
    - If the 15-minute budget in `Report` is exhausted, return an error distinguishable (via `errors.Is`/a sentinel) from a tool-execution failure (Requirement 4.5, 4.6) — the caller (`run.go`) is what logs the result locally and chooses the exit code
    - Verify compilation with `go build ./internal/runner/...`
    - _Requirements: 2.4, 2.5, 2.6, 3.1, 3.2, 4.1, 4.2, 4.3, 4.4, 4.5, 4.6_

- [x] 12. Checkpoint - Verify the reporter compiles
  - Ensure `go build ./internal/runner/...` succeeds. Ask the user if questions arise.

- [x] 13. Wire the Runner together
  - [x] 13.1 Create `internal/runner/run.go`
    - Implement `Run(ctx context.Context, cfg Config) int`: clone (task 9), select the `Plugin` matching `cfg.Tool` (Requirement 6.1 — today, only `"helmfile"` resolves to `plugin.NewHelmfilePlugin()`; an unrecognized tool is a startup error, not a Plugin-dispatch one), construct `plugin.ExecuteOptions` from `cfg` with `OnOutput` wired per the next bullet, call `Execute`, translate `*plugin.ExecuteResult` into `OperationResult` (Requirement 6.3), and call the reporter's `Report`
    - **`OnOutput` fans out to two places for every `(stream, line)`, and the first must never wait on the second (Requirement 4.7):** (1) write `line` to `os.Stdout` if `stream == "stdout"` or `os.Stderr` if `stream == "stderr"` — a direct, synchronous, unbuffered write, on the same goroutine `OnOutput` was called on, never queued behind a channel or anything network-related; then (2) call the reporter's `LogLine(stream, line)` (task 11.2), which does its own buffering/retrying independently and MAY block or take time — but only *after* step (1) has already completed, so a live `kubectl logs -f` never stalls just because the Server is unreachable or the reporter is mid-backoff
    - Connect the reporter (task 11.2's `Connect`) *before* cloning/executing, so a Runner that can never reach the Server at all fails fast per Requirement 2.3, rather than doing a full clone and plan/apply run first only to discover it can't report the outcome — note this connection attempt happens before any operation output exists, so it doesn't interact with the `OnOutput` ordering guarantee above
    - Return `0` on a successful `Report`, non-zero otherwise, logging enough context (including the full `OperationResult`, per Requirement 4.5) to `os.Stderr` before returning so a `kubectl logs` on the Pod is never empty-handed — this is in addition to, not a replacement for, the real-time mirroring `OnOutput` already did throughout execution
    - Verify compilation with `go build ./internal/runner/...`
    - _Requirements: 2.3, 4.7, 5.3, 6.1, 6.2, 6.3_

  - [x] 13.2 Update `cmd/runner/main.go`
    - Replace the Slice 0 placeholder with: `internal/runner.ConfigFromEnv(os.Getenv)`, a fatal exit on a config error, then `os.Exit(internal/runner.Run(context.Background(), cfg))`
    - Verify `go build ./...` still succeeds for the whole module
    - _Requirements: (wiring, no direct requirement)_

- [x] 14. Checkpoint - Verify the library compiles end-to-end
  - Ensure `go build ./...` succeeds and `go vet ./internal/jobs/... ./internal/rpc/... ./internal/runner/...` reports no issues. Ask the user if questions arise.

- [x] 15. Write unit tests
  - [x] 15.1 Write `internal/jobs/versions_test.go` and `internal/jobs/build_test.go`
    - Cover `resolveVersion` for each tool: a recognized explicit version, an empty version falling back to the documented default, and an unrecognized version returning an error
    - Cover `BuildJob` producing exactly one initContainer per tool with the correct vendor image and binary-copy command, a main container with every Requirement 7.2 environment variable (plus `OPERATION_ID`/`SERVER_ADDR`) present and correctly valued, `RestartPolicy: Never`, and `TTLSecondsAfterFinished` set to the 15-minute constant
    - Cover `BuildJob` returning `resolveVersion`'s error unchanged (and constructing no Job) when the requested version is unrecognized
    - _Requirements: 7.1, 7.2, 7.3, 7.4, 8.1, 8.2, 8.3, 8.4, 8.5, 9.4_

  - [x] 15.2 Write `internal/jobs/client_test.go`
    - Using `k8s.io/client-go/kubernetes/fake`, cover `Create` calling through to the fake clientset and returning the created Job, and `Delete` calling through with `PropagationPolicy: Background` set on the request the fake clientset observed
    - _Requirements: 7.1, 9.2, 9.3_

  - [x] 15.3 Write `internal/rpc/server_test.go`
    - Using `google.golang.org/grpc/test/bufconn` and a fake `OperationHandler` recording calls, cover a full `start`→`log`→`log`→`result` client stream producing the corresponding `HandleLog`/`HandleLog`/`HandleResult` calls in order and a successful `ExecuteOperationResponse`
    - Cover a `HandleResult` error aborting the RPC with a non-OK status rather than a successful response
    - _Requirements: 3.1, 3.2, 4.1, 4.2_

  - [x] 15.4 Write `internal/runner/config_test.go`
    - Cover a fully-populated env map producing the expected `Config`
    - Cover one or more missing required variables producing a single error naming all of them
    - _Requirements: 2.1_

  - [x] 15.5 Write `internal/runner/clone_test.go`
    - Against a real local git repository fixture (created via `git init`/`git commit` in `t.TempDir()`, not a mock — git is deterministic and already a test-environment dependency, matching `internal/plugin/command_test.go`'s precedent of exercising real subprocesses), cover `Clone` checking out exactly the given commit SHA, verified by reading a file only present at that commit
    - Cover `Clone` against a nonexistent commit SHA returning an error
    - Cover the token never appearing in any returned error's message (construct a table of induced-failure scenarios and grep each error string)
    - _Requirements: 5.1, 5.2, 5.3_

  - [x] 15.6 Write `internal/runner/backoff_test.go`
    - Using injected `now`/`sleep`/`rnd` seams (never real time), cover the delay sequence staying within `[0, max]` and growing exponentially up to the cap, and the loop stopping — without exceeding the configured budget by more than one in-flight attempt's duration — once the total budget elapses
    - _Requirements: 2.6_

  - [x] 15.7 Write `internal/runner/reporter_test.go`
    - Using `bufconn` and a fake `OperationServiceServer` that can be scripted to fail the first N calls (closing the stream early or returning an error) before succeeding, injecting the backoff seams from 11.1 so the test never waits on real time:
      - Cover `Connect` succeeding on the first attempt with no retries
      - Cover `Connect` retrying and eventually succeeding when the fake server fails the first few dial/call attempts
      - Cover `Connect` giving up once its budget elapses against a fake server that always fails
      - Cover `Report` resending the full buffered log content (not just lines produced since the last attempt) on a reconnect, and sending `resumed: true` on every attempt after the first
      - Cover `Report` succeeding once the fake server accepts a `start`→`result` sequence and returns `ExecuteOperationResponse{}`
      - Cover `Report` returning the distinguishable "budget exhausted" error/sentinel once its 15-minute-equivalent (scaled down via the injected seams) budget elapses against an always-failing fake server
      - Cover the log ring buffer's drop-oldest-plus-marker behavior once more lines are produced than the 256 KiB bound allows
    - _Requirements: 2.4, 2.5, 2.6, 4.3, 4.4, 4.5, 4.6_

  - [x] 15.8 Write `internal/runner/run_test.go`
    - Using a fake `Plugin` (produces a scripted sequence of `OnOutput` calls, then returns a fixed `*ExecuteResult`) and a fake reporter recording `LogLine` calls, cover `Run`'s `OnOutput` wiring writing every line to `os.Stdout`/`os.Stderr` (captured via a redirected pipe or an injected `io.Writer` seam) matching each call's `stream`, in order
    - Cover the local stdout/stderr write happening even when the fake reporter's `LogLine` blocks: have the fake `LogLine` block on a channel the test controls, and assert the captured stdout/stderr already contains the line *before* the test unblocks that channel — proving step (1) never waits on step (2) (Requirement 4.7), deterministically, with no reliance on real wall-clock timing
    - Cover an unrecognized `cfg.Tool` failing fast as a startup error before any clone or reporter `Connect` attempt (Requirement 6.1)
    - _Requirements: 4.7, 6.1, 6.2, 6.3_

- [x] 16. Checkpoint - Verify unit tests pass with target coverage
  - Ensure `go test -race ./internal/jobs/... ./internal/rpc/... ./internal/runner/... ./internal/plugin/...` passes. Coverage target: 80% for `internal/jobs`, `internal/rpc`, and `internal/runner` (this slice adopts the "Server webhook handling" bucket's rationale per design.md, since the global design's Testing Strategy doesn't name a target for gRPC/Runner/Kubernetes code); `internal/plugin` remains at its existing 90% target. Ask the user if questions arise.

- [x] 17. Write property tests
  - [x] 17.1 Write `internal/jobs/property_test.go`
    - `// Feature: multi-iac-automation-platform, Property 23: Runner Job Environment Variables` — for a random Project/OperationParams combination, assert `BuildJob`'s main container always includes the repository URL, commit SHA, Project directory, and Operation as environment variables; ≥100 iterations
    - `// Feature: multi-iac-automation-platform, Property 23a: Runner Job Tool Provisioning` — for a random tool and a random valid version for that tool, assert `BuildJob` includes exactly one initContainer using that tool's vendor image tagged with that version, and that the main container's volume mounts include the initContainer's shared volume; ≥100 iterations
    - `// Feature: multi-iac-automation-platform, Property 25: Job Cleanup After Completion` — reinterpreted per design.md's note (this slice's mechanism is `ttlSecondsAfterFinished`, not an explicit post-completion delete call): for any random Project/OperationParams combination, assert every `BuildJob` result has a non-nil `Spec.TTLSecondsAfterFinished` set to the documented constant; ≥100 iterations
    - `// Feature: multi-iac-automation-platform, Property 27: Token Propagation to Runner` — for a random installation token string, assert it appears verbatim as the `GITHUB_TOKEN` environment variable on `BuildJob`'s main container; ≥100 iterations
    - _Requirements: 7.2, 7.4, 8.1, 9.4_

  - [x] 17.2 Write `internal/runner/property_test.go`
    - `// Feature: multi-iac-automation-platform, Property 24: Runner Clones Correct Commit` — for a random sequence of commits made to a real local git repository fixture, assert `Clone` at each successive commit SHA checks out exactly that commit (verified by content unique to it), never a different one; ≥100 iterations (bounded by how many commits are practical to generate per iteration — a small fixed count per run is fine, the randomization is over which SHA in that sequence is targeted)
    - _Requirements: 5.1_

- [x] 18. Final checkpoint - Full verification
  - Ensure `go build ./...` compiles, `go test -race ./...` passes including property tests, `go mod tidy` produces no changes, and `golangci-lint run ./...` passes via `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest run ./...` (see `CLAUDE.md`). Ask the user if questions arise.

- [x] 19. Prefix Runner env var names and fix PATH composition (2026-08 amendment)
  - [x] 19.1 Prefix every `internal/jobs`-set / `internal/runner`-read environment variable with `TURNIP_`
    - Bare names like `TOOL`, `OPERATION`, `SERVER_ADDR` risked colliding with Kubernetes' own injected env vars (`enableServiceLinks`'s per-Service `<NAME>_SERVICE_HOST` variables) or a future base-image `ENV` — user-flagged review comment
    - Renamed all twelve in `internal/jobs/build.go` (`TURNIP_SERVER_ADDR`, `TURNIP_OPERATION_ID`, `TURNIP_PROJECT_NAME`, `TURNIP_PROJECT_DIR`, `TURNIP_TOOL`, `TURNIP_OPERATION`, `TURNIP_REPO_URL`, `TURNIP_COMMIT_SHA`, `TURNIP_GITHUB_TOKEN`, `TURNIP_TOOL_CONFIG`, `TURNIP_EXTRA_ARGS`, `TURNIP_PLAN_DATA`) and their `internal/runner/config.go` reads, updating `build_test.go`, `property_test.go`, and `config_test.go` accordingly
    - _Requirements: (maintenance amendment, no behavioral change)_

  - [x] 19.2 Replace the `PATH` Job-spec override with `TURNIP_TOOLS_DIR` + Runner-side composition
    - The original `PATH` env entry (`/tools:<hardcoded guess at the base image's PATH>`) was wrong on two counts: Kubernetes' `$(VAR)` env-value substitution can't reference a container image's own baked-in `PATH` (only other Pod-spec-declared vars), and hardcoding a guessed replacement value silently breaks if the Runner's base image ever changes — user-flagged review comment
    - `BuildJob` now sets `TURNIP_TOOLS_DIR` to the shared volume's mount path instead of touching `PATH` at all; `internal/runner/config.go` reads it into `Config.ToolsDir`; a new pure `pathWithToolsDir(toolsDir, path string) string` helper in `run.go` composes it with `os.Getenv("PATH")`, applied once via `os.Setenv` in the exported `Run` (not the unit-tested `runWith`, so tests never mutate real process environment)
    - Added `TestBuildJob_MainContainerHasAllEnvironmentVariables`'s `TURNIP_TOOLS_DIR` assertion (and a `NotContains(env, "PATH")` check), plus `TestPathWithToolsDir_PrependsToolsDir`/`TestPathWithToolsDir_EmptyToolsDirLeavesPathUntouched`
    - Also fixed a pre-existing design.md/implementation drift found while editing this section: `OperationParams`'s `ExtraArgs []string` field and `ConfigFromEnv`'s `env func(string) string` parameter were missing from design.md's code sketches even though the real implementation always had them
    - _Requirements: 7.2, 8.2 (composition correctness)_

  - [x] 19.3 Checkpoint - Full re-verification
    - `go build ./...`, `go test -race ./...` (jobs 85.2%, runner 84.1%), `go mod tidy` (stable), `gofmt -l .` (clean), and the real `golangci-lint` v2 all pass with 0 issues

- [x] 20. Add `Client.Status` for Job/Pod diagnostics (server-orchestration amendment)
  - [x] 20.1 Amend `client.go` and add `status.go`
    - server-orchestration (Slice 6) needs to diagnose *why* a Runner Job never started (e.g. an `ImagePullBackOff` on the tool-provisioning initContainer) once its own 5-minute start-deadline sweep claims a timeout — `internal/jobs` only offered `Create`/`Delete`, with no way to query a Job's or its Pod's actual Kubernetes state
    - Added a `pods corev1client.PodInterface` field to `Client`, set in `NewClient` via `clientset.CoreV1().Pods(namespace)` — no signature change
    - Added `JobStatus` and `Client.Status(ctx, jobName) (*JobStatus, error)` in new `status.go`: a not-found Job is `JobStatus{JobFound: false}`, not an error; Pods are found via the `batch.kubernetes.io/job-name={jobName}` label selector (the namespaced label, not the legacy `job-name` — this platform targets only currently-supported Kubernetes versions, so there's no compatibility reason to prefer the older form); init-container `Waiting` statuses are checked before regular container statuses, since the tool-provisioning initContainer (task 4.2) is the more likely place for an image-pull failure than the Runner's own image
    - _Requirements: server-orchestration's Requirement 8.4_

  - [x] 20.2 Add `status_test.go`
    - Using `k8s.io/client-go/kubernetes/fake`: not-found Job, Job with no Pod yet, a Pod with an init-container `ImagePullBackOff` (asserting it isn't overridden by a later regular container also `Waiting`), and a cleanly `Running` Pod with no `Waiting` statuses at all
    - _Requirements: (test coverage for 20.1)_

  - [x] 20.3 Checkpoint - Full re-verification
    - `go build ./internal/jobs/...` and `go test ./internal/jobs/...` pass

- [x] 21. Merge base branch into head during clone, Atlantis-style (2026-08 amendment)
  - [x] 21.1 Rewrite `internal/runner/clone.go` per design.md's Decision 3
    - Change `Clone`/`cloneWith`'s signature to `(ctx, dir, repoURL, commitSHA, baseRef, token string) error`
    - Replace the fetch/checkout `steps` slice with: one `fetch --depth
      <mergeFetchDepth> origin <commitSHA>:refs/turnip/head
      <baseRef>:refs/turnip/base` (both refs in the same fetch call — see
      design.md's Decision 3 for why this, not two separate calls, is what
      makes shared history discoverable), `checkout refs/turnip/head`, then
      `-c user.name=turnip -c user.email=turnip@localhost merge --no-ff -m
      turnip-merge refs/turnip/base`
    - Define `const mergeFetchDepth = 50`
    - On a merge failure whose combined output contains `"refusing to merge
      unrelated histories"`, re-run the fetch with no `--depth` flag and
      retry checkout+merge exactly once before giving up
    - On a merge failure whose combined output contains `CONFLICT` or
      `Automatic merge failed`, return a new `*MergeConflictError` (wrapping
      the redacted output) instead of the generic wrapped-git-error `Clone`
      returns for every other step failure
    - Continue redacting the authenticated remote URL from every error
      message and from `MergeConflictError`'s output exactly as today
    - Verify compilation with `go build ./internal/runner/...`
    - _Requirements: 5.3, 5.4, 5.5, 5.6_

  - [x] 21.2 Update `internal/runner/config.go` and `run.go`
    - Add `Config.BaseRef`, read from a new required `TURNIP_BASE_REF`
      environment variable in `ConfigFromEnv`
    - Update the `cloner` function type and `execute`'s call to `clone(...)`
      in `run.go` to pass `cfg.BaseRef`
    - _Requirements: 5.3_

  - [x] 21.3 Update `internal/jobs/build.go`
    - Add `OperationParams.BaseRef`
    - Add `{Name: "TURNIP_BASE_REF", Value: op.BaseRef}` to the main
      container's `env` alongside `TURNIP_COMMIT_SHA`
    - _Requirements: 7.2 (extended per Decision 3)_

  - [x] 21.4 Update `internal/orchestrator/execute.go` (server-orchestration, Slice 6)
    - Pass `BaseRef: pr.BaseRef` into the `jobs.OperationParams` literal in
      `executeOne` — `github.PullRequest.BaseRef` is already populated by
      the GitHub integration slice, so no webhook-parsing change is needed
    - Cross-slice touch scoped and recorded here per task 20's precedent
      (that task's reverse case: a server-orchestration need driving a
      change inside this slice's own `internal/jobs`); a matching pointer
      entry is added to `server-orchestration/tasks.md`
    - _Requirements: (server-orchestration wiring for this slice's Requirement 5.3)_

  - [x] 21.5 Update tests
    - [x] `internal/runner/clone_test.go`: extended the real
      local-git-repository fixture (`newDivergingFixture`) with a clean
      merge case (disjoint file changes), a real conflict case
      (`*MergeConflictError`, asserted via `errors.As`), and a base branch
      only reachable after the unshallow fallback (`mergeFetchDepth+10`
      filler commits past the fork point) — all three pass against a real
      `git` binary, confirming the shallow-boundary/fallback reasoning in
      design.md's Decision 3 holds in practice
    - [x] `internal/runner/config_test.go`: `TURNIP_BASE_REF` required-var
      and round-trip cases added to the existing fixture
    - [x] `internal/runner/property_test.go`: added
      `TestProperty_RunnerClonesCorrectCommit_WithBaseMerge` (tagged
      `// Feature: multi-iac-automation-platform, Property 24: Runner
      Clones Correct Commit`, reinterpreted per requirements.md's
      Introduction note) alongside the original — a `divergingFixture`
      helper builds one base branch and one head branch with disjoint
      marker files, and for a random head commit index (100 rapid
      iterations) asserts `Clone`'s resulting tree contains both that
      commit's unique content and the base branch's current tip content;
      the original test is kept as-is since it still validates the
      `baseRef == ""` no-op path
    - [x] `internal/jobs/build_test.go`: `TURNIP_BASE_REF` added to
      `testParams()` and asserted in
      `TestBuildJob_MainContainerHasAllEnvironmentVariables`
    - [x] `internal/orchestrator/execute_test.go` (server-orchestration):
      `testPR` now carries `BaseRef: "main"`; new
      `TestExecuteOne_JobCarriesPullRequestBaseRef` captures the Job
      `fakeJobCreator.Create` receives and asserts `TURNIP_BASE_REF`
      matches `pr.BaseRef`
    - _Requirements: (test coverage for 21.1-21.4)_

  - [x] 21.6 Checkpoint - Full re-verification
    - `go build ./...`, `go test -race ./...` (including the reinterpreted
      Property 24), `go mod tidy` produces no changes, `gofmt -l .` clean,
      and the real `golangci-lint` v2 (see `CLAUDE.md`) all pass — all
      confirmed green

- [x] 22. Stop gating `version` on an exhaustive allowlist
  - [x] 22.1 Rewrite `internal/jobs/versions.go`'s validation
    - Found in design review: `resolveVersion` required
      `slices.Contains(ti.versions, requestedVersion)`, so adopting any
      vendor-published release turnip hadn't already hardcoded into
      `toolImages[tool].versions` still needed a turnip code change and
      redeploy — contradicting Requirement 8's own user story ("turnip
      never lags behind a tool's latest release the way a bundled-binary
      approach would") and reintroducing, one layer up, exactly the
      release-cadence bottleneck the per-tool initContainer/vendor-image
      design exists to avoid — see design.md's new "Version validation"
      section for the full reconciliation and the alternative considered
    - Replaced the `slices.Contains` membership check with
      `versionPattern`, a permissive semver-shaped regex
      (`^[0-9]+\.[0-9]+\.[0-9]+(-[…])?(\+[…])?$`) — any well-formed
      version is accepted regardless of whether it appears in
      `toolImage.versions`, which now serves only to pick the default
      (Requirement 8.5) and as an error-message hint
    - `UnrecognizedVersionError`'s `Known []string` field renamed to
      `Examples []string` and its message reworded to make clear these are
      illustrative, not exhaustive
    - Amended global-spec-adjacent wording: `docs/configuration.md`'s
      `config` map section, and this slice's Requirement 8.3/8.4
      (`requirements.md`) — both previously described a "known-good list"
      / "recognized" check in a way that implied an allowlist
    - _Requirements: 8.3, 8.4, 8.5 (reinterpreted — see design.md)_

  - [x] 22.2 Update tests
    - `versions_test.go`: `TestResolveVersion_UnrecognizedVersion` renamed
      to `TestResolveVersion_MalformedVersionIsRejected` and its case
      changed from `"0.0.1-does-not-exist"` (which is actually valid
      semver with a prerelease tag, so it would have silently started
      passing under the new logic) to a table of genuinely malformed
      values (`not-a-version`, `latest`, `v1.9.5`, `1.x`, `1.9`); added
      `TestResolveVersion_WellFormedVersionNotInExampleListIsAccepted`,
      the crux test — a version not present in `toolImage.versions` for
      any tool still resolves successfully
    - `build_test.go`: added
      `TestBuildJob_VersionNotInExampleListStillBuilds`, the same
      assertion at the `BuildJob` level (the actual public entry point)
    - `TestBuildJob_UnrecognizedVersionReturnsErrorAndNoJob` (pre-existing,
      used `"not-a-real-version"`) needed no change — already malformed
      under the new regex too
    - _Requirements: (test coverage for 22.1)_

  - [x] 22.3 Checkpoint - Full re-verification
    - `go build ./...`, `go test -race ./...` (all packages, including
      `internal/jobs`'s existing property tests, unaffected), `gofmt -l .`
      clean, and the real `golangci-lint` v2 (see `CLAUDE.md`) both pass

- [x] 23. Add `OperationParams.ServiceAccount` to the Job builder (server-orchestration amendment)
  - `BuildJob` set no `serviceAccountName`, so every Runner Pod ran as its
    namespace's `default` ServiceAccount — no cloud identity (EKS Pod
    Identity/IRSA) and no cluster RBAC, which surfaced as a Runner failing
    to read its repository's S3-backed values with "no EC2 IMDS role found"
  - Added `OperationParams.ServiceAccount`, set as `ServiceAccountName` on
    the Pod spec; empty leaves it unset, preserving previous behavior
  - Resolution — including whether a Project's own turnip.yaml may choose
    the account — belongs to the Server, not this package: see
    `server-orchestration/tasks.md` task 22 and that slice's design.md
    Decision 5. Recorded here per task 20/21's precedent for cross-slice
    changes noted on both sides
  - _Requirements: (wiring for server-orchestration's Requirement 7.10-7.11)_

- [x] 24. Strip the Runner's sandbox path from reported output (2026-09 amendment)
  - From a real PR comment: a helmfile error ran ~650 characters on one
    line inside a fenced code block, which can only scroll sideways. The
    same `/tmp/turnip-runner-2858245619/` prefix appeared twice — ~60 of
    those characters — and means nothing to a reviewer, who recognizes
    `environments/secrets.yaml.gotmpl`, the path in their own repository
  - Worth being honest about the size of this win: it removes noise and
    makes paths recognizable, but a ~600-character single line still
    scrolls. Wrapping (the deferred option below) is what actually fixes
    the scrolling
  - `stripSandboxPath(dir, s)` (new `sandboxpath.go`) rewrites the
    `os.MkdirTemp` directory out of text: the prefix collapses to
    nothing, leaving repository-relative paths, and a bare mention of the
    directory becomes `.`
  - Applied in `execute` to everything bound for the Server — the clone
    and execute failure messages, `Output`, `ErrorMessage`, and each
    streamed log line — but deliberately *not* to the local stdout/stderr
    mirroring, since `kubectl logs` is where the absolute path is still
    worth having
  - Presentation only: no change to what runs, or to exit codes
  - Chosen over the alternatives discussed for the same comment: leaving
    the error outside a code fence (rejected by the user), splitting the
    wrap chain on `": "` (would mangle `s3://` URIs), and hard-wrapping
    at a column limit (deferred — it damages copy-paste and can split
    URLs mid-token)
  - _Requirements: 4.2, 5.3 (presentation of reported output)_

  - [x] 24.1 Tests
    - `sandboxpath_test.go`: the real repeated-prefix error shape, a bare
      directory mention, an empty dir no-op, unrelated text untouched,
      and that another Runner's directory is left alone
    - _Requirements: (coverage for 24)_

- [x] 25. Rename the Job label prefix to a domain turnip owns (2026-09 amendment)
  - `BuildJob` labelled every Runner Job with `turnip.io/operation-id` and
    `turnip.io/project`. Kubernetes doesn't verify prefix ownership, so this
    worked, but the convention is to name a domain you control and
    `turnip.io` is not one. Recorded in `roadmap.md`'s Backlog until now,
    and settled deliberately while the cost is still two literals: nothing
    selects on these keys (the only label selector in the codebase is
    `internal/jobs/status.go`'s `batch.kubernetes.io/job-name`, Kubernetes'
    own built-in label), no `deploy/` manifest or dashboard references
    them, and Job lookups go by name — so there is no in-flight-Job
    migration to stage across a rollout. That stops being true the moment
    anyone builds a selector or dashboard on them
  - Both keys move to the `turnip.ivan.vc/` prefix, and are now exported as
    `jobs.OperationIDLabel`/`jobs.ProjectLabel` rather than written as
    string literals. The duplication across a package boundary is what made
    this rename a five-file edit instead of a one-line one; the next prefix
    change is a single edit
  - `internal/orchestrator`'s `execute_test.go` and `integration_test.go`
    now read those constants instead of their own copies of the literal;
    `server-orchestration`'s `requirements.md` and `design.md` mentions
    updated to match what ships; the Backlog entry removed from
    `roadmap.md`
  - _Requirements: (maintenance amendment, no behavioral change)_

- [x] 26. The service gains an interceptor and a metadata contract
      (runner-authentication amendment, Slice 25)
  - `NewServer(handler)` becomes `NewServer(handler, opts ...Option)`,
    with `WithAuthenticator` and `WithCertificate`. This slice's design
    noted that `NewServer` took no options as a reason there was exactly
    one place to add one; Slice 25 is what took it
  - Every stream is now authenticated by a `grpc.StreamInterceptor`
    before its handler runs, including an RPC added later whose author
    adds no check. A `NewServer` built without `WithAuthenticator`
    refuses every stream rather than accepting every stream
  - The Runner attaches two metadata values to each stream it opens: the
    bearer token from its projected ServiceAccount token, and the
    Operation id it claims. Both keys are defined once, in
    `internal/rpc/metadata.go`, because a mismatch between the two sides
    is not a compile error — it is a Server that refuses every Runner
  - `ExecuteOperation` no longer reads `operationID` from the Start
    message. The id comes from `rpc.OperationIDFromContext`, and the
    message's own field is left in place but ignored, with a comment
    saying why: restoring the assignment looks like removing dead code
    and would reopen the gap
  - The connection stays plaintext. Encryption was briefly part of Slice
    25 and was split into Slice 38 (`runner-server-tls`); every change
    above is therefore staged rather than a cutover, and a new Runner
    against an old Server is a no-op
  - _Requirements: (Slice 25 amendment; see runner-authentication)_

## Notes

- `k8s.io/api`, `k8s.io/apimachinery`, and `k8s.io/client-go` are already
  direct dependencies (Slice 0 scaffolding); `k8s.io/utils/ptr` is
  currently indirect and needs promoting to direct for task 4.3's
  `ptr.To` usage — `go mod tidy` handles this once it's imported.
- No new external dependency is needed for backoff/retry logic (task
  11.1) or for gRPC test scaffolding (task 15.3/15.7's `bufconn` is a
  subpackage of the already-present `google.golang.org/grpc`) — see
  design.md's "Alternative considered" notes for why a hand-rolled
  backoff helper was chosen over a new dependency.
- `internal/runner/clone.go`'s subprocess seam is deliberately not shared
  with `internal/plugin/command.go`'s — see task 9.1's note.
- Coverage targets: 80% for the three new packages (no global-design
  target exists for this slice's domain), 90% for `internal/plugin`
  (unchanged from Slice 2).
- No real Kubernetes cluster, `kind` environment, or real GitHub network
  access is used in this slice's tests — `k8s.io/client-go/kubernetes/fake`,
  `bufconn`, and a real local (not remote) git repository fixture are
  sufficient, matching the global design's stated approach. Integration
  testing against a real cluster belongs to the global roadmap's Slice 11.

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1.1", "2.1"] },
    { "id": 1, "tasks": ["1.2", "1.3", "4.1"] },
    { "id": 2, "tasks": ["4.2", "6.1"] },
    { "id": 3, "tasks": ["4.3"] },
    { "id": 4, "tasks": ["8.1", "9.1"] },
    { "id": 5, "tasks": ["11.1"] },
    { "id": 6, "tasks": ["11.2"] },
    { "id": 7, "tasks": ["13.1"] },
    { "id": 8, "tasks": ["13.2"] },
    { "id": 9, "tasks": ["15.1", "15.2", "15.3", "15.4", "15.5", "15.6", "15.7", "15.8"] },
    { "id": 10, "tasks": ["17.1", "17.2"] }
  ]
}
```
