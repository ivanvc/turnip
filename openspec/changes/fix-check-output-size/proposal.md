# Proposal

## Why

A Project's check is completed with the Operation's full output as its
details text, and GitHub refuses a check run whose text exceeds 65535
characters. Seen 2026-10-08: a plan of 160406 characters was rejected
with `422 Invalid request. Only 65535 characters are allowed`, and the
whole update failed with it, conclusion and title included.

The check is therefore left `in_progress`, not missing: it never
completes, and a branch protection rule that requires it blocks the pull
request. The note turnip appends to the results comment says the
opposite ("only its entry in the PR's checks tab is missing"), because
it was worded for a check that could not be created.

The details text only repeats the results comment, which already carries
every Project's full output and handles its size by splitting. The
check's title and summary already say everything that fits on a line:
the counts, the step that failed, the timeout diagnostic or the refusal
reason.

## What Changes

- A completed check carries no details text. Its summary is unchanged,
  except that a failure with no counts reads `The operation failed.`
  without `See the output below.`, which would point at nothing. No
  pointer to the results comment is added: a reader reaches the check
  from the pull request, where the comment is, and live output (roadmap
  Slice 26) is what will give the check a link of its own
- The check-run note says what actually happened: a check that could not
  be created is missing; one that could not be completed is left in
  progress

## Capabilities

### New Capabilities

### Modified Capabilities
- `project-checks`: no output in the check's details; a failure's summary drops `See the output below.`; the check-run note distinguishes a missing check from one left in progress

## Impact

`internal/orchestrator` (completing a Project's check, the note). Out of
scope: retrying a check update that fails for another reason.
