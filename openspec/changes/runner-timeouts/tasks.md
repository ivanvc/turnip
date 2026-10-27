# Tasks

**The checkpoint that matters is task 6.1.** The unit tests fake the
Job client, so they show the sweep's reading of a status they invented.
Only a real Job built by `BuildJob` and stopped by Kubernetes at its
deadline shows the status the sweep will actually read: the `Failed`
condition, its `DeadlineExceeded` reason, the grace period honored.

After wave 2, a review on the main model reads the whole change against
its deltas and design, and a fix pass applies what it finds, before the
checkpoint.

## 1. The wire format

- [x] 1.1 `proto/turnip/v1/operation.proto`: `FailureCategory` gains
  `FAILURE_CATEGORY_TIMED_OUT` and `FAILURE_CATEGORY_STOPPED`
  (`make proto-gen` regenerates `internal/grpc`); `internal/rpc/failure.go`
  maps both; `internal/orchestrator/titles.go` titles them
  `<tool> stopped: no output for <idle timeout>` and
  `<tool> stopped: the Runner was told to stop`, the idle timeout
  carried in the result's error message rather than a new field.
  Verify: the existing category and title tests extended to both; `buf
  generate` leaves no diff after the regeneration
  - _Deltas: operation-lifecycle (Failure categories), project-checks
    (Titles on completion)_

## 2. Stopping the tool (`internal/plugin`)

- [x] 2.1 `execCommand` as design Decisions 1 and 2 state: the tool in
  its own process group; `ExecuteOptions.IdleTimeout` drives a watchdog
  fed only by the tool's lines; a stop sends `SIGINT` to the group and,
  30 seconds later if still running, `SIGKILL` to the group (not
  `WaitDelay` alone); the cause sets `ExecuteResult.Stopped` and the
  trailer (`stopped: no output for <idle>, after <duration>` or
  `stopped: the Runner was told to stop, after <duration>`). The
  Helmfile Plugin passes `IdleTimeout` through and copies `Stopped` back.
  The grace is a package value tests can shorten. Verify: scripted-tool
  tests for silence stopping it, steady output never stopping it,
  annotation lines not resetting the timer, a tool exiting on `SIGINT`
  keeping its exit code, a tool trapping `SIGINT` killed after the
  grace, and a child process of the tool receiving the interrupt
  - _Deltas: operation-lifecycle (Idle timeout, Stopping the tool),
    plugins (Execution transcript and interleaved output)_

## 3. The Runner (`internal/runner`, `cmd/runner`)

- [x] 3.1 The Runner reads `TURNIP_IDLE_TIMEOUT` (unset or empty means
  30 minutes) into `ExecuteOptions.IdleTimeout`; turns `SIGTERM` into a
  cancellation of the tool's context only, with the cause "stop
  requested"; reports on a context detached from it and bounded at 20
  seconds (design Decision 3); and maps `Stopped` to the timed out or
  stopped category with the delta's error messages. Verify: tests with
  an injected stop: the tool is interrupted and the result is reported
  with category stopped and its message after the tool's context was
  cancelled; a silent tool reports category timed out with
  `The tool wrote nothing for <idle>, so turnip stopped it.`; a
  malformed `TURNIP_IDLE_TIMEOUT` fails the Runner's config
  - _Deltas: operation-lifecycle (Idle timeout, Stopping the tool,
    Failure categories), runner-job (Runner environment)_

## 4. The Server (`internal/orchestrator`, `internal/jobs`)

- [x] 4.1 Settings and the Job: `TURNIP_RUNNER_IDLE_TIMEOUT` and
  `TURNIP_RUNNER_DEADLINE` parsed as the `server` delta states (defaults,
  positive, the deadline longer than the idle timeout, errors naming the
  setting), added to `SettingNames()` and to
  `docs/operating/server-settings.md`; `BuildJob` sets
  `activeDeadlineSeconds`, `terminationGracePeriodSeconds: 60`, and
  `TURNIP_IDLE_TIMEOUT` on the `runner` container only. Verify:
  `ConfigFromEnv` tests for the three `server` scenarios; `BuildJob`
  tests for the three values; the settings Reference test
  - _Deltas: server (Settings loader, Settings index), runner-job (No
    retries and a bounded afterlife, Runner environment)_
- [x] 4.2 The sweep as design Decision 4 states: `jobs.JobStatus` gains
  whether the Job finished, its `Failed` condition's reason, the first
  terminated container and its reason (init containers first), and
  whether the Pod was evicted; `ClaimForStopped` is a compare-and-set
  claim like the result's; for a started record the timeout claim
  passed over, the sweep reads its Job and, once ended or gone, claims
  and finalizes it as stopped (the check with the stopped summary and
  the delta's diagnostics, the plan or mutating timeout event, the
  outcome, the trigger), before the abandon claim. Verify: tests with
  the Job client faked for each diagnostic row; a started Operation
  whose Job still runs left alone; on miniredis, the stopped claim
  losing to a result's claim and to another replica's
  - _Deltas: operation-lifecycle (Start timeout and sweep, Exactly-once
    finalization), project-checks (Titles on completion)_

## 5. Documentation

- [x] 5.1 `docs/operating/troubleshooting.md` and
  `docs/using/troubleshooting.md`: a tool stopped for silence (raising
  `TURNIP_RUNNER_IDLE_TIMEOUT`), a Runner told to stop, a Runner that
  stopped without reporting (each check title and what to look at);
  `docs/plugins/helmfile.md` notes that a `--wait` silent for longer
  than the idle timeout is stopped. Verify: `make docs-check`
  - _Deltas: operation-lifecycle (Idle timeout, Stopping the tool,
    Start timeout and sweep)_

