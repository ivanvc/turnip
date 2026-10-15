# Proposal

## Why

A push to a pull request is meant to invalidate every plan it holds, but
today only the Projects the automatic plan matches are marked stale. A
Project planned by name or with `*` outside its `whenModified` patterns,
and every Project on a draft pull request (which dispatches no automatic
plan), keeps a `plan_ready` Lock across the push. A mutating command then
runs on the new head commit with the old plan's arguments and image
digest: something nobody reviewed.

## What Changes

- Every push (`synchronize`) moves every `plan_ready` Lock the pull
  request holds to `plan_stale`, whether or not the push dispatches a
  plan for that Project, and on drafts as well
- A push announces which plans it superseded. Today no push says so for
  any Project: the "superseded by a new commit" text exists, but plan
  dispatch discards it, so the docs' claim that turnip "says so in the
  comment each time" is untrue even for matched Projects

## Capabilities

### New Capabilities

### Modified Capabilities
- `locking`: a push invalidates every plan the pull request holds
- `pull-request-lifecycle`: what a push does to a draft's and an unmatched Project's plans

## Impact

`internal/orchestrator` (the `synchronize` handling and Lock events).
Found by the 2026-09-30 review of current behavior; the docs already
claim this behavior ("When a stored plan stops being usable").

## Dependencies

- **Overlaps `fix-lock-release-on-close` and `fix-concurrent-mutations`.**
  All three change the `locking` capability, so they are best done one
  at a time.
