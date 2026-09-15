# Implementation Plan: GitHub Client & Webhook Handler (Slice 4)

## Overview

This plan implements `internal/github` per `design.md`. Independent leaves
(`types.go`, `errors.go`) land first, followed by the four components that
depend only on those leaves plus external libraries (`parser.go`,
`client.go`, `webhook.go`, `comment.go` — none of these four depend on each
other), then `auth.go` (needs `client.go`'s `Client` type) and
`authorize.go` (needs `client.go`'s `GitHubClient` interface), then the
package doc update, then tests (unit, then property).

## Tasks

- [x] 1. Add dependencies
  - [x] 1.1 Add `github.com/google/go-github/v90` and `github.com/bradleyfalzon/ghinstallation/v2`
    - Add `github.com/google/go-github/v90` as a new direct dependency
    - Add `github.com/bradleyfalzon/ghinstallation/v2` as a new direct dependency
    - Run `go mod tidy` and verify it produces no further changes once the packages below import them (matching the `redis-lock-manager` slice's precedent: `go mod tidy` strips an unused-so-far dependency back out, so this step's "no further changes" check is only meaningful after task 7)
    - _Requirements: (dependency infrastructure, no direct requirement)_

- [x] 2. Implement data model
  - [x] 2.1 Create `internal/github/types.go`
    - Define `WebhookEvent`, `Repository`, `PullRequest`, `Comment`, `Installation`, `CheckRunOptions`, `TriggerCommand`, `ProjectResult` exactly per design.md's "Data Model" section
    - Verify compilation with `go build ./internal/github/...`
    - _Requirements: 2.3, 3.4, 4.1, 6.3, 7.1_

- [x] 3. Implement package errors
  - [x] 3.1 Create `internal/github/errors.go`
    - Define `ErrFileNotFound`, `ErrNoTrigger`, `ErrMalformedTrigger` as package-level `errors.New` sentinels
    - Define `MalformedTriggerError` (`Line int`, `Content string`) with `Error() string` and `Unwrap() error` returning `ErrMalformedTrigger`
    - Define `MalformedTriggerErrors []*MalformedTriggerError` with `Error() string` joining each element's message (mirroring `internal/config`'s `ValidationErrors`) and `Is(target error) bool` returning `target == ErrMalformedTrigger`
    - Verify compilation with `go build ./internal/github/...`
    - _Requirements: 3.2, 4.7, 4.8_

- [x] 4. Checkpoint - Verify types and errors compile
  - Ensure `go build ./internal/github/...` succeeds. Ask the user if questions arise.

- [x] 5. Implement comment trigger parser
  - [x] 5.1 Create `internal/github/parser.go`
    - Implement `ParseTriggers(body string) ([]*TriggerCommand, error)` per design.md's grammar: scan every line of `body` (trimmed) independently — not stopping at the first match — matching `/{turnip|<tool name>} <operation> [project ...] [-- extra args]`; `tool` is an unconstrained token, not a hardcoded set (see design.md's grammar note)
    - A line not starting with `/<token>` at all is silently skipped (not a candidate)
    - A well-formed line (`tool` and `operation` both present) appends one `TriggerCommand` to the result, in line order; a malformed line (`tool` present, no `operation`) appends one `*MalformedTriggerError{Line, Content}` to an accumulator instead of aborting the whole parse — mirroring `internal/config`'s validate() pattern of accumulating every problem in one call
    - Tokens between operation and the first `--` (or line end) populate that line's `Projects`; tokens after the first `--` populate `ExtraArgs` verbatim, including any literal `--` among them
    - Do not validate `Tool` or `Operation` against any known tool/operation list (no dependency on `internal/plugin` or a Plugin registry)
    - Return per design.md's table: `(nil, ErrNoTrigger)` when nothing matched at all; `(nil, MalformedTriggerErrors{...})` when only malformed lines were found; `(commands, nil)` when only well-formed lines were found; `(commands, MalformedTriggerErrors{...})` when both occurred
    - Verify compilation with `go build ./internal/github/...`
    - _Requirements: 4.1, 4.2, 4.3, 4.4, 4.5, 4.6, 4.7, 4.8_

