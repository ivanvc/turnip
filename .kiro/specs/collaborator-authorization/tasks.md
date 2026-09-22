# Implementation Plan: Authorize on Permission Level, Not Call Success (Slice 23)

## Overview

This is a bug slice, so the order is inverted from the usual: **the test
comes first, and task 2's checkpoint requires it to fail.**

That is not ceremony. The existing non-collaborator fixtures return an
*error*, which the broken code already handles correctly — so a regression
test written in the familiar shape passes before the fix and proves
nothing. The only fixture that distinguishes the two answers both
questions independently, and the only way to know it does is to watch it
fail against the current code before changing any of it.

Everything after that is small. `GitHubClient` already declares
`IsCollaborator` and `Client` already implements it against the 204/404
endpoint, so there is no new client method and no interface change — the
correct implementation has been sitting unused, and this slice's fix is to
start calling it.

The fixture churn is predictable and loud: comment-path fakes embed
`github.GitHubClient` and implement only the methods their tests reach, so
the first call to `IsCollaborator` panics on a nil embedded interface
rather than failing quietly. Expected at task 4, not before.

## Tasks

- [x] 1. The test that proves the defect
  - [x] 1.1 A fixture that answers both questions independently
    - A client fake returning `("none", nil)` from
      `GetCollaboratorPermission` **and** `(false, nil)` from
      `IsCollaborator` — the shape GitHub sends for an account that is not
      a collaborator
    - Asserted at the Authorizer: `IsCollaborator` must report false
    - _Requirements: 6.1, 6.2_
  - [x] 1.2 The same refusal at the Trigger Command level
    - A comment from that account must be refused with the permission
      reply, and must reach no configuration fetch, no Lock and no Job
    - A helper returning false proves nothing if the caller ignores it
    - _Requirements: 6.3_

- [x] 2. Checkpoint - the new test fails, and that is the point
  - `go test ./internal/github/ ./internal/orchestrator/` — task 1's tests
    **must fail** against the unfixed code, and the rest must pass.
  - If task 1's tests pass here, they are testing the machinery around the
    defect rather than the defect. Rewrite them before continuing: a
    regression test that never failed is a regression test that never will.

- [x] 3. The Authorizer asks the endpoint that answers
  - [x] 3.1 `IsCollaborator` calls the client method
    - `Authorizer.IsCollaborator` calls `client.IsCollaborator` rather than
      inferring status from a successful permission lookup
    - No interface change: `GitHubClient` already declares it
      (`client.go:24`) and `Client` already implements it
    - go-github maps the 404 to `(false, nil)` via `parseBoolResponse`, so
      "not a collaborator" arrives as a clean false and any other failure
      arrives as an error
    - _Requirements: 1.1, 1.3, 7.1_
  - [x] 3.2 Not a threshold
    - The decision SHALL NOT be `permissionRank[perm] >= …`. That would
      close the observed case and stay wrong in kind: the permission
      endpoint reports what access an account has, and access is not
      evidence of trust
    - _Requirements: 1.2_
  - [x] 3.3 One cache, two answers
    - The entry carries collaborator status and permission level
      independently, each filled when first asked, sharing one key and one
      TTL
    - _Requirements: 4.1, 4.2_

- [x] 4. The fixtures answer the new question — **no churn needed**
  - The comment-path fake gained one `IsCollaborator` method whose default
    is "was a permission configured", which is precisely what every
    fixture written before the gate asked this question meant by it. So no
    existing fixture changed.
  - **The design predicted churn and was wrong about it**, in the safe
    direction. It reasoned that fakes embedding `github.GitHubClient`
    would panic on a nil interface — true if the method is left
    unimplemented, avoidable by implementing it once with a default that
    matches the fixtures' intent.
  - Two existing Authorizer cache tests did change, for a real reason:
    they counted calls to the *permission* endpoint to prove caching, and
    `IsCollaborator` no longer touches it. They now count the endpoint
    they actually measure, and assert the permission endpoint is not
    called at all.

- [x] 5. Checkpoint - the defect is closed and nothing else moved
  - `go build ./...`, `go test -race ./...` and golangci-lint pass.
  - Task 1's tests now pass. They failed at task 2 and pass here, which is
    the whole evidence that this slice fixed something.
  - **The write gate is a bystander that must not move**: the existing
    `HasWritePermission` tests stay green untouched, and `none`, an
    unrecognised string and `read` all remain insufficient.
  - _Requirements: 3.1, 3.2_

- [x] 6. The answers turnip cannot interpret
  - [x] 6.1 An error refuses, and stays an error
    - A client error must produce an error from the Authorizer, not a
      quiet false. An under-permissioned App installation would otherwise
      be indistinguishable from a repository where nobody is a
      collaborator, and the operator has no way to tell those apart
    - GitHub requires write, maintain or admin to read collaborator
      information, so this is the failure mode most likely to be met
    - _Requirements: 2.1, 2.3_
  - [x] 6.2 An unrecognised permission is insufficient — **already pinned**
    - `permissionRank` returns 0 for an unknown string, so a role GitHub
      adds later is already treated as insufficient. Pinned by a test so a
      later "unknown means read" convenience reads as the weakening it is
    - _Requirements: 2.2_
  - [x] 6.3 The cost stays bounded
    - Asserted on call count against a recording fake: two questions about
      one account within the TTL make at most one call each, and a second
      Trigger Command from the same author makes none
    - _Requirements: 4.1, 4.2_

- [x] 7. The premise is corrected where it was written
  - [x] 7.1 `github-integration/design.md`
    - The claim that the permission endpoint "returns 404 for a
      non-collaborator" is corrected in place, recording that the 404
      belongs to `/collaborators/{username}` and that reading it otherwise
      produced a gate which admitted every account GitHub would answer
      about
    - Note alongside it that two supporting claims from the roadmap — that
      the endpoint reports `none` for a non-collaborator, and `read` for
      any user on a public repository — were checked and **neither was
      confirmed**; the defect does not depend on either
    - _Requirements: 5.1, 5.2_
  - [x] 7.2 Recorded in the roadmap
    - Slice 23's entry records the correction, per the convention the
      global spec already uses
    - _Requirements: 5.3_

- [x] 8. Documentation
  - [x] 8.1 Collaborator status, not read access
    - `docs/usage.md` already carries the role table written while
      speccing this slice. Confirm it matches what the code now does, and
      that nothing in it describes a slice that has not landed
    - _Requirements: 8.1_

- [x] 9. Roadmap status
  - Slice 23 to Complete.

- [x] 10. Checkpoint - the slice is done
  - `go build ./...`, `go test -race ./...` and golangci-lint all pass.
  - **Mutation check**: reverting `Authorizer.IsCollaborator` to infer from
    a successful permission lookup must fail task 1's tests. That is the
    same assertion task 2 made by hand, made permanent.
  - A second mutation: replacing the call with a `>= read` threshold must
    also fail, so that Requirement 1.2 is pinned rather than merely
    written down.
  - `Client.IsCollaborator` now has a production caller, so the dead-code
    sweep that would have removed it no longer applies.
