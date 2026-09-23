# Implementation Plan: Apply Exactly What Was Planned (Slice 20)

## Overview

The ordering is a dependency spine with two independent limbs hanging off
it.

The spine is the Lock: its shape has to change before anything can store
into it, storing has to work before retrieval can broaden, and the
argument refusal has to exist before a mutating Operation can safely
replay — because the replay is only unambiguous once nothing else is
allowed to supply arguments.

The two limbs are independent of that spine and of each other. Removing
`destroy` touches only the Plugin and its tests; the grammar change
touches only `internal/github`. Removing `destroy` goes **first**, because
every later task reasons about which operations exist, and it is better to
shrink that set once at the start than to keep writing "and destroy, which
is going away".

The two byte gates in Decision 3 are deliberately in the same task. Fixing
one without the other changes nothing observable: storing without
retrieving still refuses every apply, and retrieving without storing still
finds nothing. They are one bug with two locations.

Tests land after the behavior, not alongside it, for the reason Slice 19
gave: several of them assert *which path ran*, which has no meaning until
both paths exist. The exception is task 1, whose existing tests must be
updated in the same task or the checkpoint cannot pass.

## Tasks

- [x] 1. Stop Helmfile exposing `destroy`
  - [x] 1.1 Drop `"destroy"` from `GetOperations()` and update what asserts it
    - `HelmfilePlugin.GetOperations()` returns `["diff", "apply", "sync"]`
      (`internal/plugin/helmfile.go`). `Execute` needs no change — it
      already rejects any operation not in that slice, so the removal
      closes the path on its own
    - The existing assertions must move in the same task or the checkpoint
      fails: `helmfile_test.go` lines 34, 98 and 172, and
      `property_test.go`'s sampled-operation set at line 37. **Update
      them, do not delete them** — a deleted row stops exercising
      `apply`/`sync` as well
    - `plugin_test.go`'s `UnsupportedOperationError` test builds a literal
      `Supported` list and never calls the Plugin, so it passes unchanged.
      Leave it
    - No orchestrator change is needed for the refusal: `target.go`'s
      `operationRecognized` already rejects an unknown operation with a
      comment and creates no Lock, check run or Job, which is exactly the
      behavior Requirement 4.2 asks for
    - _Requirements: 4.1, 4.2, 4.3, 4.4_

- [x] 2. Checkpoint - destroy is gone, nothing else moved
  - `go build ./...` and `go test -race ./...` pass. A failure here is an
    assertion that still expects four operations, not a behavioral
    problem.

- [x] 3. Reshape the Lock around a plan record
  - [x] 3.1 `LockData` gains `HasPlan` and `PlanArgs`
    - Both fields as Decision 1 spells them, with `PlanArgs` carrying
      `omitempty` and `HasPlan` deliberately not — `HasPlan` is the only
      field that survives the round trip when a plan ran with no
      arguments, which is what Requirement 1.3 asks for
    - _Requirements: 1.1, 1.3_
  - [x] 3.2 Replace the two plan methods with `PlanRecord`
    - `StorePlan`/`GetPlan` taking and returning a `PlanRecord`, replacing
      `StorePlanData`/`GetPlanData`. Update `LockManager`, the Redis
      implementation, and both fakes in `internal/orchestrator`'s tests
    - `GetPlan` tests `HasPlan` rather than `len(data.PlanData)`. This is
      the single line that makes a Helmfile mutating Operation reachable
    - Rename `ErrNoPlanData` to `ErrNoPlan`. The condition it reports has
      changed from "the bytes are empty" to "no plan was recorded", and a
      name that says the opposite of what the code tests is the defect
      this slice keeps finding elsewhere
    - A Lock written before this slice has no `has_plan` key, so it
      decodes false and its pull request is asked to re-plan. That is the
      migration, and it is deliberate — no step, no backfill
    - _Requirements: 1.1, 1.3, 2.6_

- [x] 4. Checkpoint - the interface changed and every caller compiles
  - `go build ./...` and `go test -race ./...` pass. Behavior is
    unchanged so far: nothing stores a record yet, so nothing retrieves
    one.

- [x] 5. Record on success, release on everything else
  - [x] 5.1 Store a record for any successful plan
    - `HandleResult`'s storage condition drops `len(result.PlanData) > 0`
      (`internal/orchestrator/result.go`). A successful plan records a
      `PlanRecord` whatever the tool returned — for Helmfile `Data` is
      always empty, and that is now fine
    - The arguments stored are `rec.ExtraArgs`, read from the
      `OperationRecord` the Job was actually built from rather than
      re-derived from the trigger line. That is Requirement 1.2 satisfied
      by reading a field that is already in hand
    - _Requirements: 1.1, 1.2_
  - [x] 5.2 Release the Lock after any successful non-plan Operation
    - The `case rec.IsApply` arm becomes the fall-through: the Plugin's
      plan operation stores and keeps the Lock, anything else releases it.
      No new field on `OperationRecord` — `HandleResult` already compares
      `rec.Operation` against `GetPlanOperation()` for the store branch
    - `IsApply` was expected to stay for the replay path in `executeOne`.
      Task 6.2 then broadened that fetch to every mutating Operation,
      which left nothing reading the field at all — so it and its local
      were removed as dead weight, along with `OperationRecord.PlanData`,
      which the Job takes from a local rather than from the record
    - This is what stops a successful `sync` leaving the Project locked
      with no route back but a manual unlock
    - _Requirements: 2.5_

