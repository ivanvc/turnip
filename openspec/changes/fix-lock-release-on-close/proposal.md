# Proposal

## Why

Closing or merging a pull request releases its Locks by walking the
Projects in the head commit's configuration. A Project the pull request
renamed or removed keeps its Lock forever, and if the configuration
cannot be fetched or parsed at close, nothing is released at all (it is
only logged). Either way another pull request is blocked by a Lock no
event will ever release.

## What Changes

- Closing releases every Lock held by the pull request, found from the
  Locks themselves rather than from the configuration
- The close comment lists every Project released

## Capabilities

### New Capabilities

### Modified Capabilities
- `locking`: close releases every Lock the pull request holds
- `pull-request-lifecycle`: close no longer depends on the configuration

## Impact

`internal/lock` (finding a pull request's Locks),
`internal/orchestrator` close handling.

## Dependencies

- **`lock-overview` (planned) depends on this.** It lists held Locks
  too; the way to find a pull request's Locks is built here first.
- **Overlaps `fix-plan-survives-push` and `fix-concurrent-mutations`.**
  All three change the `locking` capability, so they are best done one
  at a time.
