# Implementation Plan: HA Validation & Documentation (Slice 11)

## Overview

This plan adds test infrastructure and documentation only — no production
code in `internal/` changes. It lands in three independent groups, plus
docs:

1. **`internal/orchestrator/ha_test.go`** (Requirement 1.1-1.3): two real
   `*Orchestrator` instances sharing one real Redis, part of the default
   `go test -race ./...` suite. Needs a `redis:` CI service container.
2. **`internal/orchestrator/load_test.go`, in-process Load Test** (Requirement
   1.4): 100 concurrent `HandlePullRequest` calls, real Redis, in-process
   fakes — not part of default CI. Lives in `internal/orchestrator`, not
   `test/load`, for the same reason as `ha_test.go`: it needs direct
   access to `Orchestrator`'s unexported fields to substitute a fake
   `GitHubClient`/`jobCreator`, which `orchestrator.New`'s public
   constructor has no seam for (confirmed while implementing task 5 — see
   design.md's amended note).
3. **`test/load/kind_test.go`, `kind`-cluster variant** (Requirement 1.5/1.6): a real
   `kind` cluster running `deployment-kustomize`'s overlay, validated via
   a direct RBAC check and a direct Service-networking check — **not** a
   replay of the webhook burst. See design.md's amended Decision: tracing
   `pullrequest.go`/`comment.go` shows real GitHub API calls
   (`fetchConfig`→`GetFile`, `IsCollaborator`→`GetCollaboratorPermission`)
   happen *before* any lock logic, so a webhook fired at a `kind`-deployed
   Server with a throwaway (unregistered) GitHub App key fails
   immediately and proves nothing — a wrong turn caught during design,
   not implementation.
4. **Documentation** (Requirement 2): `README.md`, `docs/deployment.md`,
   `docs/troubleshooting.md` — written last, since they describe the
   finished platform including this slice's own Requirement 1 artifacts.

Groups 1 and 2 share `internal/orchestrator`'s test fixtures (`haFakeClient`,
`newHAOrchestrator`, `wireFakeJobCreator`) but are otherwise independent
of each other and of group 3, which needs nothing from either. Documentation
depends on all three being done, per Requirement 2.4.

## Tasks

- [x] 1. `internal/orchestrator/ha_test.go` (Requirement 1.1-1.3)
  - [x] 1.1 Add the real-Redis skip helper and shared fake client
    - `realTestRedisAddr(t *testing.T) string`: reads
      `TURNIP_TEST_REDIS_ADDR`, `t.Skip`s (not fails) when unset
    - `haFakeClient`: a `github.GitHubClient` covering both the
      plan-trigger path (`GetFile`, `GetModifiedFiles`) and the
      comment-trigger path (`GetCollaboratorPermission` returning
      `"write"`, `GetPullRequest`), plus what both need
      (`PostComment` recording per-PR-number, mutex-guarded;
      `CreateCheckRun`/`UpdateCheckRun` no-ops; `GenerateInstallationToken`
      returning a fixed fake token) — shared by every simulated instance
      in a test, mirroring how real Server replicas all call the same
      real GitHub API
    - `newHAOrchestrator(t, addr, client) *Orchestrator`: builds one real
      `*Orchestrator` with `lock.NewRedisLockManager(redisClient)`,
      `newRecordStore(redisClient)`, a `fakeJobCreator` (existing,
      `execute_test.go`) wired to that same `redisClient` so
      `publishDone`/`waitForDone` still round-trip over the real backend,
      and `installationClient` returning the shared `haFakeClient` —
      `redisClient` is its own `*redis.Client` dialing `addr` (not shared
      across instances, matching two independent Server processes each
      with their own connection)
    - _Requirements: 1.1, 1.2, 1.3_
  - [x] 1.2 `TestHA_PlanAndApplyAcrossInstancesMatchSingleInstance` (Properties 34, 35)
    - Helper `runPlanThenApply(t, planInstance, applyInstance *Orchestrator) (applySucceeded bool)`:
      fires a `pull_request` "opened" event (unique owner/repo per
      subtest, one Helmfile project) on `planInstance`, waits for its
      posted comment, then fires an `issue_comment` "/turnip apply
      <project>" event for the same PR on `applyInstance`, waits for its
      posted comment, returns whether the apply comment indicates success
    - Run once with `planInstance == applyInstance` (single real
      `*Orchestrator` handles both) — baseline
    - Run once with two distinct `*Orchestrator` instances sharing the
      same real Redis — no Go state in common between them at all
    - `assert.Equal(t, baseline, splitAcrossInstances)` — both `true`;
      the whole point is that splitting the flow across instances changes
      nothing, since the lock and plan data live in Redis, not in either
      instance's memory
    - Actually needed the *real* `HandleResult` path (`grpcDrivingJobCreator`,
      reused from `integration_test.go`), not the simpler
      `fakeJobCreator`: `StorePlanData` only runs inside `HandleResult`
      when it receives real `PlanData` bytes over gRPC, and
      `fakeJobCreator` publishes its canned result directly, bypassing
      `HandleResult` entirely — the apply step would always see "no plan
      data stored" otherwise. `wireGRPCJobCreator`/`wireFakeJobCreator`
      helpers added alongside `newHAOrchestrator` so each test picks
      whichever it needs
    - _Requirements: 1.1_
  - [x] 1.3 `TestHA_ConcurrentLockAcquisitionAcrossInstancesExactlyOneWinner` (Property 36)
    - Two `*Orchestrator` instances sharing one real Redis; fire ≥10
      concurrent `pull_request` "opened" events for ≥10 distinct PR
      numbers, all touching the *same* Project (same owner/repo/project →
      same lock key), split round-robin across the two instances, all
      launched concurrently via goroutines + `sync.WaitGroup`
    - `require.Eventually` until every PR's comment has posted; assert
      exactly one outcome indicates the lock was acquired and every other
      outcome is a "locked by PR #N" rejection — matches design.md's
      mermaid sequence diagram, generalized from 2 to ≥10 concurrent
      attempts
    - _Requirements: 1.2_

