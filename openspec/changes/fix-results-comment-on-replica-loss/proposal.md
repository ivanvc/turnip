# Proposal

## Why

The results comment for a trigger is posted by a goroutine on the Server
replica that dispatched it. If that replica restarts while the
Operations run, the check runs still finalize (any replica handles a
result), but the pull request comment is never posted. turnip claims any
replica can serve any event; this is where it cannot.

## What Changes

- The results comment is posted by whichever replica finalizes the last
  Operation of a trigger, from state kept in Redis, not by the
  dispatching replica's goroutine
- Exactly one comment per trigger, across replicas and restarts

## Capabilities

### New Capabilities

### Modified Capabilities
- `operation-lifecycle`: finalization owns posting the trigger's results
- `pr-comment`: the comment is posted once per trigger whichever replica finishes it

## Impact

`internal/orchestrator` (trigger state in Redis, finalization). Related
to Slice 40 (`runner-timeouts`), which also changes who finalizes.
