# Implementation Plan: The Lock's Lifecycle as a State Machine (Slice 18)

## Overview

One ordering rule dominates this plan: **every writer of the state lands
before any reader of it.**

A reader flipped early is not a degraded state, it is an outage. If
admission starts requiring `PlanReady` while nothing yet maintains the
state, every Lock reads as not-`PlanReady` and every Mutating_Operation in
every repository is refused. The same hazard runs the other way through
Redis: the pilot has live Locks written without a state field, so the
tolerant decode of Requirement 1.3 must exist before anything asks a Lock
what state it is in.

So the sequence is: teach the Lock to *carry* a state (task 1), decide
what the transitions *are* in a pure function with no callers (task 3),
give the manager atomic entry points (task 4), move every writer onto them
(task 6), and only then flip the readers (task 8). `HasPlan` survives
until that last step, so each checkpoint before it has something working
to be green against.

The Plugin declaration comes third rather than later because it is an
*input* to the table: it selects between the two edges out of a successful
plan.

## Tasks

- [x] 1. The Lock carries a state
  - [x] 1.1 `LockState` and the stored field
    - `LockState` with `StatePlanning`, `StatePlanReady`, `StatePlanStale`
    - Stored inside the existing Lock value, beside `HasPlan` rather than
      replacing it yet
    - `LockStatus` gains `State`, keeping `HasPlan` so no reader breaks
    - _Requirements: 1.1, 1.4_
  - [x] 1.2 Tolerant decode
    - A stored Lock with no recognisable state decodes as **not**
      `PlanReady`
    - A test that a value written in the old shape — no state field at
      all — decodes without error and does not report `PlanReady`. This is
      the single edge where a wrong default applies something nobody
      reviewed
    - _Requirements: 1.3_

- [x] 2. Checkpoint - the field exists and nothing consults it
  - `go build ./...`, `go test -race ./...` and golangci-lint pass.
  - Behaviour is deliberately unchanged: `State` has no reader. What this
    proves is that the new field round-trips, and that Locks already in
    Redis still decode, *before* anything depends on either.

- [x] 3. The Plugin declares whether a Mutating_Operation can act without changes
  - [x] 3.1 `ActsWithoutChanges()` on the interface
    - Helmfile returns true, with `sync` recorded as the reason in the doc
      comment — `GetOperations` exposes it, and it upgrades every release
      regardless of the diff
    - _Requirements: 6.1, 6.2, 6.3, 6.4_
  - [x] 3.2 A fake Plugin that answers the other way
    - Helmfile can only ever exercise one side. The fake is what will
      prove, in task 6, that the declaration is consulted rather than
      ignored
    - _Requirements: 6.1_

- [x] 4. The transition table, as a pure function with no callers
  - [x] 4.1 Events and the table
    - The eight events of the design, including plan and mutating timeouts
      as distinct events — they move differently
    - A function from (state, event) to the resulting state, whether the
      Lock survives, and the edge taken
    - _Requirements: 3.1, 3.2, 3.3, 3.4, 3.5, 5.1, 5.2, 5.3_
  - [x] 4.2 Exhaustive table test
    - Every row of the design's table, asserted on the resulting state and
      on whether the Lock survives
    - The unreachable combinations asserted unreachable — a plan result
      arriving in `PlanReady` must be impossible, because dispatch moved
      it first. This is the test that catches Requirement 2.2 regressing
      from dispatch-time to result-time, a change that breaks nothing else
      and silently reopens the window
    - _Requirements: 3.6, 2.2_

- [x] 5. Checkpoint - the table is right before anything obeys it
  - `go test ./internal/lock/...` passes, including the exhaustive table.
  - The table has no caller yet, so a failure here is a failure of the
    rules themselves rather than of any wiring.