- [x] 6. Implement the GitHub REST client
  - [x] 6.1 Create `internal/github/client.go`
    - Define the `GitHubClient` interface and `Client` struct exactly per design.md's "GitHub Client" section, aliasing the import as `gh "github.com/google/go-github/v90/github"`
    - Implement `GenerateInstallationToken` via `itr.Token(ctx)`
    - Implement `GetFile` via `gh.Repositories.GetContents`, requiring a non-nil `fileContent` (erroring on a directory path) and decoding via `.GetContent()`; map a 404 `*gh.ErrorResponse` to `ErrFileNotFound` wrapped with owner/repo/path/ref context
    - Implement `GetModifiedFiles` via `gh.PullRequests.ListFiles`, paginating with `ListOptions{PerPage: 100}` while `resp.NextPage != 0`, collecting `.GetFilename()` across all pages
    - Implement `GetPullRequest` via `gh.PullRequests.Get`, mapping into this package's `PullRequest`
    - Implement `CreateCheckRun`/`UpdateCheckRun` via `gh.Checks.CreateCheckRun`/`UpdateCheckRun`, converting each `CheckRunOptions` field to a `*string` that is `nil` when the field is empty (so GitHub applies its own defaults) and non-nil otherwise
    - Implement `PostComment`/`UpdateComment` via `gh.Issues.CreateComment`/`EditComment`
    - Implement `IsCollaborator` via `gh.Repositories.IsCollaborator`
    - Implement `GetCollaboratorPermission` via `gh.Repositories.GetPermissionLevel`, returning `.GetPermission()`
    - Verify compilation with `go build ./internal/github/...`
    - _Requirements: 1.2, 3.1, 3.2, 3.3, 6.1, 6.2, 6.3, 7.5_

- [x] 7. Checkpoint - Verify parser and client compile, dependencies settle
  - Ensure `go build ./internal/github/...` succeeds and `go mod tidy` produces no changes now that `go-github` is imported. Ask the user if questions arise.

- [x] 8. Implement GitHub App authentication
  - [x] 8.1 Create `internal/github/auth.go`
    - Define `AppAuth` wrapping a `*ghinstallation.AppsTransport`
    - Implement `NewAppAuth(appID int64, appPrivateKeyPEM []byte) (*AppAuth, error)` via `ghinstallation.NewAppsTransport`, propagating its parse error directly (Requirement 1.4)
    - Implement `(a *AppAuth) InstallationClient(installationID int64) *Client` via `ghinstallation.NewFromAppsTransport(a.appsTransport, installationID)`, wrapping the resulting `*ghinstallation.Transport` in a `gh.Client` (`gh.NewClient(gh.WithTransport(itr))`) and this package's `Client`
    - Verify compilation with `go build ./internal/github/...`
    - _Requirements: 1.1, 1.2, 1.3, 1.4_

- [x] 9. Implement collaborator authorization
  - [x] 9.1 Create `internal/github/authorize.go`
    - Define `Authorizer` wrapping a `GitHubClient` and an unexported TTL-cached-permission map guarded by `sync.Mutex`, keyed by `owner+"/"+repo+"/"+username`, plus an unexported `now func() time.Time` field defaulting to `time.Now`
    - Implement `NewAuthorizer(client GitHubClient) *Authorizer`
    - Implement an unexported `permission(ctx, owner, repo, username string) (string, error)` that serves a live cache entry (age < 5 minutes) or calls `client.GetCollaboratorPermission` and caches the result with `now()`-based expiry
    - Implement `IsCollaborator` (permission fetch succeeds, i.e. no error) and `HasWritePermission` (permission ranks `write` or higher: `write`, `maintain`, `admin`) in terms of `permission`
    - Verify compilation with `go build ./internal/github/...`
    - _Requirements: 5.1, 5.2, 5.3, 5.4_

- [x] 10. Implement the webhook handler
  - [x] 10.1 Create `internal/github/webhook.go`
    - Define `EventHandler` interface: `HandlePullRequest(ctx, *WebhookEvent) error`, `HandleIssueComment(ctx, *WebhookEvent) error`
    - Implement `NewWebhookHandler(secret []byte, handler EventHandler) http.Handler` per design.md's five-step sequence: `gh.ValidatePayload` (401 on failure), `gh.WebHookType` (200 no-op for non-pull_request/issue_comment types), `gh.ParseWebHook` (400 on failure), map to `*WebhookEvent` (200 no-op for an `issue_comment` where `!event.Issue.IsPullRequest()`), dispatch to the matching `EventHandler` method (500 on a returned error)
    - Verify compilation with `go build ./internal/github/...`
    - _Requirements: 2.1, 2.2, 2.3, 2.4, 2.5, 2.6_