- [x] 6. Refuse arguments, replay the recorded scope
  - [x] 6.1 Refuse trailing arguments on any non-plan Operation
    - In `executeOne`, immediately after `isPlan`/`isApply` are computed
      and **before** the Lock is touched, beside the ServiceAccount and
      submodule refusals that sit there for the same reason
    - The condition keys off `!isPlan`, never a list of operation names,
      so anything a future Plugin adds is covered without being remembered
    - The refusal names the arguments it refused and says the plan's own
      scope is what will be used. Refusing rather than ignoring is the
      point: a silently dropped argument is indistinguishable from an
      honored one until the infrastructure changes
    - _Requirements: 2.2, 2.3, 2.4_
  - [x] 6.2 Fetch and replay the plan for every mutating Operation
    - The `if isApply` guard around the plan fetch broadens to every
      non-plan Operation, so `sync` replays the recorded scope instead of
      running unscoped. The Lock requirement already exists on that path —
      this adds the recorded plan to a gate rather than introducing one
    - The stored `PlanArgs` go into `jobs.OperationParams.ExtraArgs` in
      place of `t.ExtraArgs`. Task 6.1 is what makes that substitution
      unambiguous: there is exactly one candidate
    - Where no Lock with a recorded plan is held, refuse — including the
      pre-upgrade Lock from task 3.2
    - _Requirements: 2.1, 2.6_

- [x] 7. Checkpoint - the loop closes
  - `go build ./...` and `go test -race ./...` pass. The behavioral
    milestone: a Helmfile `diff` followed by an `apply` completes instead
    of being refused. If this checkpoint does not demonstrate that, the
    slice has not done its job.

- [x] 8. Let the first `-` token start the arguments
  - [x] 8.1 Extend the trigger grammar in `internal/github`
    - One scan over the tokens after the operation finds the first
      beginning with `-`. Exactly `--` is consumed as a delimiter;
      anything else is itself the first argument. All three of
      Requirement 3's criteria fall out of that single rule — resist
      writing three branches
    - Every existing `--` trigger line keeps its current meaning, which
      the existing parser tests are the check on
    - The parser still produces `ExtraArgs` for every operation. It has no
      Plugin registry and cannot tell a plan from a mutating Operation;
      task 6.1 is what rejects them, one layer later
    - _Requirements: 3.1, 3.2, 3.3_

- [x] 9. Checkpoint - grammar
  - `go build ./...` and `go test -race ./...` pass, with the existing
    parser tests unchanged.

- [x] 10. Tests
  - [x] 10.1 `internal/lock`: the record round-trips
    - `HasPlan` survives JSON in all three states — absent (a legacy
      Lock), true with empty args, true with args. The absent case is the
      migration, so it is the one that must be explicit
    - `GetPlan` returns `ErrNoPlan` when no plan was recorded and succeeds
      when one was, including when `Data` is empty
    - _Requirements: 1.1, 1.3, 2.6_
  - [x] 10.2 `internal/orchestrator`: the three regressions
    - **A Helmfile plan records a plan** — a result with `PlanData: nil`
      still calls `StorePlan` with `HasPlan` true. Fails before task 5.1
    - **An apply after a Helmfile plan is not refused** — asserted as
      behavior, not as a mock expectation, because mock expectations are
      what hid this bug. `TestExecuteOne_ApplyWithoutPlanDataIsRejected`
      is re-read in this light and kept: the refusal is still correct when
      no plan ran
    - **A successful `sync` releases the Lock** — the assertion that
      catches anyone narrowing release back to `IsApply`
    - _Requirements: 1.1, 2.5, 2.6_
  - [x] 10.3 `internal/orchestrator`: arguments in and out
    - Every mutating Operation refuses trailing arguments **with no side
      effects** — no check run, no Job, no Lock read — table-driven over
      `apply` and `sync` so a future operation is not silently exempt.
      Assert by absence: asserting the message alone passes with the guard
      in the wrong place
    - A plan still accepts arguments — the other half of that table, and
      the guard against over-broad refusal
    - The stored arguments are the Operation's, not the trigger's: store a
      record whose `ExtraArgs` differ from the trigger line's and check
      which reaches the Lock
    - `sync` replays the recorded scope, asserted on the built Job's
      `TURNIP_EXTRA_ARGS` — the only place the value is observable end to
      end
    - _Requirements: 1.2, 2.1, 2.2, 2.3, 2.4_
  - [x] 10.4 `internal/plugin` and `internal/github`
    - `destroy` is not a Helmfile operation, asserted on `GetOperations()`
      directly and end to end: a `/helmfile destroy` trigger is rejected
      as unrecognized and creates no Lock, check run or Job
    - Grammar table tests, one row per line of Decision 7's criteria
      table, plus the existing `--` cases which must keep passing
    - _Requirements: 3.1, 3.2, 3.3, 4.1, 4.2_

