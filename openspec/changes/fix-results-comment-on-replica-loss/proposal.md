# Proposal

## Why

Every trigger's results comment must be posted, whatever happens to the
Server replica that dispatched it. Today it is not.

The Operations of a trigger (an automatic plan, or the commands of one
comment) are orchestrated by a goroutine on the replica that received
the webhook: it dispatches each command's Operations, waits for them,
and posts the results comment at the end. If that replica goes away
first, two things are lost:

- **The comment.** Any replica handles a Runner's result, so the check
  runs still finalize and the Lock still moves, but no replica posts the
  comment. Seen 2026-10-05: a `/turnip apply` whose change also upgraded
  turnip replaced the dispatching Server Pod mid-apply; the check
  completed, the Lock was released, and no comment ever appeared.
- **Later commands of the same comment.** A comment's commands run one
  after another from that goroutine, so a `/turnip diff` followed by a
  `/turnip apply` in one comment never dispatches the apply if the
  replica dies during the diff. Nothing reports it.

turnip claims any replica can serve any event; a trigger's orchestration
is where it cannot. Self-upgrading turnip with turnip makes this routine,
not rare.

## What Changes

- A trigger's progress is kept in Redis, not in a goroutine: which
  commands it has, which Operations each dispatched, which have finished
- Whichever replica finalizes a trigger's last Operation posts its
  results comment, exactly once, across replicas and restarts
- A comment's later commands resume on whichever replica finishes the
  command before them, instead of being lost with the replica that
  dispatched the first
- Applies to both the automatic plan and comment triggers
- The 24-hour give-up on a started Operation that never reports moves
  from the dispatching replica's wait to the sweep, so such a trigger
  still reaches its comment

## Decision: later commands resume (2026-10-05)

The replica that finalizes command N dispatches command N+1, so a
comment runs to completion as written whichever replica is alive. Every
command is already resolved and authorized when the webhook arrives
(only their execution is sequential), so the trigger records the
resolved targets and nothing is authorized again. Rejected: posting
what finished and listing the commands that did not run, which leaves
the author to re-comment them, and self-upgrades make that routine.

## Capabilities

### New Capabilities

### Modified Capabilities
- `operation-lifecycle`: a trigger's progress kept in Redis; finalizing a command's last Operation dispatches the next command or posts the comment; the sweep abandons a started Operation after 24 hours
- `pr-comment`: exactly one results comment per trigger, whichever replica finishes it

## Impact

`internal/orchestrator` (trigger state in Redis, finalization, the two
goroutines replaced, the sweep). Independent of `runner-timeouts`: a
started Operation that never reports still holds its trigger back for
24 hours, as today, and that change shortens the wait. Out of scope:
retrying a failed comment post (a GitHub API failure), which is its own
question.
