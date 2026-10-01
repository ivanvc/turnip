# Implementation Plan: How the Runner Receives Its GitHub Token (Slice 24)

## Overview

The ordering rule is **value per unit of risk, not document order**.

Requirement 1 depends on nothing and shrinks what a leak on every surface
is worth; it goes first and can ship alone. Requirement 4 is a one-line
removal plus a proto reserve. Requirement 2 — the credential helper and
the fetch RPC — is the change that actually removes the environment
variable, and it is last because it is the only part that touches the
wire protocol, the Server's interceptors and both of the Runner's token
placements at once.

**A gate before Requirement 2.** `NewServer` installs only
`grpc.StreamInterceptor`, so a unary RPC added to it today is reached
with no authentication at all. That is a defect in Slice 25 against its
own Requirement 4.2, and it must be fixed *before* the fetch RPC exists,
not alongside it — otherwise the window in which an unauthenticated
credential endpoint is reachable is however long the rest of task 5
takes.

## Tasks

- [x] 1. The token can do only what the Operation needs
  - [x] 1.1 Give `GenerateInstallationToken` options
    - `Client.GenerateInstallationToken` becomes
      `GenerateInstallationToken(ctx, opts)` carrying repositories and
      permissions, passed through to `ghinstallation.Transport`'s
      `InstallationTokenOptions`. The interface in `internal/github`
      changes with it
    - _Requirements: 1.2_
  - [x] 1.2 Compute the repository set
    - The Operation's own repository, plus submodule repositories read
      from `.gitmodules` at the Operation's commit — the same
      file-at-a-ref call that fetches `turnip.yaml`
    - Entries on another host are left out: they already fail with their
      own error (`submodules.go`), and naming them here would not make
      them reachable
    - _Requirements: 1.1_
  - [x] 1.3 `recursive` omits repositories and still narrows permissions
    - A submodule's own submodules are not knowable before the clone, so
      the set cannot be completed. Requirement 1.4 forbids narrowing in a
      way that would fail a fetch turnip would otherwise have performed
    - Permissions stay `contents: read` in every mode
    - _Requirements: 1.4_
  - [x] 1.4 Scoping failure fails the Operation
    - No fallback to an unscoped token. A silent widening would make the
      control invisible the day it stops working
    - 1.3's deliberate width is not this: the distinguishing question is
      whether turnip ever believed it had the narrow set
    - _Requirements: 1.3_

- [x] 2. Checkpoint - the blast radius is smaller, the shape unchanged
  - `go build ./...`, `go test -race ./...` and golangci-lint pass.
  - The token is still in the Pod spec, deliberately — this task changed
    what it is worth, not where it lives.
  - A Project with a sibling private submodule still clones. That is the
    assertion that 1.1 was implemented as repositories and not as "the
    repository".

- [x] 3. The token is not sent to the Server
  - [x] 3.1 Stop populating it, reserve the field
    - `reporter.go`'s `startMessage` drops `GithubToken`; `github_token`
      leaves `OperationStart` and field 8 is marked `reserved`, so it
      cannot be reused and silently decoded by an older peer
    - `buf generate`, and leave the regenerated output for the repository
      owner to commit
    - _Requirements: 4.1, 4.2, 4.3_

- [x] 4. Close the unary hole before opening a unary endpoint
  - [x] 4.1 A unary interceptor beside the stream one
    - `NewServer` installs `grpc.UnaryInterceptor` sharing the same
      `Authenticator`, so Slice 25's Requirement 4.2 becomes true for
      both kinds of RPC rather than the one its probe happened to use
    - Appended to `runner-authentication/tasks.md` as an amendment
      there, since that slice is Complete
  - [x] 4.2 A unary probe
    - `TestInterceptor_CoversAnRPCItWasNotWrittenFor` registers a
      streaming method, which is why this was invisible. It gains a
      unary sibling asserting the same two things: refused without a
      credential, handed the established Operation id with one
    - The test must fail before 4.1 and pass after

