# Tasks

**The checkpoint that matters is task 6.1.** Every piece can pass its
own tests while the fault this change fixes is still there: the order is
lost between pieces, when something downstream of the record makes a
reader wait. Only a real process, the real capture, the Runner's real
mirror and reporter, and a Server from `rpc.NewServer` that reads slowly
show that 20,000 lines of standard output followed by 5 of standard
error are recorded, logged and delivered in that order. It runs
in-process, so it needs no cluster.

Wave 0 adds the record and the Server's stream loop, which nothing else
depends on yet. Wave 1 moves the capture onto the record and replaces
`ExecuteOptions.OnOutput`, which leaves `internal/runner` unbuilt until
wave 2. In wave 2, tasks 4.1 and 4.2 touch different files of
`internal/runner` and the package builds only with both, so they are
verified together at the end of the wave.

The tests here are concurrent; all of them run under `-race`.

## 1. The record (`internal/plugin`)

- [x] 1.1 `Record`, `Line` and `Stream` (`Stdout`, `Stderr`, `Turnip`)
  as the design's Decision 1 gives them: append-only, closed once, `Next`
  waiting for lines after a position, and the two compositions of
  Decision 4, `Text` and `Select`, with the note placed right after the
  leading `Turnip` lines; verify unit tests (a reader waiting on an open
  record is woken by an append and by the close; a reader behind gets
  every line; `Next` returns when its context ends; `Select` with a note
  that returns no text adds no line) and a property test, `// Feature:
  fix-output-order, Property 1: Select keeps turnip's lines and counts
  what it omits`, over arbitrary records
  - _Deltas: plugins (Output chosen by the tool)_

## 2. The capture (`internal/plugin`)

- [x] 2.1 `execCommand` records through one reader over both of the
  tool's pipes, as the design's Decision 2 gives it (pipes the capture
  creates and shrinks to 4 KiB on Linux; what a pipe holds taken in one
  read, the stream taken last round first when both have data; a line
  is recorded when its newline is read, an unterminated last line when
  the pipe closes; the 1 MiB limit on a line fails the capture as
  today);
  turnip's annotation lines are recorded as `Turnip`, the leading two
  before any line of the tool and the closing one after both streams
  end, and then the record is closed; `ExecuteOptions.Record` replaces
  `OnOutput`, and the capture makes its own record when none is passed;
  a command that could not be started leaves the record empty.
  `HelmfilePlugin.Execute` is adapted only as far as building needs
  (counts from the record's `Stdout` or `Stderr` lines, output from
  `Text`). `docs/using/pull-requests.md` states the order as the spec
  does, and "Adding a tool" in `docs/development/README.md` says a
  plugin chooses its output with `Text` or `Select`. Verify: a real
  process writing 20,000 lines to standard output and then 5 to
  standard error is recorded with every one of the 20,000 first
  (replacing `TestExecCommand_RecordInterleavesStreamsInArrivalOrder`);
  the warning-between-two-lines scenario; a property test, `Property 2:
  Each stream is recorded in order whatever the chunking`, over two byte
  streams delivered in arbitrary chunks; the existing transcript tests;
  a real process writing 20,000 lines in one go and then an error, in
  order in each of 50 runs; `go test ./internal/plugin/...` and
  `make docs-check`
  - _Deltas: plugins (Execution transcript and interleaved output)_

## 3. Helmfile (`internal/plugin`)

