# Requirements Document: Server Orchestration (Slice 6)

## Introduction

This slice delivers `cmd/server` and the orchestration package(s) behind it:
the code that decides *when* to call the primitives Slices 1-5 already
shipped, wiring them into the complete webhook-to-operation flow described
by the global design's "Data Flow" section. Concretely, this slice is the
`github.EventHandler` implementation (reacting to `pull_request` and
`issue_comment` webhooks), the `rpc.OperationHandler` implementation
(reacting to a Runner's log lines and final result), and the glue code that
calls `config.Parse`/`config.MatchProjects` (Slice 1), a Plugin Registry
built from `internal/plugin` (Slice 2), `lock.LockManager` (Slice 3),
`github.GitHubClient`/`Authorizer`/`BuildConsolidatedComment` (Slice 4),
and `jobs.BuildJob`/`jobs.Client` (Slice 5). Nothing in `internal/` outside
this slice depends on it; Slice 8 (HA, Observability & Deployment) is the
only later consumer, and only for deployment artifacts around the binary
this slice produces.

This slice implements Requirements 4, 5, 6, 17, and 20 from the global spec
(`.kiro/specs/multi-iac-automation-platform/requirements.md`) in full,
together with the *decide-when-to-call* sub-clauses of Requirements 7, 8, 9,
10, 14, 15, 16, and 18 that Slices 3, 4, and 5's requirements docs each
explicitly deferred to "Slice 6" in their own "Out of Scope" sections. Where
a global acceptance criterion is cited below without restating its full
text, see the numbered requirement in the global spec.

Four points in the global spec and design doc are not directly
implementable as written and need to be resolved as part of this slice:

- **Operation correlation state.** `rpc.OperationHandler.HandleLog` and
  `HandleResult` (Slice 5) are called with only an Operation ID string —
  they carry no Project, PR, or check-run context. Requirement 19 requires
  the Server to be stateless so that *any* instance can handle *any*
  request, including a Runner's gRPC callback, which may land on a
  different Server instance/pod than the one that created the Runner Job.
  No global requirement says where the Project/PR/check-run context for an
  in-flight Operation should live in the meantime. This slice resolves it
  by persisting that context — the **Operation Record** (see Glossary) — in
  Redis, keyed by Operation ID, before the Runner Job is created.
- **Job-start timeout detection.** Requirement 14.5 asks the Server to
  detect a Runner Job that "fails to start within 5 minutes," but
  grpc-runner's (Slice 5) `OperationHandler` interface has no distinct
  "the Runner started" signal — only `HandleLog` (first output line) and
  `HandleResult` (completion) — and detecting the *absence* of either
  within a window cannot be an in-process timer scoped to whichever Server
  instance created the Job: Requirement 12 requires that instance's crash
  or restart not be load-bearing for correctness, and a `time.AfterFunc`
  tied to that one process dies with it. This slice instead records a
  start deadline in the Operation Record (Redis, so any instance can see
  it) and detects a lapsed deadline via a periodic sweep every instance
  runs, claiming an expired, not-yet-started Operation Record with an
  atomic compare-and-mutate (mirroring `internal/lock`'s Lua-script CAS
  pattern) so exactly one instance's sweep acts on it even if several
  instances' sweeps overlap. Once claimed, rather than reporting a bare
  "timed out" with no further explanation, THE Server queries the
  Kubernetes API for that Operation's Job/Pod status (`jobs.BuildJob`
  already labels every Job with `turnip.ivan.vc/operation-id`, so the
  correlation key already exists) to surface the actual cause — e.g. an
  image pull failure, a scheduling failure, or a Pod still `Pending` —
  giving the reported failure real diagnostic content instead of silence
  alone. This requires extending `jobs.Client` (Slice 5) with a
  status-query method it does not currently have (today it only offers
  `Create`/`Delete`); that addition falls squarely within
  `internal/jobs`'s stated charter ("builds and manages the Kubernetes
  Jobs that run Runner Pods") and is not scope creep into a general
  Kubernetes-monitoring capability — see "Out of Scope." Consistent with
  grpc-runner's Requirement 2.4 rationale (don't kill a Runner that might
  still be applying real infrastructure changes over a transient network
  blip), a Server-detected timeout still does **not** force-delete the
  Runner Job — it only reports the failure and relies on the Job's TTL
  (grpc-runner Requirement 7.4/9.1) as the cleanup mechanism.
- **Design doc's stale-lock narrative vs. the shipped `LockManager`.** The
  global design's "Error Handling" section describes lock TTLs ("Lock TTLs
  prevent indefinite locks if a Server crashes") in one place and their
  absence ("locks do not have TTLs and persist indefinitely") in another,
  and additionally describes a stale-lock web UI and admin force-unlock
  API. redis-lock-manager's (Slice 3) shipped `LockManager` has no TTL
  (Requirement 7.4/7.6) and its requirements doc already deferred a lock UI
  / admin endpoint "indefinitely unless a future slice picks it up." This
  slice follows the shipped no-TTL `LockManager` as-is and does not
  introduce a lock UI, admin force-unlock endpoint, or periodic orphaned-
  lock/Job cleanup — the latter is explicitly assigned to Slice 8 in
  github-integration's (Slice 4) "Out of Scope."
