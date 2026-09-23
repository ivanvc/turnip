# Implementation Plan: What a Bare Command Targets (Slice 21)

## Overview

Two independent limbs, then a spine.

The limbs go first because neither depends on anything else and both
change what later tasks may assume. Reserving unaddressable names touches
only `internal/config`; it goes first so that every later task can take
"a token containing `*` is a pattern, never a name" as given rather than
hedging. Turning selection into a value is a pure refactor with no
behavioral change at all, and doing it before any behavior lands means
its checkpoint proves exactly one thing — that nothing moved — which is
the only time that proof is cheap.

The spine is selection itself: selectors have to resolve before a bare
command can default to anything, and both defaults have to exist before
the reply that reports an empty one is worth writing.

Tests land after the behavior, for the reason Slice 19 gave and Slice 20
repeated: several of them assert *which* set was selected, which has no
meaning until both selection paths exist. The exceptions are tasks 1 and
3, whose existing tests must move in the same task or their checkpoints
cannot pass.

One ordering trap worth naming. Task 5 must check bare `*` **before** it
evaluates anything as a pattern. Doing it the other way round compiles,
passes a casual reading, and silently breaks Requirement 2.1 for exactly
the repositories that motivated patterns — see Decision 7's measured
table.

## Tasks

- [x] 1. Reserve names no trigger could address
  - [x] 1.1 Reject a Project whose name contains `*` or begins with `-`
    - Two checks in `validate`'s existing per-Project loop
      (`internal/config/validate.go`), beside the duplicate-name check and
      built as ordinary `ValidationError{ProjectRef, Field: "name", ...}`
      values so they accumulate with every other violation rather than
      short-circuiting
    - The message must say *why* the name is unreachable, not merely that
      it is invalid: `*` selects every Project, and a leading `-` is read
      by `indexOfArgStart` as the start of tool arguments
    - `applyDefaults` runs at `parse.go:53`, **before** `validate` at
      `:55`, so `p.Name` is already the effective name here and a
      directory-derived name is covered for free. Do not move either call
    - _Requirements: 3.1, 3.2, 3.3_
  - [x] 1.2 Cover the defaulted-name case explicitly
    - A Project with no `name` whose `directory` is `-infra` must be
      rejected. This is the test that fails if anyone later reorders
      `applyDefaults` and `validate`, and it is the whole reason 1.1 is
      safe to write against `p.Name`
    - _Requirements: 3.1, 3.2_

- [x] 2. Checkpoint - configuration rejects what it must, nothing else changed
  - `go build ./...` and `go test -race ./...` pass. A failure here is a
    fixture using a now-illegal name, not a behavioral problem — fix the
    fixture.

- [x] 3. Selection becomes a value
  - [x] 3.1 Introduce `selection` and move `resolveTargets` onto it
    - The struct as Decision 1 spells it, assembled once in
      `HandleIssueComment` and asked about each command in turn. It
      carries `locks lock.LockManager` — the existing interface, not a
      narrower one invented here
    - `resolveUnlockCandidates` keeps sharing `toolCandidates` and
      `narrowByName`; it needs none of the new fields and should not grow
      them
    - Behavior must not change in this task. `prNumber` and `locks` are
      populated here and read by tasks 7 and 8
    - `modifiedSet` is **not** added here, despite being part of the same
      design decision. A field nothing assigns and nothing reads is
      reported by `unused`, so it lands in task 7.1 together with the code
      that fills it — `prNumber` and `locks` escape that because
      `HandleIssueComment` assigns them even while nothing reads them
    - Existing `target_test.go` callers move in this task, or the
      checkpoint cannot pass
    - _Requirements: 1.1, 4.1_

- [x] 4. Checkpoint - the shape changed and nothing else did
  - `go build ./...` and `go test -race ./...` pass with no test
    *assertions* edited — only construction. If an assertion had to
    change, the refactor took behavior with it; find out what before
    continuing.

