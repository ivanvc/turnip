# Tasks

**The checkpoint that matters is task 4.1.** Each piece can be right on
its own while an author with team access is still refused: the fault
was in which answer the gate trusted, and only a comment taken through
the orchestrator, with GitHub answering as it did for the App on
2026-10-15 (collaborator answer 404, permission answer `write`, a
private repository), shows that the `diff` and then the `apply` run. It
uses the package's fake GitHub client, so it needs no GitHub.

Wave 0 adds the repository's visibility and the role from `role_name`,
in different files of `internal/github`, so they run together. Wave 1
changes `IsCollaborator`'s signature and updates its one caller. Wave 2
separates errors from refusals at the three refusal sites.

## 1. What GitHub tells turnip (`internal/github`)

- [x] 1.1 `Repository` gains `Private`, filled by `repositoryFrom` from
  the webhook payload; a payload without it reads as public (design
  Decision 3). Verify: webhook tests for an issue comment and a pull
  request event on a private and on a public repository, and one whose
  payload has no `private` field; the existing webhook tests
  - _Deltas: authorization (Roles)_
- [x] 1.2 The permission lookup returns `role_name` when it is one of
  GitHub's five standard roles (read, triage, write, maintain, admin),
  and `permission` otherwise (design Decision 2). Verify: client tests
  against a fake GitHub API for each standard role, a custom role over
  each base role (falling back to `permission`), and a response without
  `role_name`; that `HasWritePermission` answers as before for every
  level
  - _Deltas: authorization (Roles)_

## 2. Collaborator status (`internal/github`, `internal/orchestrator`)

- [x] 2.1 `IsCollaborator` takes the repository's visibility and applies
  the rule of design Decision 1: the collaborator answer first, stopping
  on a yes; on a 404, the permission answer through the cache the write
  check reads, any role but none on a private repository, a role above
  read on a public one. The `cacheEntry` comment says this function is
  the one place one answer informs the other. The comment gate passes
  the webhook's visibility. Verify: a table test over the collaborator
  answer (yes, no, error), visibility, and every role including a
  custom one; a counting fake client (a listed author makes one call, a
  team writer who plans and then applies makes two in all); the
  existing authorization tests; `go build ./...`
  - _Deltas: authorization (Roles)_

## 3. Errors are not refusals (`internal/orchestrator`)

- [x] 3.1 The comment gate, the write check for a mutating command and
  the unlock each reply with the delta's error text when GitHub could
  not be asked, and keep their refusal text otherwise; the error is
  logged as today. `docs/using/commands.md` ("Who can trigger what")
  says how a collaborator is recognized, team access included, and why
  `read` on a public repository does not count;
  `docs/using/troubleshooting.md` gains the "could not check" reply
  beside the permission refusals; `docs/operating/github-app.md` says
  team access works with no organization permission. Verify: tests for
  each of the three error replies (the collaborator lookup failing, the
  permission lookup failing for an author the collaborator answer
  leaves out, for an apply and for an unlock) and that each refusal
  text is unchanged; `make docs-check`
  - _Deltas: authorization (Fail closed)_

## 4. Checkpoint: a team writer through the orchestrator

- [x] 4.1 An orchestrator test with the fake GitHub client answering as
  GitHub did for the App: the collaborator answer 404, the permission
  answer `write`, the repository private. The author comments
  `/turnip diff web` and then `/helmfile apply web`: the plan runs, the
  apply runs, and the client was asked each question once per comment.
  The same author on a public repository with `read` is refused as an
  outsider, and with `triage` plans but cannot apply

## 5. Final checkpoint

- [x] 5.1 `go build ./...`, `gofmt -l ./internal ./cmd` clean,
  `go vet ./...`, `make test`, `make lint` pass
- [x] 5.2 Every scenario in the delta maps to a test or a doc change;
  `openspec validate fix-team-collaborators --strict` passes;
  `openspec/roadmap.md` shows the change as implemented
