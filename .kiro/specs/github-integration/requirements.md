# Requirements Document: GitHub Client & Webhook Handler (Slice 4)

## Introduction

This slice delivers the `internal/github` package: GitHub App authentication,
webhook signature verification and event parsing, PR comment trigger
parsing, collaborator authorization, GitHub check-run lifecycle management,
and consolidated PR comment formatting/posting. It is consumed by Slice 6
(Server Orchestration), which owns *when* to call each of these primitives
during the webhook-to-operation flow (deciding to trigger a plan, updating a
check run at the right moments, posting a comment once all projects finish),
and by Slice 5 (gRPC & Runner), which receives the installation token this
slice generates for repository cloning. This slice implements the
GitHub-facing primitives themselves — it has no dependency on Redis, gRPC,
or Kubernetes, and does not decide what business action a webhook event or
trigger comment should cause.

This slice implements Requirements 15 and 16 from the global spec
(`.kiro/specs/multi-iac-automation-platform/requirements.md`) in full, and
the *parsing/formatting* sub-clauses of Requirements 5, 6, 9, 10, and 17 —
the sub-clauses that describe *deciding when* to act on a parsed trigger,
check run, or comment are Slice 6's (see "Out of Scope").

Two sub-areas of this slice are not tied to a specific global requirement
number, though both are explicit deliverables in the roadmap's Slice 4 entry
and necessary infrastructure for every other requirement this slice covers:

