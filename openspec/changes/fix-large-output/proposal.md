# Proposal

## Why

An Operation whose output is a few megabytes is lost end to end. Seen
2026-10-08: the first plan of a new cluster, CRDs included, printed
about 5 MB, which is expected for a first install.

- **The result never reaches the Server.** The Runner sends its result
  as one gRPC message, and the Server accepts at most 4 MiB (gRPC's
  default, which turnip never changes): `received message larger than
  max (5069133 vs. 4194304)`.
- **The Runner retries a rejection that cannot succeed**, for its whole
  15-minute delivery budget, then exits 1. Nothing finalizes a started
  Operation whose Runner ended without a result (`runner-timeouts`), so
  the check stays in progress and no comment is posted.
- **The Runner's own log loses the output.** Before reporting, the
  Runner logs its whole result, repeating the output, which pushed the
  container log past the node's 10 MiB rotation, and `kubectl logs`
  serves only the newest file: the operator saw one line.
- **The comment would have been unreadable anyway.** A Project's output
  is split over as many 65,536-character comments as it takes, about 80
  for 5 MB.

## What Changes

- The Runner limits the output it sends to a fixed size, keeping the
  end (where a plan's summary and a failure's error are) and saying how
  much of the start it omitted. Each plugin reads its change counts from
  the whole output before the cut, and plan data is never cut, so a
  result always fits the Server's limit
- A result the Server rejects for its size is not retried: the Runner
  reports a failure, without the output, saying the result was too
  large to deliver
- The Runner's log line for its result is a summary (outcome, exit
  code, counts, the output's size), not the output again
- The results comment gives each Project's output at most 5 comment
  bodies, or as many as `TURNIP_MAX_COMMENTS_PER_PROJECT` says, keeping
  the end and saying how much was omitted

Until the full output is stored somewhere, what is cut is in the Runner
Pod's log for operators, until the Job is deleted.

## Decisions (2026-10-09)

- **The Runner sends at most 1 MiB of output**, well inside the
  Server's 4 MiB with room for plan data and the message itself.
  Rejected: raising the Server's limit and sending more, which only
  moves the cliff and grows what Redis carries for every result
- **Each Project's output takes at most 5 comment bodies by default**
  (about 325,000 characters; raised from 3 on 2026-10-09, after
  three-part outputs turned out to be common), set by a new Server setting,
  `TURNIP_MAX_COMMENTS_PER_PROJECT` (a positive integer; anything else
  stops the Server at startup). Rejected: a `turnip.yaml` field, since
  the file is read from the pull request, which could then raise its own
  cap; how much turnip posts costs API calls and noise, which is the
  operator's to bound, as `TURNIP_MINIMIZE_OUTDATED_PLAN_COMMENTS` is

## Out of scope

- **The full output, so the comment can link to all of it**: the live
  view (Slice 26) keeps every line in Redis until a TTL purges it, and
  "Long-term operation output" on the roadmap flushes it to blob storage
  when an operator opts in. The 1 MiB cut here applies only to the
  result and the comment, never to the log the live view shows
- **Finalizing a Runner that ended without a result**: `runner-timeouts`
- **Output order and what a failed Helmfile plan shows**:
  `fix-output-order`, which also makes a failed plan's output much
  smaller

## Capabilities

### New Capabilities

### Modified Capabilities
- `operation-lifecycle`: the Runner cuts the output it sends and does not retry a size rejection
- `observability`: the Runner's result log line is a summary
- `pr-comment`: each Project's output takes at most a configured number of comment bodies
- `server`: the `TURNIP_MAX_COMMENTS_PER_PROJECT` setting in the settings index

## Impact

`internal/runner` (the cut, the size rejection, the log line),
`internal/github` (the comment's body limit per Project),
`internal/orchestrator` (the setting), `docs/operating/server-settings.md`
(its Reference row). Redis carries
at most the capped output per result, which `fix-results-comment-on-replica-loss`
will store per trigger.