## 6. Checkpoint: a real cluster

- [x] 6.1 A kind test (`test/load`): a Job built by `BuildJob` with a
  deadline of a few seconds and a container that sleeps ends `Failed`
  with reason `DeadlineExceeded`, its Pod given the 60-second grace, and
  `jobs.Status` plus the sweep's diagnostic read it as
  `Job <name>: stopped at its deadline of <deadline>`.
  `make test-kind` passes with every check run and none skipped
  (`GOFLAGS="-v -count=1"`)

## 7. Final checkpoint

- [x] 7.1 `go build ./...`, `gofmt -l ./internal ./cmd ./test` clean,
  `go vet ./...`, `make test`, `make lint`, and `buf generate` leaves no
  diff
- [x] 7.2 Every scenario in the deltas maps to a test or a doc change;
  `openspec validate runner-timeouts --strict` passes;
  `openspec/roadmap.md` shows the change as implemented
- [x] 7.3 An intermittent failure is recorded here with its full output,
  not rerun away

## Repository owner

- Review and commit, including the regenerated `internal/grpc` code
- Archive `fix-rendered-capitalization` and `fix-single-config-surface`
  before this change: its deltas are written on their versions of
  "Start timeout and sweep" and "Settings index"
- Deploy the Server and the Runner image from the same commit: the
  Server sends `TURNIP_IDLE_TIMEOUT` and reads the two new categories
- Optionally set `TURNIP_RUNNER_IDLE_TIMEOUT` and
  `TURNIP_RUNNER_DEADLINE`; the defaults are 30 minutes and 4 hours.
  Then archive this change

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "name": "Wire, settings and docs", "model": "Sonnet", "effort": "medium", "tasks": ["1.1", "4.1", "5.1"] },
    { "id": 1, "name": "Stopping the tool and the sweep", "model": "Opus", "effort": "medium", "tasks": ["2.1", "4.2"] },
    { "id": 2, "name": "The Runner", "model": "Opus", "effort": "medium", "tasks": ["3.1"] },
    { "id": 3, "name": "Real cluster", "model": "Sonnet", "effort": "low", "tasks": ["6.1"] },
    { "id": 4, "name": "Final checkpoint", "model": "Opus", "effort": "medium", "tasks": ["7.1", "7.2", "7.3"] }
  ]
}
```

## Deviations recorded at implementation

- 2026-10-24: Task 1.1. The title of a timed-out run reads the idle
  timeout from the Runner's error message, so `failedTitle` gained the
  message as a parameter; it falls back to `<tool> stopped: no output`
  if the message does not match. Enum values: `TIMED_OUT` 5,
  `STOPPED` 6.
- 2026-10-24: Task 4.1. The Server hands the timeouts to the Job builder
  through `Orchestrator.WithRunnerTimeouts`, so `New`'s signature (and
  its callers in tests) stays; `BuildJob` falls back to 30 minutes and
  4 hours when given zero.
- 2026-10-24: Task 2.1. The internal `commandRunner` signature takes the
  idle timeout and returns the stop reason. Any cancellation of the
  caller's context counts as a requested stop. The stopped trailer
  rounds to the second (the watchdog's resolution). The watchdog ticks
  every second, faster for timeouts under 10 seconds, so tests can use
  short ones.
- 2026-10-24: Task 4.2. The diagnostic order in the delta was wrong for
  the case that matters most: at the deadline the Job controller
  deletes the Pod before it marks the Job `Failed`, so "Pod gone" ahead
  of "deadline" would have hidden the deadline. The order is now: Job
  gone or unreadable, deadline (the `Failed` or `FailureTarget`
  condition), Pod gone, terminated container, evicted, otherwise; the
  delta and design were updated. A Job whose status read fails is
  retried next tick rather than finalized; a started record with no Job
  name is left for abandonment; "terminated" means a non-zero exit, so
  the clone init container's `Completed` is never the diagnostic.
  `JobStatus` also carries the Job's actual deadline, so the diagnostic
  names it.
- 2026-10-24: Task 3.1. `Run` keeps its signature and calls a new
  `RunWithStop`; only the tool-running mode catches `SIGTERM`. The
  20-second report bound applies only after a stop, so normal runs keep
  the reporter's full retry budget. A zero or negative
  `TURNIP_IDLE_TIMEOUT` is rejected, since it would silently disable
  the timeout.
- 2026-10-24: Review fixes. (1) A tool exiting on its own as the idle
  timer fired was reported as stopped; a run is now marked stopped only
  when the interrupt reached a live process group. (2) The diagnostic
  order, as above. (3) The sweep could claim a Job marked `Failed`
  while its Pod was still in its grace period, losing the Runner's own
  stopped result; a found Job now counts as ended only once its Pod is
  neither Pending nor Running (the delta says so). (4)
  `TURNIP_IDLE_TIMEOUT` was set after `runner.env`; it now comes before,
  so a Project's variables are last. (5) Two troubleshooting entries
  corrected: no Server setting controls Runner memory, and the end-user
  page now covers the stopped-Runner titles.
- 2026-10-24: Task 6.1. The kind test checks the `jobs.Status` fields
  the deadline diagnostic reads and the diagnostic's format, without
  calling the unexported `stoppedDiagnostic` itself. It strips the Job
  to a sleeping container so it needs no repository or Server, and
  takes about 66 seconds because `sleep` ignores `SIGTERM` and uses the
  full 60-second grace.
