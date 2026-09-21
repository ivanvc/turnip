# Implementation Plan: Refuse a Closed Pull Request, Plan a Reopened One (Slice 32)

## Overview

`Open` fails closed, and that decides the order.

A `PullRequest` built without it is a pull request turnip believes is
closed. So the moment the guard exists, every path that does not map the
field refuses — including every test fixture. That is the behaviour we
want in production and the reason the mapping has to land first, with a
checkpoint proving it populates *while nothing yet refuses*. Slice 15 ran
this exact sequence for `HeadRepo` and its task 2 exists for the same
reason.

The fixture churn is therefore expected at task 3, not at task 1: adding
the field breaks nothing, because nothing reads it. Eleven tests broke on
Slice 15's equivalent, and a fixture that does not say the pull request is
open is a fixture describing one that is not — so the fix is to say it,
never to flip the field's sense.

The reopen half is independent of all of this and lands last, because it
is the one part that adds an Operation rather than refusing one, and
keeping it separate keeps its three tests honest.

## Tasks

- [x] 1. The pull request's state reaches both paths
  - [x] 1.1 Add `Open`
    - `Open bool` on `github.PullRequest`, with the doc comment from
      Decision 1 — including that it is false on every `issue_comment`
      event, where only `Number` is populated, and that reading it off the
      event rather than off `GetPullRequest` would refuse every comment
      trigger in the repository
    - _Requirements: 1.1_
  - [x] 1.2 Map it from both payloads
    - `pullRequestWebhookEvent`: `e.GetPullRequest().GetState() == "open"`
    - `GetPullRequest` (`internal/github/client.go`): the same, and the
      only source on the `issue_comment` path
    - `GetMerged()` is deliberately not read: GitHub reports a merged pull
      request as `closed`, so Requirement 1.4 falls out of its model
    - Tests that both sources populate it, including a merged pull request
      mapping to **not** open
    - _Requirements: 1.2, 1.3, 1.4_

- [x] 2. Checkpoint - the field populates, and nothing refuses yet
  - `go build ./...`, `go test -race ./...` and golangci-lint pass.
  - Behaviour is deliberately unchanged: `Open` has no reader. What this
    proves is that both mappings work *before* anything depends on the
    field failing closed.
  - If any existing test fails here, the mapping changed something it
    should not have touched — stop rather than continuing to the guard.

- [x] 3. Refuse on the comment path
  - [x] 3.1 The guard
    - In `HandleIssueComment`, after the fork refusal and before
      `fetchConfig` (`comment.go:84`)
    - **After the fork refusal** so a closed *fork* pull request stays
      silent: replying "this pull request is closed" would hand back the
      signal Slice 15 withholds
    - **Before `fetchConfig`** because everything that acquires or creates
      anything is downstream of it, so one placement satisfies all of
      Requirement 2.3
    - Refuse whatever the commenter's permission level — the guard sits
      after the collaborator check only so a non-collaborator keeps
      today's permission message
    - _Requirements: 2.1, 2.2, 2.3, 2.4_
  - [x] 3.2 Reply, log, and return `ErrRefused`
    - Post one reply saying turnip will not act because the pull request
      is closed — one per Trigger Command, which follows from the guard
      sitting above the per-command loop
    - Log at `INFO`, not `WARN`: this is a colleague on the wrong tab, and
      Slice 15's `WARN` channel is for someone trying to have turnip run
      their code
    - Return `github.ErrRefused` so the delivery answers 200 and counts
      once as `rejected`. Returning nil would count a dispatch that did
      not happen; a plain error would answer 500 and have GitHub
      redeliver, reposting the reply
    - A failed post is logged and still returns `ErrRefused`, deliberately
      unlike the permission refusal at `comment.go:51`
    - _Requirements: 3.1, 3.2, 3.3_
  - [x] 3.3 Update the fixtures the guard now refuses
    - Every test building a `PullRequest` literal that should run sets
      `Open: true`
    - Expected here and not a surprise — see the Overview. The fix is to
      say the pull request is open, never to change what the field means

