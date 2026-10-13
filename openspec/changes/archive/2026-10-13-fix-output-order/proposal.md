# Proposal

## Why

A tool's output reaches the pull request scrambled: lines the tool wrote
to standard error land in the middle of what it wrote to standard
output long before, line by line. Seen 2026-10-08 on a large Helmfile
diff that failed for two releases: the error report for each failed
release was mixed, line by line, into another release's manifests, so
neither could be read.

The shared capture every plugin uses reads the two streams through
separate pipes, one reader each, and merges their lines in the order the
readers hand them over. The `plugins` capability calls that the order
the lines arrived, where only two lines written to different streams "at
the same instant can land either way round". In fact whatever one stream
has buffered when the other is written (up to about 128 KiB: the pipe
and the reader's buffer) is mixed with it, and a reader slowed by
streaming each line to the Server keeps more buffered.

Measured with a command that writes 20,000 lines to standard output and
then 5 to standard error, so every stdout line precedes every stderr line
in write order, ten runs each:

| Capture | stdout lines recorded after the first stderr line |
|---|---|
| today's merge | up to 5,422; up to 19,088 with a reader that takes 20 µs a line |
| readers that never wait (small reads, each line recorded under one lock as soon as it is read, lines handed to the Server from a queue) | 0, with or without the slow reader |

That measurement used a writer that prints one line at a time, which a
reader keeps up with. Measured again 2026-10-12 with a writer that
numbers its lines, so the true order is known, and that writes the way
Helmfile does: `helmfile diff` holds every release's diff in memory and
prints them all in one burst once the last release is done, its progress
before that burst and its errors after it, both on standard error (read
from Helmfile v1.7.4's source). Sixty runs each of "an error, a
20,000-line burst, an error, a burst, an error":

| Capture | Runs out of order | Furthest a line landed from its place |
|---|---|---|
| two readers that never wait | 60 | 9,352 lines |
| the same, on pipes shrunk to 4 KiB | 44 | 860 lines |
| one reader that watches both pipes, on pipes shrunk to 4 KiB | 0 | 0 |

Two independent readers cannot do better: a pipe holds 64 KiB, and the
reader of the quiet stream records its line while the reader of the busy
one still has that much to work through. One reader that takes what each
pipe holds, the stream it was already reading first, records a burst
before what was written after it.

No capture that keeps the streams apart is exact when a tool writes to
both within microseconds: both pipes hold data before anything is read,
and nothing says which was written first. On a writer that switches
streams on every line with no pause, every capture above misplaces
lines; with a millisecond between switches, none does. Only one pipe
shared by both streams is exact there, and it loses which stream each
line came from.

The merge also takes a decision away from the plugin. Which stream
matters, and in what order a reader wants them, depends on the tool:
Helmfile writes its diffs to standard output and its progress and every
error to standard error, which says nothing about how another tool uses
its streams. The capture imposing one layout on every tool is the same
trap as the Server knowing a tool's output: plumbing that belongs to the
plugin.

## What Changes

- The shared capture records lines as they arrive, through one reader
  that watches both of the tool's pipes and never waits on the Server,
  so the record is in write order when the tool writes to one stream and
  then to the other, a large burst included, and close to it when it
  writes to both at once. Each line keeps its stream, so each stream on
  its own is always exact. The `plugins` capability states that
  accurately instead of "the same instant"
- The plugin decides what its Operation's output is: the record as it
  happened, or a selection of it by stream. turnip's annotation lines
  still open and close whatever the plugin returns, so no plugin can
  leave them out. The Server posts the output as the plugin returns it,
  knowing nothing of streams
- The live log stream and the Runner's own log show the record as it
  happened. The Runner keeps sending every line to the Server, now read
  from the record through a cursor rather than handed over by the
  readers, so no line is dropped and a slow Server never stalls the
  capture. The Server marks the Operation started once per stream
  instead of with a Redis call on every line
- Helmfile's output is the record as it happened, except when an
  operation fails: then it is standard error alone (its progress and
  every error), omitting standard output (the diffs of a plan that
  cannot be applied), and saying how many lines it omitted
- Helmfile runs with `--no-color`, so the codes of its colored banners
  (the release tables after `apply` and `sync`) stop reaching the results
  comment and the log as text such as `[1;34m`. helm-diff already prints
  no color without a terminal, so the diffs look as they do today
- A plugin added later decides its own output; the guide for adding a
  tool says so

## Decisions (2026-10-09)

- **A failed Helmfile operation shows standard error only**, with a line
  saying how many lines of standard output were omitted. Rejected:
  standard output after standard error, which a reader of a failed plan
  has no use for and which is where a first install's megabytes are
- **The capture keeps the streams apart and never waits**, and the
  Runner keeps streaming every line to the Server, which the live output
  view (roadmap Slice 26) needs, and needs whole: a tail opened partway
  through a run shows it from the start. The sender reads lines from the
  record itself through a cursor, so nothing is dropped and nothing is
  held twice, and a slow Server slows neither the capture nor the tool.
  Rejected: a queue that drops lines when full (2026-10-09, before the
  live view was settled to show the full log); sending only the first
  line until Slice 26 (Slice 26 would rebuild the streaming)
- **How a tool's streams are captured is its plugin's choice
  (2026-10-12).** Helmfile needs them apart: everything Helmfile itself
  says goes to standard error, so its change counts and its errors-only
  output both read one stream. A tool that uses its streams
  conventionally would be better served by one pipe for both, which is
  exact in every case and is what a terminal shows; that capture is
  added with the first plugin that wants it (roadmap Slice 7), not here.
  Rejected for Helmfile: one shared pipe now (no plugin could select by
  stream, so Helmfile could not show its errors alone, and its counts
  would have to tell its progress lines from a diff by their wording)
- **The capture is one reader over both pipes, on pipes shrunk to
  4 KiB (2026-10-12)**, after building the two independent readers first
  proposed and measuring them (see Why). Rejected: the two readers, with
  or without small pipes, which leave Helmfile's errors inside the last
  releases' manifests

- **Helmfile runs with `--no-color`**, as a flag rather than `NO_COLOR`
  in its environment, so the command line turnip annotates shows why the
  output has no color. Rejected: keeping the codes and stripping them
  when composing the comment, which keeps color only for a live view
  that may render it (Slice 26), and helm-diff colors nothing without a
  terminal anyway, so only the banners would gain. Slice 26 can revisit
  this if its view renders color

## Not in this change

- After a broken stream the Runner replays only its last 256 KiB of
  lines. A live view that shows the full log needs the replay to resume
  from the last line the Server stored; that belongs to Slice 26

## Capabilities

### New Capabilities

### Modified Capabilities
- `plugins`: a capture whose record is in write order except when both streams are written at once; the plugin decides its output inside turnip's annotation lines
- `plugin-helmfile`: its output is the record as it happened, and standard error alone on failure; its command line adds `--no-color`
- `operation-lifecycle`: log lines reach the Server read from the record, never dropped and never stalling the capture; an Operation is marked started once per stream

## Impact

`internal/plugin` (the shared capture in `command.go`, Helmfile's
composition; Helmfile is the only plugin today), `internal/runner`
(sending from the record), `internal/orchestrator` (marking an Operation
started once per stream), the real-process interleaving test the
`execution-provenance` amendment added, `docs/using/pull-requests.md`
and `docs/development/README.md` (both describe interleaving), and the
plugin guide for adding a tool. Related: the large-output work, which
omitting a failed plan's diffs makes smaller but does not replace, since
a successful plan's output is still unbounded.