- [x] 3.1 `--no-color` is Helmfile's first argument, before
  `--environment`. A successful operation returns `Text`; a failed one
  returns `Select(Stderr, ...)` with the note `<N> lines of standard
  output omitted because the operation failed` (`1 line` for one, no
  note for none). Counts are still read from the record, so a failed
  `diff` reports the releases it changed. `docs/plugins/helmfile.md`
  says what a failed operation's output shows and where the omitted
  diffs are, that color is always off and `--color` is refused by
  Helmfile, and that `--detailed-exitcode` makes a plan with changes
  read as failed; `docs/using/pull-requests.md` updates its example
  command line and says a failed Helmfile operation shows its errors
  alone; `docs/using/troubleshooting.md` no longer says a failed tool
  command's full output is in the comment; the Helmfile section of
  `docs/development/README.md` follows. Verify: tests for every
  scenario of "Helmfile output" and "Helmfile command line" (the four
  command lines, a failed `diff` with 30,000 lines omitted, a failure
  with nothing on standard output, a failed `apply` keeping its tables,
  a failed `diff` still counting 2 changed), the updated annotation
  scenario of the `plugins` delta, the existing count and
  `MutationChangesReported` tests, and `make docs-check`
  - _Deltas: plugin-helmfile (Helmfile output, Helmfile command line,
    Helmfile operations, Unreported mutation counts), plugins (Output
    chosen by the tool)_

## 4. The Runner (`internal/runner`)

- [x] 4.1 `run.go`: `execute` creates the record, starts the mirror and
  the sender on it, and passes it to `Execute`; the mirror writes
  `Stdout` and `Turnip` lines to the Runner's standard output and
  `Stderr` lines to its standard error, unstripped; the record is closed
  when `Execute` returns without having run a command, so both readers
  end; the mirror is waited for before the result summary is logged;
  `resultReporter` loses `LogLine` and gains the method that starts the
  sender, which is given the workspace-path stripping. Verify (with
  4.2): the summary line follows the last line of output in the
  Runner's log; the mirror keeps absolute paths; a tool that cannot be
  started still logs its error and summary; the existing run tests
  - _Deltas: operation-lifecycle (Runner-to-Server stream contract)_
- [x] 4.2 `reporter.go`: the sender follows the record with `Next` and
  sends each line once, with the line's own time as its timestamp and
  `info` for `Stdout` and `Turnip`, `error` for `Stderr`; it stops at
  the first failed send and does not reconnect. `Report` waits until
  the sender has sent the last line or the stream has broken: on a
  stream that never broke it sends the result and replays nothing; after
  a break it opens a resumed stream and replays from the record the
  lines whose sizes sum to at most 256 KiB, counted back from the end,
  preceded by `<N> lines dropped` at `warn` when the record has more,
  again on every retry. The ring buffer is removed. Verify (with 4.1):
  a property test, `Property 3: An unbroken stream carries the record
  once, in order`, over arbitrary records and send timings; tests for a
  stream that blocks (the record still closes, and every line is sent
  once it unblocks), a break mid-run (replay with the `lines dropped`
  line, then the result), more than 256 KiB with no break (no `lines
  dropped` line), a failed `diff` whose stream still carries its
  standard output, and the existing retry and too-large tests
  - _Deltas: operation-lifecycle (Runner-to-Server stream contract, Log
    buffering)_

## 5. The Server (`internal/rpc`)

- [x] 5.1 The stream loop calls `HandleLog` for a stream's first log
  line and skips it for the rest; `HandleLog`, its argument and the
  mark-started script are unchanged, and the comment on `HandleLog`
  that reserves the per-line path for Slice 26 is updated to say where
  the lines now stop. Verify: a stream of 20,000 log lines calls the
  handler once; a second stream for the same Operation calls it once
  more; an error from that call still ends the stream; the existing
  start-latency and stream tests
  - _Deltas: operation-lifecycle (Runner-to-Server stream contract)_

## 6. Checkpoint: order through a slow Server

- [x] 6.1 An in-process test: a real process (20,000 lines to standard
  output, then 5 to standard error, exit 1) run by the Helmfile plugin's
  capture through `runner.execute` with the real reporter, against a
  server from `rpc.NewServer` whose handler delays each stream's first
  line and whose connection is read slowly. The record has every
  standard output line before the first standard error line; the
  Runner's log has the same lines; the Server received every line in
  the record's order before the result; the handler was called once for
  log lines; and the result's output is the annotation lines, the
  omission line for 20,000 lines and the 5 error lines

## 7. Final checkpoint

