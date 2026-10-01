# Proposal

## Why

Two mutating commands for the same Project can run at once: two
`/turnip apply` comments are both admitted, because the Lock stays
`plan_ready` until a result arrives. Two applies of the same plan racing
against one cluster or state is the failure the Lock exists to prevent.

## What Changes

- Admitting a mutating command moves the Lock out of `plan_ready` in the
  same atomic step, so a second mutating command for that Project is
  refused while the first runs
- The refusal says a mutating command is already running for the Project

## Capabilities

### New Capabilities

### Modified Capabilities
- `locking`: a running mutating command holds the Lock against another

## Impact

`internal/lock` (a new state or event for a mutating command in
flight), `internal/orchestrator` admission. Out of scope: queueing the
second command (Slice 30, scheduling, if wanted).