- **Webhook signature verification and event parsing** — the global spec's
  Requirement 15.5 assumes a webhook is already being processed ("WHEN
  processing a Webhook_Event") but no acceptance criterion anywhere states
  *how* an inbound HTTP request becomes a `Webhook_Event`. Something in the
  platform has to verify the request is genuinely from GitHub and turn it
  into a typed event before any of Requirements 5/6/9/10/15/16/17 can apply
  to it; this slice is where that has to live, since every other slice that
  touches GitHub data depends on this one, not the reverse.
- **Repository content/modified-file fetching** (a `GetFile`-shaped and a
  `GetModifiedFiles`-shaped operation) — Requirements 1.1 and 2.1 describe
  the Server reading turnip.yaml and retrieving a PR's modified files, but
  Slice 1 (`config-parsing`) is explicitly a pure library with "no
  dependency on GitHub" (per this repo's CLAUDE.md). The *fetching* half of
  Requirements 1.1/2.1 has to live wherever GitHub API access lives — this
  slice — while the *parsing/matching* half (Requirements 1.2-1.5,
  2.2-2.5) already shipped in Slice 1.

## Glossary

(Inherited from the global spec glossary.)

- **Installation Token**: A short-lived GitHub App installation access
  token, scoped to one repository installation, used to authenticate
  GitHub API calls and to authenticate the Runner's repository clone.
- **Trigger Comment**: A line within a PR comment following the
  `/{tool|turnip} <operation> [project ...] [-- extra args]` grammar. A
  single PR comment may contain more than one Trigger Comment.
- **Collaborator Permission**: The GitHub repository permission level
  (`none`, `read`, `triage`, `write`, `maintain`, `admin`) GitHub reports
  for a given user on a given repository.

## Requirements

### Requirement 1: GitHub App Authentication

**User Story:** As a platform operator, I want the Server to authenticate
as a GitHub App and mint per-installation tokens, so that it can access
private repositories and post comments/checks with the App's identity
rather than a personal account.

#### Acceptance Criteria

1. THE package SHALL provide a way to construct an installation-scoped
   GitHub client from an App ID, a PEM-encoded App private key, and an
   installation ID
2. THE package SHALL provide `GenerateInstallationToken(ctx) (string, error)`
   on that client, returning a valid installation access token usable by
   the Runner for repository cloning
3. Installation tokens SHALL be cached and automatically refreshed before
   expiry rather than regenerated on every call (delegated to the
   underlying transport library rather than reimplemented)
4. IF the App private key is malformed, THEN client construction SHALL
   return an error rather than a client that fails on first use
5. IF installation token generation fails (e.g. the installation was
   revoked), THEN `GenerateInstallationToken` SHALL return an error the
   caller can distinguish from a transient network failure

### Requirement 2: Webhook Signature Verification and Event Parsing

**User Story:** As a platform operator, I want inbound webhook requests
verified and parsed into a typed event, so that only genuine GitHub
requests reach business logic and that logic doesn't have to know GitHub's
wire format.

#### Acceptance Criteria

1. THE package SHALL provide an `http.Handler` that verifies the
   `X-Hub-Signature-256` header against a configured webhook secret using
   HMAC-SHA256
2. IF signature verification fails, THEN THE handler SHALL respond with
   HTTP 401 and SHALL NOT invoke any caller-supplied event handling
3. THE handler SHALL parse `pull_request` and `issue_comment` event
   payloads (identified via the `X-GitHub-Event` header) into a package
   `WebhookEvent` struct carrying event type, action, repository,
   pull request, comment, and installation identifiers as applicable
4. THE handler SHALL dispatch parsed events to a caller-supplied
   `EventHandler` (one method per event type), matching Requirement 15.2's
   assumption that a Webhook_Event is already available for token
   generation and processing
5. THE handler SHALL respond HTTP 200 without invoking the caller-supplied
   `EventHandler` for event types other than `pull_request`/`issue_comment`
6. IF the caller-supplied `EventHandler` returns an error, THEN THE handler
   SHALL respond HTTP 500

### Requirement 3: Repository Content and Modified-File Retrieval

**User Story:** As a developer relying on Slice 1's config parser, I want
the platform to fetch turnip.yaml and a PR's modified files from GitHub, so
that Slice 1's pure parsing/matching functions have bytes and a file list
to operate on.

#### Acceptance Criteria

1. THE package SHALL provide `GetFile(ctx, owner, repo, path, ref) ([]byte, error)`
   fetching a file's raw content at a specific ref
2. IF the file does not exist at that ref, THEN `GetFile` SHALL return an
   error distinguishable from other failures (network, auth, rate limit)
3. THE package SHALL provide `GetModifiedFiles(ctx, owner, repo, prNumber) ([]string, error)`
   returning every file path changed in the PR, paginating through GitHub's
   API as needed so the caller never sees a truncated list

### Requirement 4: PR Comment Trigger Parsing

**User Story:** As a developer, I want every operation trigger in a PR
comment recognized, not just the first, so that I can batch several
actions — e.g. planning two different Projects — into one comment instead
of posting one comment per action the way Atlantis requires.

#### Acceptance Criteria

1. THE package SHALL provide `ParseTriggers(body string) ([]*TriggerCommand, error)`
2. THE package SHALL recognize *every* line matching
   `/{turnip|<tool name>} <operation> [project ...] [-- extra args]` as its
   own Trigger Comment, scanning the entire comment body rather than
   stopping at the first match — a comment containing
   `/turnip plan project-1` on one line and `/turnip plan project-2` on
   another SHALL produce two `TriggerCommand`s, one per line, in the same
   comment; `<tool name>` is whatever a Plugin calls itself (Requirement
   3.1 of the global spec — "terraform", "pulumi", "helmfile" today,
   whatever a future Plugin adds later), never a fixed set this package
   hardcodes
3. `ParseTriggers` SHALL populate each resulting `TriggerCommand`'s `Tool`
   and `Operation` from its line's first two tokens, without validating
   either against a known tool or operation list (that validation needs
   the Plugin registry, which this package does not depend on)
4. WHERE one or more project-name tokens appear between a line's operation
   and an optional `--` delimiter, `ParseTriggers` SHALL populate that
   line's `TriggerCommand.Projects` with them; an absent `--` and no
   project tokens both leave the trailing fields empty rather than
   erroring
5. WHERE a `--` delimiter appears on a line, `ParseTriggers` SHALL
   populate that line's `TriggerCommand.ExtraArgs` with every token after
   it, verbatim
6. THE returned `[]*TriggerCommand` SHALL preserve the matched lines'
   order within the comment body
7. IF no line in the body matches the trigger grammar, THEN `ParseTriggers`
   SHALL return an error distinguishable from "matched but malformed" so a
   caller can silently ignore ordinary PR comments
8. IF one or more lines start with `/` followed by a single token and
   nothing else (no operation token), THEN `ParseTriggers` SHALL still
   return every other line's well-formed `TriggerCommand`s, accumulating
   every malformed line into a returned error distinguishable from "no
   trigger present at all" — mirroring this platform's existing
   `internal/config` validation convention of accumulating every problem
   in one call rather than stopping at the first, so one typo'd line
   doesn't silently discard every other action in the same comment

### Requirement 5: Collaborator Authorization

**User Story:** As a platform operator, I want comment authors verified as
repository collaborators — with write permission required for destructive
operations — so that only authorized users can trigger IaC operations.

#### Acceptance Criteria

1. THE package SHALL provide a way to check whether a username is a
   repository collaborator
2. THE package SHALL provide a way to check whether a username holds
   `write` permission or higher (`write`, `maintain`, `admin`) on a
   repository
3. Both checks SHALL cache their result per `(owner, repo, username)` for 5
   minutes, so repeated triggers from the same author within that window
   don't re-hit the GitHub API
4. THE cache SHALL be safe for concurrent use from multiple goroutines
   handling different webhook requests simultaneously

### Requirement 6: GitHub Check Run Lifecycle

**User Story:** As a developer, I want operation status surfaced as GitHub
check runs, so that I can see whether infrastructure changes are safe to
merge without leaving the PR.

#### Acceptance Criteria

1. THE package SHALL provide `CreateCheckRun(ctx, owner, repo, opts) (int64, error)`
   creating a check run and returning its ID
2. THE package SHALL provide `UpdateCheckRun(ctx, owner, repo, checkRunID, opts) error`
   updating an existing check run's status/conclusion/output
3. THE check-run creation/update primitives SHALL accept, at minimum: the
   check's name, the commit SHA it applies to, a status value, a
   conclusion value (for completed runs), and title/summary/detail-text
   fields — together sufficient to satisfy Requirement 9.1-9.4's
   in-progress/completed/conclusion/change-summary needs without this
   package having to guess what a caller wants displayed
4. THE package SHALL NOT decide when a check run transitions between
   `queued`/`in_progress`/`completed` — the caller (Slice 6) supplies the
   desired `CheckRunOptions` for each call

### Requirement 7: Consolidated PR Comment Formatting and Posting

**User Story:** As a developer, I want operation results posted as a
consolidated, readable PR comment — as few comments as GitHub's length
limit allows, never one comment per Project — so that I can review changes
across every triggered Project without leaving GitHub or hunting through
an unbounded comment thread.

#### Acceptance Criteria

1. THE package SHALL provide a pure function building one or more comment
   bodies from a list of per-Project results, each carrying at minimum a
   Project name, an Operation name, a success/failure status, and detail
   output
2. THE built comment SHALL include a summary table with one row per
   Project showing its name, Operation, and status
3. THE built comment SHALL include a collapsible (`<details>`) section per
   Project containing that Project's detail output in a fenced markdown
   code block
4. IF the combined content would exceed GitHub's maximum comment length,
   THEN THE function SHALL split it across multiple comment bodies rather
   than producing one oversized body GitHub would reject
5. THE package SHALL provide `PostComment(ctx, owner, repo, prNumber, body) (int64, error)`
   and `UpdateComment(ctx, owner, repo, commentID, body) error` as the raw
   posting primitives; deciding *whether* to post a new comment or update
   an existing one for a given PR is Slice 6's responsibility (see "Out of
   Scope")

## Out of Scope

- Deciding *when* to call `CreateCheckRun`/`UpdateCheckRun`,
  `PostComment`/`UpdateComment`, or which Projects a parsed
  `TriggerCommand` should actually execute against — Slice 6 (Server
  Orchestration)
- Deciding how to execute multiple `TriggerCommand`s parsed from one
  comment — sequentially or in parallel, what happens if one fails, and
  how per-command results get reported back — Slice 6; this slice only
  guarantees every well-formed line is parsed out, in order
- Persisting which PR already has a consolidated comment (so Slice 6 knows
  whether to call `PostComment` or `UpdateComment`) — Slice 6's job, likely
  backed by Redis; this slice only implements both primitives
- Validating that a parsed `TriggerCommand.Operation` is a real operation
  for `TriggerCommand.Tool`, and executing the operation — Slice 6/Slice 2
  (Plugin registry)
- Custom retry/backoff logic for transient GitHub API failures (rate
  limits, 5xx) beyond whatever the chosen GitHub client dependency already
  provides — deferred; not required by any numbered acceptance criterion
  in this slice
- Periodic cleanup of orphaned Kubernetes jobs or stale locks — Slice 8
  (HA, Observability & Deployment)
- GitLab/Bitbucket clients — no numbered requirement in the global spec
  asks for one; the platform's constraints section only names GitHub
- Rendering Terraform/Pulumi/Helmfile-specific syntax highlighting beyond
  a generic fenced code block — Requirement 10.5 asks for "markdown code
  blocks," which this slice provides; tool-aware highlighting (e.g. a
  `diff` language hint for Terraform's `+`/`-` plan lines) is a formatting
  nicety Slice 6 can layer on when it builds the actual per-Project detail
  text this slice's formatter embeds verbatim