- **Manual-unlock authorization.** The global design's Error Handling
  narrative says manual unlock requires the user to be "the PR author or a
  repository admin," but Requirement 16 and the shipped `Authorizer`
  (Slice 4) only expose `IsCollaborator` and `HasWritePermission` — there is
  no "PR author" or "repository admin" check available. This slice treats
  manual unlock as requiring `HasWritePermission`, the same bar Requirement
  16.4 sets for apply/destroy, rather than inventing a third authorization
  primitive Slice 4 never shipped.
- **Consolidated comment update strategy.** Requirement 10.4 of the global
  spec ("update the existing comment instead of creating a new one") reads
  naturally as editing one comment's body in place — but that collides
  with Requirement 10.6's mandate to split output across multiple comments
  once it's too large: an edit-in-place strategy has no good answer for a
  later plan needing *more* or *fewer* parts than what's already posted,
  since a newly-added part can only be appended at the current bottom of
  the PR thread, visually tearing one plan's output apart from its own
  earlier parts across however much unrelated conversation happened in
  between. This slice instead adopts the approach a comparable tool
  already ships for the same problem: Atlantis's `--hide-prev-plan-comments`
  feature marks a superseded plan comment "outdated" (collapsed, expandable
  on click) via GitHub's `minimizeComment` mutation — GraphQL-only, no REST
  equivalent exists — rather than editing or deleting it, then posts an
  entirely fresh comment. This slice does the same for plan results, gated
  behind a Server-level flag defaulting to disabled (`minimizeComment` has
  a long-standing, unresolved GitHub bug around its `classifier` argument,
  and no REST fallback, so shipping it opt-in is the appropriate level of
  caution for a new dependency on that surface). Apply results are
  deliberately excluded from this treatment entirely — always posted
  fresh, never minimized, never looked up again — since an apply is a
  permanent record of what happened to real infrastructure, not a
  superseded prediction the way an older plan is. One consequence:
  `UpdateComment`/`DeleteComment` (both shipped by github-integration,
  Slice 4) end up unused by this slice's final design.

## Glossary

(Inherited from the global spec glossary, plus terms Slices 3/4/5 already
added to their own.)

- **Operation Record**: The per-Operation context this slice persists in
  Redis, keyed by Operation ID, before creating a Runner Job — the Project
  (name, directory, tool, config), Repository, PR number/URL, triggering
  username, Project Key, GitHub check run ID (once created), Runner Job
  name (once created), a start deadline (Job-creation time + 5 minutes),
  and whether the Operation has been recorded as started (Requirement 8.2).
  Lets any Server instance's gRPC handler — or periodic sweep — resolve or
  act on an Operation ID it did not itself create the Job for (Requirement
  19).
- **Project Key**: Constructed by this slice as
  `{repo_owner}/{repo_name}/{project_name}`, per redis-lock-manager's
  Glossary — the caller-supplied opaque string `LockManager` keys Locks by.
- **Plugin Registry**: This slice's mapping from a tool name (`"helmfile"`
  today; `"terraform"`/`"pulumi"` once Slice 7 ships) to its `plugin.Plugin`
  implementation, used to validate a `TriggerCommand`'s tool/operation and
  to know each tool's plan/apply operation names via
  `GetPlanOperation()`/`GetApplyOperation()`.
- **Plan Comment Record**: This slice's persisted mapping, in Redis, from
  `(owner, repo, PR number)` to the GraphQL node ID(s) of the most
  recently posted plan comment(s) for that PR — used only to know which
  comment(s) to mark outdated the next time a plan completes (Requirement
  10.3). Maintained only when the minimize-outdated-plan-comments flag is
  enabled (Requirement 10.5); has no apply-side equivalent, since apply
  comments are never looked up again once posted (Requirement 10.7).
- **Target**: One `(Project, Operation)` pair resolved from a
  `TriggerCommand` (Requirement 4) or from auto-plan matching (Requirement
  2) — the unit this slice creates at most one Runner Job and one check run
  for.

## Requirements

### Requirement 1: turnip.yaml Retrieval and Validation

**User Story:** As a developer, I want the Server to read and validate my
repository's turnip.yaml before doing anything else for a given webhook, so
that a missing or broken configuration fails fast with a clear message
instead of a confusing partial failure downstream.

#### Acceptance Criteria

1. WHEN the Server begins handling a `pull_request` event with action
   `opened`/`synchronize`, or an `issue_comment` event that will need
   Project context, THE Server SHALL attempt to fetch the config file via
   `GitHubClient.GetFile(ctx, owner, repo, ".turnip/config.yaml", headSHA)`
   first; IF that call returns an error wrapping `github.ErrFileNotFound`,
   THE Server SHALL retry at `turnip.yaml` in the repository root before
   treating the file as absent (Requirement 1.1 of the global spec). THE
   Server SHALL pass whichever location's content is found to
   `config.Parse` before taking any other action for that event.
   `.github/turnip.yaml` was accepted by earlier versions and is no longer
   read — superseded by `project-schema-v1alpha2`'s Requirement 8, which
   replaces it with a dedicated `.turnip/` directory a repository can keep
   related files in
2. IF `GetFile` returns an error wrapping `github.ErrFileNotFound` for
   BOTH locations, THEN THE Server SHALL NOT proceed further for that
   event — no Lock acquisition, Runner Job, or check run (Requirement 1.4
   of the global spec). WHERE the event is a Trigger Comment, THE Server
   SHALL post a comment stating turnip.yaml is missing; WHERE the event
   is an automatic plan (`pull_request` `opened`/`synchronize`), THE
   Server SHALL post no comment at all — a repository without a
   turnip.yaml has not opted into turnip, and this handler runs on every
   PR open and every push to one, so commenting would put "turnip.yaml
   was not found" on every pull request in that repository. A turnip.yaml
   that exists but is invalid (1.4) still comments on both paths: that
   repository has opted in, so its breakage stays visible