- [x] 11. Amendments to earlier slices
  - [x] 11.1 Append amendment tasks to the four slices this changes
    - `plugin-helmfile` — task **17**: `GetOperations()` drops `destroy`;
      amend its Requirements 3.2 and 3.8, and its "Reconciling
      `GetOperations()` with global Requirement 13" section, recording
      that the reversal answers a different question than that section did
    - `redis-lock-manager` — task **18**: `LockData` gains
      `HasPlan`/`PlanArgs`; `StorePlan`/`GetPlan` replace the old pair;
      `ErrNoPlanData` becomes `ErrNoPlan`; the "empty `PlanData` means no
      plan data" rule in its Data Model and Edge Cases is replaced by
      `HasPlan`
    - `github-integration` — task **25**: the grammar and Requirement
      4.2's `[-- extra args]` wording gain the implicit delimiter
    - `server-orchestration` — task **27**: `executeOne` gains the
      argument refusal and the replay substitution; `HandleResult` loses
      its byte test and its apply-specific release
    - _Requirements: (spec maintenance, no behavioral change beyond the tasks above)_
  - [x] 11.2 Amend the global spec
    - **Requirements 13.7 and 13.9** are overturned by task 1 and need
      amendment markers. 13.2, which lists only `diff`, `apply` and
      `sync`, becomes correct as written and needs no change
    - **Design Property 22** drops its destroy clause, after which its
      "Validates: Requirements 13.2–13.5" line matches what it claims
    - Requirement 7.5 needs no marker — it is implemented here rather than
      overturned — and 6.2 is extended rather than contradicted
    - _Requirements: 4.1, 4.3_

- [x] 12. Documentation
  - [x] 12.1 `docs/usage.md`
    - Show a scoped plan written without `--`, and state that a mutating
      operation takes no arguments because it replays the plan's own —
      framed as what makes an apply match its diff, not as a restriction
    - Fix the example this slice invalidates, `/turnip apply web --
      --auto-approve`, and the `[-- extra args]` grammar line
    - Drop `destroy` from the Helmfile operation lists at lines 54 and
      117, and describe the reviewed alternative: mark a release
      `installed: false`, which `diff` reports as a pending removal and
      `apply` performs
    - _Requirements: 5.1, 5.2, 5.3_
  - [x] 12.2 `docs/troubleshooting.md` and `docs/configuration.md`
    - Both name `destroy` as a Helmfile operation — `troubleshooting.md`
      line 172 and `configuration.md` line 353. Remove it from each
    - _Requirements: 5.3_

- [x] 13. Final checkpoint - full verification
  - `go build ./...`, `go test -race ./...`, `go mod tidy` clean,
    `gofmt -l .` clean, and the real golangci-lint v2 via
    `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest run ./...`

## Notes

- **The two byte gates are one bug.** Task 5.1 and task 3.2 fix opposite
  ends of it, and neither is observable alone. If task 7's checkpoint does
  not show a Helmfile apply completing, one end was missed.
- **`destroy` removal is separable and goes first** precisely because it
  is not entangled: it touches the Plugin and four test sites, and doing
  it early means every later task reasons about three operations rather
  than four.
- **The migration is a non-event by design.** A pre-upgrade Lock decodes
  with `HasPlan` false and its pull request is asked to re-plan. No
  backfill, no version field, and the condition clears itself on the next
  plan — the conservative direction, since the alternative is replaying a
  scope nobody recorded.
- **The regression most worth guarding** is someone narrowing the release
  back to `IsApply` because it reads as the "apply releases the lock"
  rule. Task 10.2's `sync` assertion is the only thing that catches it;
  keep its comment explicit about what it defends.
- **No new dependencies and no new configuration.** Deliberately no flag
  for destroy: an operation nobody can review is not one an operator
  should be able to switch on, which is the argument recorded in
  Decision 6 against mirroring Atlantis's `--allow-commands` here.
- **Coverage target**: 80% for touched packages, consistent with prior
  slices.

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1.1", "8.1"] },
    { "id": 1, "tasks": ["3.1"] },
    { "id": 2, "tasks": ["3.2"] },
    { "id": 3, "tasks": ["5.1", "5.2"] },
    { "id": 4, "tasks": ["6.1"] },
    { "id": 5, "tasks": ["6.2"] },
    { "id": 6, "tasks": ["10.1", "10.2", "10.3", "10.4"] },
    { "id": 7, "tasks": ["11.1", "11.2", "12.1", "12.2"] }
  ]
}
```
