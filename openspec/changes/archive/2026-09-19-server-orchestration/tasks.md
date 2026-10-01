# Implementation Plan: Server Orchestration (Slice 6)

## Overview

This plan implements two small, independent amendments to already-complete
packages (`internal/github`'s comment-lifecycle methods, `internal/jobs`'s
`Status`), then builds the new `internal/orchestrator` package from its
foundational, dependency-free files (Plugin Registry, the Redis-backed
records and their Lua scripts, the Pub/Sub notify helper, env config)
outward to the files that depend on them (Target resolution → the
Lock→check-run→Job execution flow and plan/apply comment posting → the
gRPC callback handlers and timeout sweep → the two webhook event
handlers), then wires it all into `cmd/server`, then tests. Tasks are
ordered so a task never depends on a later one; the Task Dependency Graph
at the end marks which tasks have no dependency on each other and can be
worked in parallel.

## Tasks

- [x] 1. Amend `internal/github` for plan-comment lifecycle management (Slice 4 amendment)
  - [x] 1.1 Add `PostedComment`, change `PostComment`'s return type, add `MinimizeComment` and `graphql.go`
    - In `internal/github/client.go`: define `type PostedComment struct { ID int64; NodeID string }`; change `PostComment`'s signature from `(int64, error)` to `(*PostedComment, error)`, populating `NodeID` from the existing `go-github` create-comment response's `GetNodeID()` (already available on that response — no extra API call)
    - Update `GitHubClient` interface's `PostComment` signature to match; there are no other callers yet in this codebase, so this is a plain signature change, not a new method alongside the old one (design.md's "Backward Compatibility" note)
    - Create `internal/github/graphql.go`: a `MinimizeComment(ctx context.Context, nodeID string) error` method on `*Client`, sending one hand-rolled GraphQL POST — `mutation($id: ID!) { minimizeComment(input: {subjectId: $id, classifier: OUTDATED}) { minimizedComment { isMinimized } } }` — to `https://api.github.com/graphql` via `&http.Client{Transport: c.itr}` (reusing the same installation-authenticated transport `c.gh` already uses), decoding the response only far enough to detect a GraphQL-level error and return it wrapped
    - Add `MinimizeComment(ctx context.Context, nodeID string) error` to the `GitHubClient` interface
    - Verify compilation with `go build ./internal/github/...`
    - _Requirements: Slice 6's Requirement 10.3, 10.4 (this is the primitive those acceptance criteria call for)_

  - [x] 1.2 Update existing `PostComment` tests and add `MinimizeComment`/`graphql.go` tests
    - Update any existing `client_test.go` assertions on `PostComment`'s return value for the new `*PostedComment` shape
    - `graphql_test.go`: against an `httptest.Server` standing in for `api.github.com/graphql`, assert the outgoing request body's shape (`query`/`variables.id`/`variables` implying `classifier: OUTDATED` per the fixed mutation string), assert a GraphQL-level error in the response is surfaced as a Go error, assert a successful response returns nil
    - Verify `go test ./internal/github/...` passes
    - _Requirements: test coverage for 1.1_

  - [x] 1.3 Record the amendment in github-integration's `tasks.md`
    - Append a new numbered task there (this repo's audit-trail convention) summarizing 1.1/1.2 and linking back to this slice's Requirement 10 as the reason
    - _Requirements: (process, no direct requirement)_

- [x] 2. Amend `internal/jobs` for Job/Pod status (Slice 5 amendment)
  - [x] 2.1 Add `JobStatus` and `Client.Status`
    - In `internal/jobs/status.go` (new file): define `JobStatus` per design.md's Data Model, and `func (c *Client) Status(ctx context.Context, jobName string) (*JobStatus, error)` per design.md's "`internal/jobs/status.go` (Slice 5 amendment)" section — `c.jobs.Get`, `JobStatus{JobFound: false}` on not-found (not an error), then `c.pods.List` with the `batch.kubernetes.io/job-name={jobName}` selector, checking init-container `Waiting` statuses before regular container statuses
    - In `internal/jobs/client.go`: add a `pods corev1client.PodInterface` field to `Client`, set in `NewClient` via `clientset.CoreV1().Pods(namespace)` — no signature change
    - Verify compilation with `go build ./internal/jobs/...`
    - _Requirements: Slice 6's Requirement 8.4 (this is the primitive that acceptance criterion calls for)_

  - [x] 2.2 Write `internal/jobs/status_test.go`
    - Using `k8s.io/client-go/kubernetes/fake`: a not-found Job → `JobStatus{JobFound: false}`, nil error; a Job with no Pods yet → `JobFound: true`, zero `PodPhase`; a Job whose Pod has an init container in `Waiting{Reason: "ImagePullBackOff"}` → that reason surfaced, even when a later regular container is also `Waiting`; a Job whose Pod is cleanly `Running` with no `Waiting` statuses → `PodPhase: Running`, empty `PodReason`
    - _Requirements: test coverage for 2.1_

  - [x] 2.3 Record the amendment in grpc-runner's `tasks.md`
    - Append a new numbered task there summarizing 2.1/2.2 and linking back to this slice's Requirement 8 as the reason
    - _Requirements: (process, no direct requirement)_

- [x] 3. Checkpoint - Verify the amended Slice 4/5 packages compile and their existing tests still pass
  - Ensure `go build ./...` succeeds and `go test ./internal/github/... ./internal/jobs/...` passes, including every pre-existing test (no behavior other than `PostComment`'s signature changed for existing callers, and there were none). Ask the user if questions arise.

- [x] 4. Implement `internal/orchestrator`'s foundational, dependency-free files
  - [x] 4.1 Create `internal/orchestrator/registry.go`
    - Define `PluginRegistry map[string]plugin.Plugin` and `NewPluginRegistry() PluginRegistry`, mapping `config.ToolHelmfile` to `plugin.NewHelmfilePlugin()` (the only entry until Slice 7 adds Terraform/Pulumi)
    - Verify compilation with `go build ./internal/orchestrator/...`
    - _Requirements: 7.1_

  - [x] 4.2 Create `internal/orchestrator/record.go`
    - Define `OperationRecord` per design.md's Data Model
    - Define the two Lua scripts (`markStarted`, `claimForFinalization`) as package-level string constants, exactly per design.md's "Atomicity via Lua Scripts" section
    - Define `recordStore` and implement `Create`, `SetJobName`, `SetCheckRunID` (plain GET/mutate/SET, single-writer — no script needed), `MarkStarted` (wraps `markStarted`), `ClaimForResult` and `ClaimForTimeout` (both wrap `claimForFinalization`, passing `mode: "result"`/`"timeout"` respectively; `ClaimForTimeout` also passes `now`), `Delete`, and `ScanOperationKeys` (a `SCAN` cursor loop over `operation:*`, per this repo's "paginate through all, never truncate" convention already established in `internal/github.GetModifiedFiles`)
    - `Create` sets the key with a 24-hour `EX` TTL (design.md's Redis Key Format section); every other write (`SetJobName`, `SetCheckRunID`, and both Lua scripts' `SET`) uses `KEEPTTL` so the original 24-hour countdown is never reset by a routine update
    - Verify compilation with `go build ./internal/orchestrator/...`
    - _Requirements: 7.2, 7.3, 7.8, 7.9, 8.1, 8.2, 8.3_

  - [x] 4.3 Create `internal/orchestrator/plancommentrecord.go`
    - Define `PlanCommentRecord` per design.md's Data Model
    - Implement `Get(ctx, owner, repo string, prNumber int) (*PlanCommentRecord, error)` (nil, nil on missing key — mirroring `internal/lock`'s "not found is not an error" convention), `Set(ctx, owner, repo string, prNumber int, nodeIDs []string) error` (plain SET with a 90-day `EX` TTL, reset on every call — design.md's Redis Key Format note on why this one refreshes unlike `operation:*`'s), and `Delete(ctx, owner, repo string, prNumber int) error`
    - Verify compilation with `go build ./internal/orchestrator/...`
    - _Requirements: 10.3, 10.5, 10.6, 11.7_

  - [x] 4.4 Create `internal/orchestrator/notify.go`
    - Implement `publishDone(ctx context.Context, client *redis.Client, operationID string, result github.ProjectResult) error` (JSON-marshal `result`, `PUBLISH` to `operation-done:{operationID}`) and `waitForDone(ctx context.Context, client *redis.Client, operationID string) (github.ProjectResult, error)` (`SUBSCRIBE` to the same channel, block on the subscription's message channel until one arrives or `ctx` is done, JSON-unmarshal the payload)
    - Subscribing *before* the Job is created is not required here — the channel is created by `SUBSCRIBE` itself and Redis Pub/Sub has no "replay," so `waitForDone` must be called (subscription established) before there is any chance `publishDone` could fire for that operation ID; task 7.1 is responsible for that ordering, not this file
    - Verify compilation with `go build ./internal/orchestrator/...`
    - _Requirements: 10.1 (the mechanism 7.8/7.9's publish-on-finalize and this task's wait depend on)_

  - [x] 4.5 Create `internal/orchestrator/config.go`
    - Implement `ConfigFromEnv(env func(string) string) (Config, error)` mirroring `internal/runner.ConfigFromEnv`'s testability convention: GitHub App ID/private key (path or inline PEM), webhook secret, Redis address, Kubernetes namespace, HTTP listen address, gRPC listen address, and `TURNIP_MINIMIZE_OUTDATED_PLAN_COMMENTS` (bool, default `false`)
    - Return an error naming every missing required variable at once (this repo's accumulate-everything validation convention)
    - Verify compilation with `go build ./internal/orchestrator/...`
    - _Requirements: (wiring for 10.3's flag; no other direct requirement)_

  - [x] 4.6 Create `internal/orchestrator/orchestrator.go`
    - Define `Orchestrator` (fields per design.md: `appAuth`, `locks`, `jobs`, `plugins`, `records` — the `recordStore`, `redis` — the raw `*redis.Client` `plancommentrecord.go`/`notify.go` use directly, `minimizeOutdatedPlanComments`, `startTimeout`, `sweepInterval`) and `New(appAuth *github.AppAuth, locks lock.LockManager, jobsClient *jobs.Client, plugins PluginRegistry, redisClient *redis.Client, minimizeOutdatedPlanComments bool) *Orchestrator`
    - Leave `Run` as a stub returning `nil` for now — task 11.1 fills it in once `sweepOnce` exists; leave the `var _ github.EventHandler`/`var _ rpc.OperationHandler` assertions out of this file until task 13's checkpoint, since no method exists yet for either interface
    - Verify compilation with `go build ./internal/orchestrator/...`
    - _Requirements: (wiring, no direct requirement)_

- [x] 5. Checkpoint - Verify `internal/orchestrator`'s foundational files compile
  - Ensure `go build ./internal/orchestrator/...` succeeds. Ask the user if questions arise.

- [x] 6. Implement Target resolution
  - [x] 6.1 Create `internal/orchestrator/target.go`
    - Define `Target` and `resolveTargets(cfg *config.Config, plugins PluginRegistry, cmd *github.TriggerCommand, author string, authorizer *github.Authorizer) (targets []Target, rejected []github.ProjectResult, wholeCommandErr error)` implementing Requirement 4.1-4.7 in order, exactly per design.md's `target.go` section
    - `resolveTargets` is a pure function with respect to network/Redis — `authorizer.HasWritePermission` is its only side-effecting dependency, and that's already an injected `*github.Authorizer` a test can back with a fake `GitHubClient`
    - Verify compilation with `go build ./internal/orchestrator/...`
    - _Requirements: 4.1, 4.2, 4.3, 4.4, 4.5, 4.6, 4.7_

- [x] 7. Implement the Lock → check run → Job execution flow
  - [x] 7.1 Create `internal/orchestrator/execute.go`
    - Implement `executeOne(ctx, client github.GitHubClient, repo github.Repository, pr github.PullRequest, t Target) github.ProjectResult` exactly per design.md's flowchart: plan-vs-non-plan Lock branch (Requirement 6.1-6.5), soft-failing `CreateCheckRun` (Decision 3), Operation ID generation (`uuid.NewString()`) and `OperationRecord` creation (task 4.2's `recordStore.Create`, `StartDeadline: now.Add(5*time.Minute)`), `GenerateInstallationToken` immediately before `jobs.BuildJob` (design.md's "Installation Token Timing" note — this is the *only* place in the whole package that calls it), `jobs.Client.Create`, and — critically — calling `waitForDone` (task 4.4) *before* `jobs.Client.Create` even returns is wrong ordering; the correct order is: `recordStore.Create` → establish the Pub/Sub subscription (so it's guaranteed active before any possible finalization) → `jobs.BuildJob`/`jobs.Client.Create` → `recordStore.SetJobName` → block on the already-established subscription → return its result (design.md's corrected "Result Delivery Is a Redis Pub/Sub Wait" section)
    - Every rejection path (`RJ1`-`RJ5` in design.md's flowchart) returns a `github.ProjectResult{Success: false, ...}` directly, without creating a Pub/Sub subscription at all
    - Implement `executeTargets(ctx, client github.GitHubClient, repo github.Repository, pr github.PullRequest, targets []Target) []github.ProjectResult`: one goroutine per Target calling `executeOne`, a `sync.WaitGroup`, and a mutex-guarded results slice (Requirement 2.4/17.1's concurrency requirement)
    - `execute.go` depends on a small locally-defined `jobCreator` interface (`BuildJob`/`Create`/`Delete`-shaped) that `*jobs.Client` satisfies, purely for this package's own test fakes (matching `internal/lock`'s `LockManager` interface convention) — `jobs.Client` itself stays the concrete struct Slice 5 shipped
    - Verify compilation with `go build ./internal/orchestrator/...`
    - _Requirements: 6.1, 6.2, 6.3, 6.4, 6.5, 7.2, 7.4, 7.5, 7.6, 9.1, 9.5, 15.2, 15.4_

- [x] 8. Implement plan/apply comment posting
  - [x] 8.1 Create `internal/orchestrator/comments.go`
    - Implement `func (o *Orchestrator) postResults(ctx, client github.GitHubClient, repo github.Repository, prNumber int, results []github.ProjectResult)` exactly per design.md's `comments.go` flowchart: partition by kind (Operation equals that result's Project's tool's `GetPlanOperation()` vs everything else, resolving each result's Project tool through `o.plugins`), then for the plan-kind group: minimize-if-`o.minimizeOutdatedPlanComments`-and-record-exists (best-effort, logged not returned as an error) → `PostComment` every plan body fresh → replace the `PlanCommentRecord` (via `o.redis`) if the flag is enabled; for the apply-kind group: `PostComment` every body fresh, no lookup, no record; skip a group entirely (no `BuildConsolidatedComment` call) when it's empty
    - A rejected Target's `ProjectResult` already carries its Operation from `target.go`/`execute.go`, so it partitions into the correct group automatically — no special-casing needed here
    - Verify compilation with `go build ./internal/orchestrator/...`
    - _Requirements: 10.1, 10.2, 10.3, 10.4, 10.5, 10.6, 10.7, 10.8, 10.9_

- [x] 9. Checkpoint - Verify `target.go`, `execute.go`, and `comments.go` compile together
  - Ensure `go build ./internal/orchestrator/...` succeeds. Ask the user if questions arise.

- [x] 10. Implement gRPC callback handling
  - [x] 10.1 Create `internal/orchestrator/result.go`
    - Implement `HandleLog(ctx, operationID string, line rpc.LogLine) error`: a single call to `records.MarkStarted`
    - Implement `HandleResult(ctx, operationID string, result rpc.OperationResult) error` exactly per design.md's flowchart: `ClaimForResult` (no-op return on `!claimed`, Requirement 7.9), reconstruct a `github.GitHubClient` via `o.appAuth.InstallationClient(rec.InstallationID)`, `UpdateCheckRun` if `rec.CheckRunID != 0` (Requirement 9.2/9.3), then on `result.Success`: `StorePlanData` for a plan-kind record with non-empty `PlanData` (Requirement 6.6), or `ReleaseLock` + append the "Lock released" note to `Output` for an apply-kind record (`rec.IsApply`, Requirement 6.7) — neither branch fires for a non-plan, non-apply operation or a failed result (Requirement 6.8) — then `records.Delete` and `publishDone` (task 4.4) as the very last step
    - Verify compilation with `go build ./internal/orchestrator/...`
    - _Requirements: 6.6, 6.7, 6.8, 7.7, 7.8, 7.9, 9.2, 9.3_

- [x] 11. Implement the periodic timeout sweep
  - [x] 11.1 Create `internal/orchestrator/sweep.go`
    - Implement `sweepOnce(ctx context.Context)` exactly per design.md's flowchart: `ScanOperationKeys`, then per key, `ClaimForTimeout(ctx, operationID, now)` (skip on `!claimed`), `jobs.Status(ctx, rec.JobName)` (best-effort — nil-safe on error, per Decision 3's soft-failure category), format a diagnostic `ProjectResult` from the `JobStatus` (design.md's three message templates), `UpdateCheckRun` with `Conclusion: "failure"` if a check run exists, `records.Delete`, `publishDone`
    - No `ReleaseLock` call anywhere in this function (Requirement 6.8/8.5 — a timed-out Operation leaves its Lock held) and no `jobs.Client.Delete` call (Requirement 8.6 — rely on the Job's TTL, don't risk killing a Runner that's still actually running)
    - Fill in `orchestrator.go`'s `Run(ctx) error` (task 4.6's stub): a `time.Ticker` at `o.sweepInterval` (30s default) calling `sweepOnce` each tick, exiting cleanly when `ctx` is canceled
    - Verify compilation with `go build ./internal/orchestrator/...`
    - _Requirements: 8.1, 8.2, 8.3, 8.4, 8.5, 8.6, 8.7_

- [x] 12. Implement the two webhook event handlers
  - [x] 12.1 Create `internal/orchestrator/pullrequest.go`
    - Implement `HandlePullRequest(ctx, event *github.WebhookEvent) error`: construct `client := o.appAuth.InstallationClient(event.Installation.ID)`, dispatch on `event.Action`
    - `"opened"`/`"synchronize"`: fetch+parse turnip.yaml (repo root, falling back to `.github/turnip.yaml` on `ErrFileNotFound` — Requirement 1.1-1.5), `GetModifiedFiles` + `config.MatchProjects` (Requirement 2.1), build one Target per matched Project at `GetPlanOperation()`/`TriggeredBy: "auto"` (Requirement 2.3), `executeTargets` (task 7.1), `postResults` (task 8.1) — skip both entirely on zero matched Projects (Requirement 2.2)
    - `"closed"`: fetch/parse turnip.yaml at `event.PullRequest.HeadSHA`, falling back to whatever `Config` this same call already parsed earlier if the final fetch fails (Requirement 11.4); for every configured Project (not `whenModified`-filtered), construct its Project Key and call `IsLockedByPR` then `ReleaseLock` where true (Requirement 11.1-11.3); post one comment naming unlocked Projects if any were (Requirement 11.5); delete the PR's `PlanCommentRecord` unconditionally (task 4.3's `Delete`, Requirement 11.7) — this happens whether or not any Lock was released, since a closed PR needs no further plan tracking either way
    - Every other action is a no-op
    - Verify compilation with `go build ./internal/orchestrator/...`
    - _Requirements: 1.1, 1.2, 1.3, 1.4, 1.5, 2.1, 2.2, 2.3, 2.4, 2.5, 11.1, 11.2, 11.3, 11.4, 11.5, 11.6, 11.7_

  - [x] 12.2 Create `internal/orchestrator/comment.go`
    - Implement `HandleIssueComment(ctx, event *github.WebhookEvent) error` exactly per design.md's flowchart: `ParseTriggers` (no-op on `ErrNoTrigger`, Requirement 3.2; process every well-formed command even alongside `MalformedTriggerErrors`, replying with the malformed lines' content, Requirement 3.3), `Authorizer.IsCollaborator` gate with a reply comment on failure (Requirement 3.4), fetch+parse turnip.yaml — calling `GetPullRequest` first specifically for this event type to obtain `HeadSHA` (Requirement 1.1's note on `issue_comment` payloads), then for each `TriggerCommand` **in order** (not concurrently — this is what makes a `plan project-a` followed by `apply project-a` in the same comment behave correctly, per design.md's Edge Cases): if `Operation == "unlock"`, the separate Requirement 5 path (resolve Projects per 4.1-4.4 skipping 4.5's operation validation, require `HasWritePermission`, `ReleaseLock` per Project, accumulate a confirmation/error result — no Runner Job, no check run, no Operation Record); otherwise `resolveTargets` (task 6.1) then `executeTargets` (task 7.1), accumulating both its results and `resolveTargets`' `rejected` slice
    - After every `TriggerCommand` is processed, call `postResults` (task 8.1) once with everything accumulated across the whole comment (Requirement 10.2's cross-`TriggerCommand` combination)
    - Verify compilation with `go build ./internal/orchestrator/...`
    - _Requirements: 3.1, 3.2, 3.3, 3.4, 3.5, 4.1, 4.2, 4.3, 4.4, 4.5, 4.6, 4.7, 5.1, 5.2, 5.3, 5.4, 5.5_

- [x] 13. Checkpoint - Assemble and verify `internal/orchestrator` implements both target interfaces
  - Add `var _ github.EventHandler = (*Orchestrator)(nil)` and `var _ rpc.OperationHandler = (*Orchestrator)(nil)` to `orchestrator.go` now that every method exists. Ensure `go build ./internal/orchestrator/...` succeeds with both assertions compiling. Ask the user if questions arise.

- [x] 14. Wire `cmd/server`
  - [x] 14.1 Replace `cmd/server/main.go`'s placeholder
    - `orchestrator.ConfigFromEnv(os.Getenv)`, fatal exit on a config error; construct the Redis client, `github.NewAppAuth`, `lock.NewRedisLockManager`, a Kubernetes clientset (in-cluster via `rest.InClusterConfig()`, falling back to `KUBECONFIG`/`~/.kube/config`) and `jobs.NewClient`, `orchestrator.NewPluginRegistry()`, and `orchestrator.New(...)`
    - Run the HTTP webhook server (`github.NewWebhookHandler(secret, orch)`), the gRPC server (`rpc.NewServer(orch)`), and `orch.Run(ctx)` concurrently under one `errgroup.Group`, with graceful `Shutdown`/`GracefulStop` on a termination signal
    - Verify `go build ./...` succeeds for the whole module
    - _Requirements: 7.1 (wiring); no other direct requirement_

- [x] 15. Checkpoint - Verify the whole module compiles
  - Ensure `go build ./...` succeeds and `go vet ./internal/orchestrator/... ./internal/github/... ./internal/jobs/... ./cmd/server/...` reports no issues. Ask the user if questions arise.

- [x] 16. Write unit tests for `internal/orchestrator`
  - [x] 16.1 Write `internal/orchestrator/record_test.go`
    - Against `miniredis`: `Create`/`Delete` round-trip, `SetJobName`/`SetCheckRunID` updates surviving alongside the original TTL, `MarkStarted` idempotent (a second call is a harmless no-op) and returning "no-op" once `finalized` is true, `ClaimForResult`/`ClaimForTimeout` each returning `claimed: false` on a missing/already-finalized record, `ClaimForTimeout` returning `claimed: false` when `started` is true or the deadline hasn't passed, and — the important one — concurrent goroutines racing `ClaimForResult` against `ClaimForTimeout` (or against each other) on the same key, asserting exactly one ever observes `claimed: true`
    - `ScanOperationKeys` returning every `operation:*` key across a paginated `SCAN`, none missed, none duplicated
    - _Requirements: 7.2, 7.3, 7.8, 7.9, 8.1, 8.2, 8.3_

  - [x] 16.2 Write `internal/orchestrator/plancommentrecord_test.go`
    - `Get` on a missing key returns `(nil, nil)`; `Set` then `Get` round-trips the node IDs; `Set` called twice resets the TTL rather than leaving the first call's countdown running (assert via `miniredis`'s `TTL`/`FastForward`); `Delete` on a missing key is a no-op
    - _Requirements: 10.3, 10.5, 10.6, 11.7_

  - [x] 16.3 Write `internal/orchestrator/notify_test.go`
    - Against `miniredis` (which supports Pub/Sub): a `waitForDone` subscribed before a matching `publishDone` receives exactly that result; `waitForDone` against a canceled `ctx` returns promptly rather than hanging
    - _Requirements: 10.1_

  - [x] 16.4 Write `internal/orchestrator/target_test.go`
    - Table-driven, no network/Redis: `"turnip"` vs a specific tool token narrowing candidates (4.1/4.2); named-project narrowing and an unmatched name producing `wholeCommandErr` (4.3); empty `Projects` targeting every candidate (4.4); an operation unrecognized for a candidate's tool routed to `rejected`, not `wholeCommandErr` (4.5) — including the mixed-tool `"turnip"` case where one candidate's tool doesn't recognize the operation but another's does; a non-plan operation without `HasWritePermission` routed to `rejected` (4.6); `ExtraArgs` passed through verbatim (4.7)
    - _Requirements: 4.1, 4.2, 4.3, 4.4, 4.5, 4.6, 4.7_

  - [x] 16.5 Write `internal/orchestrator/execute_test.go`
    - Against fakes of `lock.LockManager`, `github.GitHubClient`, the `jobCreator` interface (task 7.1), and `miniredis`: each rejection branch (`RJ1`-`RJ5`) returns the correct `Success: false` result without creating a Job; a successful path creates the `OperationRecord`, calls `GenerateInstallationToken` exactly once, calls `jobs.BuildJob`/`Create`, and — via a `publishDone` fired by the test directly into the same `miniredis` instance — `executeOne` returns exactly the published `ProjectResult`; `executeTargets` runs multiple Targets concurrently (assert via a fake that blocks until N concurrent calls are in flight) and returns once all have published
    - _Requirements: 6.1, 6.2, 6.3, 6.4, 6.5, 7.2, 7.4, 7.5, 7.6, 9.1, 9.5, 15.2, 15.4_

  - [x] 16.6 Write `internal/orchestrator/comments_test.go`
    - Against a fake `github.GitHubClient` and `miniredis`: plan-kind and apply-kind results correctly partitioned and posted as separate comment sets; flag disabled → `MinimizeComment` never called and no `PlanCommentRecord` written, regardless of whether one already existed; flag enabled + existing record → `MinimizeComment` called for each listed node ID before posting, and the record replaced afterward; flag enabled + no record → minimize step skipped, posting still proceeds (no error); an empty group calls neither `BuildConsolidatedComment` nor any posting method
    - _Requirements: 10.1, 10.2, 10.3, 10.4, 10.5, 10.6, 10.7, 10.8, 10.9_

  - [x] 16.7 Write `internal/orchestrator/result_test.go`
    - Against fakes/`miniredis`: `HandleLog` calls `MarkStarted`; `HandleResult` on an unclaimable operation ID returns nil without touching any fake; a successful plan-kind result calls `StorePlanData` but not `ReleaseLock`; a successful apply-kind result calls `ReleaseLock` and appends the release note to `Output`; a failed result calls neither; every path deletes the record and publishes exactly once
    - _Requirements: 6.6, 6.7, 6.8, 7.7, 7.8, 7.9, 9.2, 9.3_

  - [x] 16.8 Write `internal/orchestrator/sweep_test.go`
    - Against fakes/`miniredis`: an expired, unstarted record is claimed, diagnosed via a fake `jobs.Status`-shaped result, reported as a check-run failure, deleted, and published; a not-yet-expired or already-started record is left untouched; `ReleaseLock` and `jobs.Client.Delete` are never called by this function (assert via a fake that fails the test if either is invoked)
    - _Requirements: 8.1, 8.2, 8.3, 8.4, 8.5, 8.6, 8.7_

  - [x] 16.9 Write `internal/orchestrator/pullrequest_test.go`
    - `"opened"`/`"synchronize"` with zero matched Projects takes no action at all (no comment, no fake calls beyond the fetch/match); a matched Project flows through to `executeTargets`+`postResults`; `.github/turnip.yaml` fallback fires only after a root-level `ErrFileNotFound`; `"closed"` releases only Locks actually held by that PR, posts the unlock comment only when at least one was released, and always deletes the `PlanCommentRecord` regardless
    - _Requirements: 1.1, 1.2, 1.3, 1.4, 1.5, 2.1, 2.2, 2.3, 2.4, 2.5, 11.1, 11.2, 11.3, 11.4, 11.5, 11.6, 11.7_

  - [x] 16.10 Write `internal/orchestrator/comment_test.go`
    - `ErrNoTrigger` is a silent no-op; a `MalformedTriggerErrors` alongside well-formed commands still processes the well-formed ones and replies about the malformed line; a non-collaborator author is rejected before any `TriggerCommand` is processed; two `TriggerCommand`s in one comment are processed in order (assert via a fake `LockManager` that a plan's `AcquireLock`/`StorePlanData` calls are fully ordered before a subsequent apply's `IsLockedByPR`); an `"unlock"` command never creates a Job/check-run/`OperationRecord`; results from every `TriggerCommand` in the comment reach one `postResults` call
    - _Requirements: 3.1, 3.2, 3.3, 3.4, 3.5, 4.1, 4.2, 4.3, 4.4, 4.5, 4.6, 4.7, 5.1, 5.2, 5.3, 5.4, 5.5_

  - [x] 16.11 Write `internal/orchestrator/config_test.go`
    - A fully-populated env map produces the expected `Config`, including `TURNIP_MINIMIZE_OUTDATED_PLAN_COMMENTS` defaulting to `false` when unset and parsing `"true"`/`"false"` when set; missing required variables are named together in one error
    - _Requirements: (test coverage for 4.5)_

- [x] 17. Checkpoint - Verify unit tests pass with target coverage
  - Ensure `go test -race ./internal/orchestrator/... ./internal/github/... ./internal/jobs/...` passes. Coverage target: 80% for `internal/orchestrator` (matching the "Server webhook handling"/gRPC-Runner-Kubernetes bucket rationale grpc-runner's `tasks.md` already established for this domain, since the global design's Testing Strategy names no target for it). Ask the user if questions arise.

- [x] 18. Write property tests
  - [x] 18.1 Write `internal/orchestrator/property_test.go`
    - `// Feature: multi-iac-automation-platform, Property 5: PR Event Triggers Plan Operations` — for a random `Config`/modified-file-list combination, assert every matched Project resolves to a Target at its `GetPlanOperation()`; ≥100 iterations
    - `// Feature: multi-iac-automation-platform, Property 6: Runner Creation Per Triggered Project` — for a random set of resolved Targets, assert `executeTargets` attempts exactly one Job creation per Target that clears its Lock step; ≥100 iterations
    - `// Feature: multi-iac-automation-platform, Property 7: Comment Trigger Pattern Recognition` / `Property 8: Selective Project Triggering from Comments` — for random `TriggerCommand`s against a random `Config`, assert `resolveTargets`' candidate set matches Requirement 4.1-4.4's rules exactly; ≥100 iterations
    - `// Feature: multi-iac-automation-platform, Property 9: Lock Acquisition Prevents Concurrent Operations` — for two random Targets on the same Project Key from different PR numbers, assert at most one's `executeOne` clears the Lock step; ≥100 iterations
    - `// Feature: multi-iac-automation-platform, Property 10: Lock Release After Operation Completion` / `Property 11: Plan-Apply Lock Consistency` — for a random successful apply-kind `HandleResult`, assert the Lock is released and its `PlanData` matches what `GetPlanData` returned before the apply ran; ≥100 iterations
    - `// Feature: multi-iac-automation-platform, Property 15: Consolidated Comment Per PR` / `Property 16: Comment Update Not Duplication` / `Property 17: Comment Contains All Project Results` — for a random `[]ProjectResult`, assert `postResults`' partitioned bodies together account for every result exactly once, and re-running with an unchanged flag/record state never orphans a group; ≥100 iterations
    - `// Feature: multi-iac-automation-platform, Property 26: Installation Token Generation Per Webhook` — reinterpreted per design.md's "Installation Token Timing" note (once per Target, not once per webhook): for a random set of resolved Targets, assert `GenerateInstallationToken` is called exactly once per Target that reaches Job creation, never for a rejected one; ≥100 iterations
    - `// Feature: multi-iac-automation-platform, Property 30: Parallel Project Execution` / `Property 31: Synchronization Before Comment Posting` / `Property 32: Failure Propagation in Consolidated Results` — for a random set of Targets with random pass/fail outcomes, assert `executeTargets` returns only after every one has published, and every failure appears in its corresponding `ProjectResult`; ≥100 iterations
    - _Requirements: 2.1, 2.3, 4.1, 4.2, 4.3, 4.4, 6.1, 6.2, 6.7, 7.4, 10.1, 10.2, 15.2, 17.1_

- [x] 19. Write integration-style tests
  - [x] 19.1 Write `internal/orchestrator/integration_test.go`
    - Using `bufconn` (an in-process `rpc.NewServer(orch)`), `miniredis`, a fake `github.GitHubClient`, and a fake `jobCreator` that immediately "runs" a scripted Runner outcome by calling `HandleLog`/`HandleResult` back through the `bufconn` connection: drive one full auto-plan flow (webhook in → Lock → Job → gRPC callbacks → consolidated plan comment out) and one full comment-triggered apply flow (webhook in → authorization → Lock verify → Job → gRPC callbacks → separate apply comment out, Lock released) end to end, without a real Kubernetes cluster or GitHub API
    - _Requirements: (end-to-end coverage; no single direct requirement)_

- [x] 20. Final checkpoint - Full verification
  - Ensure `go build ./...` compiles, `go test -race ./...` passes including property and integration-style tests, `go mod tidy` produces no changes (`github.com/google/uuid` promoted from indirect to direct), and `golangci-lint run ./...` passes via `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest run ./...` (see `CLAUDE.md`). Ask the user if questions arise.

- [x] 21. Pass PR `BaseRef` through to `jobs.OperationParams` (2026-08 amendment, grpc-runner-driven)
  - grpc-runner's Decision 3 (merging the PR's base branch into the clone,
    Atlantis-style) needs `executeOne` in `internal/orchestrator/execute.go`
    to set `BaseRef: pr.BaseRef` on the `jobs.OperationParams` literal it
    builds — `github.PullRequest.BaseRef` is already populated by the
    GitHub integration slice, so this is a one-line addition, not new
    parsing
  - Full task detail (the driving change, in `internal/jobs`/
    `internal/runner`) lives in `grpc-runner/tasks.md`'s task 21; this
    entry exists so this slice's own audit trail records the touch to its
    file, per task 20's precedent for cross-slice changes recorded on both
    sides
  - _Requirements: (wiring for grpc-runner's Requirement 5.3)_

- [x] 22. Runner Pod ServiceAccount, Server-configured with an opt-in turnip.yaml override
  - [x] 22.1 Add the two Server settings
    - Driven by a real deployment failure: a Runner executing `helmfile
      diff` against a repository whose environment values come from S3
      failed with "no EC2 IMDS role found" — the Pod ran as the
      namespace's `default` ServiceAccount, so it had no AWS identity
      (and no cluster RBAC either)
    - `Config.RunnerServiceAccount` from `TURNIP_RUNNER_SERVICE_ACCOUNT`
      (optional; empty keeps the previous behavior of setting nothing)
      and `Config.AllowServiceAccountFromConfig` from
      `TURNIP_RUNNER_SERVICE_ACCOUNT_ALLOW_FROM_CONFIG` (optional bool,
      default false, parsed like `TURNIP_MINIMIZE_OUTDATED_PLAN_COMMENTS`)
    - Both threaded through `New` into `Orchestrator`, and set in
      `cmd/server/main.go`
    - _Requirements: 7.10, 7.11_

  - [x] 22.2 Resolve and apply the ServiceAccount
    - New `serviceaccount.go`: `resolveServiceAccount(project, default,
      allowFromConfig)` plus `ServiceAccountNotPermittedError`, whose
      message names the Project, the requested account, and the env var
      that would permit it
    - `executeOne` resolves *before* `AcquireLock`, so a refusal can't
      strand a Lock, and passes the result as
      `jobs.OperationParams.ServiceAccount`
    - `internal/jobs.BuildJob` sets `ServiceAccountName` on the Pod spec;
      empty stays unset so existing deployments behave as before
    - See design.md's Decision 5 for why the override is gated and why a
      boolean was chosen over an allow-list
    - _Requirements: 7.10, 7.11_

  - [x] 22.3 Tests and docs
    - `serviceaccount_test.go`: default used when unset, refusal when the
      gate is off, honored when on, empty-default stays empty
    - `execute_test.go`: the Job carries the Server default; a refused
      request creates no Job; an allowed request reaches the Pod spec
    - `build_test.go`: `ServiceAccountName` set from params, and left
      empty when unset
    - `config_test.go`: both new variables, including the invalid-bool case
    - `docs/configuration.md`: the `serviceAccount` config key and both
      env vars; `deploy/base` ships the gate explicitly as `false`
    - _Requirements: (test coverage and documentation for 22.1-22.2)_

- [x] 23. Automatic plans stay silent when a repository has no turnip.yaml (2026-09 amendment)
  - Reported from a real deployment: every pull request in a repository
    that isn't using turnip got a "turnip.yaml was not found in this
    repository" comment, on open and again on every push, because
    `handlePlanTrigger` posted `configErrorComment` for any `fetchConfig`
    failure — including `ErrConfigMissing`
  - `handlePlanTrigger` now returns silently on `ErrConfigMissing`. Two
    cases deliberately still comment: an explicit Trigger Comment
    (`HandleIssueComment`, unchanged — a human asked, so silence would be
    the confusing answer), and a turnip.yaml that exists but fails to
    parse or validate (that repository has opted in, so its breakage
    should stay visible rather than being silently skipped)
  - _Requirements: 1.2 (amended)_

  - [x] 23.1 Tests
    - `pullrequest_test.go`: a missing turnip.yaml on `opened`/
      `synchronize` posts no comment and creates no Job; an invalid one
      still comments
    - _Requirements: (regression coverage for 23)_

- [x] 24. Surface check-run failures in the PR comment (2026-09 amendment)
  - Follow-up to github-integration's task 22: that fixed the 422 that
    broke every check run, but the deployment only revealed it because
    someone read the Server log. The PR itself showed a result comment
    and no check run, with no hint one was attempted
  - `appendCheckRunNote(result, err)` (execute.go) appends a line naming
    the underlying GitHub error to a Target's `ProjectResult.Output`,
    which the consolidated comment already renders — chosen over a new
    `ProjectResult` field so `BuildConsolidatedComment`'s size-sensitive
    splitting (`splitDetailSection`/`packSections`/`maxCommentLength`)
    stays untouched, and following `result.go`'s existing precedent of
    appending "Lock released — …" to the same field
  - Applied at all three sites that touch a check run: creation
    (`executeOne`, via a named return plus one `defer`, so every return
    path after the creation attempt carries the note rather than six
    call sites each remembering), the result-time update (`result.go`),
    and the sweep's timeout update (`sweep.go`)
  - Requirement 9.5 and design.md's Decision 3 both said "logged and
    ignored"; both are amended in place with the reasoning
  - _Requirements: 9.5 (amended)_

  - [x] 24.1 Tests
    - `execute_test.go`: a failing `CreateCheckRun` still runs the Job,
      keeps the Runner's own output, and adds a note containing the
      GitHub error; a succeeding one adds nothing
    - _Requirements: (regression coverage for 24)_

- [x] 25. Carry each check run's outcome in its title and summary (2026-09 amendment)
  - Follow-up to task 24: that surfaced check-run *reporting* failures in
    the PR comment. This makes the check run itself say what happened in
    the checks tab, where previously the only signal was the conclusion
    icon — the title merely repeated the name (github-integration's task
    22 defaults `Title` to the check run's name when a caller leaves it
    unset), so the tab showed `turnip/project/diff` twice and nothing else
  - Deliberately *not* in the name: the name is the check run's stable
    identity, matched by required status checks and branch protection. A
    name varying with the outcome would register as a separate check per
    run and never satisfy a rule configured against the original
  - All four call sites now supply both fields: `in progress` at creation
    (`execute.go`), `failure` + "the Runner Job could not be created"
    when `jobs.Client.Create` fails (`execute.go`), `success`/`failure`
    plus the change counts at result time (`result.go`, via the pure
    `checkRunResultTitle`/`checkRunResultSummary` helpers), and
    `timed out` from the sweep (`sweep.go`)
  - _Requirements: 9.1, 9.2, 9.3, 9.3a (new)_

  - [x] 25.1 Tests
    - `result_test.go`: the two helpers directly — success with and
      without changes, failure with and without changes, and that a
      failure summary is never empty (GitHub rejects output without a
      summary; see github-integration 6.5)
    - _Requirements: (coverage for 25)_

- [x] 26. Render config-error replies as GitHub alerts (2026-09 amendment)
  - These replies were plain paragraphs, easy to miss in a busy PR
    thread. GitHub has no `[!ERROR]` type, so severity is ranked across
    the five it does have, by consequence rather than by "this is an
    error":
    - **WARNING** — turnip didn't run and the author can fix it in their
      own repository: `turnip.yaml` missing (Requirement 1.2) or invalid
      (1.4). Nothing is left in a bad state
    - **CAUTION** — not the author's to fix: fetching `turnip.yaml`
      failed on auth, permissions, or rate limits (1.3), which needs
      whoever runs turnip. Keeping red for this class is the point; if
      every failure is red, the one needing an operator stops standing
      out
  - Detail blocks (the validation output's code fence, the fetch error's
    `<details>`) sit *after* the alert, never inside it: GitHub alerts
    don't render with another element nested in them, degrading to
    literal "[!WARNING]" text. `assertNothingNestedInsideAlert` in
    `configfetch_test.go` pins that, since it fails silently in
    production
  - Same constraint rules alerts out of the consolidated result comment,
    whose per-Project output lives inside `<details>` sections — so
    task 24's check-run note stays plain text deliberately
  - _Requirements: 1.2, 1.3, 1.4 (presentation only; behavior unchanged)_

- [x] 27. Apply exactly what was planned (2026-09 `plan-scoped-apply` amendment)
  - `HandleResult`'s storage condition drops `len(result.PlanData) > 0`: a
    successful plan records a `PlanRecord` whatever the tool returned,
    because Helmfile returns nothing and that is not the same as no plan.
    The arguments stored are `rec.ExtraArgs` — the Operation's own, read
    from the record the Job was built from rather than re-derived from the
    trigger line
  - **Releasing stops being apply-specific.** `case rec.IsApply` becomes
    the fall-through, so every successful non-plan Operation discharges the
    Lock. Previously a successful `sync` left the Project locked with no
    route back but a manual unlock. Narrowing this back to `IsApply` is the
    regression worth guarding — it reads like the rule
  - `executeOne` gains an argument refusal keyed off `!isPlan`, placed
    before the Lock is touched, beside the ServiceAccount and submodule
    refusals and for the same reason: an Operation that was never going to
    run should leave no Lock, check run or Job behind. Refused rather than
    ignored, since a silently dropped argument is indistinguishable from an
    honored one until the infrastructure changes
  - The plan fetch broadens from `if isApply` to every non-plan Operation,
    and the recorded `PlanArgs` are substituted into both the
    `OperationRecord` and `jobs.OperationParams` via one `execArgs` value.
    The refusal is what makes that substitution unambiguous
  - **Carries a citation fix.** The comment on the failure path cited
    "Requirement 6.8", which does not exist — Requirement 6 is *Plan with
    Destroy Flag* and has five criteria, none about Lock lifecycle. The
    governing requirement is 7. The wrong citation is what made this
    behavior look specified when nothing specified it
  - **Also drops two now-dead `OperationRecord` fields.** Making release
    the fall-through, and broadening the plan fetch to every mutating
    Operation, left `IsApply` written and never read; `PlanData` was
    already redundant, since the Job takes the plan bytes from a local
    rather than from the record. Both were removed, along with the
    `isApply` local and `createTestRecord`'s parameter. This design's
    `OperationRecord` sketch is amended to match — it describes
    `record.go` rather than recording history, so leaving it stale would
    misdescribe the current shape
  - _Requirements: (dead-code removal, no behavioral change)_
  - _Requirements: no new behavior beyond `plan-scoped-apply`'s own_

## Notes

- No new third-party dependencies (design.md's "Dependencies" section);
  `github.com/google/uuid` moves from indirect to direct.
- `internal/orchestrator`'s `jobCreator` interface (task 7.1) is a small,
  locally-defined testability seam over `*jobs.Client` — the same pattern
  `internal/lock`'s `LockManager` interface and this slice's own reuse of
  `lock.LockManager` already establish; `jobs.Client` itself is not
  changed into an interface.
- `PlanCommentRecord`'s 90-day TTL and `operation:*`'s 24-hour TTL are
  both safety nets, not correctness mechanisms — see design.md's Redis Key
  Format section for why the two durations differ by three orders of
  magnitude.
- No real Kubernetes cluster, `kind` environment, or real GitHub/GraphQL
  network access is used anywhere in this slice's tests —
  `k8s.io/client-go/kubernetes/fake`, `bufconn`, `miniredis`, and
  `httptest.Server` are sufficient, matching grpc-runner's precedent.
  Integration testing against a real cluster belongs to the global
  roadmap's Slice 11.
- Coverage target: 80% for `internal/orchestrator` (no global-design
  target exists for this slice's domain; grpc-runner's `tasks.md` already
  set this precedent for the same class of package).

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1.1", "2.1", "4.1", "4.2", "4.3", "4.4", "4.5"] },
    { "id": 1, "tasks": ["1.2", "1.3", "2.2", "2.3", "4.6", "6.1"] },
    { "id": 2, "tasks": ["7.1", "8.1"] },
    { "id": 3, "tasks": ["10.1", "11.1"] },
    { "id": 4, "tasks": ["12.1", "12.2"] },
    { "id": 5, "tasks": ["14.1"] },
    { "id": 6, "tasks": ["16.1", "16.2", "16.3", "16.4", "16.5", "16.6", "16.7", "16.8", "16.9", "16.10", "16.11"] },
    { "id": 7, "tasks": ["18.1"] },
    { "id": 8, "tasks": ["19.1"] }
  ]
}
```
