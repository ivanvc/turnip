# Implementation Plan: What a Pull Request Must Satisfy Before a Mutating Operation (Slice 34)

## Overview

Two independent foundations first: what GitHub can tell turnip (reviews,
mergeability) and the Server setting. Then the evaluation of each
requirement in isolation, then the gate wired into the comment path, where
the property that matters is tested: a Mutating_Operation on a pull
request that does not satisfy the Requirement_Set creates nothing at all.

**The checkpoint that matters is task 4.** An evaluator that answers
correctly is worthless if the gate is skipped on one path, fires for a
plan or `unlock`, or leaves a Lock, Job or check run behind. Absence is
asserted through fakes that record calls, never through state that
happens to look unchanged.

Tasks in the same wave of the dependency graph (at the end) touch
disjoint files and run in parallel. The tree builds after every wave.

## Tasks

- [x] 1. What GitHub tells turnip (`internal/github`)
  - [x] 1.1 `Review{Author, State}` and `ListReviews` on `GitHubClient`
    and `Client`, reading every page; the full fake in
    `authorize_test.go` gains a stub (the orchestrator's fakes embed the
    interface and need none)
  - [x] 1.2 `PullRequest.Mergeable *bool`, mapped in `GetPullRequest` from
    the conflict-only `mergeable` field, `nil` while GitHub has not
    computed it; never `mergeable_state`
  - [x] 1.3 Tests against an `httptest` server: pagination of reviews;
    `mergeable` true, false and absent
  - _Requirements: 3.1, 4.1, 4.2_

- [x] 2. The setting (`internal/orchestrator/config.go`)
  - [x] 2.1 `TURNIP_APPLY_REQUIREMENTS`, comma-separated, whitespace
    trimmed, into `Config.ApplyRequirements`; an unknown name fails
    `ConfigFromEnv` naming it and listing `approved`, `mergeable`
  - [x] 2.2 Tests: unset, empty, each name, both, whitespace, an unknown
    name; `knownOverridePaths` unchanged
  - _Requirements: 1.1–1.4, 2.1–2.3_

- [x] 3. Evaluating the Requirement_Set (new file in `internal/orchestrator`)
  - [x] 3.1 `approved` per the design's table: each account's latest
    `APPROVED`, `CHANGES_REQUESTED` or `DISMISSED` review is its
    standing; the author excluded case-insensitively; met when any
    remaining approver has write permission, asked through the
    comment's `Authorizer`
  - [x] 3.2 `mergeable`: the pull request already read; when `nil`, up to
    3 further reads 1 second apart, through an injectable sleep; still
    `nil` is unmet as not yet known
  - [x] 3.3 A GitHub error makes its requirement unmet as "could not
    check", and is logged
  - [x] 3.4 The reply text per the design's table, naming every unmet
    requirement in one message
  - [x] 3.5 Table tests for each case in the design's Testing section
    under `approved` and `mergeable`, the retry count and waits asserted
    through the injected sleep
  - _Requirements: 3.1–3.5, 4.1–4.4, 5.1, 5.2_

- [x] 4. The gate in the comment path (`comment.go`, `target.go`)
  - [x] 4.1 `Orchestrator` carries the Requirement_Set from `Config`;
    `cmd/server` wires it
  - [x] 4.2 In `resolve`, after the write-permission check: when the
    Requirement_Set is not empty and a Mutating_Operation Target
    remains, evaluate it, memoized for the comment; unmet withholds the
    command's Mutating_Operation Targets and adds one reply for the
    command; plan Targets are unaffected
  - [x] 4.3 The refusal logged at INFO with the unmet names and the
    commenter
  - [x] 4.4 Tests through `HandleIssueComment`, with recording fakes:
    - a plan runs with requirements unmet, and GitHub is not asked
    - `unlock` runs with requirements unmet
    - an apply with both unmet is withheld: one reply naming both, and
      no Job, Lock event, check run or Pull_Request_Record write
    - an apply with both met runs as today
    - two apply commands in one comment ask GitHub once
    - an empty Requirement_Set asks GitHub nothing
    - a commenter without write permission is refused as today, and
      GitHub is not asked about reviews
  - _Requirements: 5.3–5.5, 6.1–6.4_

- [x] 5. Checkpoint: the whole tree
  - `go build ./...`, `go test -race ./...`, golangci-lint (v2) pass
  - Mutation check: removing the gate call from `resolve` must fail 4.4

- [x] 6. Documentation
  - [x] 6.1 `docs/configuration.md`: the setting in the Server settings
    table, and a section on apply requirements covering Requirement 7.1–7.5
  - [x] 6.2 `docs/troubleshooting.md`: each reply line and what to do
  - [x] 6.3 `SECURITY.md`: without `approved`, one collaborator can plan
    and apply their own pull request; the setting that prevents it
  - _Requirements: 7.1–7.6_

- [x] 7. Final checkpoint
  - Build, tests and lint pass; every acceptance criterion maps to a task
  - Roadmap status updated
  - Any intermittent test failure is recorded in the roadmap Backlog's
    intermittent-failure entry, with the full output kept, rather than
    rerun and dropped
  - Deviations recorded at implementation:
    - A failed permission lookup for one approver does not by itself
      make `approved` unmet: later approvers are still asked, and the
      requirement is "could not check" only when none of them has write
      permission. The design says a failed lookup makes the requirement
      unmet; the gate still fails closed, since the error decides the
      answer only when no approver settles it
    - A context canceled during the bounded mergeability wait is
      "could not check", like a failed read. The design names no case
      for it
    - `TURNIP_APPLY_REQUIREMENTS` collapses a repeated name, and the
      reply lists unmet requirements in a fixed order (`approved`, then
      `mergeable`) however the setting was written
    - The reply quotes the command with its Project selectors
      (`/turnip apply web`), leaving out extra arguments; the design's
      example shows a bare command only
    - `resolve` gained a fifth return value, the command's refusal
      reply, and the gate lives in a helper it calls (`gateMutating`).
      The injectable sleep is an Orchestrator field
      (`requirementSleep`, nil sleeps for real)
    - Two gate tests beyond task 4.4's list: helmfile's `sync` is
      withheld as `apply` is (Requirement 6.1), and a plan in the same
      comment as a withheld apply still runs

- [x] 8. Amendment: named for mutation, not apply
  - [x] 8.1 `TURNIP_APPLY_REQUIREMENTS` renamed `TURNIP_MUTATION_REQUIREMENTS`,
    and the Go identifiers, files (`mutationrequirements*.go`) and log
    message with it. "Apply" is one tool's word, and one of Helmfile's
    operation names, while the gate covers every Mutating_Operation
    (Helmfile's `sync` as much as its `apply`; Pulumi has no apply at
    all). No compatibility alias, pre-1.0
  - [x] 8.2 `docs/usage.md` defines plan commands and mutating commands,
    per tool, and what the split decides; the configuration,
    troubleshooting and `SECURITY.md` sections renamed to match
  - The spec directory and slice id stay `apply-requirements`, as
    historical labels

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1.1", "1.2", "1.3", "2.1", "2.2"] },
    { "id": 1, "tasks": ["3.1", "3.2", "3.3", "3.4", "3.5"] },
    { "id": 2, "tasks": ["4.1", "4.2", "4.3", "4.4"] },
    { "id": 3, "tasks": ["5"] },
    { "id": 4, "tasks": ["6.1", "6.2", "6.3"] },
    { "id": 5, "tasks": ["7"] }
  ]
}
```
