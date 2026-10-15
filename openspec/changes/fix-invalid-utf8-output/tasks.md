# Tasks

**The checkpoint that matters is task 2.1.** The bug is an encoding
failure that only exists where a real message is marshaled: unit tests
on the cleaning function can pass while some string bound for the
Server still skips it. Only the real Runner (`runner.Run`, the Helmfile
plugin running a stand-in `helmfile` that prints invalid bytes, the
real reporter) talking to a Server from `rpc.NewServer` shows that the
log lines and the result arrive on the first attempt. It runs
in-process, so it needs no cluster.

Each wave builds on the one before it, in one package, so they run one
after another.

## 1. The cleaning step (`internal/runner`)

- [x] 1.1 The one function that prepares text for the Server (design
  Decision 1): the workspace path stripped as today, then each run of
  invalid bytes replaced with one U+FFFD. Verify: a property test,
  `// Feature: fix-invalid-utf8-output, Property 1: What leaves for the
  Server is valid UTF-8`, over arbitrary byte strings and workspace
  paths (the result is valid UTF-8; valid input comes back with only
  the path stripped; each run of invalid bytes becomes one U+FFFD); unit
  tests for the delta's examples (`caf` followed by `0xE9`, three
  invalid bytes between `a` and `b`, `naïve 💡` unchanged); the existing
  workspace-path tests
  - _Deltas: operation-lifecycle (Text sent to the Server is valid UTF-8)_
- [x] 1.2 Every path uses it: `execute`'s `clean` (log lines through the
  sender and its replay, the result's output before the 1 MiB cut, its
  error message) and `RunClone`'s failure message.
  `docs/using/pull-requests.md` ("What turnip ran") says bytes that are
  not valid UTF-8 are shown as `�`, and
  `docs/operating/troubleshooting.md` ("Reading a Runner's log") that
  the Runner's log keeps them. Verify: tests that a log line, the
  result's output, its error message and a clone failure, each holding
  invalid bytes, are sent valid; that the Runner's log keeps the bytes;
  that a 3 MiB output full of invalid bytes is sent at no more than
  1 MiB and still valid; that a Helmfile `diff` whose only body line is
  invalid still counts the release; the existing Runner tests;
  `make docs-check`
  - _Deltas: operation-lifecycle (Text sent to the Server is valid UTF-8)_

## 2. Checkpoint: invalid bytes over real gRPC

- [x] 2.1 An in-process test beside the large-output checkpoint in
  `internal/runnerauth`: a stand-in `helmfile` that prints a line with
  invalid bytes, more lines after it, and exits 0, run by `runner.Run`
  against a server from `rpc.NewServer`. The Runner opens one stream;
  the log lines after the invalid one arrive; the result arrives once,
  on the first attempt, with `�` in place of the bytes; the Runner's
  own log has the bytes as printed

## 3. Final checkpoint

- [x] 3.1 `go build ./...`, `gofmt -l ./internal ./cmd` clean,
  `go vet ./...`, `make test`, `make lint` pass
- [x] 3.2 Every scenario in the delta maps to a test or a doc change;
  `openspec validate fix-invalid-utf8-output --strict` passes;
  `openspec/roadmap.md` shows the change as implemented
- [x] 3.3 An intermittent failure is recorded here with its full output,
  not rerun away

## Repository owner

- Review and commit
- Deploy a Runner image built from this change (the Server is
  unchanged)
- Confirm, if a real case comes up, that its comment shows `�` and its
  check completes; then this change is archived

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1.1"] },
    { "id": 1, "tasks": ["1.2"] },
    { "id": 2, "tasks": ["2.1"] },
    { "id": 3, "tasks": ["3.1", "3.2", "3.3"] }
  ]
}
```

## Deviations recorded at implementation

- 2026-10-14: Review fix. The large-output test in `run_test.go` used
  one run of 3 MiB of `0xFF`, which cleaning turns into a single
  U+FFFD, so the cut never ran and the test passed with the cut and the
  cleaning in either order. It now uses `"a\xff\n"` repeated to 3 MiB,
  which cleaning grows to 5 MiB, and checks that the cut ran (the
  omission line is present), that the result is at most 1 MiB, valid
  UTF-8 and free of `0xFF`. Checked to fail when `execute` cuts before
  cleaning (about 1.7 MiB sent).
- 2026-10-14: Review fix. The `create workdir` error message in
  `execute` did not pass through `cleanForServer`, although the design
  routes every string bound for the Server through it; that message
  names `TMPDIR`, which a hand-run Runner may set to a path that is not
  valid UTF-8. It is now cleaned, with a test
  (`TestExecute_WorkdirFailureMessageIsSentValid`) that fails without
  the change.
- 2026-10-14 (3.3): no intermittent failure was seen; the checkpoint
  passed 20 times under `-race`.
