# Tasks

**The checkpoint that matters is task 5.1.** Unit tests on the cut can
pass while the message still does not fit: the result message carries
more than its output (plan data, fields, protobuf framing), and the
Server's limit is only as real as the option passed to gRPC. Only the
real reporter talking to a real gRPC server built by `rpc.NewServer`
shows that a 5 MB output arrives cut and that an oversized result fails
fast instead of retrying for 15 minutes. It runs in-process, so it needs
no cluster.

Wave 0 names the limit and caps the comment; the comment's new
parameter leaves `internal/orchestrator` unbuilt until wave 1 passes
it. Wave 1 adds the Runner's cut, its size check and the setting; wave 2
the log line and the checkpoint.

## 1. The limit (`internal/rpc`)

- [x] 1.1 A constant for the largest result message (4 MiB), passed to
  the Server as `grpc.MaxRecvMsgSize` in `rpc.NewServer`; verify a test
  that a message over it is refused with `ResourceExhausted` and one
  under it is accepted

## 2. The Runner (`internal/runner`)

- [x] 2.1 The cut: after `Execute` and the path stripping, an output
  over 1 MiB keeps its leading `@@ turnip:` lines and its closing one,
  drops tool lines from the start until the rest fits, and puts
  `@@ turnip: <K> earlier lines omitted (<size>) @@` after the leading
  lines (KiB below 1 MiB, MiB from 1 MiB, one decimal); a single line
  longer than the budget keeps its own end; plan data untouched; verify
  tests for the spec's 5,069,133-byte scenario (`3.8 MiB`), an output
  under the limit (unchanged), a single oversized line, and counts taken
  from the whole output
  - _Deltas: operation-lifecycle (Output sent with a result)_
- [x] 2.2 Before sending a result, its message size is checked against
  1.1's constant; one that does not fit is never sent, and a failed
  result with no output, no plan data and no category, whose error
  message is the spec's, is delivered instead; a `ResourceExhausted`
  answer to a result is final, never retried; verify reporter tests for
  both paths, asserting a single attempt
  - _Deltas: operation-lifecycle (Retry budgets)_
- [x] 2.3 The result log line is the spec's summary, with
  `runner: operation error: <message>` before it when the result has an
  error message, and never the output; `docs/operating/troubleshooting.md`
  describes reading a Runner's log (the output once, then the summary);
  verify a test on the logged lines for a large output and for a failed
  result with an error message, and `make docs-check`
  - _Deltas: observability (Runner logs)_

## 3. The comment (`internal/github`)

- [x] 3.1 `BuildConsolidatedComment` takes the most parts per Project;
  `splitDetailSection` cuts the start of an output that would need more,
  after its leading `@@ turnip:` lines (the Runner's omission line among
  them), counting the lines removed, and puts `…(<K> earlier lines
  omitted)` after them; `docs/using/pull-requests.md` says how a long
  output is shortened, and `docs/using/troubleshooting.md` explains the
  two omission notes; verify tests for 80 parts capped to 3, a cap of 5,
  the marker's count, the Runner's line surviving, the existing
  property tests, and `make docs-check`
  - _Deltas: pr-comment (Size limits, splitting and dropping)_

## 4. The setting (`internal/orchestrator`, `cmd/server`)

- [x] 4.1 `ConfigFromEnv` reads `TURNIP_MAX_COMMENTS_PER_PROJECT`
  (default 3; a value that is not a positive integer is a startup error
  naming the setting and the value), and the orchestrator passes it to
  `BuildConsolidatedComment`; `docs/operating/server-settings.md` gets
  its Reference row; verify config tests (unset, `5`, `0`, `x`),
  `TestServerSettingsReference_MatchesConfigFromEnv`, and
  `go build ./...`
  - _Deltas: pr-comment (The TURNIP_MAX_COMMENTS_PER_PROJECT setting),
    server (Settings index)_

## 5. Checkpoint: a large result over real gRPC

- [x] 5.1 An in-process test: a server from `rpc.NewServer` and the
  Runner's real reporter. A result with 5,069,133 bytes of output
  arrives with at most 1 MiB of output and the omission line, on the
  first attempt. A result whose plan data alone exceeds the limit is
  never sent; the "too large to deliver" result arrives instead, in one
  attempt, well inside the retry budget

## 6. Final checkpoint

- [x] 6.1 `go build ./...`, `gofmt -l ./internal ./cmd` clean,
  `go vet ./...`, `make test`, `make lint` pass
- [x] 6.2 Every scenario in the four deltas maps to a test or a doc
  change; `openspec validate fix-large-output --strict` passes
- [x] 6.3 An intermittent failure is recorded with full output, not
  rerun away

## 7. Follow-up: the default cap

- [x] 7.1 Raise `TURNIP_MAX_COMMENTS_PER_PROJECT`'s default from 3 to 5
  (2026-10-09: three-part outputs turned out to be common); the proposal,
  the `pr-comment` delta, the design, the Reference row and both user
  pages say 5; verify the config test (unset gives 5, `8` gives 8),
  `make docs-check`, and `openspec validate fix-large-output --strict`

## Repository owner

- Review and commit
- Archive `redis-tls-auth` before this change (both rewrite the Settings
  index; this delta is written on `redis-tls-auth`'s version), which
  needs its ElastiCache confirmation
- Deploy; a first-install plan delivers its result, its check completes,
  and its section takes at most 5 comments; then this change is archived

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1.1", "3.1"] },
    { "id": 1, "tasks": ["2.1", "2.2", "4.1"] },
    { "id": 2, "tasks": ["2.3", "5.1"] },
    { "id": 3, "tasks": ["6.1", "6.2", "6.3"] }
  ]
}
```

## Deviations recorded at implementation

- Judgment calls in the Runner's cut: the leading and closing annotation
  lines are kept only while they, with the omission line, take at most
  half the budget (a real transcript never comes close); when a single
  oversized line is cut inside, `<K>` counts only whole lines, so the
  marker can read `0 earlier lines omitted`; the "too large to deliver"
  result keeps the tool's exit code with zeroed counts; `category` uses
  the wire enum's names (`tool_exited`, `clone_failed`, ...), `none`
  when there is none.
- Review fixes: the comment cap dropped all of the tool's output when
  its cut landed inside an oversized last line; it now keeps that line's
  end, cut at a rune boundary, as the Runner does
  (`TestSplitDetailSection_OversizedLastLineKeepsItsEnd`). A size just
  under 1 MiB read `1024.0 KiB`; the unit now follows the rounded value.
  A result a few bytes over the limit read `4.0 MiB; the Server accepts
  at most 4 MiB`; it now gives the exact byte count when rounding would
  read as the limit. Clone failures now log the error and summary lines
  too. The docs promised more of the Runner's log than it keeps (log
  rotation, the Job's 15-minute TTL) and misplaced the omission line;
  both corrected.
- Accepted: `MutationChangesReported` runs on the Server against the cut
  output, not the whole transcript; its comment now says so, and the
  banners it looks for print at the end of an apply, which the cut keeps.
  When a Server configured with a lower limit refuses a result that
  passed the Runner's check, the error message still names 4 MiB and the
  summary line already logged describes the original result; both sides
  share one constant, so this is a defensive path only.
- Found by the review and not fixed here, since it predates this change:
  output or a log line containing invalid UTF-8 cannot be marshaled, so
  the Runner retries for its whole budget and the check stays in
  progress. To be proposed as its own `fix-*` change.
