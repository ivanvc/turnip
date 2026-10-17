# Proposal

## Why

A Project's section in the results comment ends with the commands that
act on it alone. The apply command is offered only for a successful
result with at least one change (`pr-comment`, "Section trailer and next
steps"), while the unlock command follows the Lock. The two disagree in
three ways:

- **A plan with no changes whose Lock is kept offers no apply.** Helmfile
  keeps a Project's Lock, with its plan recorded, when a `diff` finds no
  changes, because `sync` acts on every release regardless. The section
  then offers "Re-plan it" and "Release its lock", but not the apply the
  Lock admits. Seen 2026-10-14: the apply, run by hand, went through.
- **A plan whose counts are not reported offers no apply.**
  `fix-helmfile-diff-removals` makes a quiet Helmfile plan (`-q`,
  `--log-level warn` and up) read `succeeded`, with its counts not
  reported and its Lock kept as applicable. Its counts are zero, so the
  "at least one change" rule offers no apply for a plan the Lock admits.
- **A successful apply with changes offers to apply again.** Every result
  carries its tool's apply operation, and the rule does not ask whether
  the result was a plan, so an apply that changed something is followed
  by "Apply just this project".

The unlock line already follows the Lock rather than the counts; the
apply line should too.

## What Changes

- The apply command is offered for a successful plan whenever this pull
  request still holds the Project's Lock, whatever its counts and
  whether they were reported. A tool that does not act without changes
  releases its Lock on a clean plan, so for it nothing changes
- It is never offered for the result of a mutating operation
- The "Re-plan it" and "Release its lock" lines are unchanged
- `docs/using/pull-requests.md` says when a section offers the apply

## Decision: the offer is the tool's apply operation (2026-10-17)

A clean Helmfile `diff` offers `apply`, Helmfile's apply operation, as
any plan does. Rejected: letting a tool name the operation its clean
plan offers, which for Helmfile would be `sync`: with nothing to change,
`sync` still upgrades every release, so offering it would suggest the
heavier command exactly when the plan found nothing. `sync` stays
available to whoever means it.

## Not in this change

- What a plan does to the Lock (the `locking` capability, each tool's
  "acts without changes", and `fix-helmfile-diff-removals` for a plan
  whose counts are not reported)
- Which commenter may run the command offered: the trailer offers it to
  whoever reads the comment, and authorization decides when it is run
  (`authorization`, and `fix-team-collaborators` for access through a
  team)

## Dependencies

- **Builds on `fix-helmfile-diff-removals`** (implemented, not yet
  archived) for the case of a plan whose counts are not reported. This
  change can land first: the rule follows the Lock, so that case is
  covered whenever both are in.

## Capabilities

### New Capabilities

### Modified Capabilities
- `pr-comment`: the apply command follows the Lock and is offered for plans only

## Impact

`internal/github` (`nextSteps` in `comment.go`, and its tests): a
result already carries its operation, its tool's plan and apply
operations, and whether this pull request holds the Lock, so nothing
needs to be added to it. `docs/using/pull-requests.md`.
`internal/orchestrator` (`result.go`) changed after all, found by the
review: a plan whose Lock another pull request took while it ran now
reports that this pull request no longer holds it (see `tasks.md`).