- [x] 6. The manager applies the table atomically
  - [x] 6.1 `AcquireForPlan`
    - Acquires, or confirms this pull request already holds it, applying
      the dispatch edge in the **same** evaluation — a re-acquire in
      `PlanReady` rewrites the state to `PlanStale`
    - Returns false only when a different pull request holds it, changing
      no state
    - _Requirements: 2.1, 2.3, 2.4_
  - [x] 6.2 `Apply`
    - Applies a result or timeout event and reports the `Transition`
    - Exactly one Lock mutation per call: the classification precedes the
      write, and `StorePlan`-then-release never happens, because
      `ReleaseLock` deletes the key and the plan record inside it
    - _Requirements: 3.6_
  - [x] 6.3 Atomicity against a real Redis
    - The dispatch edge is a claim about what two Servers can observe.
      miniredis serialises every command and cannot fail the way a real
      instance would, so this belongs with the `TURNIP_TEST_REDIS_ADDR`
      tests, which skip when no instance is available
    - _Requirements: 2.3_

- [x] 7. Checkpoint - the manager is correct, the orchestrator untouched
  - `go build ./...`, `go test -race ./...` and golangci-lint pass.
  - The orchestrator still calls `AcquireLock`/`StorePlan`/`ReleaseLock`,
    so behaviour is unchanged. Everything that follows is wiring.

- [x] 8. Every writer moves onto the entry points
  - [x] 8.1 Dispatch
    - `executeOne`'s plan path calls `AcquireForPlan`
    - First behaviour change in the slice: a re-plan invalidates the
      stored plan at dispatch
    - _Requirements: 2.1, 2.2_
  - [x] 8.2 Results
    - `HandleResult` maps its situation to an event and calls `Apply`;
      `ActsWithoutChanges` selects between the two successful-plan edges
    - `Locked` is set from the returned `Transition`, after it returns
      without error — never alongside the decision
    - _Requirements: 3.1, 3.2, 3.3, 3.4, 5.1, 5.3, 7.4_
  - [x] 8.3 Timeouts
    - `sweepOnce` applies its event through the same `Apply`, not a
      decision of its own
    - `reportTimeout` sets `Locked` from the `Transition`. Today it never
      sets it at all, so a held Lock is absent from the footer and never
      offered `unlock` — held and invisible at once
    - _Requirements: 1.5, 3.5, 5.2, 7.7_
  - [x] 8.4 Unlock and pull-request close
    - Both become events through the same entry point, replacing their
      direct `ReleaseLock` calls at `comment.go:247` and
      `pullrequest.go:148`
    - Their existing separate comments are unchanged; this is about the
      table being complete, not about what the reader sees
    - _Requirements: 1.6_
  - [x] 8.5 A mutating result is honoured whatever state it arrives in
    - A plan dispatched while an apply is in flight moves the Lock to
      `PlanStale` beneath it; the apply's result must still release or
      invalidate rather than being dropped because the state moved
    - An event for a Lock that no longer exists is a no-op: closing a
      pull request mid-apply deletes it, and the result must still reach
      the pull request
    - _Requirements: 5.4, 1.7_

- [x] 9. Checkpoint - state is maintained everywhere, read nowhere
  - `go build ./...`, `go test -race ./...` and golangci-lint pass.
  - Admission still reads `HasPlan`, so a stale plan is still admitted at
    this point. That is expected and is exactly why task 10 exists — but
    it means this checkpoint is not a shippable state, and the two should
    land together.

- [x] 10. The readers flip, and `HasPlan` goes
  - [x] 10.1 Admission
    - A Mutating_Operation is admitted only from `PlanReady`
    - The two refusals are worded differently: nothing was ever recorded,
      versus the recorded plan is no longer valid. The second tells the
      author something happened
    - _Requirements: 4.1, 4.2, 4.3_
  - [x] 10.2 Selection
    - `target.go`'s bare-Mutating_Operation selection reads the state, so
      a Project with an invalid plan is not selected and then refused one
      by one
    - _Requirements: 4.4_
  - [x] 10.3 Remove `HasPlan`
    - From `LockStatus` and the stored value, with the fakes updated
    - Nothing should read it by now; if something does, this is where that
      is discovered rather than in review
    - _Requirements: 1.2_

