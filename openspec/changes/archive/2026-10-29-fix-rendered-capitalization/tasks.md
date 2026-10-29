# Tasks

**The checkpoint that matters is task 4.1:** the guard of task 2.1 run
over the whole tree once every string is changed. Task 2.1 proves the
guard catches a bad literal in a fixture; only the full run proves the
tree has none left, and that the one allowlist entry is all it needs.

## 1. The strings

- [x] 1.1 Lowercase the nouns in every string of the proposal's table,
  exactly as its "After" column reads: `internal/github/comment.go`,
  `internal/orchestrator/pullrequest.go`, `execute.go`, `lockstate.go`
  (all four "This Project is free" lines, the fourth not quoted by any
  spec, and all three "The Lock is still held" lines), `sweep.go`, and
  `internal/metrics/metrics.go`. The tests asserting a changed string
  follow (`repository_secrets_test.go` among them). Verify:
  `go test ./internal/...`; `git grep` finds none of the "Today"
  strings in `internal/`
  - _Deltas: pr-comment (Footer), locking (What releases a Lock, Results
    report what happened to the Lock), project-checks (Check name and
    lifecycle, Check failures are reported, not fatal),
    operation-lifecycle (Start timeout and sweep)_

## 2. The guard

- [x] 2.1 `test/style/capitalization_test.go` as design Decision 1
  describes: it parses every non-test Go file under `internal/` and
  `cmd/`, skips `slog` arguments and the allowlist (one entry:
  `retryTrigger`'s `"completing a Project"` in `carry.go`, with its
  reason), and fails naming file, line and literal for a mid-sentence
  capitalized lock, project, operation or plan (singular, plural or
  possessive). Verify: a table test of the matcher (mid-sentence
  capital matches; sentence-initial "Lock released:" does not;
  "Server", "Runner", `Job`, `Pod`, `Secret` do not); a fixture under
  `test/style/testdata` (not compiled) with one bad literal and one
  `slog` message, which the scan reports and skips respectively
  - _Deltas: none; the rule is `CLAUDE.md`'s, guarded per design
    Decision 1_

## 3. Documentation

- [x] 3.1 `docs/using/troubleshooting.md` and
  `docs/operating/troubleshooting.md` quote the changed strings in
  their new wording. Verify: `make docs-check`; `git grep` finds no
  "Today" string in `docs/`
  - _Deltas: project-checks (Check failures are reported, not fatal),
    operation-lifecycle (Start timeout and sweep)_

## 4. Checkpoint: the whole tree

- [x] 4.1 `go test ./test/style/...` passes on the changed tree, with no
  allowlist entry beyond `carry.go`'s

## 5. Final checkpoint

- [x] 5.1 `go build ./...`, `gofmt -l ./internal ./cmd ./test` clean,
  `go vet ./...`, `make test`, `make lint` pass
- [x] 5.2 Every scenario in the deltas maps to a test or a doc change;
  `openspec validate fix-rendered-capitalization --strict` passes;
  `openspec/roadmap.md` shows the change as implemented
- [x] 5.3 An intermittent failure is recorded here with its full output,
  not rerun away

## Repository owner

- Review and commit
- Deploy a Server built from this change (the Runner is unchanged); the
  next close comment and lock-transition lines read in lowercase. Then
  archive this change

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "name": "Strings, guard and docs", "model": "Sonnet", "effort": "medium", "tasks": ["1.1", "2.1", "3.1"] },
    { "id": 1, "name": "Whole tree", "model": "Sonnet", "effort": "low", "tasks": ["4.1"] },
    { "id": 2, "name": "Final checkpoint", "model": "Opus", "effort": "medium", "tasks": ["5.1", "5.2", "5.3"] }
  ]
}
```

## Deviations recorded at implementation

- 2026-10-23: Task 2.1. The fixture is `testdata/bad.go.txt`, which
  the scan parses and Go never compiles. The matcher also treats a
  capital after a hyphen as mid-sentence, so "Webhook-to-Operation"
  counts, slightly wider than design Decision 1's wording.
- 2026-10-23: Review fix. The guard skipped any call named like a log
  method (`.Error(`, `.Info(`) whatever its receiver, so a capitalized
  `status.Error` or `http.Error` message would have passed. It now
  skips only the eight `slog` methods the design names, called on
  `slog`, `log` or `logger`, the receivers the code uses; the fixture
  gained `status.Error` and `http.Error` lines the scan must report.
- 2026-10-23: Final checkpoint. A capitalized literal added to a
  scratch file in `internal/orchestrator` made the whole-tree test fail
  naming its file and line; the scratch file was removed. Go comments
  keeping the capitals (`lock.go`, `metrics.go`) are not rendered text
  and were left.