- [x] 2. Checkpoint - Verify `ha_test.go` against a real local Redis
  - Confirmed: both tests pass against a real `docker run redis:7-alpine`
    with `-race`, across 10 repeated runs (no flakiness). Confirmed both
    report `SKIP` (not fail) with no `TURNIP_TEST_REDIS_ADDR` set. Note: a
    single unrelated flake surfaced once in the package's pre-existing
    `TestProperty_*` suite when run back-to-back with `ha_test.go`'s new
    tests (10 further clean runs didn't reproduce it) — flagged to the
    user, not chased further; not caused by this slice's changes.

- [x] 3. Wire the real Redis into CI (Requirement 1.3)
  - [x] 3.1 Amend `.github/workflows/ci.yml`
    - Added a `services: redis: image: redis:7-alpine, ports:
      ['6379:6379']` block to the `ci` job, and
      `TURNIP_TEST_REDIS_ADDR: localhost:6379` to the `Test` step's `env:`
    - _Requirements: 1.3_

- [x] 4. ~~`test/load/webhook.go`~~ — dropped
  - Superseded by task 5's discovery: the in-process Load Test moved into
    `internal/orchestrator` (same package as `ha_test.go`), which drives
    `HandlePullRequest` directly and never needs an HTTP layer or
    hand-built webhook JSON payloads at all. Nothing else needs this
    file — see design.md's amended Overview/Package Layout.

- [x] 5. `internal/orchestrator/load_test.go` — the Load Test (Requirement 1.4)
  - [x] 5.1 Create `internal/orchestrator/load_test.go` (`//go:build load`)
    - Discovered while implementing: `orchestrator.New`'s public
      constructor always wires a real `*github.Client` internally (via
      `AppAuth.InstallationClient`), with no seam to substitute a fake
      `GitHubClient` — so a `test/load` (external package) implementation
      calling the public constructor can never avoid making real,
      doomed-to-fail GitHub API calls. Moved into `internal/orchestrator`
      instead, reusing `ha_test.go`'s `haFakeClient`, `newHAOrchestrator`,
      and `wireFakeJobCreator` directly (same package, same file's
      helpers) — this is now a straightforward scale-up of
      `TestHA_ConcurrentLockAcquisitionAcrossInstancesExactlyOneWinner`
    - `TestLoad_100ConcurrentEventsNoDeadlock`: skip if
      `TURNIP_TEST_REDIS_ADDR` unset (via `realTestRedisAddr`); one
      `*Orchestrator` instance (a single Server replica is enough to
      prove Requirement 1.4's "no deadlock" — the multi-instance angle is
      `ha_test.go`'s job); fire 100 concurrent `HandlePullRequest` calls
      for 100 distinct PR numbers, all on one shared Helmfile project
      (real lock contention)
    - Whichever PR *wins* proceeds to a real (fake-clientset) Job
      creation, then blocks in the background on `waitForDone` — there's
      no real Runner to call back, so that one PR's own result comment
      never posts within the test's window. That's expected: the
      assertion only needs the 99 *rejected* PRs' comments (which post
      immediately — `executeOne` returns as soon as `AcquireLock` fails),
      sufficient to prove exactly one winner existed
    - Assert: all 100 `HandlePullRequest` calls return within a bounded
      wall-clock window (e.g. 10s total, via a `WaitGroup` + a `select`
      against `time.After`) — Requirement 1.4's actual "no deadlock"
      claim — and, via `require.Eventually` polling the shared fake
      client's posted-comments map, exactly 99 of the 100 PRs show a
      "locked by PR #_" rejection
    - _Requirements: 1.4_

- [x] 6. Add the `test-load` Makefile target
  - [x] 6.1 Amend `Makefile`
    - `test-load: ; go test -tags load -race ./internal/orchestrator/... -run TestLoad -timeout 60s`
    - _Requirements: 1.4, 1.7_

- [x] 7. Checkpoint - Verify `make test-load`
  - Confirmed: passes in ~1.1s across 5 repeated fresh (`-count=1`) runs
    against a real `docker run redis:7-alpine`. Confirmed
    `go test -race ./internal/orchestrator/... -list ".*"` (no `-tags`)
    does not surface `TestLoad_*` at all.

- [x] 8. `test/load/kind_test.go` — the `kind`-cluster variant (Requirement 1.5/1.6)
  - [x] 8.1 Create `test/load/kind_test.go` (`//go:build kind`)
    - `KindTimeoutError`: wraps a `context.DeadlineExceeded` from either
      check below, so `TestKind_RBACAndNetworking` can report it via
      `t.Skip` (inconclusive, per Requirement 1.6) rather than `t.Fail`
    - `TestKind_RBACAndNetworking`, reading `TURNIP_TEST_NAMESPACE`
      (default `"default"`):
      1. RBAC check: `exec.CommandContext(ctx, "kubectl", "auth",
         "can-i", "create", "jobs.batch", "--as=system:serviceaccount:"+ns+":turnip-server",
         "-n", ns)` (and a second `can-i get pods.` call) — asserts exit 0
         (`"yes"`), proving `deploy/base/role.yaml`'s grant
         (`deployment-kustomize`, Requirement 1.1) is sufficient in the
         live cluster
      2. Networking check: `exec.CommandContext(ctx, "kubectl", "run",
         "turnip-ha-netcheck", "--rm", "-i", "--restart=Never",
         "--image=busybox", "-n", ns, "--", "sh", "-c", "nc -z -w3
         turnip-server."+ns+".svc.cluster.local 9090")` — asserts exit 0,
         proving the Service's DNS name resolves and its gRPC port is
         reachable from another Pod in the namespace (the real path a
         Runner Pod's gRPC dial would take)
      3. Both `exec.CommandContext` calls use a shared outer
         `context.WithTimeout` (e.g. 90s combined) — a `DeadlineExceeded`
         wraps into `KindTimeoutError` and the test calls `t.Skip`, per
         Requirement 1.6; any other non-zero exit is a real `t.Error`
    - _Requirements: 1.5, 1.6_

- [x] 9. `test/load/redis.yaml` — throwaway Redis for the `kind` cluster
  - [x] 9.1 Create `test/load/redis.yaml`
    - A plain `Deployment` + `Service` named `redis` (`redis:7-alpine`,
      no persistence, no auth) — `deployment-kustomize`'s base
      deliberately owns no Redis (Requirement 1.3 of that slice), and this
      is exactly the "test setup" that slice's `kind`-overlay comment
      already names this slice as responsible for providing
    - _Requirements: 1.5_

- [x] 10. Add the `test-kind` Makefile target
  - [x] 10.1 Amend `Makefile`
    - A `test-kind` target scripting, in order: `kind create cluster
      --name turnip-ha`; build the Server/Runner images locally (reuse
      `goreleaser build --snapshot --clean` or plain `docker build -f
      build/server/Dockerfile .` / `build/runner/Dockerfile`) and `kind
      load docker-image ... --name turnip-ha`; generate a throwaway RSA
      key (`openssl genrsa 2048`) and `kubectl create secret generic
      turnip-github-app --from-literal=webhook-secret=test
      --from-file=private-key=<that key>` (the same Secret
      `deploy/overlays/kind`'s own kustomization.yaml comment documents as
      a prerequisite — no real GitHub App needed, since nothing in task 8
      calls GitHub); `kubectl apply -f test/load/redis.yaml`; `kubectl
      apply -k deploy/overlays/kind`; `kubectl wait
      --for=condition=available deployment/turnip-server --timeout=120s`
      (this alone proves the readiness probe — and therefore Redis
      connectivity over real cluster networking — passed); `go test -tags
      kind -race ./test/load/...`; always `kind delete cluster --name
      turnip-ha` afterward (trap on exit, not just the happy path)
    - _Requirements: 1.5, 1.6, 1.7_

- [x] 11. Checkpoint - Run `make test-kind` end-to-end
  - First run failed: kube-proxy crash-looped with "too many open files"
    and the Deployment never went `Available` within the 120s wait. Root
    cause (confirmed by inspecting Pod events/logs on a scratch cluster
    kept alive for debugging): the host's `fs.inotify.max_user_instances`
    was 128 — below `kind`'s own documented minimum (512+) for exactly
    this failure mode. Fixed by raising it to 8192 (no `sudo` available
    in this environment, so applied via a privileged Docker container
    writing `/proc/sys/fs/inotify/max_user_instances` directly — `fs.inotify`
    limits aren't network/user-namespaced on this kernel, so the write
    reached the real host value); documented as a prerequisite in the
    `Makefile`'s `test-kind` target comment for whoever hits this next.
  - Second run succeeded end-to-end: cluster created, both images built
    and loaded, the throwaway Secret and `test/load/redis.yaml` applied,
    `deploy/overlays/kind` applied, `deployment.apps/turnip-server
    condition met` (Available), `go test -tags kind -race ./test/load/...`
    passed in 6.8s (both `kubectl auth can-i` RBAC subtests and the `nc`
    networking subtest), and the cluster tore down cleanly. Confirmed no
    stray `turnip-ha*` clusters or containers left behind afterward.

- [x] 12. Documentation (Requirement 2)
  - [x] 12.1 Create `README.md`
    - What turnip is (summarize `CLAUDE.md`'s "What this is" — don't
      duplicate it at length), a Server/Runner architecture diagram
      (reuse the global `design.md`'s Mermaid component diagram rather
      than redrawing it), and a link to `deploy/` (`deployment-kustomize`)
      as the primary deployment path
    - _Requirements: 2.1_
  - [x] 12.2 Create `docs/deployment.md`
    - Prerequisites (Redis/Valkey, a GitHub App, a Kubernetes cluster),
      the values an overlay must supply (cross-referencing
      `deploy/overlays/kind/kustomization.yaml` as the concrete worked
      example), and a "verify your deployment" section built on `curl
      .../healthz` / `curl .../readyz` (`metrics`, Slice 9)
    - _Requirements: 2.2_
  - [x] 12.3 Create `docs/troubleshooting.md`
    - One entry per failure mode in the global `design.md`'s "Error
      Handling" section (Configuration Errors, Lock Contention, GitHub API
      Errors, Runner Execution Errors, Plugin Execution Errors,
      Authorization Errors), each translated into "what you'll see (the
      PR comment / check-run text), and what to do about it" — operator-facing,
      not a restatement of that section's developer-facing "what the code
      does" framing
    - _Requirements: 2.3_