- [x] 11. Implement the consolidated comment builder
  - [x] 11.1 Create `internal/github/comment.go`
    - Implement `BuildConsolidatedComment(results []ProjectResult) []string` per design.md's format: a summary table (once, first body only) plus one `<details>` section per result with its `Output` in a fenced code block
    - Pack sections greedily into the first body until the next section would exceed `maxCommentLength = 65536`; start a new body (prefixed `_(continued N/M)_` instead of the table) for overflow
    - Truncate the fully-assembled body (table/header + joined sections), not each section individually, appending `[output truncated]` as the literal last thing written — truncating a section alone before assembly risks the table/continuation-header overhead pushing the assembled body back over the limit and clipping the marker itself (see design.md's "Truncation happens once, at final body assembly" note)
    - Verify compilation with `go build ./internal/github/...`
    - _Requirements: 7.1, 7.2, 7.3, 7.4_

- [x] 12. Update package documentation
  - [x] 12.1 Replace the Slice 0 placeholder in `internal/github/doc.go`
    - Replace the "Slice 4" placeholder doc comment with one describing the package's actual purpose (GitHub App auth, webhook verification/parsing, trigger comment parsing, collaborator authorization, check runs, and consolidated PR comments)
    - Verify `go build ./...` still succeeds for the whole module
    - _Requirements: (documentation, no direct requirement)_

- [x] 13. Checkpoint - Verify library compiles end-to-end
  - Ensure `go build ./...` succeeds and `go vet ./internal/github/...` reports no issues. Ask the user if questions arise.

- [x] 14. Write unit tests
  - [x] 14.1 Write `internal/github/parser_test.go`
    - Cover each single-command example from design.md/the global design's `CommentParser` sketch (`/turnip plan`, `/terraform apply vpc-project`, `/helmfile sync`, `/pulumi preview`, `/turnip plan -- -destroy`) each producing a one-element `[]*TriggerCommand`
    - Cover multiple project tokens, multiple extra-arg tokens, and a literal `--` appearing among the extra args (only the first `--` on a line is that line's delimiter)
    - Cover a comment with explanatory text before, between, or after trigger lines — only the `/`-prefixed lines produce `TriggerCommand`s, everything else is skipped
    - Cover a non-trigger comment (no line starts with `/<token>`) returning `(nil, ErrNoTrigger)`
    - Cover `/turnip` alone (no operation) returning `(nil, MalformedTriggerErrors)` of length 1, and confirm `errors.Is(err, ErrMalformedTrigger)` is true
    - Cover a leading word that is neither `turnip` nor a real IaC tool (e.g. `/deploy plan`) still parsing successfully as one `TriggerCommand{Tool: "deploy", Operation: "plan"}` — this package has no Plugin registry to reject it against
    - Cover two well-formed lines in one body (e.g. `/turnip plan project-1` and `/turnip plan project-2`) producing two `TriggerCommand`s in line order — the Atlantis-vs-this-platform batching behavior driving Requirement 4's user story
    - Cover a well-formed line, a malformed line, and another well-formed line (in that order) in one body producing both `TriggerCommand`s (for the two well-formed lines, in order) *and* a non-nil `MalformedTriggerErrors` of length 1 for the middle line — the malformed line must not discard the two well-formed ones
    - _Requirements: 4.1, 4.2, 4.3, 4.4, 4.5, 4.6, 4.7, 4.8_

  - [x] 14.2 Write `internal/github/client_test.go`
    - Using an `httptest.NewServer`-backed `*gh.Client` (per design.md's Testing Strategy), cover each `GitHubClient` method against a hand-built `http.ServeMux` route: request method/path/body assertions plus response decoding into this package's return types
    - Cover `GetFile` on a 404 response mapping to `ErrFileNotFound`, and on a directory-content response returning a non-`ErrFileNotFound` error
    - Cover `GetModifiedFiles` paginating across at least two pages (mux serving `Link`/`page` semantics matching `go-github`'s `Response.NextPage`)
    - Cover `CreateCheckRun`/`UpdateCheckRun` with an empty `CheckRunOptions` field omitted from the request body JSON (nil pointer) and a non-empty field present
    - _Requirements: 1.2, 3.1, 3.2, 3.3, 6.1, 6.2, 6.3, 7.5_

  - [x] 14.3 Write `internal/github/auth_test.go`
    - Generate a throwaway RSA key at test time with `crypto/rsa.GenerateKey` (never a committed fixture), PEM-encode it, and cover `NewAppAuth` succeeding with a valid key
    - Cover `NewAppAuth` returning an error for a malformed/garbage PEM value, without panicking
    - Cover `InstallationClient` returning a non-nil `*Client` whose embedded `gh.Client` is non-nil
    - _Requirements: 1.1, 1.4_

  - [x] 14.4 Write `internal/github/authorize_test.go`
    - Using a fake `GitHubClient` (test-local struct recording calls and returning a scripted permission/error), cover `IsCollaborator` true/false and `HasWritePermission` true (`write`/`maintain`/`admin`) / false (`none`/`read`/`triage`/unrecognized string)
    - Cover a second call within 5 minutes for the same `(owner, repo, username)` hitting the cache (fake client's call count stays at 1)
    - Cover a call after the injected `now` seam advances past 5 minutes re-fetching (fake client's call count increments)
    - Cover concurrent calls for different usernames not racing (run under `-race`)
    - _Requirements: 5.1, 5.2, 5.3, 5.4_

  - [x] 14.5 Write `internal/github/webhook_test.go`
    - Using fixture JSON payloads shaped like real `pull_request`/`issue_comment` deliveries, HMAC-sign each with a test secret, and post them at `NewWebhookHandler`'s `http.Handler`
    - Cover a valid `pull_request` payload dispatching to `HandlePullRequest` with a correctly populated `*WebhookEvent`, responding 200
    - Cover a valid `issue_comment` payload on a PR dispatching to `HandleIssueComment`, responding 200
    - Cover a valid `issue_comment` payload on a plain issue (`issue.pull_request` absent) not dispatching, responding 200
    - Cover an invalid signature responding 401 without dispatching (fake `EventHandler`'s call count stays at 0)
    - Cover an unrelated event type (e.g. `ping`) responding 200 without dispatching
    - Cover a fake `EventHandler` returning an error responding 500
    - _Requirements: 2.1, 2.2, 2.3, 2.4, 2.5, 2.6_

  - [x] 14.6 Write `internal/github/comment_test.go`
    - Cover a small result set producing exactly one body containing the summary table and every project's `<details>` section
    - Cover a result set whose combined content exceeds `maxCommentLength` splitting into multiple bodies, each valid standalone markdown, with the table only in the first
    - Cover a single `ProjectResult.Output` alone exceeding `maxCommentLength` being truncated with `[output truncated]` rather than looping
    - Cover an empty `results` slice returning either `nil` or a single header-only body (pick one and assert it) rather than panicking
    - _Requirements: 7.1, 7.2, 7.3, 7.4_

- [x] 15. Checkpoint - Verify unit tests pass with target coverage
  - Ensure `go test ./internal/github/...` passes. Coverage target: 80% ("Server webhook handling" per the global design doc's Testing Strategy — this package's webhook/check-run/comment/auth surface falls under that bucket) for the package overall, 90% for `parser.go` specifically ("Comment parser" line item). Ask the user if questions arise.

- [x] 16. Write property tests
  - [x] 16.1 Write `internal/github/property_test.go`
    - `// Feature: multi-iac-automation-platform, Property 7: Comment Trigger Pattern Recognition` — for a single-line body assembled from a random tool token (unconstrained, not drawn from a fixed set), random operation token, and random extra-arg tokens (`/<tool> <operation> -- <extraArgs...>`), assert `ParseTriggers` returns exactly one `TriggerCommand` recovering the same `Tool`/`Operation`/`ExtraArgs`; ≥100 iterations
    - `// Feature: multi-iac-automation-platform, Property 8: Selective Project Triggering from Comments` — reinterpreted at this slice's scope per design.md's note: for a random single project-name token, `ParseTriggers("/turnip apply " + project)`'s one `TriggerCommand.Projects` is exactly `[project]`; ≥100 iterations
    - `// Feature: multi-iac-automation-platform, Property 15: Consolidated Comment Per PR` — reinterpreted per design.md's note: for a random set of `ProjectResult`s whose total content stays under `maxCommentLength`, `BuildConsolidatedComment` returns exactly one body; ≥100 iterations
    - `// Feature: multi-iac-automation-platform, Property 17: Comment Contains All Project Results` — for a random set of `ProjectResult`s, every project name appears somewhere in the concatenation of all returned bodies; ≥100 iterations
    - `// Property (slice-local, Requirements 4.2/4.6/4.8): Multi-Line Trigger Ordering and Partial-Failure Isolation` — for a random interleaving of N well-formed lines and M malformed lines assembled into one body, assert `ParseTriggers` returns exactly N `TriggerCommand`s in the well-formed lines' original order, and — when M > 0 — a `MalformedTriggerErrors` of length M; ≥100 iterations
    - Use `rapid.Check(t, func(t *rapid.T) {...})` directly per property (its default `checks` count already satisfies ≥100 iterations), matching the pattern established in `internal/config/property_test.go`
    - _Requirements: 4.2, 4.3, 4.4, 4.5, 4.6, 4.8, 7.2, 7.3, 7.4_

- [x] 17. Final checkpoint - Full verification
  - Ensure `go build ./...` compiles, `go test -race ./internal/github/...` passes including property tests (`-race` matters for 14.4's concurrent-authorization test), `go mod tidy` produces no changes, and `golangci-lint run ./internal/github/...` passes (or `go vet`/`gofmt -l` if golangci-lint is unavailable locally — see CLAUDE.md). Ask the user if questions arise.
  - Verified: `go build ./...` OK; `go vet ./internal/github/...` clean; `gofmt -l internal/github/` empty; `go mod tidy` stable (byte-identical `go.mod` across repeated runs); `go test -race ./internal/github/...` passes all tests (26 unit across parser/client/auth/authorize/webhook/comment/errors, 5 property at ≥100 iterations each) — 92.8% package coverage (target 80%), parser.go individually above the 90% "Comment parser" target. The asdf-pinned `golangci-lint` (v1.64.8) can't parse this repo's v2 config, but the real v2 binary runs fine locally via `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest run ./...` (network access permitting) — 0 issues, repo-wide (see `CLAUDE.md`'s corrected note).

- [x] 18. Adopt `testify` for unit test assertions (2026-08 amendment)
  - [x] 18.1 Rewrite `internal/github`'s unit test files against `github.com/stretchr/testify`
    - Adopted repo-wide (see `CLAUDE.md`) to replace hand-rolled `if ... { t.Fatalf(...) }`/`t.Errorf(...)` checks with `require`/`assert`
    - `parser_test.go`, `client_test.go`, `auth_test.go`, `authorize_test.go`, `webhook_test.go`, `comment_test.go`, `errors_test.go`: `require` where the original check was fatal, `assert` where it was non-fatal (including the concurrent-goroutines authorization test, where `assert` is safe to call from any goroutine the same way `t.Errorf` was)
    - `property_test.go`: `*rapid.T` satisfies testify's `TestingT` interface directly, so `rapid.Check` bodies use `require` the same way
    - Verify `go test -race ./internal/github/...` passes with unchanged behavior (26 unit, 5 property) and coverage
    - _Requirements: (maintenance amendment, no behavioral change)_

  - [x] 18.2 Update dependencies
    - Add `github.com/stretchr/testify` as a direct dependency
    - _Requirements: (dependency infrastructure, no direct requirement)_

  - [x] 18.3 Checkpoint - Full re-verification
    - Ensure `go build ./...`, `go vet ./internal/github/...`, `gofmt -l internal/github/`, `go test -race ./internal/github/...` (92.8% coverage), and the real `golangci-lint` v2 (`go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest run ./...`) all pass

- [x] 19. Enable `testifylint` in `.golangci.yml` (2026-08 amendment)
  - [x] 19.1 Fix findings in `internal/github`
    - `.golangci.yml` gained `testifylint` in `linters.enable` (repo-wide, not slice-specific — see `CLAUDE.md`)
    - `require-error`: `authorize_test.go` and `parser_test.go` each had one bare `assert.Error`/`assert.ErrorIs` changed to `require`
    - `go-require`: `client_test.go`'s `httptest` handler closures (`TestClient_GetModifiedFiles_Paginates`'s default case, and the `CreateCheckRun`/`UpdateCheckRun`/`UpdateComment` request-body decode checks) used `require` inside handler goroutines — changed to `assert`; the shared `writeJSON` test helper (called from every handler in this file) had the same problem and was fixed the same way, even though the linter's call-site-local detection didn't flag it directly
    - Verify `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest run ./...` reports 0 issues repo-wide
    - _Requirements: (maintenance amendment, no behavioral change)_

- [x] 20. Split oversized single-Project output across bodies instead of truncating (2026-08 amendment)
  - [x] 20.1 Rewrite `comment.go`'s oversized-output handling
    - Task 11.1's original design truncated a single `ProjectResult.Output` too large for one body with an `[output truncated]` marker, silently dropping the tail — flagged as a real defect (a truncated body also didn't close its own code fence/`<details>` tag, since the marker was plain text): Requirement 7.4 already calls for *splitting* oversized content across bodies, and there's no reason that guarantee should stop applying just because the overflow comes from one Project's `Output` instead of many Projects' combined content
    - Replace `buildDetailSection` with `buildDetailSectionPart(r, chunk, part, total)`, appending a `(output part N/M)` suffix to the `<summary>` only when `total > 1`
    - Add `splitDetailSection(r, reserve) []string`: returns the whole rendered piece unsplit when it already fits within `maxCommentLength-reserve`; otherwise chunks `r.Output` so each resulting piece (with its own full `<details>`/fence scaffold) fits within that budget, sizing the scaffold estimate against a pessimistic 4-digit `part`/`total` placeholder so the real suffix never pushes a piece over budget
    - Add `reserveFor(table) int`, returning `max(len(table), minReserve)` — the summary table (group 0's overhead) is virtually always the largest overhead any piece could face, so reserving against it is conservative for every piece regardless of which body it lands in
    - `BuildConsolidatedComment` now builds one `pieces []string` slice via `splitDetailSection` per result (rather than one `sections` entry per result) before handing them to the unchanged `packSections`
    - `truncateBody`'s marker changes from `"\n\n[output truncated]"` to `` "\n```\n\n_(truncated)_\n</details>" `` — it's now a last-resort safety net for the residual per-piece join overhead `packSections` doesn't count, not the primary oversized-output mechanism, but when it does fire it must still close the fence/`<details>` tag it's cutting through
    - Verify `go build ./internal/github/...`
    - _Requirements: 7.4_

  - [x] 20.2 Update tests
    - `comment_test.go`: replace `TestBuildConsolidatedComment_TruncatesOversizedSingleOutput` with `TestBuildConsolidatedComment_SplitsOversizedSingleOutputAcrossBodies`, asserting multiple bodies, each within `maxCommentLength`, no `[output truncated]` marker, and — using a large distinguishable `numberedLines(n)` Output — that extracting and concatenating every body's fenced content byte-for-byte reconstructs the original `Output` exactly (no loss, no duplication, correct order)
    - Add `TestSplitDetailSection_ReassemblesExactly` and `TestSplitDetailSection_FitsWithoutSplitting`, testing `splitDetailSection` directly
    - Add `TestTruncateBody_ClosesOpenFenceAndDetails` and `TestTruncateBody_NoOpUnderLimit`, testing `truncateBody` directly
    - Verify `go test -race ./internal/github/...` passes with 100% coverage on every function in `comment.go`
    - _Requirements: (test coverage for 20.1, no new requirement)_

  - [x] 20.3 Checkpoint - Full re-verification
    - Ensure `go build ./...`, `go vet ./internal/github/...`, `gofmt -l internal/github/`, `go test -race ./internal/github/...`, and the real `golangci-lint` v2 (`go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest run ./...`) all pass

- [x] 21. Add `MinimizeComment` and a comment's GraphQL node ID (server-orchestration amendment)
  - [x] 21.1 Amend `client.go` and add `graphql.go`
    - server-orchestration (Slice 6) needs to mark a superseded plan comment "outdated" on GitHub, which only exists as a GraphQL mutation (`minimizeComment`) — no REST equivalent — and needs the comment's GraphQL node ID as that mutation's `subjectId`, which `PostComment`'s numeric-ID-only return didn't carry
    - Added `PostedComment{ID, NodeID}`, changed `PostComment`'s return from `(int64, error)` to `(*PostedComment, error)` — a plain signature change, not a new method alongside the old one, since no caller existed yet anywhere in this codebase
    - Added `GitHubClient.MinimizeComment(ctx, nodeID string) error`, implemented in new `graphql.go` as a single hand-rolled GraphQL POST (not a new client library dependency) to `https://api.github.com/graphql`, reusing `c.itr` (the same installation-authenticated transport `c.gh` already uses) as the `http.RoundTripper` — guarding against `c.itr` being a nil `*ghinstallation.Transport` stored in a non-nil `http.RoundTripper` interface value (a real Go gotcha that panicked in this package's own tests until caught), falling back to `http.DefaultTransport` in that case
    - `classifier` is hardcoded to `OUTDATED` — this package has no other use for the mutation
    - Added `graphQLURL` as an unexported test-only override field on `Client`, since the endpoint is otherwise a fixed constant with no way for a test to redirect it
    - Updated `authorize_test.go`'s `fakeGitHubClient` and `client_test.go`'s `TestClient_PostComment` for the new signatures
    - _Requirements: server-orchestration's Requirement 10.3, 10.4_

  - [x] 21.2 Add `graphql_test.go`
    - Against an `httptest.Server` standing in for `api.github.com/graphql` (via `graphQLURL`): asserts the outgoing request body's shape (`query` containing `minimizeComment`/`OUTDATED`, `variables.id` matching the given node ID), and that a GraphQL-level `errors` entry in the response is surfaced as a Go error
    - _Requirements: (test coverage for 21.1)_

  - [x] 21.3 Checkpoint - Full re-verification
    - `go build ./internal/github/...` and `go test ./internal/github/...` pass

## Notes

- No Redis, gRPC, or Kubernetes dependencies are introduced — this slice
  only talks to GitHub (via a local `httptest` server in tests), matching
  the design's stated scope.
- `types.go` and `errors.go` have no dependency on each other; `parser.go`,
  `client.go`, `webhook.go`, and `comment.go` each depend only on those two
  leaves (plus external libraries) and not on each other — the dependency
  graph below reflects the true parallel opportunity, with `auth.go` and
  `authorize.go` as the only tasks that join on `client.go`.
- Coverage targets: 80% package-wide ("Server webhook handling"), 90% for
  `parser.go` ("Comment parser"), per the global design doc's Testing
  Strategy line items.
- No real GitHub API or webhook delivery is used in this slice's tests —
  `httptest` plus hand-built signed fixture payloads are sufficient, per
  design.md's Testing Strategy. Integration testing against real GitHub
  belongs to the global roadmap's Slice 11.

- [x] 22. Fix the check run `output` object: never send it partially populated (2026-09 amendment)
  - Found in a real deployment, not in tests: every `CreateCheckRun` call
    failed with `422 Invalid request: "summary", "title" weren't
    supplied`, so a PR got its result comment but no check run at all.
    The failure was invisible because server-orchestration's Decision 3
    treats check-run errors as soft (logged, operation proceeds)
  - Cause: `toCheckRunOutput` always returned a non-nil
    `*gh.CheckRunOutput` built from `strPtr` on each field. GitHub's
    contract is all-or-nothing — `output` is optional, but when present
    both `title` and `summary` are required — so a caller that set none
    of them produced `"output": {}`, and one that set only `Text` or only
    `Summary` produced an object missing a required field
  - All four call sites were affected, creation simply failed first:
    `execute.go`'s create (no output fields) and job-failure update
    (`Text` only), `sweep.go`'s timeout update (`Text` only), and
    `result.go`'s final-result update (`Summary`+`Text`, no `Title`)
  - Fixed at the seam rather than in each caller, so no future caller can
    emit an invalid payload: return `nil` when title, summary and text are
    all empty; otherwise fill `title` from the check run's own name and
    `summary` from the title when unset
  - _Requirements: 6.2, 6.5_

  - [x] 22.1 Tests
    - `client_test.go`: three direct `toCheckRunOutput` cases, one per
      real call-site shape (nothing to report, text-only, summary-without-
      title), plus an end-to-end assertion that a creation request carries
      no `output` key at all when there is nothing to report
    - _Requirements: (regression coverage for 22)_

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1.1"] },
    { "id": 1, "tasks": ["2.1", "3.1"] },
    { "id": 2, "tasks": ["5.1", "6.1", "10.1", "11.1"] },
    { "id": 3, "tasks": ["8.1", "9.1"] },
    { "id": 4, "tasks": ["12.1"] },
    { "id": 5, "tasks": ["14.1", "14.2", "14.3", "14.4", "14.5", "14.6"] },
    { "id": 6, "tasks": ["16.1"] }
  ]
}
```
