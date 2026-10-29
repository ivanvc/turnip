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
- The Runner stops its tool gracefully, an interrupt and then, after 30
  seconds, a kill, both for the idle timeout and when the Runner itself
  is told to stop
- An absolute deadline enforced by Kubernetes bounds every Runner
- A started Runner whose Job ended without a result is noticed and
  finalized as a failure

## Decisions (2026-10-24)

The archived roadmap's five decisions stand (the Runner enforces the
idle timeout; only the tool's lines reset it; interrupt, then kill; a
Kubernetes deadline as the backstop; the sweep finalizes a started
Runner whose Job ended). The open questions were settled:

1. **Defaults:** both settings have one, documented as safety bounds,
   not sizing: `TURNIP_RUNNER_IDLE_TIMEOUT` 30 minutes,
   `TURNIP_RUNNER_DEADLINE` 4 hours. The deadline must be longer than
   the idle timeout, so the clearer idle message normally wins.
2. **No `turnip.yaml` override in this change:** it would let a pull
   request lengthen how long it holds a node, and nothing needs it yet.
3. **No warning line before stopping:** it pays off only with a live
   view of the output.
4. **A fixed grace period:** 30 seconds between interrupt and kill, not
   a setting; the Runner Pod's termination grace period is 60 seconds,
   so the kubelet does not kill the Runner before the tool has stopped.

## Capabilities

### New Capabilities

### Modified Capabilities
- `operation-lifecycle`: an idle timeout, stopping the tool gracefully, two failure categories, and the sweep finalizing a started Runner that ended without a result
- `runner-job`: the Job's deadline, the Pod's grace period, and the idle timeout in the Runner's environment
- `server`: the two timeout settings, their defaults and validation
- `plugins`: the transcript's closing line when turnip stopped the tool
- `project-checks`: the check titles for a stopped tool and for a Runner that stopped without reporting

## Impact

`internal/runner`, `internal/plugin` (stopping the tool),
`internal/jobs`, `internal/orchestrator` (settings and the sweep),
`docs/operating/server-settings.md`.

Out of scope, kept in the backlog:
- A `runner:` override of either value in `turnip.yaml`, at both levels
  when it comes
- A warning line in the output before the idle timeout stops a tool

## Dependencies

- **Archive after `fix-rendered-capitalization` and
  `fix-single-config-surface`.** This change modifies "Start timeout and
  sweep" and "Settings index", which those also modify; its deltas are
  written on their versions, so archiving it last keeps their edits.
  (`fix-rendered-capitalization` was archived 2026-10-29.)

- **`operation-scheduling` depends on this, in part.** It leaves
  completing a queued check stranded by a replica that stopped to this
  change.
- **`runner-pod-disruption` (planned) depends on this.**
