# Tasks

**The checkpoint that matters is task 3.1.** The count is made on the
Runner, "not reported" is decided on the Server, and the Lock moves on
the Server from both; each half can be right while the pull request
still reads "no changes". Only the real Helmfile plugin, running a
stand-in `helmfile` that prints what v1.8.1 prints for a removal, with
its result taken through `HandleResult`, shows the comment reading
`-1` (and `succeeded` for a quiet plan) and the Lock kept. It runs
in-process, so it needs no cluster.

The waves run one after another: 1.1 and 1.2 share the parser's use,
2.1 renames a method 1.2's file implements, and 2.2 edits the file 2.1
changes on the Server.

## 1. Counting removals (`internal/plugin`)

- [x] 1.1 A parser in `helmfile_parse.go` for Helmfile's
  `Affected releases are:` summaries on standard error (design Decision
  1): each `  <name> (<chart>) DELETED` line counts one, summaries are
  summed, `UPDATED` lines count nothing, a summary ends at the first
  line of neither form, color codes are stripped first. Verify: tests
  for one removal, two, two summaries, `UPDATED` lines, a summary
  followed by other log lines, color codes, and no summary at all
  - _Deltas: plugin-helmfile (Change counts for diff)_
- [x] 1.2 A `diff`'s `Destroy` is that parser's count over the record's
  standard error, beside the changed count from standard output.
  `docs/plugins/helmfile.md` ("Change counts") says a `diff` reports a
  release it will remove as `-`, read from Helmfile's summary, and the
  Helmfile section of `docs/development/README.md` says the same.
  Verify: `Execute` tests for every scenario of the delta's "Change
  counts for diff" (the corrected "Pending removal", removals with
  changes, two helmfiles, `UPDATED` lines, the summary's text on
  standard output not counted); the existing count tests;
  `make docs-check`
  - _Deltas: plugin-helmfile (Change counts for diff)_

## 2. Counts not reported, for a plan (`internal/plugin`, `internal/orchestrator`)

- [x] 2.1 `Plugin.MutationChangesReported(output)` becomes
  `ChangesReported(operation, output)` (design Decision 2), and
  `HandleResult` asks it for every successful operation. Helmfile
  answers as before for `apply` and `sync`, and false for a `diff`
  whose command line sets the log level above info. Every fake plugin
  in the tests implements the new method. `docs/plugins/helmfile.md`
  says a quiet plan reads `succeeded`, and warns against changing
  Helmfile's log level (flags, or `HELMFILE_LOG_LEVEL` and
  `HELMFILE_QUIET` in the image) because counts may then be inaccurate;
  `docs/development/README.md` names the new method. Verify:
  `ChangesReported("diff", ...)` is false for `-q`, `--quiet` and
  `--log-level warn`, true for `--debug -q` and for no flag;
  `HandleResult` marks a quiet plan `Unreported` and its comment and
  check read succeeded; the existing unreported tests, renamed; no
  reference to `MutationChangesReported` remains; `go build ./...`;
  `make docs-check`
  - _Deltas: plugin-helmfile (Unreported mutation counts), plugins
    (Unreported mutation counts)_
- [x] 2.2 `lockEventFor` keeps a plan whose counts were not reported
  as applicable (design Decision 3). Verify: a test with a tool that
  does not act without changes, a successful plan with zero counts and
  `Unreported`, keeps its Lock with the plan recorded; the same plan
  with its counts reported releases it, as today; the existing Lock
  tests
  - _Deltas: plugins (Unreported mutation counts)_

## 3. Checkpoint: a removal through to the comment

- [x] 3.1 An orchestrator test using the real Helmfile plugin with a
  stand-in `helmfile` script on `PATH` that prints diffs for one release
  on standard output and, on standard error, `Affected releases are:`
  with one `DELETED` release, exactly as v1.8.1 prints them. Its result,
  taken through `HandleResult`, renders `+0 ~1 -1` in the comment and
  the check, and keeps the Lock with its plan. The same script run with
  `-q` renders `succeeded`, never `no changes`, and keeps the Lock

## 4. Final checkpoint

- [x] 4.1 `go build ./...`, `gofmt -l ./internal ./cmd` clean,
  `go vet ./...`, `make test`, `make lint` pass
- [x] 4.2 Every scenario in the deltas maps to a test or a doc change;
  `openspec validate fix-helmfile-diff-removals --strict` passes;
  `openspec/roadmap.md` shows the change as implemented
- [x] 4.3 An intermittent failure is recorded here with its full output,
  not rerun away

## Repository owner

- Review and commit
- Deploy a Server and a Runner image built from this change (the count
  is made on the Runner, the rest on the Server)
- The next plan that removes a release reads `-1`; then this change is
  archived

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1.1"] },
    { "id": 1, "tasks": ["1.2"] },
    { "id": 2, "tasks": ["2.1"] },
    { "id": 3, "tasks": ["2.2"] },
    { "id": 4, "tasks": ["3.1"] },
    { "id": 5, "tasks": ["4.1", "4.2", "4.3"] }
  ]
}
```

## Deviations recorded at implementation

- 2026-10-17, 2.1: the existing orchestrator subtest "plan from the
  same plugin still renders its counts" asserted the old rule (a plan is
  never asked). It now asserts the new one, a plan from a Plugin that
  answers false renders `succeeded`, rather than being kept and renamed
  only. Helmfile's `ChangesReported` answers true for an operation other
  than `diff`, `apply` or `sync` (none exists today), and does not look
  for the older banners in a `diff`, since they are an apply's.
- 2026-10-17, fix pass (review): a quiet plan kept in the Lock lost its
  unreported flag, so the apply replaying it was titled
  `running the recorded plan, no changes`. The flag is now recorded with
  the plan (`lock.PlanRecord.Unreported`, stored as `plan_unreported`,
  set by `lockEventFor`), and the replay's running title reads
  `succeeded` for it. The proposal's "the Lock need nothing" no longer
  holds for this one field. A scenario "A quiet plan replayed" was added
  to the `plugins` delta; tests: `TestStorePlan_RecordsUnreported`,
  `TestExecuteOne_ApplyOfUnreportedPlanIsNotTitledNoChanges`, and an
  assertion in `TestLockEventFor_UnreportedPlanIsKept`.
- 2026-10-17 (final checkpoint): the review's fix stores whether a
  plan's counts were reported in the Lock's plan record, which the
  `locking` capability describes field by field, so the change gained a
  `locking` delta ("The plan record") and the proposal lists the
  capability.
- 2026-10-17 (4.3): no intermittent failure was seen; the checkpoint
  and the review's regression test passed 20 times under `-race`, and
  the Lock and orchestrator packages pass against a real Valkey 8.