- [x] 13. Final checkpoint - Full verification
  - Confirmed: `go build ./...`, `go build -tags load ./...`,
    `go build -tags kind ./...` all succeed. `gofmt -l .` empty. `go mod
    tidy` no diff. `TURNIP_TEST_REDIS_ADDR=... go test -race ./... -count=1`
    passes repo-wide (every package `ok`, including `ha_test.go`'s two
    tests). `go test -race ./test/load/...` (no `-tags`) reports "matched
    no packages" — `test/load` is fully invisible without `-tags kind`,
    not just "no test files," since its only source file is entirely
    build-tag-gated. golangci-lint v2 reports 0 issues. Tasks 2, 7, and 11
    already re-verified `ha_test.go`, `make test-load`, and `make
    test-kind` individually during their own checkpoints; not re-run a
    third time here since nothing changed after task 11 passed.

- [x] 14. Amendment - Expand documentation beyond Requirement 2's literal scope
  - User feedback after this slice was marked complete: the docs
    (`README.md` + `docs/deployment.md` + `docs/troubleshooting.md`)
    covered installing turnip but not configuring or using it. Requirement
    2 didn't explicitly ask for a `turnip.yaml`-schema or trigger-syntax
    reference, but it's a natural extension of the same "operator can
    stand up and use turnip without reading source" goal.
  - [x] 14.1 Create `docs/configuration.md`
    - `turnip.yaml`'s full schema (field-by-field, `config` map's known
      keys, tool-support status table — only Helmfile has a working
      Plugin today, Terraform/Pulumi are recognized by the parser but not
      yet implemented, per roadmap Slice 7), the Server's own env var
      reference table (cross-referenced against
      `internal/orchestrator/config.go` directly, not memory), and
      GitHub App setup (permissions inferred from the actual API calls in
      `internal/github/client.go`, explicitly flagged as worth
      double-checking against GitHub's own UI since exact permission
      names can drift)
  - [x] 14.2 Create `docs/usage.md`
    - Automatic plan-on-PR behavior, the trigger-comment grammar
      (`/turnip <op> [project...] [-- extra args]` vs `/<tool> ...`,
      pulled from `internal/github/parser.go` and its test fixtures
      verbatim), why apply needs a prior plan, `/turnip unlock`, the
      two-tier authorization model (collaborator for anything, write
      permission for anything beyond a plan), and what a check
      run/consolidated comment actually looks like
  - [x] 14.3 Cross-link everything
    - `README.md` gained a "Documentation" table indexing all four docs;
      `docs/deployment.md`'s GitHub App prerequisite now points at
      `docs/configuration.md`'s fuller section instead of duplicating a
      permissions table that could drift out of sync; `docs/deployment.md`
      gained a "Next" section pointing at `docs/configuration.md`/`docs/usage.md`
    - Verified every relative Markdown link across `README.md` and
      `docs/*.md` resolves to a real file
    - _Requirements: 2.1, 2.2 (extended beyond the original literal scope)_

