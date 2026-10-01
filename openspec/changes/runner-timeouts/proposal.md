# Proposal

## Why

Once an Operation's first line reaches the Server, nothing bounds it: a
silent, hung or dead Runner holds its check run in progress, its Lock
and its node until the 24-hour record expiry, and the dispatching
replica posts a bare deadline error without moving the Lock. This is
roadmap Slice 40, carried over unchanged; its roadmap entry holds the
analysis and the five decisions made so far.

## What Changes

- The Runner stops a tool that prints nothing for a configured idle
  period and reports an ordinary failure saying so
- An absolute deadline enforced by Kubernetes bounds every Runner
- A started Runner whose Job ended without a result is noticed and
  finalized as a failure

## Capabilities

### New Capabilities

### Modified Capabilities
- `operation-lifecycle`: an idle timeout, an absolute deadline, and detection of a Runner that ended without a result
- `runner-job`: the Job's deadline
- `server`: the timeout settings

## Impact

`internal/runner`, `internal/jobs`, `internal/orchestrator` (sweep).
Open questions carried from the roadmap: defaults, and whether a
`runner:` override is offered (Slice 43's rule applies if so).
