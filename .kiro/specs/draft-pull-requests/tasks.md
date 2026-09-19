# Implementation Plan: Draft Pull Requests (Slice 19)

## Overview

The smallest slice so far: a field, a guard, and one action added to a
switch. The ordering exists for one reason — the field has to be carried
before anything can act on it, and carrying it is invisible, so it lands
first and proves it changed nothing.

After that the whole behaviour is a single task. Splitting the guard from
the `ready_for_review` action would be artificial: they are the same
`case` arm, and the guard is what makes the new action work rather than
something the action must then work around.

Tests come after the behaviour rather than alongside it, because what
they assert is *which path ran* — a question with no meaning until there
are two paths.

## Tasks

- [x] 1. Carry the draft flag
  - [x] 1.1 Add `Draft` to `github.PullRequest` and populate it
    - The field goes beside `HeadSHA`/`BaseRef`/`HeadRef`, populated in
      `pullRequestWebhookEvent` from the payload turnip already receives
      (Decision 1). No API call: asking GitHub for something the webhook
      carried would cost a round trip on the busiest handler there is
    - `issue_comment` events construct a `PullRequest` with `Number`
      alone and are left exactly as they are — that is what makes
      Requirement 4.4 structural rather than a rule to remember
    - Nothing reads the field yet, and its zero value is `false`, so every
      existing pull request behaves identically
    - _Requirements: 1.1, 4.4_

- [x] 2. Checkpoint - the flag is carried and changes nothing
  - `go build ./...` and `go test -race ./...` pass. The field exists and
    is unread, so a failure here is a compilation mistake rather than a
    behavioural one.

- [x] 3. Skip the automatic plan for a draft
  - [x] 3.1 Handle `ready_for_review` on the plan-trigger arm
    - The action joins `opened` and `synchronize`. GitHub sends it with
      the payload's draft field already false, so it needs no special
      case — Decision 2's guard lets it through on its own
    - Without it, skipping drafts would be a trap: a draft marked ready
      would sit with no plan until someone pushed again
    - _Requirements: 2.1, 2.2_
  - [x] 3.2 Add the guard as the arm's first statement
    - Returns before `handlePlanTrigger` is called, so a skipped draft
      costs one webhook and no API call — `handlePlanTrigger`'s first act
      is fetching `turnip.yaml` (Requirement 1.4)
    - **Not before the switch.** That placement would skip `closed` too
      and silently stop releasing Locks, for exactly the pull requests
      most likely to be abandoned. Keeping the guard inside the arm makes
      `closed` unreachable from it by construction rather than by anyone
      remembering
    - Skipping means doing nothing: no Lock, no Job, no comment, no check
      run
    - _Requirements: 1.1, 1.2, 1.3, 1.4, 3.2_

- [x] 4. Checkpoint - end to end
  - `go build ./...` and `go test -race ./...` pass.

- [x] 5. Tests
  - [x] 5.1 `internal/github`: the payload is read correctly
    - A `pull_request` payload with `draft: true` yields `Draft: true`;
      one without yields false. The false case is what protects every
      existing pull request and every existing test
    - _Requirements: 1.1_
  - [x] 5.2 `internal/orchestrator`: which path runs
    - A draft `opened` and a draft `synchronize` reach no plan, asserted
      through **absence of side effects** — no Job created, no Lock
      acquired, no comment posted. Asserting "returns nil" would pass
      even with the guard in the wrong place, which is the whole failure
      this slice is avoiding
    - **`closed` on a draft still releases Locks.** The assertion that
      fails if the guard is ever hoisted out of the arm — the one
      regression Decision 2 exists to prevent
    - `ready_for_review` plans, with `Draft` false as GitHub sends it
    - A comment trigger on a draft executes normally and takes its Lock:
      being a draft changes when turnip acts on its own, not what it can
      be asked to do
    - _Requirements: 1.2, 1.3, 2.1, 3.1, 3.2, 4.1, 4.2, 4.3_

- [x] 6. Documentation
  - [x] 6.1 `docs/usage.md`
    - Drafts are not planned automatically; marking one ready produces a
      plan; a comment trigger works on a draft and takes Locks like any
      other pull request
    - Say plainly that this is not configurable, so a reader does not go
      looking for the setting that does not exist
    - _Requirements: 5.1, 5.2_

- [x] 7. Final checkpoint - full verification
  - `go build ./...`, `go test -race ./...`, `go mod tidy` clean,
    `gofmt -l .` clean, and the real golangci-lint v2 via
    `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest run ./...`

## Notes

- **No new dependencies, no new configuration.** Deliberately no flag:
  Atlantis has `--allow-draft-prs`, turnip does not. Planning work its
  author declared unfinished has no constituency, and anyone who wants it
  can comment.
- **The zero value carries the slice.** `Draft` defaults to false, so
  every existing test, every non-draft pull request, and every payload
  that predates this field behave exactly as before. That is why task 1
  can land on its own and prove it changed nothing.
- **The one regression worth guarding** is the guard drifting out of the
  arm. It is silent, it only affects abandoned drafts, and it is caught
  by exactly one assertion (task 5.2's `closed` case). Worth keeping that
  test's comment explicit about what it is defending.
- **Coverage target**: 80% for touched packages, consistent with prior
  slices.

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1.1"] },
    { "id": 1, "tasks": ["3.1", "3.2"] },
    { "id": 2, "tasks": ["5.1", "5.2"] },
    { "id": 3, "tasks": ["6.1"] }
  ]
}
```
