# Proposal

## Why

A Project's output too long for one comment is split into
`output part N/M` pieces at an exact byte count. The cut lands wherever
that count does: inside a line (seen 2026-10-14, a `port: 8888` shown as
`port: 8` at the end of one part and `888` at the start of the next) or
inside a multi-byte character, which then renders as a broken character
on both sides. Present since the first comment code; on the roadmap
since 2026-10-09.

## What Changes

- A part ends at the last whole line that fits; the next part starts
  with the line after it
- A line longer than a whole part is the only one split inside, and then
  at a character boundary, never inside a multi-byte character
- The last-resort cut, when a single section is still too large for a
  comment, keeps the end of the output from the start of a whole line
  in the same way (it too cuts at an exact byte count today)
- Parts may therefore be shorter than the most that fits, and a Project
  may need one more part than today; the cap on parts per Project
  (`TURNIP_MAX_COMMENTS_PER_PROJECT`) and the cut it makes from the
  start of the output apply as today, counted on the new parts

## Not in this change

- How much of a long output is kept, or where the cap cuts it (the
  large-output change settled both)
- The Runner's 1 MiB cut, which already cuts at a line boundary

## Capabilities

### New Capabilities

### Modified Capabilities
- `pr-comment`: where an output part ends

## Impact

`internal/github` (`splitDetailSection` in `comment.go`, and its tests,
including the property tests on splitting).