- [x] 4. Checkpoint - closed refuses, open still runs
  - `go build ./...`, `go test -race ./...` and golangci-lint pass.
  - An ordinary open pull request still plans and applies. If it does not,
    a fixture is describing a closed pull request rather than the guard
    being wrong.
  - **Requirement 4 is verified by tests that already exist**:
    `TestHandlePullRequest_ClosedDraftStillReleasesLocks` and the
    `TestHandlePRClosed_*` family must be green and untouched. Red here
    means the guard can reach the cleanup path.

- [x] 5. Plan a reopened pull request
  - [x] 5.1 `reopened` joins the existing arm
    - `case "opened", "synchronize", "ready_for_review", "reopened":`
    - Nothing else: the matched-Project set comes from
      `handlePlanTrigger`, the draft guard and the fork refusal are
      already at the top of that arm
    - _Requirements: 5.1, 5.2, 5.3_

- [x] 6. Checkpoint - a reopen plans what an open would
  - `go build ./...`, `go test -race ./...` and golangci-lint pass.
  - A reopened pull request plans the Projects its changes match — not
    every configured Project.

- [x] 7. The tests that pin the decisions
  - [x] 7.1 The refusal matrix
    - Open runs; closed refuses; merged refuses. The merged row matters
      most, because it is the one a reader is most likely to assume is
      harmless
    - _Requirements: 2.1, 1.4_
  - [x] 7.2 Ordering: a closed fork pull request is silent
    - Both guards apply; the fork one must win. Nothing else in the suite
      would notice if they were swapped, and swapping them undoes Slice
      15's decision rather than this slice's
    - _Requirements: 3.1_
  - [x] 7.3 Absence, positively
    - A refused comment fetches no configuration, acquires no Lock and
      creates no Job — from fakes that record calls. `getFileCallLog` is
      the strongest witness, since `fetchConfig` is the first thing past
      the guard
    - _Requirements: 2.3_
  - [x] 7.4 Fail-closed is explicit
    - A `PullRequest` with `Open` left at its zero value is refused. This
      pins Decision 1's direction against a future reader who "fixes" the
      fixture churn by flipping the field's sense
    - _Requirements: 1.1_
  - [x] 7.5 The reopen half needs three
    - A reopened pull request plans the matched set; a reopened draft
      plans nothing; a reopened fork pull request is refused. The second
      and third are what prove the arm was *joined* rather than
      duplicated — a separate arm would pass the first and fail these
    - _Requirements: 5.1, 5.2, 5.3_

  - [x] 7.6 Closing still releases Locks, and the guard cannot reach it
    - The existing `TestHandlePullRequest_ClosedDraftStillReleasesLocks`
      and `TestHandlePRClosed_*` family stay green and untouched — the
      same hazard Slice 15's Requirement 2.3 exists to prevent, and the
      same shape: a guard one level too high stops the cleanup as well as
      the Operation, stranding exactly the Locks it was meant to protect
    - Add one test that a `closed` event on a pull request reporting
      `Open: false` still releases, so the property is pinned by a test
      that names this slice's field rather than only by tests that predate
      it
    - _Requirements: 4.1, 4.2_

- [x] 8. Documentation
  - [x] 8.1 Closed, merged, and reopened
    - turnip does not act on a closed or merged pull request, on either
      trigger path, and says so on the pull request when asked
    - Reopening one plans it again, for the Projects its changes match
    - _Requirements: 6.1, 6.2_

- [x] 9. Roadmap status
  - Slice 32 to Complete.

- [x] 10. Checkpoint - the slice is done
  - `go build ./...`, `go test -race ./...` and golangci-lint all pass.
  - Mutation checks on both halves: removing the guard must fail the
    refusal matrix, and removing `reopened` from the arm must fail 7.5.
    Absence assertions come from fakes that record calls, never from state
    that happens to look unchanged.
  - The stranded Lock this slice exists to prevent: a plan triggered by
    comment on a closed pull request no longer acquires one.