## Notes

- No production code in `internal/` changes — purely additive test
  infrastructure and docs, matching design.md's Backward Compatibility
  section.
- `ha_test.go`'s two-instance tests deliberately use `lock.NewRedisLockManager`
  (the real implementation) instead of any `fakeLockManager` — the whole
  point is exercising real Redis semantics under real (or genuinely
  concurrent, for task 1.3) conditions, which `miniredis`'s single-threaded
  loop and any in-memory fake could paper over.
- Task 5's `*jobs.Client` is real but backed by
  `k8s.io/client-go/kubernetes/fake` (a genuine value of the type
  `orchestrator.New` requires, satisfying it without a real cluster) —
  this is standard, not a hack; `internal/jobs`'s own unit tests already
  use the same fake clientset for the same reason.
- The `kind`-cluster variant (tasks 8-11) validates RBAC and networking
  directly rather than replaying the webhook burst — see design.md's
  amended Decision under Requirement 1 for why the naive "fire the Load
  Test at a real deployed Server" idea doesn't reach any lock logic at
  all without a real, registered GitHub App.

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1.1"] },
    { "id": 1, "tasks": ["1.2", "1.3"] },
    { "id": 2, "tasks": ["2", "3.1", "5.1", "9.1"] },
    { "id": 3, "tasks": ["6.1", "8.1"] },
    { "id": 4, "tasks": ["7", "10.1"] },
    { "id": 5, "tasks": ["11"] },
    { "id": 6, "tasks": ["12.1", "12.2", "12.3"] },
    { "id": 7, "tasks": ["13"] }
  ]
}
```