- [x] 11. Checkpoint - a stale plan is refused
  - `go build ./...`, `go test -race ./...` and golangci-lint pass.
  - The three scenarios that motivated the slice, end to end: plan, push,
    apply is refused; plan, failed apply, retry is refused; plan, timeout,
    Lock reported as held with `unlock` offered.
  - A pre-upgrade Lock — a stored value with no state — refuses a
    Mutating_Operation rather than admitting one.

- [x] 11b. Remove the superseded entry points
  - `AcquireLock`, `StorePlan` and `ReleaseLock` have no production caller
    once task 8 lands — verified, all three at zero. Left on the interface
    they are doors that bypass the table, which is exactly the defect the
    transition audit found in `comment.go` and `pullrequest.go`
  - Their tests move onto `AcquireForPlan` and `Apply`, which is better
    coverage regardless: it exercises the doors production actually uses
  - _Requirements: 1.5_

- [x] 12. The comment says what happened and why
  - [x] 12.1 `LockNote` on `ProjectResult`
    - Set only from a confirmed `Transition`, in the same place as
      `Locked`, so the two cannot disagree
    - Empty means neither a release nor an invalidation happened — which
      includes a rejected Operation, since `rejectedResult` never held a
      Lock
    - _Requirements: 7.1, 7.2, 7.4_
  - [x] 12.2 Render it in the trailer
    - Beside `nextSteps`, outside the fence. Not appended to `Output`,
      which renders inside the tool's code block and is what gets split
      across `part N/M`
    - The existing "Lock released — …" sentence moves here and gains its
      reason
    - _Requirements: 7.1, 7.5_
  - [x] 12.3 One message per edge, not per state
    - `PlanStale` is reached by a push, a failed re-plan, a failed apply
      and a timed-out apply. "This plan is stale" tells an author nothing;
      "your apply failed part-way, re-plan before retrying" does
    - _Requirements: 7.3_
  - [x] 12.4 Announce nothing when the transition failed
    - A transition returning an error leaves `Locked` as it was and
      `LockNote` empty. No happy-path test reaches this, so it needs its
      own
    - _Requirements: 7.4, 7.6_

- [x] 13. The specification and the record
  - [x] 13.1 Amend global Requirement 7.3 and 7.4 in place
    - 7.3 keeps the Lock only where the plan leaves something to apply;
      7.4 gains this slice's releases among the triggers that end a Lock's
      life
    - Recorded in the roadmap's Slice 18 entry, per the convention the
      global spec already uses
    - _Requirements: 8.1, 8.2, 8.3_
  - [x] 13.2 Correct the citations
    - `result.go:45` cites "Requirement 6.6-6.8"; `sweep.go:89` cites
      "6.8/8.5". Requirement 6 is *Plan with Destroy Flag* and has five
      criteria, none about Lock lifecycle
    - No behaviour changes
    - _Requirements: 9.1, 9.2, 9.3_
  - [x] 13.3 Amendment task in `redis-lock-manager`
    - Its documented lifecycle is now a state machine with more release
      triggers than it records
    - _Requirements: 8.3_

- [x] 14. Documentation
  - [x] 14.1 The states and what moves between them
    - That pushing a commit invalidates a stored plan, and a
      Mutating_Operation then needs a new plan
    - That a failed or timed-out Mutating_Operation requires a re-plan
      before it can be retried, and why — infrastructure may be partly
      changed, so the re-plan is what reveals what actually happened
    - _Requirements: 10.1, 10.2, 10.3_

- [x] 15. Checkpoint - the slice is done
  - `go build ./...`, `go test -race ./...` and golangci-lint all pass.
  - Mutation checks on the new edges: disabling each must fail its row.
    Absence assertions come from fakes that record calls, never from state
    that happens to look unchanged.
  - The `ActsWithoutChanges` fake exercises both answers — a Plugin
    returning false releases on a no-change plan, one returning true does
    not. Helmfile alone proves only half of it.
  - Roadmap: Slice 18 to Complete.
