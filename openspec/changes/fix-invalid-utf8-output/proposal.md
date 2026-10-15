# Proposal

## Why

A Runner reports to the Server in protobuf messages whose text fields
(a log line's message, a result's output and its error message) are
`string` fields, which must be valid UTF-8. When a tool prints a byte
sequence that is not, the message cannot be encoded, and sending it
fails every time:

- **The result.** Every delivery attempt fails the same way, so the
  Runner retries for its whole 15-minute budget and exits 1. The
  Operation was marked started by its first log line, so nothing times
  it out: its check stays in progress, and its trigger's comment and
  later commands wait, until the sweep abandons it after 24 hours.
- **The log stream.** Since `fix-output-order` the Runner sends every
  line of a tool's output as it is recorded. A line with invalid bytes
  fails its send and stops the sender, and the replay on the stream
  that replaces it carries the same line and fails again. (Follows from
  the encoding rule; not reproduced.)

Found by the review of `fix-large-output` (2026-10-09). A tool is not
expected to print invalid UTF-8, but binary content in a diff, a
truncated multi-byte character in a tool's own output, or a file in
another encoding are enough, and none of them is the author's mistake
to fix.

## What Changes

- Everything the Runner sends the Server as text is valid UTF-8: each
  invalid byte sequence is replaced with the Unicode replacement
  character (U+FFFD, `�`). That covers each log line, the result's
  output and its error message, and the clone container's failure
  message
- The replacement happens in the step that already strips the workspace
  path from all of these, before the 1 MiB cut, so the limit applies to
  what is sent and the cut, which already ends at a character boundary,
  never creates a new invalid sequence
- The Runner's own log keeps the tool's bytes as they were written, as
  it keeps the absolute paths
- Change counts are read from the record as the tool wrote it, so a
  replaced byte never changes a count

## Decision: replace, rather than change the wire format

Alternative considered: make the three fields `bytes`. Rejected: it
changes the protocol and every consumer of it (the Server, the comment,
the future live view), all of which handle the text as text, to carry
bytes nobody can read in a pull request comment. Replacing them at the
one place text leaves the Runner keeps the rest of turnip on valid
UTF-8.

## Not in this change

- Detecting or reporting that a tool printed invalid UTF-8: the
  replacement character in the comment is the report
- Text the Server builds itself (comments, check runs), which comes from
  what the Runner sent and from Go strings turnip writes

## Capabilities

### New Capabilities

### Modified Capabilities
- `operation-lifecycle`: what leaves the Runner for the Server is valid UTF-8, alongside the workspace path being stripped

## Impact

`internal/runner` (the cleaning step in `run.go` and the clone failure
path, `workspacepath.go` or a neighbor), and a checkpoint that a tool
printing invalid bytes delivers its result on the first attempt. Depends
on nothing open, and nothing depends on it.
