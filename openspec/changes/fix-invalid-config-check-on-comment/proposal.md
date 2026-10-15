# Proposal

## Why

An invalid `turnip.yaml` fails the `turnip` check when the automatic
plan finds it, but not when a comment trigger finds it. The same broken
file can leave branch protection's required check in two different
states depending on how turnip was asked.

## What Changes

- A comment trigger that finds an invalid `turnip.yaml` marks the
  `turnip` check as an invalid configuration, as the automatic path does

## Capabilities

### New Capabilities

### Modified Capabilities
- `merge-check`: an invalid configuration fails the check on either path
- `configuration`: the comment path reports an invalid file to the check

## Impact

`internal/orchestrator` (comment-path config handling).

## Dependencies

- **`operation-scheduling` depends on this.** It reports an invalid
  dependency graph as any invalid `turnip.yaml` is, which on the comment
  path fails the `turnip` check only once this lands.