- [x] 5.3 An intermittent failure is recorded here with its full output,
  not rerun away

## Repository owner

- Review and commit
- Deploy a Server built from this change (the Runner is unchanged)
- The author who was refused comments `/turnip diff` on the pilot, and
  it runs; then this change is archived

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1.1", "1.2"] },
    { "id": 1, "tasks": ["2.1"] },
    { "id": 2, "tasks": ["3.1"] },
    { "id": 3, "tasks": ["4.1"] },
    { "id": 4, "tasks": ["5.1", "5.2", "5.3"] }
  ]
}
```

## Deviations recorded at implementation
- 2026-10-15 (2.1): a role the permission answer reports that turnip does
  not recognize (neither a standard role nor a legacy permission, or
  empty) admits no one on either visibility. Decision 1 says "any role
  other than none" on a private repository; read literally, an
  unrecognized string would admit. It is ranked below none instead, the
  stricter reading and the one "Fail closed" already applies to the
  write check. The table test covers it.
- 2026-10-15 (2.1): the existing fixture
  `TestAuthorizer_IsCollaborator_SuccessfulLookupIsNotCollaboration`
  (permission none or read, collaborator answer 404) now passes a public
  repository: on a private one, read through a 404 is a collaborator by
  the new rule.
- 2026-10-15 (3.1): the orchestrator test fake `fakeCommentEventClient`
  no longer answers an empty `permission` with an error from both
  lookups; it answers "not listed" and `none`, as GitHub does for an
  account with no access. An error is now scripted with `collaboratorErr`
  and `permissionErr`. Without this, the existing outsider fixture would
  read as "could not check" under the new reply.
- 2026-10-15 (3.1): the write check in `target.go` had no log line and
  still has none ("logging as today"); only the reply text changed.
- 2026-10-15 (4.1): `fakeCommentEventClient` gained counters for the two
  lookups (`questionCounts`) so the test can state the questions asked per
  comment. The test is `internal/orchestrator/team_collaborator_test.go`.
  The Authorizer is built per comment, so "once per comment" is asserted
  as one collaborator and one permission question per comment (two of
  each over the diff and the apply), not as two permission questions in all.
- 2026-10-15 (fix pass): an internal repository (GitHub Enterprise)
  sends `"private": true` with `"visibility": "internal"`, and every
  enterprise member holds read there, so reading `private` alone admitted
  them. `repositoryFrom` now sets `Private` only when `private` is true and
  `visibility` is `private`; internal, public and a payload without the
  fields take the public rule. `TestWebhookHandler_CarriesRepositoryVisibility`
  gained internal and private-without-visibility rows. The delta's Roles
  requirement names internal repositories and gained the "Enterprise member
  reading an internal repository" scenario; design Decision 3, the proposal,
  `docs/using/commands.md` and `docs/operating/github-app.md` say the same.
- 2026-10-15 (fix pass): the write check in `target.go` now logs its error
  ("checking write permission"), which "Fail closed" requires and the docs
  promise; this replaces the 3.1 note above that it had no log line. It is
  also asked once per command, an error included, so a failed lookup is not
  repeated for every Project. `TestTeamCollaborator_WriteLookupErrorIsAskedOnceAndLogged`
  covers both.
- 2026-10-15 (fix pass): the outsider refusal is now asserted verbatim in
  `comment_test.go` and `team_collaborator_test.go`, and the triage test
  asserts the full apply refusal and that it is not the could-not-check
  text. `docs/using/commands.md` no longer says GitHub's collaborator answer
  counts team access; team access counts through the permission answer.
- 2026-10-15 (final checkpoint): the visibility rule depends on GitHub
  sending `repository.visibility` (a payload without it reads as
  public). A real `issue_comment` delivery to the pilot's App, read back
  through the App's delivery log, carries `private: true` and
  `visibility: private`, so a private repository is judged as private.
- 2026-10-15 (5.3): no intermittent failure was seen; the checkpoint
  passed 20 times under `-race`.
