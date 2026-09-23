# Implementation Plan: One Check Branch Protection Can Require (Slice 37)

## Overview

Four parts, built bottom-up so each is tested before anything calls it:
the rename, the record, the verdict and publisher, then the wiring into
the sites that already finalize Operations.

**Two GitHub facts are confirmed first**, because the design leans on
them and neither was checked when it was written: that an app can set
`conclusion: skipped`, and that branch protection judges a required check
by the most recent run of that name. If either is wrong, the design changes
before any code depends on it.

**The rename goes early and alone.** It is one line in `checkRunName` and
test assertions, it is independently useful, and landing it separately
keeps the later diffs about the record rather than about names.

**The checkpoint that matters is task 9**: two Operations of one pull
request, finalized on two Orchestrators sharing one Redis, end with one
`turnip` check carrying the verdict of both. Every unit test up to then can
pass with a publisher that only works on a single instance.

No migration: turnip is pre-1.0, and a pull request planned before this
slice deploys simply re-plans.

## Tasks

- [x] 1. Confirm the GitHub behavior the design assumes
  - [x] 1.1 `conclusion: skipped` is settable by an app
    - Check the Checks API reference for create and update. If it is not
      settable, the fallback is `success` titled "no projects affected";
      amend Requirement 6.3 and the design's verdict table before task 5
    - _Requirements: 6.3_
  - [x] 1.2 Which run a required check is judged by
    - Confirm that with several check runs of one name on a commit, branch
      protection evaluates the most recent. Decision 3 and correctness
      property 2 depend on it
    - Confirm a required check that never reported shows as "Expected" and
      blocks the merge — Requirement 6.1's rationale
    - Record the findings, with links, in the design
    - **Found**: `skipped` is settable (only `stale` is reserved); the most
      recently *updated* run of a name is the one evaluated (secondary
      source — GitHub's reference does not say). Recorded under "Confirmed
      GitHub behavior". Not found: any documented way to move a completed
      run back to in progress, which reshaped task 6.1
    - _Requirements: 6.1_

- [x] 2. Rename the per-Project check runs
  - [x] 2.1 `checkRunName` (`execute.go:365`) returns
    `turnip/<operation>/<project>`
    - The four call sites (`execute.go:212`, `:298`, `result.go:87`,
      `sweep.go:108`) already go through it; confirm no test or site
      builds the name by hand
    - _Requirements: 2.1, 2.2, 2.3_
  - [x] 2.2 Update tests asserting the old form

- [x] 3. Checkpoint — the rename changes nothing else
  - `go build ./...`, `go test -race ./...` and golangci-lint (v2) pass
  - The only behavioral difference is the name on each Project_Check

- [x] 4. The Pull_Request_Record
  - [x] 4.1 A store for the record, beside `recordStore`
    - Key `pr-status:<owner>/<repo>#<number>@<head-sha>`, a hash
    - Outcome write: `HSET p:<project>`, `m:mutated` for `applied` and
      `apply_failed`, `HINCRBY m:version`, `EXPIRE` 90 days — one `MULTI`
    - Metadata writes for `m:config` and `m:empty` bump `m:version`; the
      publisher's `m:check_run` and `m:check_done` do not — they are not
      verdict inputs, and bumping would make every publish republish.
      `m:published` was dropped: `m:check_run` being set means the same
    - A Project's field holds JSON `{outcome, operation, tool}`, not a bare
      Outcome, so the summary can name the Project_Check and the tool
    - Read: one `HGETALL`, decoded into Project Outcomes and metadata
    - _Requirements: 3.1, 3.2, 3.4, 3.6_
  - [x] 4.2 The Outcome type and its derivation from `lock.Event`
    - A pure function covering every row of the design's Outcomes table,
      including `EventPlanTimedOut` → not planned and
      `EventMutatingTimedOut` → apply failed
    - _Requirements: 4.1–4.6_
  - [x] 4.3 Tests against miniredis
    - Two Projects written concurrently both survive
    - Writes to different head commits never meet
    - The TTL is set and refreshed

- [x] 5. The verdict
  - [x] 5.1 A pure function from a decoded record to status, conclusion,
    title and summary
    - Rows evaluated in the design's order: invalid config, empty,
      unsupported, apply failed, success, otherwise in progress
    - `success` requires at least one Project
    - Title counts done over total; summary lists each Project with its
      Outcome and Project_Check name, sorted by name
    - _Requirements: 5.1–5.5, 6.3, 7.1, 7.3_
  - [x] 5.2 Whether to publish
    - Publish when `m:published` or `m:mutated` is set, or the verdict is
      completed
    - _Requirements: 6.1, 6.2_
  - [x] 5.3 Table-driven tests: every row, the precedence between rows,
    and the publish test's four cases

- [x] 6. The publisher
  - [x] 6.1 Create or update — **claim dropped, deviation recorded**
    - Task 1.2 found no documented way to reopen a completed check run, so
      leaving a completed verdict creates a new run; several runs per
      commit are then unavoidable, and correctness already rests on GitHub
      reading the most recently updated one. With that given, the creation
      claim only reduced duplicates in a rare race, while adding a Lua
      script, a second key and the crash window the design listed as a
      known limit. Removed; design Decision 5 records why
    - `m:check_done` is a hint for "update or create"; a refused update
      falls back to creating
    - _Requirements: 1.1, 8.1_
  - [x] 6.2 The re-read loop
    - After every create or update, re-read `m:version` and `m:check_run`;
      if either moved, publish again
    - **Cap raised from "a handful" to 100, deviation recorded.** The
      convergence property (6.4) failed intermittently with a cap of five:
      a slow publisher gave up while its own stale verdict was the last one
      GitHub had received. The rounds needed grow with the number of
      Outcomes arriving at once, so the cap is only a bug guard, and
      exhausting it returns an error rather than stopping silently
    - _Requirements: 6.2, 8.1_
  - [x] 6.3 Soft failure
    - Every Redis or GitHub error is returned to the caller as a note, not
      a failure of the Operation
    - _Requirements: 8.2_
  - [x] 6.4 Convergence property test
    - `// Feature: aggregate-check-run, Property 1: Published verdict
      converges`
    - `rapid` over miniredis and a fake GitHub client: two or three
      simulated instances, arbitrary interleavings of Outcome writes and
      publisher steps, including out-of-order GitHub updates. Once writes
      stop, the run GitHub would evaluate carries the verdict of the final
      record. (Not "one run created" — see 6.1.) Stressed at 12,000
      iterations after the 6.2 fix
    - Property 3 (no Outcome but `applied`/`nothing` reaches `success`)
      as a second `rapid` property over the verdict alone

- [x] 7. Record Outcomes where Operations are finalized
  - [x] 7.1 `HandleResult` (`result.go`)
    - Write the Outcome from the `lock.Event` it already computes — before
      or regardless of `locks.Apply`'s result — then publish
    - A publisher note joins the `ProjectResult`, like
      `appendCheckRunNote`
    - _Requirements: 3.2, 3.3, 4.1–4.6, 8.2_
  - [x] 7.2 `reportTimeout` (`sweep.go`)
    - Same, from `EventPlanTimedOut` / `EventMutatingTimedOut`
    - _Requirements: 4.4, 4.6_
  - [x] 7.3 `executeOne` refusals (`execute.go`)
    - The `reject` closure records not planned when `t.Operation` is the
      Plugin's plan Operation, and nothing otherwise
    - `selection.resolve`'s refusals are left alone — assert in a test
      that a comment naming an unknown operation writes nothing
    - _Requirements: 4.4, 4.7_
  - [x] 7.4 Tests
    - A refused apply leaves an `applied` Project `applied`
    - `/turnip unlock` leaves the record untouched
    - _Requirements: 3.5, 4.7_

- [x] 8. The automatic path
  - [x] 8.1 Invalid `turnip.yaml` → `m:config`, publish, then comment as
    today; a missing one stays silent with no record
    - _Requirements: 7.1, 7.2_
  - [x] 8.2 No Project matched → `m:empty`, publish `skipped`
    - _Requirements: 6.3_
  - [x] 8.3 Projects with no Plugin
    - `planTargetsFor` also returns the matched Projects it skipped
    - Each is recorded `unsupported`, and a notice naming the Project and
      tool rides at the top of the plan comment
    - When every matched Project is unsupported, the notice is posted on
      its own
    - _Requirements: 7.3, 7.4_
  - [x] 8.4 Tests for each branch of the design's automatic-path flowchart

- [x] 9. Checkpoint — two instances, one verdict
  - `go build ./...`, `go test -race ./...` and golangci-lint (v2) pass
  - Run against `redis:7-alpine` (CI's image); the new HA tests passed 5×
    their 20 concurrent iterations each
  - **Pre-existing, not fixed here**: `TestHA_PlanAndApplyAcrossInstances…`
    panics — `haFakeClient` lacks `IsCollaborator`, which
    `HandleIssueComment` has called since `e3ebe64`. It only runs with a real
    Redis, and no CI run has covered this branch
  - Extending `ha_test.go`: a pull request with two Projects, each applied
    with its result delivered to a different Orchestrator over one Redis.
    `turnip` ends `success` "2/2"; with one apply failing, `failure`
  - A plan-only run leaves no `turnip` check; a second, narrower re-plan
    after a failure does not turn it green

- [x] 10. Documentation
  - [x] 10.1 `docs/usage.md`: require `turnip`, not the per-Project checks;
    what each state means; that unlocking does not satisfy it
    - _Requirements: 9.1, 9.2, 9.3_
  - [x] 10.2 `docs/troubleshooting.md`: `turnip` absent after a plan is
    expected; `failure` for an unsupported tool; requiring it on a
    repository with no `turnip.yaml` blocks every pull request
    - _Requirements: 9.2, 9.4_
  - [x] 10.3 Update existing references to `turnip/<project>/<operation>`
    in `docs/` and `README.md`
  - [x] 10.4 A release-note line for the owner: per-Project checks renamed
    to `turnip/<operation>/<project>`, and `turnip` is the one to require
    - Handed over in the implementation summary; there is no release-notes
      file in the repository to write it into
    - _Requirements: 9.5_

- [x] 11. Final checkpoint
  - `go build ./...`, `go test -race ./...`, golangci-lint (v2) pass
  - Every acceptance criterion in `requirements.md` maps to a task above
  - Roadmap status updated
