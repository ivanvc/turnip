# Tasks

**The checkpoint that matters is task 2.1.** The rule reads `Locked`,
which only `HandleResult` sets, from the transition the Lock manager
confirms; a unit test on `nextSteps` can pass with a `Locked` value no
real result would carry. Only a result taken through `HandleResult`,
from a Helmfile plan with no changes (whose Lock is kept), a quiet one,
and an apply, shows the trailer each really gets. It uses the
orchestrator's harness, so it needs no GitHub.

## 1. The trailer (`internal/github`)

- [x] 1.1 `nextSteps` offers the apply for a successful result whose
  operation is its tool's plan operation, whose tool has an apply
  operation, and whose `Locked` is set; the counts take no part (design
  Decision 1). `docs/using/pull-requests.md`, below the example list of
  a section's commands, says the apply is offered while the pull
  request holds the Project's Lock after a successful plan, including a
  plan with no changes for a tool that acts without them, and never
  after an apply. Verify: a table test over success, plan or mutating
  operation, `Locked`, counts (none, some, not reported) and a
  registered or unregistered tool; a test per scenario of the delta on
  the rendered section; the existing trailer tests; `make docs-check`
  - _Deltas: pr-comment (Section trailer and next steps)_

## 2. Checkpoint: real results through the Server

- [x] 2.1 An orchestrator test taking results through `HandleResult`
  with the Helmfile plugin: a `diff` with no changes (Lock kept)
  renders the apply, re-plan and unlock lines; a quiet `diff` reading
  `succeeded` renders the apply line; a successful `apply` with changes
  renders no apply line; and, with a fake tool that does not act
  without changes, a clean plan whose Lock is released renders only the
  re-plan line

## 3. Final checkpoint

- [x] 3.1 `go build ./...`, `gofmt -l ./internal ./cmd` clean,
  `go vet ./...`, `make test`, `make lint` pass
- [x] 3.2 Every scenario in the delta maps to a test or a doc change;
  `openspec validate fix-apply-suggested-with-lock --strict` passes;
  `openspec/roadmap.md` shows the change as implemented
- [x] 3.3 An intermittent failure is recorded here with its full output,
  not rerun away

## Repository owner

- Review and commit
- Deploy a Server built from this change (the Runner is unchanged)
- The next Helmfile plan with no changes offers the apply; then this
  change is archived

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1.1"] },
    { "id": 1, "tasks": ["2.1"] },
    { "id": 2, "tasks": ["3.1", "3.2", "3.3"] }
  ]
}
```

## Deviations recorded at implementation

- 2026-10-17: Review fix. `HandleResult` (`internal/orchestrator/result.go`)
  now sets `Locked` to false when the Lock manager answers
  `lock.ErrLockedByOtherPR`. A Lock that was unlocked while the plan ran
  and then taken by another pull request left `Locked` true, so the apply
  (and the unlock) were offered although the Lock would refuse both,
  contrary to the delta's "while this pull request still holds the
  Project's Lock" and the design's claim that `Locked` is true exactly
  when an apply would be admitted. Other Lock errors (Redis failing)
  still leave `Locked` true, as design.md's Risks section accepts. Test:
  the "plan whose Lock another pull request took" case in
  `internal/orchestrator/apply_offer_test.go`, which fails without the
  fix. The sweep's timed-out path (`sweep.go`) has the same switch; it
  offers only the unlock, which predates this change, and was left as
  it is.
- 2026-10-17: Review fix. Added `TestNextSteps_PlanWithChangesKeepsItsLock`
  (`internal/github/comment_test.go`) for the delta scenario "Plan with
  changes keeps its Lock", asserting the exact three-line trailer in
  order. The rendering was already correct; the scenario had no test of
  its own.
- 2026-10-17 (final checkpoint): the proposal's Impact said nothing on
  the Server needed to change; the review's fix changed `result.go`, so
  the proposal now says so. The sweep's timeout path, which leaves
  `Locked` set when another pull request has taken the Lock (offering
  an unlock that is refused, never an apply), predates this change and
  is left as found.
- 2026-10-17 (3.3): no intermittent failure was seen; the checkpoint
  passed 20 times under `-race`.