3. IF `GetFile` fails for any other reason (network, auth, rate limit), THEN
   THE Server SHALL log the error, post a comment stating that fetching
   turnip.yaml failed with the underlying error message included inside a
   collapsible `<details>` block — matching the `<details>`/fenced-code-
   block convention `github.BuildConsolidatedComment` already uses for
   per-Project output — so the PR author can see the actual cause (e.g. a
   rate-limit or auth failure) without needing Kubernetes pod/log access,
   and SHALL NOT proceed further for that event
4. IF `config.Parse` returns a `*config.ParseError` or `config.ValidationErrors`,
   THEN THE Server SHALL post a comment enumerating every reported problem
   (leveraging `ValidationErrors`' accumulate-all-violations behavior, per
   this repo's `internal/config` convention) and SHALL NOT proceed further
   for that event
5. On success, THE Server SHALL retain the parsed `*config.Config` for the
   remainder of that event's processing rather than re-fetching or
   re-parsing per Project

### Requirement 2: Automatic Plan on PR Open/Synchronize

**User Story:** As a developer, I want a plan to run automatically when I
open or update a PR, so that I can review infrastructure changes without
manually triggering anything.

#### Acceptance Criteria

1. WHEN `HandlePullRequest` receives action `opened` or `synchronize`, AFTER
   Requirement 1 succeeds, THE Server SHALL call
   `GitHubClient.GetModifiedFiles` and `config.MatchProjects` to determine
   the triggered Projects for that event (Requirement 2.1-2.3 of the global
   spec)
2. IF `MatchProjects` returns zero Projects, THE Server SHALL take no
   further action for that event — no comment, Lock, or Runner Job
   (Requirement 2.5 of the global spec)
3. FOR EACH matched Project, THE Server SHALL resolve it to a Target whose
   Operation is that Project's tool's `Plugin.GetPlanOperation()`, with
   `TriggeredBy` = `"auto"` and no `ExtraArgs`, and execute it via the Lock
   Lifecycle (Requirement 6) and Runner Job (Requirement 7) flows
4. THE Server SHALL execute every matched Project's Target concurrently
   (Requirement 17.1 of the global spec), not sequentially
5. WHEN every Target from this event has reached a final outcome (result or
   timeout), THE Server SHALL post the consolidated comment (Requirement
   10) — waiting for the slowest Target, per Requirement 17.2 of the global
   spec

### Requirement 3: Comment Trigger Detection, Parsing, and Collaborator Authorization

**User Story:** As a platform operator, I want every PR comment checked for
trigger commands and every author checked for collaborator status before
anything executes, so that only genuine, authorized requests reach the
orchestration logic.

#### Acceptance Criteria

1. WHEN `HandleIssueComment` receives action `created`, THE Server SHALL
   call `github.ParseTriggers` on the comment body
2. IF `ParseTriggers` returns `(nil, github.ErrNoTrigger)`, THE Server SHALL
   take no action — an ordinary discussion comment is not a Trigger Comment
   (Requirement 4.7 of github-integration's requirements)
3. IF `ParseTriggers` returns one or more `TriggerCommand`s alongside a
   non-nil `MalformedTriggerErrors`, THE Server SHALL still process every
   returned well-formed `TriggerCommand` (Requirement 4.8 of
   github-integration's requirements: one typo'd line must not discard
   every other action in the same comment) and SHALL include the malformed
   lines' content in a reply comment, so the author sees which line was
   ignored and why
4. BEFORE processing any `TriggerCommand` from a comment, THE Server SHALL
   verify via `Authorizer.IsCollaborator` that the Comment's Author is a
   repository collaborator; IF NOT, THE Server SHALL post a reply comment
   indicating insufficient permission and SHALL NOT process any
   `TriggerCommand` from that comment (Requirement 16.1-16.2 of the global
   spec)
5. THE Server SHALL log every collaborator-authorization decision made
   under this requirement with the comment author's username and the
   decision (Requirement 16.5 of the global spec)

### Requirement 4: Comment Trigger Target Resolution and Per-Target Validation

**User Story:** As a developer, I want `/turnip apply`, `/terraform apply
project-a`, and similar comments to resolve to exactly the Projects and
operation I meant, and to be told clearly when they don't, so that I can
trust a triggered operation ran against what I intended.

#### Acceptance Criteria

1. FOR a `TriggerCommand` whose `Tool` token is `"turnip"`, THE Server SHALL
   consider every configured Project a Target candidate regardless of that
   Project's own tool
2. FOR a `TriggerCommand` whose `Tool` token is a specific tool name (e.g.
   `"terraform"`), THE Server SHALL consider only configured Projects whose
   `Tool` field matches that token a Target candidate
3. WHERE `TriggerCommand.Projects` is non-empty, THE Server SHALL further
   narrow Target candidates to Projects whose `Name` appears there
   (Requirement 5.4 of the global spec); IF a listed name matches no
   Target candidate, THE Server SHALL post an error comment naming the
   unmatched Project and SHALL NOT resolve any Target from that
   `TriggerCommand`
4. WHERE `TriggerCommand.Projects` is empty, THE Server SHALL target every
   remaining Target candidate (Requirement 5.3 of the global spec: "all
   Projects configured in turnip.yaml") — unfiltered by `whenModified`,
   unlike the auto-plan flow (Requirement 2)
5. FOR EACH Target candidate resolved by 4.1-4.4, THE Server SHALL validate
   `TriggerCommand.Operation` against that candidate's tool's Plugin (via
   the Plugin Registry) `GetOperations()`; IF unrecognized for that
   candidate's tool, THE Server SHALL exclude that candidate from
   execution and instead record a rejected outcome for it (Requirement
   10.6) rather than failing the whole `TriggerCommand` — this matters
   specifically for a `"turnip"`-tool command spanning Projects of
   different tools, where an operation name valid for one tool (e.g.
   Terraform's `"apply"`) may not exist for another (e.g. Pulumi's
   equivalent is `"up"`, once Slice 7 ships)
6. FOR EACH Target whose Operation is not that Project's tool's
   `GetPlanOperation()` (i.e. apply, sync, destroy, or any other mutating
   tool-native operation), THE Server SHALL require the comment Author to
   hold `HasWritePermission` on the repository (Requirement 16.4 of the
   global spec) before executing it; IF the author lacks it, THE Server
   SHALL exclude that Target and record a rejected outcome for it
   explaining that write permission is required, without affecting any
   other Target from the same or a different `TriggerCommand` in the
   comment
7. `TriggerCommand.ExtraArgs`, when present, SHALL be passed verbatim as
   `ExecuteOptions.ExtraArgs`/`jobs.OperationParams.ExtraArgs` to every
   Target resolved from that `TriggerCommand` (Requirement 6.2 of the
   global spec)

### Requirement 5: Manual Unlock via Comment

**User Story:** As a developer, I want to release a stuck lock by commenting
`/turnip unlock`, so that I don't need direct Redis access to recover from
an abandoned or superseded PR.

#### Acceptance Criteria

1. THE Server SHALL recognize a `TriggerCommand` whose `Operation` is
   `"unlock"` as a request to release Locks rather than execute a Plugin
   Operation, resolving its Targets' Projects exactly as Requirement 4.1-4.4
   describes, but SKIPPING Requirement 4.5's Plugin-operation validation —
   `"unlock"` is never a Plugin operation
2. BEFORE releasing a given Project's Lock, THE Server SHALL require the
   comment Author to hold `HasWritePermission` (Requirement 16.4, resolved
   per this document's Introduction)
3. FOR EACH resolved Project, THE Server SHALL call
   `LockManager.ReleaseLock(ctx, projectKey, prNumber)` using the
   triggering PR's number
4. IF `ReleaseLock` succeeds (including its no-op case where no Lock
   existed), THE Server SHALL name that Project in a confirmation comment
   listing every Project unlocked (Requirement 20.4 of the global spec); IF
   it returns `lock.ErrLockedByOtherPR`, THE Server SHALL instead post an
   error naming which PR actually holds that Project's Lock, referencing it
   by number in GitHub's `#123` shorthand (Requirement 6.2's convention)
5. Processing an `"unlock"` `TriggerCommand` SHALL NOT create a Runner Job,
   GitHub check run, or Operation Record — it is a Lock-and-comment-only
   action

### Requirement 6: Lock Lifecycle Orchestration

**User Story:** As a developer, I want the Server to acquire, verify, and
release locks at exactly the right points in a plan/apply lifecycle, so
that concurrent operations on the same Project are impossible and an apply
always uses exactly what was planned.

#### Acceptance Criteria

1. BEFORE executing a Target whose Operation is its Project's tool's
   `GetPlanOperation()`, THE Server SHALL call
   `LockManager.AcquireLock(ctx, projectKey, prNumber, pullRequestURL, lockedBy)`,
   constructing `pullRequestURL` from the well-known
   `https://github.com/{owner}/{repo}/pull/{number}` pattern (neither
   `github.PullRequest` nor `github.WebhookEvent` carries a PR URL field)
   and `lockedBy` from `TriggeredBy` (`"auto"` or the comment author)
2. IF `AcquireLock` returns `false`, THE Server SHALL NOT create a Runner
   Job or check run for that Target, and SHALL instead call
   `LockManager.GetLockStatus` to include, in the eventual consolidated
   comment, a message stating that Project cannot run because it is locked
   by another PR, referencing that PR by number in GitHub's `#123` shorthand
   (`LockStatus.PRNumber`) rather than a raw URL, so GitHub autolinks it
   within the repo (Requirement 7.2 of the global spec)
3. BEFORE executing a Target whose Operation is NOT its Project's
   `GetPlanOperation()` (apply, sync, destroy, or any other mutating
   tool-native operation), THE Server SHALL call
   `LockManager.IsLockedByPR`; IF `false`, THE Server SHALL post an error
   explaining that no Lock (or a different PR's Lock) is held and that a
   new plan is required, and SHALL NOT execute that Target
4. FOR a Target whose Operation is specifically its Project's
   `GetApplyOperation()`, once 6.3's verification passes, THE Server SHALL
   additionally call `LockManager.GetPlanData` and pass the returned bytes
   as `jobs.OperationParams.PlanData` for the Runner Job (Requirement 7.5 of
   the global spec); IF `GetPlanData` returns `lock.ErrNoPlanData`, THE
   Server SHALL post an error requiring a new plan and SHALL NOT execute
   that Target
5. FOR a Target whose Operation is neither `GetPlanOperation()` nor
   `GetApplyOperation()` (e.g. Helmfile's `"sync"`/`"destroy"`), THE Server
   SHALL pass no `PlanData` (nil) after 6.3's Lock verification passes —
   only the designated apply operation consumes stored plan data
6. WHEN a plan-operation Target completes with `HandleResult.Success ==
   true` and non-empty `PlanData`, THE Server SHALL call
   `LockManager.StorePlanData` with that `PlanData` and `Changes` before
   considering the Target complete (Requirement 7.3 of the global spec)
7. WHEN an apply-operation Target (Operation == `GetApplyOperation()`)
   completes with `HandleResult.Success == true`, THE Server SHALL call
   `LockManager.ReleaseLock` for that Project (Requirement 7.6 of the
   global spec) and SHALL note, in that Target's `ProjectResult.Output`
   (Requirement 10), that the Lock was released and the Project is free
   for another PR to plan against — so a release triggered by a successful
   apply is stated explicitly, the same as a release triggered by manual
   unlock (Requirement 5.4) or by PR merge/close (Requirement 11.5), rather
   than being implied only by the apply's own success message
8. A Target that completes with `HandleResult.Success == false`, or times
   out (Requirement 8), SHALL NOT trigger any Lock mutation beyond 6.6/6.7
   — a failed or timed-out plan leaves the Lock held with no new plan data,
   and a failed or timed-out apply leaves the Lock held rather than
   released, so the PR can retry without needing to re-plan from scratch

### Requirement 7: Runner Job Creation, Operation Record Persistence, and Result Handling

**User Story:** As a platform developer, I want the Server to create a
Runner Job for each Target and correctly route that Runner's log lines and
final result back to the right Lock/check-run/comment actions — even if a
different Server replica handles the callback than the one that created
the Job — so that horizontal scaling doesn't break correctness.

#### Acceptance Criteria

1. THE Server binary SHALL run, within one process, the HTTP webhook
   handler (`github.NewWebhookHandler`, implementing `github.EventHandler`),
   the gRPC `OperationService` server (`rpc.NewServer`, implementing
   `rpc.OperationHandler`), and hold a `jobs.Client`, all sharing one Redis
   connection and one `github.GitHubClient`/`AppAuth`; THE Server SHALL
   construct its Plugin Registry at startup from `internal/plugin`'s
   available Plugins
2. FOR EACH Target the Server decides to execute (Requirements 2, 4, 5), THE
   Server SHALL generate a unique Operation ID before creating any
   Kubernetes resource for it
3. BEFORE calling `jobs.Client.Create`, THE Server SHALL persist an
   Operation Record to Redis keyed by that Operation ID (see Glossary and
   this document's Introduction), so that any Server instance's gRPC
   handler — not necessarily the instance that created the Job — can
   resolve that Operation ID's context
4. THE Server SHALL call `jobs.BuildJob` and `jobs.Client.Create` with an
   `OperationParams` populated from the Operation Record (including
   `PlanData` per Requirement 6.4/6.5), and SHALL record the created Job's
   name into the Operation Record
5. IF `BuildJob` returns an `*jobs.UnrecognizedToolError` or
   `*jobs.UnrecognizedVersionError`, THE Server SHALL post an error comment
   identifying the Project and the invalid tool/version and SHALL NOT
   create a Job (Requirement 18.9 of the global spec) — an unrecognized
   `Tool` value itself was already rejected earlier by `config.Parse`
   (Requirement 1), so this path is reached only for an invalid `version`
   value within an otherwise-valid Project
6. IF `jobs.Client.Create` itself fails (a Kubernetes API error), THE
   Server SHALL post a failure comment and mark that Target's check run —
   if already created — as failed (Requirement 14.5's Job Creation Failure
   handling in the global design)
7. THE Server's `HandleLog` implementation SHALL resolve the Operation
   Record for the given `operationID` and treat receiving it as evidence
   the Operation has started, for Requirement 8's timeout
8. THE Server's `HandleResult` implementation SHALL resolve the Operation
   Record, perform the Lock (Requirement 6), check-run (Requirement 9), and
   consolidated-comment (Requirement 10) actions the result implies, and —
   once every follow-up action has been attempted — delete the Operation
   Record from Redis
9. THE Server SHALL treat a `HandleResult` call for an `operationID` whose
   Operation Record no longer exists as a successful no-op: it SHALL return
   without error and SHALL NOT repeat any Lock, check-run, or comment
   action — this is what makes a Runner's retried final-result delivery
   (grpc-runner's Requirement 4.4-4.5) idempotent on the Server side, an
   item grpc-runner's requirements doc explicitly left for this slice
10. THE Server SHALL resolve the Kubernetes ServiceAccount each Target's
    Runner Job Pod runs as — from its own `TURNIP_RUNNER_SERVICE_ACCOUNT`
    configuration — and SHALL do so before acquiring any Lock for that
    Target, so a refusal never leaves a Lock held for an Operation that
    never runs
11. WHERE a Project sets `config.serviceAccount` in turnip.yaml, THE
    Server SHALL honor it only IF its own
    `TURNIP_RUNNER_SERVICE_ACCOUNT_ALLOW_FROM_CONFIG` is true; otherwise
    THE Server SHALL reject that Target with a comment naming the Project
    and the requested ServiceAccount, and SHALL NOT create a Runner Job
    for it

### Requirement 8: Runner Job Start Timeout and Diagnosis

**User Story:** As a developer, I want to be told promptly — and told why —
if my plan/apply never actually started running, so that I'm not left
waiting indefinitely on a Runner Job that failed to schedule, and so the
failure message tells me something more useful than "timed out."

#### Acceptance Criteria

1. WHEN THE Server creates a Runner Job for a Target (Requirement 7.4), THE
   Server SHALL record a start deadline of Job-creation time + 5 minutes
   into that Target's Operation Record (Requirement 14.5 of the global
   spec) — a value any Server instance can observe, not an in-process timer
   scoped to the instance that created the Job (Requirement 12: no
   instance-pinned state may be load-bearing for correctness)
2. WHEN `HandleLog` or `HandleResult` is received for an Operation ID, THE
   Server SHALL atomically record that Operation as started — e.g. via a
   compare-and-mutate that only succeeds if it has not already been
   recorded as started — so a concurrently-running sweep (8.3) cannot race
   a genuinely-arrived callback into being reported as a timeout
3. THE Server SHALL run a periodic sweep, on every instance, that finds
   Operation Records whose start deadline has passed and which have not
   been recorded as started (8.2), frequently enough that a stuck Job is
   reported within a bounded delay after its 5-minute deadline (the exact
   interval is a design.md/tasks.md implementation detail, not fixed by
   this requirement); THE Server SHALL claim each such record via an
   atomic compare-and-mutate (mirroring `internal/lock`'s Lua-script CAS
   pattern) so exactly one instance's sweep processes a given expired
   Operation even when multiple instances' sweeps overlap
4. FOR an Operation claimed by 8.3, THE Server SHALL query the Kubernetes
   API — via a status-query method this slice adds to `jobs.Client`
   (Requirement 7.1), looked up by the Operation Record's Job name or the
   `turnip.ivan.vc/operation-id` label `jobs.BuildJob` already sets — for
   that
   Job's Pod status, and SHALL include what it finds (e.g. an image-pull
   failure, a scheduling failure, a Pod still `Pending`, or no Job/Pod
   found at all) in the reported failure's detail text, rather than a bare
   "timed out" message alone
5. THE Server SHALL treat an Operation claimed by 8.3 as a timeout failure:
   mark its check run completed/failure, include it (with 8.4's diagnostic
   detail) in the consolidated comment (Requirement 10), and — per
   Requirement 6.8 — leave its Lock held rather than releasing it
6. THE Server SHALL NOT call `jobs.Client.Delete` as part of reporting a
   timeout failure. A Runner that connects after the Server gives up (a
   slow image pull, a delayed scheduling decision) may still be running
   real tool work; forcibly deleting its Job risks killing that work
   mid-flight, mirroring grpc-runner's Requirement 2.4 rationale. The Job's
   TTL (grpc-runner Requirement 7.4/9.1) is this path's cleanup mechanism,
   not an explicit delete
7. IF `HandleLog` or `HandleResult` IS received for an Operation ID before
   8.3's sweep claims it (per 8.2's atomic recording), THE Server SHALL
   take no timeout action for that Operation, even if the eventual
   `HandleResult` itself reports a failed Operation — a reported failure is
   a normal outcome, not a timeout

### Requirement 9: GitHub Check Run Lifecycle Orchestration

**User Story:** As a developer, I want each Project's operation status
surfaced as its own GitHub check run, so that I can see per-Project status
without leaving the PR's checks tab.

#### Acceptance Criteria

1. AFTER a Target's Lock step (Requirement 6.1-6.2 or 6.3-6.4) succeeds and
   BEFORE creating its Runner Job, THE Server SHALL call
   `GitHubClient.CreateCheckRun` with `Status: "in_progress"` and `HeadSHA`
   set to the PR's head commit (Requirement 9.1 of the global spec),
   storing the returned check run ID in that Target's Operation Record
2. WHEN `HandleResult` reports `Success: true` for a Target, THE Server
   SHALL call `UpdateCheckRun` with `Status: "completed"`,
   `Conclusion: "success"`, and a `Summary`/`Text` including the
   `Changes` add/change/destroy counts (Requirement 9.2, 9.4 of the global
   spec)
3. WHEN `HandleResult` reports `Success: false` for a Target, THE Server
   SHALL call `UpdateCheckRun` with `Status: "completed"`,
   `Conclusion: "failure"`, including `ErrorMessage`/`Output` in `Text`
   (Requirement 9.3 of the global spec)
3a. THE Server SHALL carry each check run's outcome in its `Title` and
   `Summary` — never by varying its `Name`, which is the check run's
   stable identity and what a repository's required status checks and
   branch protection rules match on; a name varying per run would
   register as a separate check each time and never satisfy a rule
   configured against the original. Every check-run call SHALL therefore
   supply both fields: `in progress` at creation (9.1), `success`/
   `failure` at result time (9.2/9.3), `failure` when the Runner Job
   could not be created (Requirement 7.6), and `timed out` from the
   sweep (Requirement 8). This also satisfies github-integration's
   Requirement 6.5, which requires both fields whenever any check-run
   output is sent at all
4. THE Server SHALL create one check run per Target — never share one check
   run across multiple Projects (Requirement 9.5 of the global spec)
5. IF `CreateCheckRun` or a later `UpdateCheckRun` for a Target fails,
   THE Server SHALL log the error and proceed regardless — check-run
   reporting is a secondary status surface, not a precondition for
   running the operation the user asked for (mirrors the global design's
   Error Handling section: "falling back to comment-only status
   reporting") — AND THE Server SHALL append a note naming the underlying
   error to that Target's `ProjectResult.Output`, so the consolidated
   comment (Requirement 10) states that the check run is missing and why.

   **Amended 2026-09**: the original criterion stopped at "log the error
   and proceed". In practice a real 422 from `CreateCheckRun` (see
   github-integration's task 22) produced a PR with a result comment and
   no check run at all, with the reason reachable only by reading the
   Server's log — the information existed and was discarded. "Falling
   back to comment-only status reporting" is only honest if the comment
   says the fallback happened.

### Requirement 10: Consolidated PR Comment Orchestration

**User Story:** As a developer, I want plan results and apply results
posted as clear, uncluttered comment threads, so that a long-running PR's
history of decisions stays readable instead of accumulating into an
unreviewable flood.

#### Acceptance Criteria

1. FOR one triggering webhook event (a PR open/sync, or one `issue_comment`
   possibly containing several `TriggerCommand`s), THE Server SHALL wait
   for every resolved Target to reach a final outcome — a `HandleResult` or
   a Requirement 8 timeout — before posting anything (Requirement 17.2 of
   the global spec)
2. THE Server SHALL partition the event's accumulated `[]ProjectResult`
   into two groups by each result's Operation: **plan-kind** (Operation
   equals that Project's tool's `GetPlanOperation()`) and **apply-kind**
   (every other operation — apply, sync, destroy, or any other mutating
   tool-native operation), and SHALL build each non-empty group's comment
   body/bodies independently via `github.BuildConsolidatedComment` —
   including, within each group, Targets resolved from different
   `TriggerCommand`s parsed out of the same comment (Requirement 17.1,
   17.3-17.4 of the global spec; resolves github-integration's Out-of-Scope
   item on multi-`TriggerCommand` result reporting)
3. FOR the plan-kind group, WHERE a Server-level configuration flag
   (default disabled) is enabled AND a Plan Comment Record (see Glossary)
   exists for that PR, THE Server SHALL mark every comment ID it lists as
   outdated — best-effort, logging rather than failing the request if a
   call errors — before posting anything new
4. THE Server SHALL post the plan-kind body/bodies as entirely new
   comments — never via an update to a previous comment — regardless of
   whether the flag in 10.3 is enabled or whether a Plan Comment Record
   existed
5. WHERE the flag in 10.3 is enabled, THE Server SHALL replace that PR's
   Plan Comment Record with the newly-posted plan comments' identifiers
   after 10.4 completes; WHERE the flag is disabled, THE Server SHALL NOT
   create or update a Plan Comment Record at all
6. IF the flag in 10.3 is enabled but no Plan Comment Record exists for
   the PR (e.g. its safety-net expiry — Requirement 11 covers the primary
   lifecycle — elapsed since the last plan, or this is the PR's first plan
   since the flag was enabled), THE Server SHALL skip 10.3's marking step
   and proceed directly to 10.4 — a missed "mark outdated" is accepted as
   a rare, low-severity degradation, and THE Server SHALL NOT perform any
   fallback search across the PR's comment history to recover it
7. FOR the apply-kind group, THE Server SHALL always post entirely new
   comments (mirroring 10.4) and SHALL NOT mark any previous plan or apply
   comment as outdated, maintain any record for it, or ever update a
   previous apply comment in place — an apply is a permanent record of
   what actually happened to real infrastructure, never a superseded
   prediction the way an older plan is, so nothing about it collapses out
   of view
8. A Target rejected under Requirement 6.2 (locked by another PR), 6.3/6.4
   (apply attempted without a valid Lock/plan data), 4.5 (operation
   unrecognized for that Project's tool), or 4.6 (insufficient write
   permission) SHALL be included as a `ProjectResult` (`Success: false`,
   `Output` explaining the rejection) in whichever group (10.2) its own
   Operation places it in — these are per-Project outcomes of an otherwise-
   normal batch, not whole-comment failures. Whole-comment-level
   rejections that never resolve to any Project at all — Requirement 3.4's
   non-collaborator gate and Requirement 4.3's unmatched-project-name
   error — are reported via a standalone reply comment instead, since
   there is no Project row, or even a determined kind, to attach them to
9. IF a group (10.2) is empty, THE Server SHALL take no action for that
   group at all — no `BuildConsolidatedComment` call, no posting, and (for
   the plan-kind group specifically) no marking-outdated and no Plan
   Comment Record change, since an empty group means nothing about that
   thread changed for this event. IF both groups are empty (Requirement
   2.2's empty `MatchProjects` result, or every `TriggerCommand` in a
   comment was rejected at the whole-comment level per 10.8), no comment
   activity happens at all for the event — Requirement 2.5's "skip
   operation execution" extends this far

### Requirement 11: Lock Release on PR Merge/Close

**User Story:** As a developer, I want locks I hold automatically released
when my PR is merged or closed, so that I never have to remember to unlock
manually.

#### Acceptance Criteria

1. WHEN `HandlePullRequest` receives action `"closed"`, THE Server SHALL
   determine the Project Keys that might hold a Lock for that PR by
   re-fetching turnip.yaml/`Config` at the PR's last known head SHA
   (Requirement 1) and constructing a Project Key (Requirement 6's
   `{owner}/{repo}/{project}` form) for every configured Project — not only
   ones matched by `whenModified` — since a Lock may exist for any
   configured Project regardless of what most recently changed
2. THE Server SHALL treat `"closed"` identically whether or not GitHub's
   payload marks the PR merged: both cases release Locks the same way
   (Requirement 20.1-20.2 of the global spec both call for release; this
   platform draws no locking-behavior distinction between them)
3. FOR EACH such Project Key, THE Server SHALL call
   `LockManager.IsLockedByPR` then `ReleaseLock` where true
4. IF turnip.yaml cannot be fetched or parsed at close time (e.g. it was
   deleted in the PR's final commit), THE Server SHALL fall back to
   attempting release using whatever Project Keys it can still determine
   from the last successfully parsed `Config` for that PR, if any, and
   SHALL log — rather than fail the whole webhook over — any Project it
   could not check as a result
5. WHEN one or more Locks are released via this flow, THE Server SHALL post
   a single comment to the PR naming which Projects were unlocked
   (Requirement 20.4 of the global spec); IF zero Locks were held, THE
   Server SHALL post no comment
6. A PR reopened after being closed SHALL find no Lock for any Project this
   flow already released, and so SHALL require a fresh plan before any
   apply — exactly like an ordinary first-time PR (Requirement 20.5 of the
   global spec); no logic beyond Requirements 2 and 6 is needed to satisfy
   this
7. WHEN `HandlePullRequest` handles action `"closed"` (11.1), THE Server
   SHALL also delete that PR's Plan Comment Record (Requirement 10), if
   any — a closed PR has no further plan activity for that record to
   support, making this the primary, precise cleanup path; the record's
   safety-net expiry (Requirement 10.6) exists only for the rare case this
   delete is itself missed (e.g. the webhook is never delivered)

### Requirement 12: Server Statelessness and Multi-Instance Operation

**User Story:** As a platform operator, I want to run several Server
replicas behind a load balancer with zero coordination between them, so
that a rolling deploy or an instance crash never drops an in-flight
operation.

#### Acceptance Criteria

1. THE Server process SHALL hold no in-process state that this slice's
   correctness depends on across separate requests; every piece of
   cross-request state this slice introduces — Operation Records, Plan
   Comment Records — SHALL live in Redis, addressed by keys
   derived from stable identifiers (Operation ID; `owner/repo/PR number`),
   never an in-memory map (Requirement 19.1, 19.3, 19.7 of the global spec)
2. THE Server's HTTP webhook handler and gRPC `OperationService` SHALL both
   be exposed by the same binary and SHALL both be safe to run as multiple
   concurrent replicas behind a load balancer/Service — extending
   Requirement 19.2's "ANY Server instance SHALL be able to process ANY
   webhook event" to "ANY Server instance SHALL be able to handle ANY
   Runner's gRPC callback," which Requirement 7.3's Redis-backed Operation
   Record is what makes possible; Requirement 8.3's periodic timeout sweep
   is held to the same bar — every replica runs it independently, with no
   replica's participation required for correctness (Requirement 8.3's
   compare-and-mutate claim is what makes redundant, uncoordinated sweeps
   safe rather than a source of duplicate timeout reports)
3. THE Server SHALL NOT require a Runner Job, the GitHub webhook sender, or
   any other caller to be pinned to the specific Server instance/pod that
   initiated an Operation (Requirement 19.4-19.6 of the global spec)

## Out of Scope

- Terraform and Pulumi Plugin implementations, and any dispatch logic
  specific to them — Slice 7 (`terraform-pulumi-plugins`); this slice's
  Plugin Registry works with whatever `internal/plugin` provides, currently
  Helmfile only
- Periodic cleanup of orphaned Kubernetes Jobs or stale Locks, a
  lock-status web UI, and an admin force-unlock API — mentioned in the
  global design's Error Handling/Resource Cleanup narrative but explicitly
  deferred by earlier slices: redis-lock-manager's requirements
  ("deferred indefinitely unless a future slice picks it up") and
  github-integration's requirements (orphaned-Job/lock cleanup → Slice 8)
- Metrics, structured logging conventions, and dashboards — Slice 8 (HA,
  Observability & Deployment)
- Kubernetes manifests/Helm chart for deploying the Server itself — Slice 8
- GitHub API retry/backoff beyond what the underlying client library
  already provides, and rate-limit-aware operation queuing — mentioned in
  the global design's narrative but not tied to any numbered acceptance
  criterion; deferred, unchanged from github-integration's Out of Scope
- A distinct RPC/handler signal for "the Runner has started," separate
  from its first log line — grpc-runner's `OperationHandler` (Slice 5)
  exposes only `HandleLog`/`HandleResult`; this slice treats the first
  `HandleLog`/`HandleResult` as the start signal (see Introduction) rather
  than asking Slice 5 to add a new RPC message
- General Kubernetes Job/Pod health monitoring, alerting, or a persistent
  watch beyond the one-time, on-timeout diagnostic status query Requirement
  8.4 describes — that query only fires once an Operation has already been
  claimed as timed out, purely to enrich the failure message; anything
  broader (dashboards, alerting on Job failures generally) is Slice 8 (HA,
  Observability & Deployment)
- GitLab/Bitbucket support — no numbered requirement in the global spec
  asks for it; the platform's constraints section only names GitHub
- A fallback scan across a PR's full comment history to recover a lost or
  expired Plan Comment Record — Requirement 10.6 accepts the rare miss
  instead; not built
- Minimizing/hiding comments for any reason other than a superseded plan
  comment (`RESOLVED`, `DUPLICATE`, `SPAM`, etc., or minimizing anything
  other than turnip's own previous plan comments) — this slice's
  `MinimizeComment` usage is narrowly for the `OUTDATED` case Requirement
  10.3 describes