- [x] 5. Selectors: `*`, patterns, and refusing the mix
  - [x] 5.1 Resolve bare `*` before any pattern matching
    - `*` is a reserved word meaning every candidate, tested for by exact
      equality and handled before a token is ever treated as a glob
    - The ordering is the requirement, not a style preference:
      `doublestar.Match("*", "gcp/project")` is **false**, so evaluating
      `*` as a pattern silently drops every Project named for its path.
      Decision 7 carries the measured table
    - _Requirements: 2.1, 2.2, 2.5_
  - [x] 5.2 Match any other `*`-bearing token as a name pattern
    - `doublestar.Match(pattern, project.Name)` — the same call
      `projectMatches` makes for `whenModified` (`internal/config/match.go`),
      so one configuration file never carries two glob dialects
    - Patterns compose with names and with each other; the result is the
      union. Only `*` is exclusive, because it already means all of them
    - The tool filter applies first, so `/helmfile plan gcp/*` selects
      Helmfile Projects matching `gcp/*` and nothing else
    - _Requirements: 2.4, 2.6_
  - [x] 5.3 Refuse `*` combined with names
    - `MixedSelectorError` as Decision 3 spells it, rendered by the
      handler branch that already renders `UnmatchedProjectError`
    - Refused during resolution, not in `ParseTriggers`: the line parsed
      correctly and means something specific that turnip declines to do,
      and the malformed-trigger path would tell the reader the opposite
    - _Requirements: 2.3_
  - [x] 5.4 Report an unmatched pattern without failing the command
    - A pattern matching nothing yields the single reply, quoting the
      pattern back — "no project matched `gpc/*`". A named Project that
      does not exist keeps failing the whole command via
      `UnmatchedProjectError`
    - The echo is what makes the asymmetry defensible: a transposed
      pattern is diagnosable at a glance without being treated as a
      failure
    - _Requirements: 2.7, 2.8_

- [x] 6. Checkpoint - selectors resolve, defaults untouched
  - `go build ./...` and `go test -race ./...` pass. A bare command still
    targets every configured Project at this point — that is correct and
    changes in the next two tasks.

- [x] 7. A bare plan targets the Modified_Set
  - [x] 7.1 Add the memoized modified-files seam
    - `selection.modifiedSet` fetches via the existing
      `client.GetModifiedFiles` on first use and caches the result,
      including the error, so one event never pays twice or retries three
      times
    - No mutex. It is safe **because** `HandleIssueComment`'s command loop
      is sequential; record that at the site, since making commands
      concurrent later would turn this into a race
    - _Requirements: 1.1_
  - [x] 7.2 Select the Modified_Set when a plan names no selector
    - `config.MatchProjects` applied to `toolCandidates`, so the tool
      filter and the glob filter compose and the matching is literally the
      automatic plan's own
    - Naming a Project still reaches it whether or not the pull request
      touched it. Requirement 1 narrows the *default*, never an explicit
      request
    - _Requirements: 1.1, 1.2_
  - [x] 7.3 Reply once when nothing matched, and warn on a truncated listing
    - `NoModifiedProjectsError` per Decision 5, rendered by the same
      handler branch. The reply must point at `*`, since the reader typed
      a command and is owed a way forward
    - A listing that comes back holding exactly GitHub's maximum (3000)
      is the truncation signal; add the note to the reply. Name the
      constant rather than spelling `3000` at the comparison
    - The heuristic warns spuriously on a pull request with exactly 3000
      changed files. That is the intended direction to be wrong in
    - _Requirements: 1.3, 1.4_

- [x] 8. A bare apply targets what this pull request planned
  - [x] 8.1 Select Projects whose Lock this PR holds with a plan
    - `GetLockStatus` per candidate, keeping those where `Locked`,
      `PRNumber == prNumber`, and `HasPlan` all hold. No new lock method
      and no schema change
    - One round trip per configured Project, accepted deliberately.
      Isolate the query so that swapping it for Slice 28's `SCAN`-based
      enumeration later touches one function
    - _Requirements: 4.1_
  - [x] 8.2 One reply instead of one refusal per Project
    - `NoPlannedProjectsError` when no candidate qualifies, pointing at
      running a plan first
    - Do **not** narrow the per-Project refusal at
      `internal/orchestrator/execute.go:142`. Naming a Project that holds
      no plan must still be told so individually — that refusal is only
      noise when nobody asked for those Projects by name
    - _Requirements: 4.2, 4.3_

