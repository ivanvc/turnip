# Implementation Plan: Refuse Fork Pull Requests (Slice 15)

## Overview

One ordering constraint dominates this plan, and getting it wrong is not
a degraded state but an outage.

Decision 2 fails closed: an empty `HeadRepo` is foreign. That is the right
default — a payload turnip cannot read should not be executed — but it
means the refusal must never land before the mapping that fills the field.
If it does, every `HeadRepo` is empty, every pull request is foreign, and
turnip refuses everything in the repository. So the field and both
mappings go first, and task 2 exists to prove they populate *while nothing
yet refuses*.

The sentinel lands second, alone. `ErrRefused` needs no producer to be
tested — a stub handler returning it exercises the webhook branch
directly — so it can be verified before anything depends on it. That
matters because the branch it adds sits in front of an existing one that
answers 500, and a mistake there makes GitHub retry deliveries.

The refusals come third, by which point both halves they rely on are
proven. The absence tests come after both paths exist: several of them
assert that *nothing* happened, which has no meaning until there is a path
that would otherwise have done something.

## Tasks

- [x] 1. Head-repository identity on both trigger paths
  - [x] 1.1 Add `HeadRepo` and `IsForeign`
    - `HeadRepo Repository` on `github.PullRequest`, with the doc comment
      from Decision 1 recording that an empty value means GitHub reported
      no head repository — a fork deleted after the pull request was
      opened
    - `func (p *PullRequest) IsForeign(base Repository) bool`, comparing
      `Owner` and `Name`. **Not** the `fork` flag: turnip installed on a
      repository that is itself a fork is an ordinary case, and keying on
      the flag would refuse legitimate work while detecting nothing extra
    - Empty `HeadRepo.Owner` or `HeadRepo.Name` returns true explicitly,
      rather than relying on `""` happening to differ from the base's
      owner — that only holds while the base is non-empty, which is
      accidental rather than guaranteed
    - _Requirements: 1.1, 1.4_
  - [x] 1.2 Map it from both payloads
    - `pullRequestWebhookEvent`: `e.GetPullRequest().GetHead().GetRepo()`
      through the existing `repositoryFrom` mapper
    - `GetPullRequest` (`internal/github/client.go`): the same, from
      `pr.GetHead().GetRepo()`. This is the *only* source on the
      `issue_comment` path — that payload carries a pull request number
      and nothing else
    - Tests that both sources populate `HeadRepo`, including the nil-repo
      case. These are the tests that stand between a correct slice and
      refusing every pull request in the repository
    - _Requirements: 1.2, 1.3_

- [x] 2. Checkpoint - the field populates, and nothing refuses yet
  - `go build ./...` and `go test -race ./...` pass.
  - Behaviour is deliberately unchanged: `IsForeign` has no caller. What
    this proves is that the mapping works *before* anything depends on it
    failing closed.
  - If any existing test starts failing here, the mapping changed
    behaviour it should not have touched — stop and find out why rather
    than continuing to the refusal.

- [x] 3. The refusal sentinel and its webhook branch
  - [x] 3.1 Add `ErrRefused` and answer 200, counting once
    - `ErrRefused` in `internal/github`, with Decision 3's reasoning: a
      handler declined to act on a well-formed, correctly-signed delivery,
      and nothing failed
    - In `ServeHTTP`, test for it **before** the existing
      `if handleErr != nil` branch (`webhook.go:92`), record
      `metrics.WebhookEvent(eventType, "rejected")`, and answer **200**
    - The 200 is the point: the existing branch answers 500
      (`webhook.go:93`), and a 500 makes GitHub retry a delivery turnip
      refused deliberately
    - The `rejected` outcome replaces the `dispatched` at `webhook.go:91`
      rather than adding to it, so one delivery remains one counted
      outcome
    - Testable now, with no producer: a stub handler returning
      `ErrRefused` exercises the branch directly
    - _Requirements: 4.4_

