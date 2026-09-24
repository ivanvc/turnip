# Implementation Plan: A Blocked Operation Blocks the Merge, Visibly (Slice 36)

## Overview

The bug goes first, proven before it is fixed: tests assert that a
failure saving the Operation Record (`execute.go:278`) or building the
Runner Job (`execute.go:313`) completes the Project_Check — for a plan and
for an apply — and all must fail against today's code. Then the
vocabulary — titles, the record's new Outcome and fields, the verdict —
which nothing calls yet. Then the one
change that uses them all: `reject` takes a typed refusal.

**The checkpoint that matters is task 7.** Every refusal site is
classified in the design's table, and a misclassified one looks fine in
isolation — a Lock_Wait reported as a failure still produces a check. The
checkpoint walks the table, one test per row, so a wrong kind shows up as
a wrong status or Title rather than not at all.

## Waves

Tasks within a wave touch disjoint files and run in parallel; a wave
starts when the previous one is done.

| Wave | Tasks | Files |
|---|---|---|
| 1 | 1 (prove the bug) and 2 (titles), in parallel | a new test file; `titles.go`, `titles_test.go` |
| 2 | 3 and 4 (record, verdict), then checkpoint 5 | `prstatus.go`, `verdict.go` and their tests |
| 3 | 6 (the typed refusal) | `execute.go` |
| 4 | 7 (site-table tests) and 8 (docs), in parallel | a new test file; `docs/` |
| 5 | review against the requirements, fixes, then checkpoint 9 | as findings require |

## Tasks

- [x] 1. Prove the stuck-check bug
  - [x] 1.1 A test where saving the Operation Record fails after the
    Project_Check was created, asserting the check is completed as
    `failure`
  - [x] 1.2 The same for building the Runner Job
  - [x] 1.3 Both, for a plan and for an apply — the check is created for
    every Operation, so an apply gets stuck the same way. The apply cases
    also assert the Pull_Request_Record is unchanged
  - [x] 1.4 Run them against today's code and record that they fail
  - _Requirements: 4.4_

- [x] 2. The Titles
  - [x] 2.1 In `titles.go`: `lockWaitTitle`, `notPermittedTitle`,
    `refusedTitle`, `lockNotAcquiredTitle`, `recordNotSavedTitle`,
    `jobNotBuiltTitle`
  - [x] 2.2 Table rows for each in the existing title tests, including
    `lockWaitTitle(0)` and `refusedTitle`'s "and N more"
  - _Requirements: 1.2, 1.3, 3.2, 3.4, 4.2, 5_

- [x] 3. The record
  - [x] 3.1 `OutcomeRefused`; `ProjectEntry` gains `BlockedBy` and
    `Setting`, both `omitempty`
  - [x] 3.2 A test that an entry written without them decodes unchanged
  - _Requirements: 1.4, 3.3_

- [x] 4. The verdict
  - [x] 4.1 `refused` fails the verdict, ranked below `unsupported` and
    above a failed apply, with `refusedTitle` naming the first refused
    Project by name
  - [x] 4.2 The summary's lines for `not_planned` with a blocking pull
    request, and for `refused` with its setting
  - [x] 4.3 Tests for the precedence, the Title and both summary lines;
    `TestProperty_OnlyDoneOutcomesSucceed` gains `refused` among the
    outcomes it draws
  - _Requirements: 1.4, 3.3, 3.4_

- [x] 5. Checkpoint — the vocabulary compiles and nothing calls it yet
  - `go build ./...`, `go test -race ./...`, golangci-lint (v2) pass,
    except task 1's tests, which still fail

- [x] 6. The typed refusal
  - [x] 6.1 `refusal` and `refusalKind`; `reject` takes a `refusal`
  - [x] 6.2 Declare the check run's ID before `reject`, so the closure can
    update a check that already exists
  - [x] 6.3 In `reject`: create the Project_Check by kind for a plan;
    complete a check that already exists as `failure` for **any**
    Operation; record the Outcome by kind for a plan only — per the
    design's two tables
  - [x] 6.4 Classify every call site per the design's sites table; the two
    override refusals by `errors.As` on their error types, their setting
    from the existing override constants
  - [x] 6.5 Move `:327`'s inline check update into `reject`
  - [x] 6.6 A GitHub error creating or updating a refusal's check is noted
    on the result, as `appendCheckRunNote` does
  - _Requirements: 1.1–1.4, 2, 3.1–3.3, 3.5, 4.1–4.4, 7_

- [x] 7. Checkpoint — every row of the sites table
  - One test per row of the design's sites table, asserting the
    Project_Check created or updated (status, conclusion, Title) and the
    Outcome recorded
  - `:175` and `:177` differ only in the blocking pull request in the Title
  - A refused Mutating_Operation and a tool with no Plugin create no check
  - Task 1's tests now pass, plan and apply
  - Refusals of the whole trigger (fork, closed, non-collaborator) still
    create no check run — the existing tests for each, unchanged, plus
    `TestWholeTriggerRefusals_CreateNoCheckRun`, added after the final
    checkpoint found 6.2 covered only indirectly. It carries a control case
    (an open pull request, which does create the queued check), because
    its first version passed vacuously: a bare `/turnip diff` targeted no
    Project at all
  - `TestRefusalSite_LaterPlanReplacesTheQueuedCheck` covers Requirement 2,
    also added after the checkpoint found it untested
  - `go build ./...`, `go test -race ./...`, golangci-lint (v2) pass
  - _Requirements: 1–4, 6, 7_

- [x] 8. Documentation
  - [x] 8.1 `docs/usage.md`: the queued Project_Check in the title table,
    and that turnip does not re-plan on its own when a Lock is released
  - [x] 8.2 `docs/troubleshooting.md`: each refusal Title and what to do;
    the `turnip` check's `not permitted: …` Title
  - _Requirements: 8_

- [x] 9. Final checkpoint
  - Build, tests and lint pass; every acceptance criterion maps to a task
  - Roadmap status updated
  - Any intermittent test failure is recorded in the roadmap Backlog's
    intermittent-failure entry, with the full output kept, rather than
    rerun and dropped. None occurred: three full `go test -race ./...`
    runs passed
  - **Deviations recorded**: a fallback Title, `operation could not be
    started` (`notStartedTitle`), completes an existing check for a refusal
    that names no step, so a later refusal site cannot leave a check stuck.
    Review also found a Runner that dies after starting leaves its check in
    progress; it predates this slice and is not a refusal, so it is
    recorded in design.md ("A known gap") and the roadmap Backlog rather
    than fixed here
