# Design

## Context

See `proposal.md` for why, and the delta for the behavior. As the code
stands:

- **`nextSteps` builds the trailer from the result alone**
  (`internal/github/comment.go`). It offers the apply when the result
  succeeded, its counts are not all zero, and its tool has an apply
  operation; the unlock when `Locked` is set.
- **A result already carries what the new rule needs**: its
  `Operation`, its tool's `PlanOperation` and `ApplyOperation`, its
  counts (with `Unreported`), and `Locked`.
- **`Locked` is set from the transition the Lock manager confirms**
  (`HandleResult`): true, then false when the transition released the
  Lock or found none. After a successful plan it is therefore true
  exactly when the plan was recorded and the Lock kept, which is when an
  apply would be admitted. A clean plan for a tool that does not act
  without changes releases its Lock, so `Locked` is false.

## Goals / Non-Goals

**Goals**
- The apply line follows the Lock, as the unlock line does
- The change stays inside the comment's rendering

**Non-Goals**
- Any change to the Lock, the counts, or who may run the command

## Decisions

### 1. The rule reads the result's operation and `Locked`, not its counts

`nextSteps` offers the apply when the result succeeded, its operation
is its tool's plan operation, its tool has an apply operation, and
`Locked` is set. The counts, reported or not, no longer take part, and
a mutating operation's result never offers it, since its operation is
not the plan's.

An unregistered tool leaves both operation names empty, so its result
is never taken for a plan, as it is never offered an apply today.

- **Alternative considered:** carry an explicit "applicable" flag on the
  result, set from the Lock event `lockEventFor` chose.
- **Rejected because:** `Locked`, set from the confirmed transition, is
  already that fact for a successful plan; a second field would be one
  more thing to keep in agreement with it.

## Verification

| What | How |
|---|---|
| The rule | a table test over success, plan or mutating operation, `Locked`, counts (none, some, not reported) and a registered or unregistered tool: the apply line appears exactly for a successful, locked plan of a registered tool |
| The delta's scenarios | a test per scenario, on the rendered section |
| What stays | the existing trailer tests: re-plan and unlock lines unchanged, a refused Operation offers nothing, a timed-out one only the unlock |

## Risks / Trade-offs

- [Redis fails to apply a successful plan's Lock event] → `Locked`
  stays true, so the apply is offered; if it is run, the Lock's own
  check refuses it with its reason. The unlock line behaves the same
  way today.
- [A plan whose pull request was closed while it ran] → its Lock is
  gone, `Locked` is false, and no apply is offered, where today a plan
  with changes would offer one that could not run.

## Migration Plan

None.