- [x] 4. Checkpoint - a refused delivery is 200 and counted once
  - `go test ./internal/github/` passes, including the new branch.
  - Assert the counter directly: a refused delivery increments
    `webhook_events_total` exactly once, with `rejected` — not twice, and
    not as `dispatched`. This is the assertion that would catch Decision 3
    being undone by someone "simplifying" the branch away.

- [x] 5. Refuse on both trigger paths
  - [x] 5.1 The automatic path
    - In `HandlePullRequest`, refuse before `handlePlanTrigger` — which
      means before `fetchConfig` (`pullrequest.go:54`), because on a
      foreign pull request that file is attacker-controlled and reading it
      is already a step too far
    - Refuse by returning `ErrRefused`, so no Lock, no Job and no check
      run are created and the delivery is counted once
    - **Do not guard the whole handler.** `closed` must still reach
      `handlePRClosed`: it executes nothing, and refusing it would strand
      a Lock a foreign pull request may hold from before this slice
    - Log at `WARN` with base repository, pull request number, head
      repository and the pull request's author
    - _Requirements: 2.1, 2.2, 2.3, 4.1, 4.2, 4.3_
  - [x] 5.2 The comment path
    - In `HandleIssueComment`, refuse after `GetPullRequest`
      (`comment.go:56`) — the first point at which the head repository is
      known — and before `fetchConfig` (`comment.go:61`)
    - Placed after the existing collaborator check on purpose: a
      non-collaborator keeps today's permission message rather than
      silence. Moving it earlier would issue an API call on behalf of an
      unauthorized commenter, and the fact that turnip is installed is
      already public from any comment it has posted
    - The refusal holds whatever the commenter's permission level: the
      collaborator check authorizes the *trigger*, not the *code*
    - Log at `WARN`, with the commenter as the actor
    - _Requirements: 3.1, 3.2, 3.3, 4.1, 4.2, 4.3_

- [x] 6. Checkpoint - refusals hold, nothing else moved
  - `go build ./...`, `go test -race ./...` and golangci-lint pass.
  - An ordinary same-repository pull request still plans. If it does not,
    the mapping is wrong and everything is being treated as foreign —
    which task 2 exists to have ruled out already.

- [x] 7. The absence tests
  - [x] 7.1 The refusal matrix, both paths
    - Same repository runs; different repository refuses; absent head
      repository refuses. Both trigger paths, so six cases
    - _Requirements: 2.1, 3.1, 1.4_
  - [x] 7.2 Assert that nothing happened, positively
    - No Lock acquired, no Job created, no check run, no comment — from
      fakes that **record calls**, never inferred from an absent comment.
      "Did not act" is a claim about what did not happen, and only a
      recording fake can witness it
    - _Requirements: 2.1, 4.1_
  - [x] 7.3 `closed` still releases a Lock on a foreign pull request
    - The regression Requirement 2.3 exists to prevent, and the easiest
      thing to break with an early return placed one line too high
    - _Requirements: 2.3_

- [x] 8. Documentation
  - [x] 8.1 State the refusal and name the log entry
    - turnip runs no Operation on a pull request opened from a fork, on
      either trigger path, and the refusal is silent on the pull request
    - Name the `WARN` entry an operator can look for, and say why it
      matters: repeated refusals mean someone outside the repository is
      attempting to have turnip execute their code
    - _Requirements: 6.1, 6.2_

- [x] 9. Roadmap status
  - Slice 15 to Complete. Record that the installation token in
    `.git/config` — noted in this slice's entry — was deliberately left
    out and still has no slice of its own.
  - _Requirements: 6.1_

- [x] 10. Checkpoint - the slice is done
  - `go build ./...`, `go test -race ./...` and golangci-lint all pass.
  - No configuration exists — environment variable, `turnip.yaml` key, or
    allowed-override path — that permits Operations on a foreign pull
    request. Requirement 5.1 is satisfied by absence, so the check is that
    nothing was added rather than that something works.
  - _Requirements: 5.1_