- [x] 7.1 `go build ./...`, `gofmt -l ./internal ./cmd` clean,
  `go vet ./...`, `make test`, `make lint` pass
- [x] 7.2 Every scenario in the three deltas maps to a test or a doc
  change; `openspec validate fix-output-order --strict` passes;
  `openspec/roadmap.md` shows the change as implemented
- [x] 7.3 An intermittent failure is recorded here with its full
  output, not rerun away; the order tests are run 20 times under
  `-race` (`go test -race -count=20`) before this is ticked

## Repository owner

- Review and commit
- Deploy a Runner image and a Server built from this change (either
  order; the wire format is unchanged)
- A plan that fails for some releases shows their errors alone, with the
  omission line; a successful `apply` shows its release tables without
  color codes; then this change is archived

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1.1", "5.1"] },
    { "id": 1, "tasks": ["2.1"] },
    { "id": 2, "tasks": ["3.1", "4.1", "4.2"] },
    { "id": 3, "tasks": ["6.1"] },
    { "id": 4, "tasks": ["7.1", "7.2", "7.3"] }
  ]
}
```

## Deviations recorded at implementation

- 2026-10-12, task 2.1: the capture first designed (two readers, each
  appending as it reads) was built and measured, and did not put a
  burst and what follows it in order (60 of 60 runs wrong, up to 9,352
  lines out), which is how Helmfile writes. The proposal, the `plugins`
  delta's ordering wording and scenarios, and the design's Decision 2
  were revised to one reader over both pipes on 4 KiB pipes, and the
  design gained Decision 8 (a plugin chooses its capture; only the
  separate one is built here). Task 2.1's text was updated to match.
  The warning-between-two-lines test paces its writes, since lines
  written to both streams with no pause can land either way in any
  capture that keeps the streams apart.
- 2026-10-12, task 4.2: a line longer than 1 MiB still fails the
  capture, but the reader now keeps reading that stream to its end
  without recording it, so the tool cannot block on a pipe nobody
  empties (before, the reader of that stream stopped).
- 2026-10-12, task 6.1: `rpc.NewServer` hands its handler only a
  stream's first log line, so it cannot show the order of the rest. The
  checkpoint is two tests in
  `internal/runnerauth/output_order_checkpoint_test.go`: the order of
  every line and the result, through a Server of the test's own that
  keeps every message and reads slowly; and the real Server with a slow
  handler, which is asked about one log line of 20,005. The existing
  large-output checkpoint's expected command line gained `--no-color`.
- 2026-10-12, task 7.3, an intermittent failure, recorded in full. Under
  `go test -race -count=20 ./internal/plugin/ -run
  'TestExecCommand_|TestRecord_|TestProperty_'`, 2 of 8 batches (each
  1,000 runs of the burst case) failed like this:

  ```
  --- FAIL: TestExecCommand_ErrorsAfterABurstFollowIt (0.66s)
      command_test.go:121:
          Error Trace:  internal/plugin/command_test.go:121
          Error:        Not equal:
                        expected: plugin.Line{Stream:1, Text:"before", At:time.Date(1, time.January, 1, 0, 0, 0, 0, time.UTC)}
                        actual  : plugin.Line{Stream:0, Text:"1", At:time.Date(1, time.January, 1, 0, 0, 0, 0, time.UTC)}
          Test:         TestExecCommand_ErrorsAfterABurstFollowIt
  FAIL
  FAIL  github.com/ivanvc/turnip/internal/plugin  58.300s
  ```

  The tool's first line, on standard error, was recorded after the first
  line of the burst that followed it. Cause: the reader started only
  after the process had been launched, so under load both pipes held
  data before its first look, and nothing said which was written first.
  Fixed in the capture: the reader is waiting on the pipes before the
  process starts, and holds what it reads until the transcript's header
  is in the record. After the fix, 8 of 8 batches passed. The test also
  gained a 20 ms pause between an error and the burst after it, since
  that boundary depends on timing in any capture that keeps the streams
  apart; an error right after a burst, the case the reader's rule
  decides, is still written with no pause.