- [x] 9. Checkpoint - both defaults are narrow
  - `go build ./...` and `go test -race ./...` pass. A bare plan and a
    bare apply now select subsets; `*` still reaches everything.

- [x] 10. Tests
  - [x] 10.1 The trigger table, row by row
    - All thirteen rows of the design's table as table-driven cases. A row
      without a test is a row that will drift
    - _Requirements: 1.1, 2.1, 2.3, 2.4, 2.6, 4.1, 4.3_
  - [x] 10.2 Equivalence with the automatic plan, as a property
    - For any generated Project set and file list, bare-plan selection and
      the automatic plan select the same Projects. Tagged
      `// Feature: project-selection, Property: bare plan equals autoplan
      selection`, ≥100 iterations via `rapid.Check`'s default
    - This is how Requirement 1.2 is actually enforced. "Call the same
      function" is reviewable only by eye; a parallel implementation
      cannot pass this
    - _Requirements: 1.2_
  - [x] 10.3 The regression that motivated the reserved word
    - A Project named `gcp/project` must be selected by `*`. This fails if
      anyone ever "simplifies" `*` into a pattern, which is the single
      most plausible future regression in this slice
    - _Requirements: 2.1, 2.5_
  - [x] 10.4 Fetch counts and the single reply
    - A comment with three bare plans performs exactly one
      `GetModifiedFiles`; a comment naming Projects performs none
    - A bare apply across several configured Projects produces one reply
      and zero `ProjectResult`s. The count creeping back up is the
      regression this slice exists to prevent
    - _Requirements: 1.1, 4.2_
  - [x] 10.5 Mutation check
    - Restore "start from every candidate" and confirm 10.2 fails; restore
      pattern-evaluation of bare `*` and confirm 10.3 fails. Then restore
      both byte-identically. A property that still passes is not testing
      what it claims
    - _Requirements: 1.1, 2.5_

- [x] 11. Documentation
  - [x] 11.1 Rewrite what a bare command targets
    - `docs/usage.md:55` is wrong *today* — omitting the list targets every
      Project, matching or not — and `:51` is accurate today and becomes
      wrong with this slice. Both in one pass, or the result is two
      half-truths that disagree
    - Document `*`, name patterns, and that `*` matches within a path
      segment while `**` crosses them, matching `whenModified`
    - _Requirements: 5.1, 5.2, 5.3_
  - [x] 11.2 The two things the parser accepts but nobody can type
    - A Project name containing whitespace parses as two selectors and can
      never be addressed, though the configuration parser accepts it.
      Documented rather than rejected: rejecting would break
      configurations that already parse
    - A trigger line carrying two `*` characters renders as italics in a
      pull request comment; write it in backticks. turnip is unaffected —
      the webhook delivers the raw body — but the author sees something
      other than what they typed
    - Note the 3000-file limit beside `*`, which is the escape hatch when
      a pull request is that large
    - _Requirements: 5.3, 5.4, 5.5_

- [x] 12. Spec amendments this slice owes
  - [x] 12.1 Global Requirement 5.3
    - Rewritten in place: a bare apply targets the Projects this pull
      request holds a plan for, not "all Projects configured in
      turnip.yaml". The global spec carries no amendment markers, so the
      amendment is recorded in the roadmap's Slice 21 details block
    - Requirement 5.4 is untouched — naming a Project still means what it
      said, and `*` stands beside a name rather than changing one
    - _Requirements: 4.1_
  - [x] 12.2 `LockStatus`'s doc comment
    - `internal/lock/lock.go` records that `HasPlan` and `PlanSummary`
      have no production reader. Task 8.1 gives `HasPlan` one, so that
      note becomes false — and a stale "nothing reads this" comment is
      exactly what a later dead-code sweep acts on
    - _Requirements: 4.1_
  - [x] 12.3 Roadmap status
    - Slice 21 to Complete in the slice table, with the Requirement 5.3
      amendment recorded in its details block
    - _Requirements: 4.1_

- [x] 13. Checkpoint - the slice is done
  - `go build ./...`, `go test -race ./...`, and
    `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest run ./...`
    all pass. `docs/usage.md` describes what the code now does, and no
    spec document still describes what it used to.
