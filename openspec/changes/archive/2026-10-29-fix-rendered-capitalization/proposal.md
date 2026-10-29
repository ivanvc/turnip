# Proposal

## Why

Text turnip renders capitalizes "Project", "Lock" and "Operation" in
the middle of a sentence ("This Project is free for another pull
request to plan against", "The Lock is still held"), as German
capitalizes nouns. The specs capitalize those words as defined terms,
and the convention leaked from them into what users read on a pull
request. `CLAUDE.md`'s writing style now says rendered text writes them
in lowercase (2026-10-20); this change brings the existing strings in
line.

## What Changes

Each rendered string below writes the noun in lowercase mid-sentence;
the words themselves do not change, and a capital that starts a
sentence ("Lock released: ...") stays.

| Where it is rendered | Today | After |
|---|---|---|
| The results comment, below a mutating command's sections | "...narrower than the whole Project." | "...narrower than the whole project." |
| The close comment | "Unlocked the following Projects since this PR closed:" | "Unlocked the following projects since this PR closed:" |
| A project check run's summary while running | "Running `diff` for Project `web`." | "Running `diff` for project `web`." |
| The note when a project check run could not be created or completed (two variants) | "Note: this Project's GitHub check run could not be ..." | "Note: this project's GitHub check run could not be ..." |
| The lock-transition lines of a result (four variants) | "... This Project is free for another pull request to plan against." | "... This project is free for another pull request to plan against." |
| The lock-transition lines when a plan is stale or failed (three variants) | "... The Lock is still held ..." (one also "... act on this Project meanwhile.") | "... The lock is still held ..." ("... act on this project meanwhile.") |
| A start-timeout or abandoned result | "No Job/Pod found for this Operation; ..." | "No Job/Pod found for this operation; ..." |
| A start-timeout result naming the repository's Secret | "This Project mounts keys ..." | "This project mounts keys ..." |
| The Prometheus help text of two metrics | "Total number of Operations dispatched, by Project tool, Operation, and outcome.", "Webhook-to-Operation-result latency, by Project tool and Operation." | "Total number of operations dispatched, by project tool, operation, and outcome.", "Webhook-to-operation-result latency, by project tool and operation." |

The spec deltas replace each affected requirement whole, as OpenSpec's
MODIFIED format does, so the change is only visible inside their quoted
strings: the requirements' own prose keeps Lock, Project and Operation
capitalized as defined terms. This table is the change to review.

"Server" and "Runner" stay capitalized as the names of turnip's two
components, and `Job`, `Pod` and `Secret` as Kubernetes kinds.

## Capabilities

### New Capabilities

### Modified Capabilities
- `pr-comment`: the results comment's note on a mutating command's scope
- `locking`: the close comment and the lock-transition lines
- `project-checks`: the running summary and the check-run failure note
- `operation-lifecycle`: the start-timeout and abandoned results

## Impact

`internal/github` (comment rendering), `internal/orchestrator`
(`pullrequest.go`, `execute.go`, `lockstate.go`, `sweep.go`),
`internal/metrics` (its help text is quoted by no spec, so it changes
with no delta), their tests, and `docs/using/troubleshooting.md`
and `docs/operating/troubleshooting.md`, which quote two of the
strings. No behavior changes beyond the wording.

Out of scope:
- `No Projects matched this unlock command.`: lowercased by
  `fix-unlock-without-config`, which already modifies that requirement
- The specs' own prose, where the capitalized terms stay

## Dependencies

- **Overlaps `fix-unlock-without-config`,** which owns the unlock
  reply's wording; the two touch different requirements.
