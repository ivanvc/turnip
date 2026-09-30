# Implementation Plan: What a Mutating Command Changed (Slice 47)

## Overview

Two independent foundations first: Helmfile counting its tables in
`internal/plugin`, and "unreported" counts rendering in `internal/github`.
Then the Server joins them, asking the Plugin whether a mutating run's
counts mean anything.

**The checkpoint that matters is task 4.** A parser that counts right and
a renderer that says "succeeded" are both worthless if the Server never
connects them: the bug being fixed is exactly a correct renderer shown
the wrong counts. The end-to-end tests go from a Runner result through
`HandleResult` to the comment and the Project_Check Title.

Tasks in the same wave of the dependency graph (at the end) touch
disjoint files and run in parallel. The tree builds after every wave.

## Tasks

- [x] 1. Helmfile counts its mutating commands (`internal/plugin`)
  - [x] 1.1 `parseReleaseTables` over stderr, per the design's Parsing
    table: ANSI removed, the `=`-framed banner only, the header row
    skipped, rows until a blank line or the next banner; Updated and
    Reinstalled count as changed, Deleted as destroyed, Failed and Failed
    to Delete recognized and not counted; summed over every table
  - [x] 1.2 `Execute` calls it for every Operation that is not the plan;
    `diff` keeps `parseChangedReleases` over stdout
  - [x] 1.3 `MutationChangesReported(output string) bool` on the Plugin
    interface; Helmfile returns false when the transcript carries an
    older banner, true otherwise; every test fake implementing Plugin
    (`internal/runner/run_test.go`, `internal/orchestrator/target_test.go`)
    returns true
  - [x] 1.4 Fixture tests per the design's Testing section, with
    placeholder names only; `Execute` through the command-runner seam for
    `apply`, `sync` and `diff`; `MutationChangesReported` for an older
    transcript, a current one, and a current one whose stdout diff holds
    the older banner's text
  - _Requirements: 1.1–1.7, 2.1, 4.1, 4.2, 5.1, 5.2_

- [x] 2. "Unreported" renders as succeeded (`internal/github`)
  - [x] 2.1 `ChangeCounts.Unreported bool`, zero value false
  - [x] 2.2 Each renderer in the design's table: `ChangeText`
    ("succeeded"), the single-Project headline ("Succeeded in `web`."),
    the multi-Project headline and its note ("Succeeded across 3
    projects", "N succeeded"); a failed result still renders as failed
  - [x] 2.3 Tests for each row, single-Project and multi-Project, and a
    mix of counted and unreported results
  - _Requirements: 3.1, 5.3_

- [x] 3. The Server joins them (`internal/orchestrator`)
  - [x] 3.1 Where a mutating Operation's result becomes the
    `ProjectResult`, ask its Plugin `MutationChangesReported` with the
    result's transcript and set `Unreported` on the counts when it says
    false; never for a plan
  - [x] 3.2 `completedTitle` needs no change of its own (it renders
    through `ChangeText`); confirm, and test the Title
  - _Requirements: 5.3–5.5_

- [x] 4. Checkpoint: from a Runner result to what the reader sees
  - [x] 4.1 Tests through `HandleResult`:
    - a successful apply carrying `+0 ~3 -0` reaches the comment's
      summary line and the Project_Check Title
    - with a fake Plugin returning false, a successful apply renders
      "succeeded" in the summary line, the headline and the Title, and
      never "no changes"; its plan still renders counts; a failed apply
      still renders as failed
  - [x] 4.2 `go build ./...`, `go test -race ./...`, golangci-lint (v2)
    pass
  - _Requirements: 4.3_

- [x] 5. Documentation
  - [x] 5.1 `docs/configuration.md`, beside the Helmfile Plugin's
    documentation: counts after an apply or sync need Helmfile v1.6.0 or
    later; older versions show "succeeded"
  - [x] 5.2 `docs/usage.md`, "What you'll see on the PR": what an apply's
    line shows, and when it reads "succeeded" instead of counts
  - _Requirements: 1.8_

- [x] 6. Final checkpoint
  - Build, tests and lint pass; every acceptance criterion maps to a task
  - Roadmap status updated
  - Any intermittent test failure is recorded in the roadmap Backlog's
    intermittent-failure entry, with the full output kept, rather than
    rerun and dropped

  **Deviations recorded at implementation:**
  - `MutationChangesReported` reads the whole transcript (both streams),
    not stderr alone, so it can also see turnip's own command-annotation
    line and Helmfile's argv to detect a silenced log level (`-q`,
    `--quiet`, `--log-level warn` and up), beyond the older banner the
    design named. This extends Requirement 5's "given that run's
    transcript" rather than conflicting with it; the design's own
    accepted trade-off (an older banner's text inside a stdout diff
    hiding a count) already assumed the whole transcript is read there.
  - `resultTitle` and `checkRunResultSummary` were changed to take the
    already-computed `github.ChangeCounts` (with `Unreported` set)
    instead of re-deriving counts from `rpc.ChangeSummary`, so the Title,
    Summary and comment read the same value; the design did not specify
    this signature change but it follows directly from "Confirm, and
    test the Title" in task 3.2.

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1.1", "1.2", "1.3", "1.4", "2.1", "2.2", "2.3"] },
    { "id": 1, "tasks": ["3.1", "3.2"] },
    { "id": 2, "tasks": ["4.1", "4.2"] },
    { "id": 3, "tasks": ["5.1", "5.2"] },
    { "id": 4, "tasks": ["6"] }
  ]
}
```