- [x] 5. The Runner fetches its credential
  - [x] 5.1 `FetchCloneCredential`, with an empty request
    - The request message carries no operation id and no arguments. The
      credential is determined by the caller's authenticated binding via
      `rpc.OperationIDFromContext` and by nothing else — an
      implementation cannot read a field that does not exist
    - _Requirements: 2.3, 2.6_
  - [x] 5.2 Mint on the fetch
    - `OperationRecord` already carries `InstallationID`, `Owner`,
      `Repo`, `HeadSHA` and the Project, so this is a move rather than
      new state. The credential stops being live through queueing,
      scheduling, image pulls and tool provisioning
    - _Requirements: 2.4_
  - [x] 5.3 The credential helper
    - A `credential` subcommand on the Runner binary, invoked by git as
      `/runner credential get`, which reads the projected ServiceAccount
      token, calls 5.1, and writes git's credential format to stdout
    - _Requirements: 2.2_
  - [x] 5.4 Both token placements go
    - `clone.go`'s `embedToken` (the authenticated remote URL) and
      `submodules.go`'s token-bearing `GIT_CONFIG_VALUE_n` entries. The
      `insteadOf` rewrites keep their scheme-rewriting half and lose the
      credential
    - _Requirements: 3.1, 3.2, 3.4_
  - [x] 5.5 It leaves the Pod spec
    - `TURNIP_GITHUB_TOKEN` is removed from `internal/jobs/build.go` and
      from the Runner's Config. This is the criterion the whole slice is
      named for
    - _Requirements: 2.1, 2.5_

- [x] 6. Prove it is not merely moved
  - [x] 6.1 The Workspace is scanned, on both paths
    - After a successful clone and after a failed one, no file under the
      Workspace contains the credential. The failure path is the one that
      catches a cleanup-based implementation
    - _Requirements: 3.1, 3.3_
  - [x] 6.2 Nothing carries it but the pipe
    - The git subprocess's argv and environment contain no substring of
      the token. This is the assertion that separates "not persisted"
      from "moved somewhere less obvious", and it fails for four of the
      five mechanisms the design rejected
    - _Requirements: 3.4_
  - [x] 6.3 Reported output is clean
    - Asserted against the Operation's reported output on both paths,
      not against `redact` in isolation. `redact`/`redactArgs` stay: they
      should now have nothing to find, and that is a claim a test makes
      rather than the design asserting it
    - _Requirements: 3.5_

- [x] 7. Checkpoint - the variable is gone
  - `go build ./...`, `go test -race ./...` and golangci-lint pass.
  - A Runner Job's Pod spec contains no credential in any container.
  - **Mutation checks**: returning a caller-named Operation's token must
    fail 5.1's test; minting at dispatch must fail 5.2's; restoring
    `embedToken` must fail 6.1 and 6.2.

- [x] 8. Documentation
  - [x] 8.1 How the credential arrives, and what it is scoped to
    - Including that `recursive` widens the repository scope, so the
      width is visible rather than surprising
    - No claimed protection the mechanism does not provide: not against
      cluster-admin, and not against the tool the repository chooses to
      run
    - _Requirements: 5.1, 5.2, 5.3_

- [x] 9. Amendments and status
  - [x] 9.1 Slice 25 `runner-authentication`
    - The unary interceptor and its probe (task 4), appended there
  - [x] 9.2 Slice 2 `clone-submodules`
    - `submoduleConfigEnv` stops carrying a credential; its host-mismatch
      error and `accessHint` are unchanged, and scoping makes them more
      likely to be hit rather than less accurate
  - [x] 9.3 Roadmap
    - Slice 24 to Complete; the Job TTL finding recorded (a finished Pod
      holds a live credential for 15 minutes, against a token valid for
      60) wherever it lands

**Outcome of task 7** (2026-09-22): `go build ./...`, `go test -race
./...`, golangci-lint v2 and the `kind`-tagged harness all pass. Six
mutants were run and all six killed: a credential served for a
caller-named Operation, dispatch recording no scope, submodule rewrites
carrying a credential again, the helper answering for any host,
permissions no longer narrowed, and the mint using the installation
transport instead of the App's.

The last of those survived its first run — both transports produce a
request the stub answers, so nothing noticed. In production it would
fail, because `POST /app/installations/{id}/access_tokens` needs the
App's JWT and an installation token cannot mint another. Closed by
asserting the mint's Authorization header is a JWT.

**Deviations from the design, recorded rather than hidden:**

- **Redaction was removed, not kept.** `design.md`'s Decision 6 said
  `redact`/`redactArgs` stay as a guard. They are deleted, along with
  `redact_test.go` and `TestRunWith_TokenInToolOutputIsRedacted`, because
  the Runner process no longer holds a credential to redact *against* —
  the helper is a separate invocation and its output goes to git. The
  guarantee moved to assertions that no credential reaches git's argv or
  environment (6.2) and that reported output is clean (6.3), which is
  what the design's own testing strategy asked for.
- **The scope is computed at dispatch, not on the fetch.** The design's
  flow read `.gitmodules` when the credential was requested. It is read
  when the Job is created — where the Project, the commit and the mode
  are already in hand — and stored on the OperationRecord as
  `TokenRepositories`. Requirement 2.4 is about when the token is
  *minted*, which is still the fetch; this avoids a second GitHub API
  call on the clone's critical path.
