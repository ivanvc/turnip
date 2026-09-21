# Platform Roadmap — Implementation Slices

This document captures the incremental delivery plan for the multi-IaC automation platform (turnip). Each slice is a self-contained, implementable unit with its own spec (requirements, design, tasks).

The global spec in this directory (`requirements.md`, `design.md`, `tasks.md`) serves as the north-star vision. Individual slice specs live in their own directories under `.kiro/specs/`.

## Slice Overview

| # | Slice | Spec Directory | Status | Depends On |
|---|-------|---------------|--------|------------|
| 0 | Project Scaffolding | `project-scaffolding` | Complete | — |
| 1 | Config Parsing & Project Matching | `config-parsing` | Complete | Slice 0 |
| 2 | Plugin System & Helmfile Plugin | `plugin-helmfile` | Complete | Slice 0 |
| 3 | Redis Lock Manager | `redis-lock-manager` | Complete | Slice 0 |
| 4 | GitHub Client & Webhook Handler | `github-integration` | Complete | Slice 0 |
| 5 | gRPC & Runner | `grpc-runner` | Complete | Slices 0, 2 |
| 6 | Server Orchestration | `server-orchestration` | Complete | Slices 1–5 |
| 7 | Terraform & Pulumi Plugins | `terraform-pulumi-plugins` | Not Started | Slice 2 (interface) |
| 8 | Structured Logging | `structured-logging` | Complete | Slice 6 |
| 9 | Metrics & Health Endpoints | `metrics` | Complete | Slice 6 |
| 10 | Deployment: Kustomize & Release Images | `deployment-kustomize` | Complete | Slices 6, 9 |
| 11 | HA Validation & Documentation | `ha-validation` | Complete | Slices 6, 9, 10 |
| 12 | Runner Workspace & Project Environment | `runner-workspace-environment` | Complete | Slices 1, 5 |
| 13 | Project Schema v1alpha2 | `project-schema-v1alpha2` | Complete | Slice 12 |
| 14 | Per-Tool Provisioning | `tool-provisioning` | Complete | Slices 2, 12 |
| 15 | Refuse Fork Pull Requests | `fork-pull-requests` | Complete | Slice 6 |
| 16 | Cloning Submodules | `clone-submodules` | Complete | Slice 5 |
| 17 | Pull Request Comment Output | `comment-output` | Complete | Slices 4, 6 |
| 18 | The Lock's Lifecycle as a State Machine | `lock-release-rules` | Complete | Slices 3, 6 |
| 19 | Draft Pull Requests | `draft-pull-requests` | Complete | Slices 4, 6 |
| 20 | Apply Exactly What Was Planned | `plan-scoped-apply` | Complete | Slices 3, 6 |
| 21 | What a Bare Command Targets | `project-selection` | Complete | Slices 1, 6 |
| 22 | Refuse Tool Arguments That Name an Executable | `tool-argument-policy` | Not Started | Slices 4, 6 |
| 23 | Authorize on Permission Level, Not Call Success | `collaborator-authorization` | Not Started | Slice 4 |
| 24 | How the Runner Receives Its GitHub Token | `runner-token-delivery` | Not Started | Slices 5, 6 |
| 25 | Authenticating the Runner to the Server | `runner-authentication` | Not Started | Slices 5, 6 |
| 26 | Real-Time Operation Output | `operation-output-stream` | Not Started | Slices 5, 6 |
| 27 | Authenticated and Encrypted Redis | `redis-tls-auth` | Not Started | Slices 3, 10 |
| 28 | Seeing and Dropping Locks Without Hunting for the PR | `lock-admin-ui` | Not Started | Slices 3, 4 |
| 29 | Move the Webhook Off the Root Path | `webhook-path` | Complete | Slices 4, 10 |
| 30 | Scheduling: Concurrency and Execution Order | `operation-scheduling` | Not Started | Slices 6, 7 |
| 31 | Runner Pod Placement: Node Selectors and Tolerations | `runner-pod-placement` | Not Started | Slices 5, 13 |
| 32 | Refuse a Closed Pull Request, Plan a Reopened One | `closed-pull-requests` | Complete | Slices 4, 6 |
| 33 | Show What Ran and With What Scope | `execution-provenance` | Not Started | Slices 2, 17, 20 |

## Slice Details

### Slice 0: Project Scaffolding

**Goal**: Establish the Go project skeleton so all subsequent slices have a foundation to build on.

**Delivers**:
- Go module with dependency management
- Directory structure (`cmd/server`, `cmd/runner`, `internal/...`)
- Dockerfiles for server and runner images
- Protobuf setup and code generation
- Makefile / task runner
- Basic CI pipeline
- Linting and formatting configuration

**No business logic** — purely structural.

---

### Slice 1: Config Parsing & Project Matching

**Goal**: Parse `turnip.yaml` and determine which projects should trigger based on file changes.

**Delivers**:
- YAML schema definition and parser
- Project configuration validation
- Glob pattern matching for `whenModified` rules
- Property tests for round-trip parsing and pattern matching

**Global requirements covered**: 1, 2, 18

---

### Slice 2: Plugin System & Helmfile Plugin

**Goal**: Define the unified Plugin interface and implement Helmfile as the first plugin.

**Delivers**:
- Plugin interface (`GetOperations`, `Execute`, `Name`, `GetPlanOperation`, `GetApplyOperation`)
- `ExecuteOptions`, `ExecuteResult`, `ChangeSummary` types
- Helmfile plugin: diff, apply, sync, destroy operations
- Output parsing for changed releases
- Environment configuration support
- Property tests for plugin correctness

**Global requirements covered**: 3, 13

---

### Slice 3: Redis Lock Manager

**Goal**: Implement Redis-based locking to prevent concurrent operations and store plan data.

**Delivers**:
- `LockManager` interface implementation
- Lock acquisition with `SET NX` (no TTL)
- Plan data storage and retrieval
- Lock release on apply/merge/close/manual unlock
- Lock status querying
- Property tests for lock safety

**Global requirements covered**: 7, 20

---

### Slice 4: GitHub Client & Webhook Handler

**Goal**: Handle GitHub App auth, receive webhooks, parse comments, manage check runs and PR comments.

**Delivers**:
- GitHub App authentication (private key, installation tokens)
- Webhook HTTP handler with signature verification
- Comment parser (trigger patterns, project names, extra args)
- Check run creation/updates
- PR comment posting/updating (consolidated, collapsible)
- Collaborator authorization checks
- Property tests for parsing and authorization

**Global requirements covered**: 5, 6, 9, 10, 15, 16, 17

---

### Slice 5: gRPC & Runner

**Goal**: Establish communication between Server and Runner, implement runner lifecycle.

**Delivers**:
- Protobuf service definition (`OperationService`)
- gRPC server implementation (in Server process)
- gRPC client implementation (in Runner process)
- Runner startup: env parsing, repo cloning, plugin execution
- Log streaming from Runner to Server
- Kubernetes Job creation and cleanup
- Per-tool initContainer provisioning (vendor-official images, version selected from `config.version`)
- Property tests for runner behavior

**Global requirements covered**: 8, 14

---

### Slice 6: Server Orchestration

**Goal**: Wire all components together into the complete webhook-to-operation flow.

**Delivers**:
- PR event handler (opened, synchronized, closed, merged)
- Comment event handler (trigger detection, authorization, execution)
- Operation orchestration (parallel execution, result collection)
- Lock acquisition → runner creation → result → comment/check flow
- Integration tests for end-to-end workflows

**Global requirements covered**: 4, 5, 6, 17, 19, 20

---

### Slice 7: Terraform & Pulumi Plugins

**Goal**: Add remaining IaC tool support.

**Delivers**:
- Terraform plugin: plan, apply, -destroy flag, output parsing, workspace support
- Pulumi plugin: preview, up, destroy, output parsing, stack support
- OpenTofu as a supported tool. It is Terraform-compatible, so whatever
  Plugin runs one runs the other, and a tool nothing can execute yet has
  nowhere useful to live until this slice — adding it earlier would be
  scope that delays the Helmfile MVP. Note that OpenTofu considers direct
  use of its images unsupported as of 1.10 and documents copying the binary
  out of its `:…-minimal` variant instead, which is what turnip's copy-out
  provisioning already does
- Pulumi's provisioning strategy (Slice 14 defers it here): its CLI
  orchestrates separate language-host binaries and runtime-fetched provider
  plugins, and a program needs a language runtime chosen by the user's
  code — so it is unlikely to be a copy-out tool, and deciding that belongs
  with the work that makes Pulumi actually run
- Property tests for each plugin

**Global requirements covered**: 11, 12

---

### Slices 8-11: Production Readiness (formerly one "HA, Observability & Deployment" slice)

What was originally scoped as a single Slice 8 turned out to be too
broad for one implementable unit and was split into four, sequenced by
actual dependency rather than the original bullet order. Each has its
own spec directory (`requirements.md`/`design.md`); Slice 7 (Terraform &
Pulumi Plugins) is independent of all four and can be picked up in any
order relative to them.

### Slice 8: Structured Logging

**Goal**: Replace unstructured `log.Printf`/`fmt.Fprintf` calls with
`log/slog`-based structured, leveled logging.

**Delivers**:
- `internal/logging` package (`TURNIP_LOG_LEVEL` parsing, `log/slog` setup)
- Every existing Server/Runner log call site rewritten to structured logging

**Global requirements covered**: non-functional

---

### Slice 9: Metrics & Health Endpoints

**Goal**: Give the Server an HTTP observability surface.

**Delivers**:
- `/healthz`/`/readyz` endpoints (and the `http.ServeMux` routing change
  they require in `cmd/server/main.go`)
- Prometheus metrics (`/metrics`) — webhook, Operation, lock, and Runner
  Job Start Latency signals
- A Grafana dashboard (JSON) for those metrics

**Global requirements covered**: non-functional

---

### Slice 10: Deployment — Kustomize & Release Images

**Goal**: Make turnip actually deployable, with real versioned images.

**Delivers**:
- Kustomize base (Deployment, Service, RBAC) + an example `kind` overlay
  + an opt-in Grafana-dashboard-provisioning component
- Versioned release images: replace the Server/Runner images' `:latest`
  tag (a stand-in since Slice 0) with a real release version, via
  `goreleaser` — the Runner image is now a required
  `TURNIP_RUNNER_IMAGE` config value (`internal/jobs.OperationParams.RunnerImage`),
  not a package constant

**Global requirements covered**: non-functional

---

### Slice 11: HA Validation & Documentation

**Goal**: Prove the stateless/multi-instance HA design holds under real
concurrency and a real deployment; document the finished platform.

**Delivers**:
- Stateless server validation (no in-memory state) — real-Redis,
  multi-instance integration tests, part of default CI
- Multi-instance testing under concurrent load, including a `kind`-cluster
  variant deployed via Slice 10's overlay (run on demand, not default CI)
- Documentation (README, deployment guide, troubleshooting)

**Global requirements covered**: 19

---

### Slice 12: Runner Workspace & Project Environment

**Goal**: Give the Runner a fixed, documented filesystem layout, and let a
Project declare environment variables for its Operation — so that
configuration a repository already commits can be referenced by tools
that have no configuration hook of their own.

**Delivers**:
- A single `/turnip` namespace: Workspace at `/turnip/src`, Tools
  directory at `/turnip/tools`, as two separate `emptyDir` volumes so the
  tool-provisioning initContainer cannot reach the Workspace
- The Workspace path passed to the Runner by environment variable
  (mirroring `TURNIP_TOOLS_DIR`), with a temporary-directory fallback so
  unit tests and local runs are unaffected
- An `env` map on a Project in `turnip.yaml`, applied to the IaC_Tool
  subprocess, with `TURNIP_`-prefixed names and `PATH` rejected at parse
  time
- An enforced, unambiguous schema version: `schemaVersion: v1alpha1`
  replacing the current `version: 1`, which is parsed but never validated
  and reads as a product version turnip doesn't have. Alpha states that
  the schema will keep breaking pre-1.0; it graduates to `v1` when turnip
  reaches 1.0, at which point schema and project major coincide
- Documentation of all three in `docs/configuration.md`

**Not in this slice**: provisioning additional binaries (helm plugins,
cloud CLIs), remote-cluster authentication, and Azure Workload Identity
pod labels — the last is in the Backlog below.

**Global requirements covered**: none directly. Extends the shared-volume
mechanism of global Requirement 14.2a; adjacent to Requirement 18 without
changing its `config` map.

---

### Slice 13: Project Schema v1alpha2

**Goal**: Reshape a Project around the three questions its settings
actually answer — what to run, how to call it, where it runs — and make an
unrecognised key an error rather than silence.

**Delivers**:
- `uses: <tool>[@<version>]`, replacing the `tool` field and
  `config.version`, with a leading `v` accepted and normalized
- `with:`, replacing `config` as a map the Plugin alone reads. Today's
  `config` holds three recognised keys of which only `environment` reaches
  a Plugin; `version` and `serviceAccount` are read by turnip itself
- `runner:`, grouping `serviceAccount` and `env` — the settings that shape
  the Pod rather than the tool
- `TURNIP_ALLOWED_OVERRIDES`, a list of dotted paths a repository may set,
  replacing the single `TURNIP_RUNNER_SERVICE_ACCOUNT_ALLOW_FROM_CONFIG`
  boolean, which does not generalise as sensitive settings accumulate
- Unrecognised keys rejected through the existing accumulating validation
  path — the gap that let a misplaced top-level `serviceAccount` be read
  and silently discarded during the pilot, costing a deploy cycle to
  diagnose from an unrelated-looking credentials error
- `schemaVersion: v1alpha2`, with no compatibility machinery
- `.turnip/config.yaml` as the preferred configuration location, giving a
  repository a directory for turnip-related files rather than a single
  root file; root `turnip.yaml` stays accepted, `.github/turnip.yaml` is
  dropped, and only the `.yaml` extension is read. Two probes, as today,
  so the not-found path costs no more than it already does

**Not in this slice**: resolving a floating version, deriving a version
from the IaC code's own constraint, provisioning additional binaries, and
per-project Runner resources or timeouts. The first two are in the Backlog
below; `runner:` gives the last an obvious home when it is wanted.

**Global requirements covered**: amends Requirement 18, restated around
`uses`/`with` — which also repairs 18.8, whose "a version the Server
recognizes" wording still described the allowlist `grpc-runner` task 22
replaced — and Requirement 14.2a's cross-reference to it.

---

### Slice 14: Per-Tool Provisioning

**Goal**: Provision each IaC_Tool the way that tool actually works, rather
than assuming every tool is a single static binary.

**Delivers**:
- A Provisioning_Strategy per tool: **copy-out** (an initContainer copies
  the binary onto a shared volume, as today) or **run-in-image** (the
  Runner Job's main container *is* the vendor image, with turnip's runner
  binary supplied to it)
- Helmfile on run-in-image. It is a runtime, not a binary: it shells out to
  `helm`, `helmfile diff` needs the helm-diff plugin, helm-secrets needs
  `sops`, and helm finds its plugins through an environment the vendor
  image sets and turnip's Runner does not
- Cloning moved into an initContainer running turnip's own image, so the
  Runner depends on nothing but its own binary and the tool — which is what
  lets one Runner serve both strategies
- Terraform unchanged on copy-out, which remains correct for a tool that
  genuinely is one static binary

**Why not just copy more files**: it works until the vendor reorganises
their image. turnip's provisioning table would become a mirror of someone
else's Dockerfile, re-creating in a new form the bottleneck the
vendor-image design exists to avoid — adopting a tool release would again
wait on a turnip change.

**Discovered from the pilot**, which failed with `exec: "helm": executable
file not found in $PATH` after AWS authentication was working — earlier
than the helm-plugins problem that had been anticipated as the next
blocker.

**Not in this slice**: Pulumi's strategy and OpenTofu as a tool, both
routed to Slice 7; installing helm plugins a vendor image does not bundle;
cloud CLIs; remote-cluster authentication.

**Traps recorded during research**, none of which bite today but each of
which would bite silently: `ghcr.io/opentofu/opentofu:*-minimal` is
`FROM scratch` (no shell, no git, no CA certificates), and
`pulumi/pulumi:*-nonroot` runs as UID 1000, which would break writes to a
mounted workspace without an `fsGroup`. Every image turnip uses today runs
as root with a full userland.

**Global requirements covered**: amends Requirement 14.2a, which specifies
the copy-one-binary mechanism as though it were the only way a Runner Job
can obtain its tool.

---

### Slice 15: Refuse Fork Pull Requests

**Goal**: Never run an Operation on code from a repository other than the
one turnip is installed on.

**What's missing today**: turnip has no concept of a fork — no occurrence
in production code, and `github.PullRequest` carries `Number`, `HeadSHA`,
`BaseRef` and `HeadRef` but nothing identifying the head *repository*, so
there is no field to compare even if a check existed. An automatic plan
runs on `pull_request`/`opened` with no authorization check by design,
which means a fork's head commit would be cloned and executed in a Pod
holding cloud credentials — and the configuration file is read from that
same commit, so its author also chooses the project list and environment.

**Delivers**: head-repository identity on `PullRequest`, parsed from the
webhook payload and from `GetPullRequest` for `issue_comment` events, and
a refusal when it differs from the base repository.

**Applies to both trigger paths.** The collaborator check does not cover
this: a trusted collaborator commenting `/turnip plan` on a fork PR runs
untrusted code. The trigger is authorized; the code is not.

**Why it was not scheduled earlier**: the only deployment was a single
private test repository with no forks in play, so this is a real gap
without being a present risk. Prioritising it over the Helmfile MVP would
be fixing the wrong thing first.

**Resolved on delivery**: the refusal is **silent** on the pull request —
an attacker gets no feedback, and the legitimate fork contributor is
covered by documentation instead. It is paired with a `WARN` log entry
carrying the base repository, pull request number, head repository and
actor, so an operator can see refusals happening; repeated entries naming
one head repository are a detection signal, not noise. No opt-in was
added: there is no environment variable, `turnip.yaml` key or override
that permits an Operation on a foreign pull request, because the question
is where the code comes from rather than who is asking.

**What a 2026-09-19 security review added.** The review confirmed
everything above, including that the comment path needs the same check.
It found one thing this entry did not cover, and it is separable from the
fork question: `internal/runner/clone.go` runs
`git remote add origin <authenticated URL>`, which persists the GitHub App
installation token into `.git/config` inside the **shared workspace
volume** that the tool container also mounts. That defeats the deliberate
scoping of the token to the clone container — `internal/jobs/build.go`
keeps `TURNIP_GITHUB_TOKEN` out of the main container's environment
precisely so the tool never sees it — because any code executing in the
tool container can simply read it out of the repository it was handed.

The fix is independent of the fork check and worth doing regardless: a
credential helper or `http.extraHeader` in the clone step, or rewriting
the remote to a clean URL before the tool container starts.

**The token in `.git/config` was deliberately left out of Slice 15**
(2026-09-20), because it is a separate exposure with a separate fix and
folding it in would have widened a security slice mid-flight. It still
has **no slice of its own** — it is recorded only in this entry and in
the workspace-exposure table above, which means nothing is scheduled to
fix it. Whoever picks it up should give it a slice rather than attaching
it to an unrelated one.

**Delivered** (2026-09-20): head-repository identity on
`github.PullRequest` (`HeadRepo`, `Author`) mapped from both the webhook
payload and `GetPullRequest`, an `IsForeign` comparison that fails closed
when GitHub reports no head repository, refusals on both trigger paths,
and a `github.ErrRefused` sentinel that makes a refused delivery answer
**200** and count once as `rejected` — a 500 would have made GitHub retry
a delivery turnip declined on purpose. `closed` deliberately still
reaches `handlePRClosed`, so a fork pull request holding a Lock from
before this slice can still release it.

The review also confirmed a mitigation that **does** hold, worth recording
so it is not re-litigated: `runner.serviceAccount` cannot be chosen from
the pull request's own configuration unless an operator opted in, because
the allowed-overrides default permits nothing. A fork's pull request
inherits the operator's default ServiceAccount rather than selecting one.

---

### Slice 16: Cloning Submodules

**Goal**: Check out a repository's submodules, configurably, so a tool
reaching through a submodule path finds files rather than an empty
directory.

**What was missing**: `clone.go` ran `init`, `remote add`, a
bounded-depth `fetch`, `checkout` and a merge, and stopped. A submodule
path was present but empty, and nothing reported it.

**Found in the pilot** as `Error: repo .. not found` from `helm pull
../helm-charts/...` — helm could not read the path as a local chart, so it
parsed it as `<repo>/<chart>` and reported a missing repository named
`..`. The actual cause appears nowhere in that message.

**Delivers**: submodule initialisation after the merge; a three-state mode
(`none`/`top-level`/`recursive`) defaulting to `top-level` — not
`shallow`, which in git means a depth-limited fetch and would promise the
opposite of what happens, since submodules are fetched at full depth; a
Server-level default (`TURNIP_CLONE_SUBMODULES`) with a repository-level
override (`clone.submodules`, nested under a new `clone:` block beside
`projects:` rather than loose at the top level) through the existing
`TURNIP_ALLOWED_OVERRIDES` gate; token authentication for private
submodules, including URLs written in SSH or any other scheme git accepts,
rewritten to authenticated HTTPS; and a loud failure instead of a silently
incomplete checkout.

**Prior art, diverged from deliberately**: `actions/checkout` names its
input `submodules` and takes the same three states, but defaults to
`false`. The name is adopted; the default is not — that action checks out
repositories for arbitrary purposes, where turnip clones specifically to
run IaC that may reference submodule paths, so defaulting off would make
every repository with a submodule meet the failure above before anything
worked. Atlantis has no native support — its issue has been open since
October 2018 and users work around it with server-side pre-workflow
hooks, which is a gap nobody closed rather than a considered rejection.

**Carries a security fix**: redaction currently replaces only arguments
*exactly equal* to the authenticated URL, so a token carried inside a
larger argument — as submodule authentication requires — would reach a
pull request comment. Redacting the token itself is part of this slice
rather than a follow-up.

**Not in this slice**: per-repository server-side configuration (Backlog
below), a configuration UI, submodules hosted on a host other than the
repository's own — a GitHub installation token authenticates nothing on
GitLab or an internal server, so those are reported rather than attempted
— and submodule-aware `whenModified` matching.

---

### Slice 17: Pull Request Comment Output

**Goal**: Make the consolidated comment report what an Operation did, not
merely that it happened.

**What's missing today**: a pilot run that changed four releases produced
a three-column table and nothing else. The Runner had computed the change
counts and the Server had stored them in the Lock; none of it reached the
reader.

**Delivers**: a verdict line opening the comment; one collapsed section
per Project whose summary line carries status and change counts while
shut; per-Project commands to apply, re-plan and unlock that Project
alone; the Lock this pull request holds named explicitly; and truncation
that preserves the end of a comment rather than its beginning.

**Prior art, diverged from deliberately**: Atlantis prints a heading per
project with per-project commands beneath it, and a `Plan Summary`
footer. Its per-project commands are adopted; its heading-per-project
structure is not — headings cost a screen for three projects, where a
collapsed `<details>` summary costs one line and stays legible because
GitHub renders summary text while the section is shut. That fact is what
makes the summary table unnecessary rather than merely unfashionable.

**Not in this slice**: rewriting tool output so `+`/`-`/`~` markers
highlight (Slice 7 — helmfile needs none, Terraform will); draft pull
requests; hiding no-change projects; operator-customisable templates; and
when a comment is posted versus updated, which `server-orchestration`
already settled as minimize-then-repost.

**Global requirements covered**: implements the unimplemented half of
Requirement 7.2 (the contention message names the blocking PR but omits
the link `LockStatus.PullRequestURL` already carries); implements and
**amends** Requirement 17.3; **amends** Requirement 10.2. Both amendments
drop a mandated *summary table* in favour of stating the intent — each
Project's name, status and change counts legible without expanding
anything. The table was the cheapest shape to generate when those
criteria were first written, not a decision anyone made.

---

### Slice 18: The Lock's Lifecycle as a State Machine

**Goal**: Make the Lock's lifecycle explicit, so a Lock is held exactly
while it guards something, and a stored plan is appliable exactly while it
is still true.

**Two faults. The second was found while speccing and is the sharper one.**

*A Lock can guard nothing.* A plan that fails leaves the Lock held with
nothing recorded, blocking every other pull request from planning that
Project until a human intervenes, and giving the author no hint that they
are the obstacle.

*A Lock can stay appliable when it should not be.* `StorePlan` runs only
on success, so a stored plan survives a new commit, a failed apply and a
timed-out apply — and the Lock records nothing that tells those apart.
Helmfile stores no plan artifact (`Execute` returns `PlanData: nil`), so
an apply replays only the recorded arguments while the Runner clones at
the pull request's *current* head. Three reachable consequences: applying
after a push runs unreviewed code; applying after a failed apply runs
against partly-changed infrastructure; applying after a timed-out apply
can run on top of an Operation that is still executing.

**Why a state machine rather than more flags**: expressing this with
booleans needs three — a plan exists, it is stale, and this pull request
once established one. The third exists only because clearing the first
loses the history that distinguishes "nothing was ever planned here, so
releasing evicts nobody" from "this author had a working plan and pushed a
typo". Two of the eight combinations are meaningless. A state remembers by
being a state, and repeated failures become a self-loop rather than a
contradiction.

**Delivers** three states within a held Lock, and the edges between them:

| State | Meaning | Mutating_Operation |
|---|---|---|
| `Planning` | held; nothing successfully planned yet | refused — no plan recorded |
| `PlanReady` | held; the recorded plan still describes the delta at the current head | **admitted** |
| `PlanStale` | held; a plan was recorded and something invalidated it | refused — re-plan required |

The edges that carry the slice: a plan **dispatch** moves `PlanReady` to
`PlanStale`, atomically with acquisition, so the invalidation lands at the
push rather than minutes later when the plan reports; a plan that fails
from `Planning` releases the Lock, while one that fails from `PlanStale`
holds it; a Mutating_Operation that fails or times out moves to
`PlanStale` and **keeps** the Lock, because infrastructure may be partly
changed and another pull request must not apply on top of it.

**Every release and every invalidation is announced with its reason**,
because several edges arrive at `PlanStale` for different causes and
"this plan is stale" tells an author nothing actionable.

**The many-similar-environments case, and what it does not get.** A
repository with a fleet of near-identical environments cannot narrow its
blast radius with `whenModified`, because the coupling is *true*: an edit
to a shared component genuinely affects every environment deploying it.
Measured on a real repository of that shape: a shared helmfile read by six
environments carried 50 releases, of which **34 were unconditional** and
only **3** were guarded by environment name. Restructuring was attempted
on one environment, approved, and abandoned as not worth merging.

The no-change release was written for that case, and it does not reach it.
Helmfile's `GetOperations` exposes `sync`, which upgrades every release
regardless of the diff, so a Helmfile Project has something to apply even
when its diff is clean — releasing would make `sync` permanently
unreachable for any Project whose diff came back clean, since a re-plan
finds no changes and releases again. The relief arrives for tools whose
whole mutating surface is inert without changes, when Slice 7 lands
Terraform and Pulumi. Relief for Helmfile without that cost needs
contention-based release, which is a different mechanism.

**Where it lands**: `internal/lock` gains the state, two entry points
(`AcquireForPlan`, `Apply`) and the transition table; `execute.go`,
`result.go`, `sweep.go` and `target.go` map their situations to edges. The
atomicity primitive is unchanged — `SET NX` and the compare-and-mutate
script still decide who holds a Lock, and the state lives inside the value
they already guard. A Lock written before this slice decodes with no
state and is treated as not `PlanReady`, so it asks for a re-plan rather
than being promoted to appliable.

**Carries a documentation fix**: `result.go` and `HandleResult` cite
"Requirement 6.6-6.8" for Lock behaviour, and `sweep.go` cites "6.8/8.5".
Requirement 6 is *Plan with Destroy Flag* and has five criteria, none
about Lock lifecycle; the governing requirement is 7. The wrong citation
is what made this behaviour look deliberate when nothing specified it.

**Global requirements covered**: the halves stand differently. *Releasing
after a failed plan fills a gap* — failure appears nowhere in Requirement
7, so nothing is overturned. *Releasing after a plan that found nothing
amends Requirement 7.3*, which said a plan that "completes successfully"
keeps its Lock.

**Amendment made** (2026-09-21), in place, per the convention the global
spec already uses:

- **7.3** now reads "WHEN a plan Operation completes successfully **and
  leaves something to apply**, THE Server SHALL store the plan result in
  the Lock and THE Lock SHALL remain held. A plan leaves something to
  apply when it reported changes, or when the Project's tool can act
  without them."
- **7.4** now ends "…manually unlocked, a successful mutating Operation
  completes, **or a plan ends with nothing to apply — whether because it
  failed with nothing recorded, or because it found no changes for a tool
  that cannot act without them**."

`redis-lock-manager` carries a matching amendment task, since its
documented lifecycle no longer described the code.

**Interacts with Slice 17, but requires little from it.** Slice 17's
comment offers `unlock` and the "holds locks on …" footer from
`ProjectResult.Locked`, which `HandleResult` sets from what actually
happened rather than inferring from success — so released Projects drop
out on their own. What this slice adds there is one field carrying the
reason, rendered in the trailer rather than appended to `Output`, which
today renders turnip's own sentence inside the tool's code fence.

**Deferred**: recording the head commit on the Lock. Requirement 2 reaches
the same outcome through dispatch without comparing commits. The case a
commit would close is two plans completing out of order, the older
overwriting the newer — `OperationRecord` already carries `HeadSHA`, so a
later slice can refuse an out-of-order store.

---

### Slice 19: Draft Pull Requests

**Goal**: Stop turnip acting on its own for work its author has marked
unfinished.

**What's wrong today**: GitHub sends `opened` for a draft pull request
and `synchronize` on every push to one, so turnip plans drafts exactly as
it plans anything else — running IaC against a real cluster, holding
cloud credentials, and taking a Lock on every matched Project. Because a
plan's Lock is held until applied, unlocked, or the pull request closes,
a developer iterating in a draft blocks everyone else from planning those
Projects without being told.

**turnip has no concept of a draft at all.** `github.PullRequest` carries
`Number`, `HeadSHA`, `BaseRef` and `HeadRef`; the webhook parser reads no
draft flag. So this is not a condition to add to an existing check — the
information never reaches the orchestrator, the same shape of gap Slice
16 met with `Target` and `*config.Config`.

**Delivers**: a `Draft` field carried from the webhook payload; the
automatic plan skipped for drafts at no cost beyond receiving the
webhook; `ready_for_review` handled so marking a pull request ready
produces a plan; and `closed` left untouched so a draft's Locks are still
released.

**Prior art, diverged from deliberately**: Atlantis gates this behind
`--allow-draft-prs`, defaulting to `false`, and treats `ready_for_review`
as a freshly opened pull request. Both behaviours are adopted. **The flag
is not**: planning work its author declared unfinished has no
constituency, and anyone who wants it can comment. A setting nobody
should switch on is a setting not worth having.

**Only the automatic plan observes it.** A comment trigger on a draft
runs normally, takes its Lock, stores plan data, and is appliable — being
a draft changes *when turnip acts on its own*, never what it is capable
of. That is structural rather than enforced: the `issue_comment` path
builds a `PullRequest` carrying only `Number`, so the comment path cannot
consult the flag even by accident.

**The decision worth reading** is where the guard goes. It is the first
statement of the plan-trigger arm, not a check before the switch — the
natural-looking "if it's a draft, ignore it" placement skips `closed`
too, and silently stops releasing Locks for exactly the pull requests
most likely to be abandoned rather than closed cleanly.

**Global requirements covered**: **amends Requirement 4.1**, which says
"WHEN a PR is opened, THE Server SHALL trigger plan Operations for all
matching Projects" without qualification, and GitHub sends `opened` for
drafts. Requirement 4.2's `synchronize` clause is amended for the same
reason.

---

### Slice 20: Apply Exactly What Was Planned

**Goal**: Make a Helmfile apply do exactly what its diff showed.

**What's wrong today**: the Lock exists to guarantee that an apply does
what its plan showed. For Terraform that guarantee rides on a plan file —
the plan is an artifact, the artifact is stored, and applying it cannot
touch anything the plan did not. **Helmfile has no such artifact, and
turnip stores nothing in its place.** `HelmfilePlugin.Execute` returns
`PlanData: nil`, so a Helmfile Lock carries a change summary and an empty
byte slice. What actually decided the scope of that diff — the arguments
— is discarded the moment the Operation finishes.

Arguments do reach the tool (`args = append(args, opts.ExtraArgs...)`),
and they reach *any* Operation: `executeOne` forwards `ExtraArgs`
unconditionally, with no branch on plan versus apply. So a scoped diff
genuinely runs scoped, and the apply after it genuinely runs unscoped:

| Sequence | Result today |
|---|---|
| scoped diff, then bare apply | applies **everything** — the widest possible blast radius |
| scoped diff, then differently-scoped apply | applies a scope **nobody reviewed** |
| bare diff, then scoped apply | applies less than planned — harmless, still not what was approved |

The second is the dangerous one: it looks deliberate, reads plausibly in
the pull request thread, and nothing marks the applied scope as one that
was never diffed.

**This is not a missing feature.** It is the Lock's central guarantee
silently not holding for Helmfile, and failing toward doing *more* than
was reviewed rather than less.

**The reasoning**: the arguments are the artifact. Where Terraform stores
a plan file, Helmfile stores the arguments that produced the diff —
because for Helmfile those arguments *are* the description of what was
examined.

**Delivers**: a successful plan stores its trailing arguments in the Lock
beside the plan data; an apply replays them; an apply **refuses**
arguments of its own rather than ignoring them; and `--` becomes
optional, the first `-`-prefixed token starting the arguments.

**Prior art**: Atlantis' `apply` accepts no extra Terraform arguments, for
precisely this reason — it applies a stored plan file, so an argument
could only contradict it. turnip reaches the same rule from the opposite
direction: it has no plan file, so the arguments must *be* the stored
scope.

**The decision worth reading** is refusing rather than ignoring an
argument on apply. A silently discarded argument is indistinguishable
from an honoured one until the infrastructure changes, and the author's
belief about what they applied would be wrong with nothing on the page to
correct it.

**Explicitly out of scope**: teaching turnip which flags affect scope —
the no-arguments rule exists precisely so that no per-tool knowledge of
flag semantics is needed, where a list of "scope-affecting flags" would
need updating every time a tool gained one; Terraform and Pulumi plan
artifacts, which are Slice 7's to decide, so this slice must not assume a
plan artifact exists; and validating at apply time that stored arguments
still match anything — the Lock records what was planned, the tool
reports what it finds.

**Global requirements covered**: implements the unimplemented substance of
**Requirement 7.5** — "WHEN an apply Operation is triggered, THE Server
SHALL verify the Lock is held by the current PR and retrieve the plan data
from the Lock" — which turnip satisfies literally (it retrieves the bytes)
but not in effect, since for Helmfile the bytes are empty and the scope is
not retrieved at all. Extends **Requirement 6.2**'s extra-argument
handling from the `-destroy` flag to arguments generally, and makes
**Requirement 6.5** — apply "uses the plan from the lock" — true for
Helmfile, where today it holds only for tools that have a plan file.

It also **amends Requirements 13.7 and 13.9**, which mandated Helmfile
destroy support. Both are rewritten in place: 13.7 now states that the
Plugin does not expose destroy and why, and 13.9 that a trigger naming it
is rejected as unrecognized. **Requirement 13.2 needs no change** — it
already listed only `diff`, `apply` and `sync`, and becomes correct as
written. Global design **Property 22** drops its destroy clause, after
which its "Validates: Requirements 13.2–13.5" line matches what it
actually claims.

---

### Slice 21: What a Bare Command Targets

**Goal**: Make `/turnip plan` mean what the pull request actually touched.

**What's wrong today**: two paths choose which Projects an Operation runs
for, and they disagree. The automatic plan filters —
`MatchProjects(cfg.Projects, modifiedFiles)` selects only Projects whose
`whenModified` globs match the pull request's changed files. A comment
trigger does not: `resolveTargets` starts from *every* configured Project
and narrows only when the trigger names some. So `/turnip plan` means
every Project in the repository, regardless of what the pull request
touched.

**Invisible with one Project and painful with eight.** It is not merely
slow. A plan acquires a Lock per Project and holds it until applied or
released, so one person typing four words locks the entire repository
against everyone else. Under today's lock rules a failed or no-change plan
among those keeps its Lock too, so the locks outlive the mistake.

A bare apply is noisy for the same reason: it targets every configured
Project, and `executeOne` rejects each one holding no Lock — so applying
one planned Project among eight produces one apply and seven refusals.

**Delivers**: a bare plan targets the Modified_Set, using the same
matching the automatic plan uses rather than a parallel implementation of
it; a bare apply targets only the Projects this pull request holds a plan
for, reporting *once* when there are none rather than once per configured
Project; `*` targets everything deliberately; and the configuration parser
rejects a Project named `*` or beginning with `-`, so a name that could
never be addressed fails when it is written rather than when someone tries
to plan it.

**Prior art**: Atlantis' bare `atlantis plan` re-runs the autoplan set —
*"runs plan on the projects that were modified as determined by the
`when_modified` config"* — and its bare `atlantis apply` applies *"all
unapplied plans from this pull request"*. Both defaults are narrow, and
its `-p`/`-d` flags are how you reach past them. turnip has the opposite
default and no way to ask for the narrower thing, which is the gap this
slice closes.

**Why `*` rather than `all`**: `all` is a plausible Project name, so it
could only become a selector by stealing a name someone might legitimately
use. `*` cannot collide once the parser rejects it as a name — which is
why that rejection is part of this slice rather than a tidy-up after it.

**Explicitly out of scope**: selecting Projects by directory —
`narrowByName` matches `Project.Name` only and Atlantis' `-d` has no
turnip equivalent, a real gap but one about *addressing* Projects rather
than about what a bare command defaults to; and changing the automatic
plan, which already filters correctly. This slice brings the comment path
into line with it, not the reverse.

**Global requirements covered**: the two halves stand differently, as in
Slice 18.

*The bare apply half **amends Requirement 5.3***, which says a detected
apply trigger "SHALL trigger apply Operations for all Projects configured
in turnip.yaml" — written when every configured Project was the only
selection turnip had. Requirement 5.4's named-Project clause is extended
rather than amended, since `*` is a new selector standing beside a name.

*The bare plan half fills a gap.* Requirement 4 governs the automatic plan
only, Requirement 5 governs apply, and Requirement 6.1 mentions a
comment-triggered plan solely to carry `-destroy`. No numbered criterion
says which Projects a comment-triggered plan targets, so nothing is
overturned.

**Relationship to Slice 18**: they compound without depending on each
other. Slice 18 decides which outcomes release a Lock; this slice reduces
how many Locks a careless command takes in the first place.

**Sequencing — worth taking before Slice 20.** Both change
`resolveTargets`: this slice changes which Projects it returns, Slice 20
changes what it does with the tokens after them. This is the larger
structural change of the two, so taking it second would mean reworking
Slice 20's argument handling around it.

---

### Slice 22: Refuse Tool Arguments That Name an Executable

**Goal**: Stop a pull request comment's trailing arguments from choosing
which binary the Runner executes.

**What's wrong today**: trailing arguments travel from the comment to the
tool's argv with **no filtering at any hop** —
`internal/github/parser.go` captures everything after `--` verbatim, and
`target.go` → `execute.go` → `internal/jobs/build.go` (as JSON in
`TURNIP_EXTRA_ARGS`) → `internal/runner/config.go` → `run.go` →
`internal/plugin/helmfile.go` are each a pure pass-through. The only
allow/deny machinery turnip has, `TURNIP_ALLOWED_OVERRIDES`, governs
`turnip.yaml` fields and not comment arguments.

**The exposure is narrower than it first looks, and sharper.** No shell is
involved — `exec.CommandContext` takes an argv slice — so shell
metacharacters are inert. The problem is that several helmfile flags name
a *path that helmfile then executes or reads*:

| Flag | Where registered | Effect |
|---|---|---|
| `-b`, `--helm-binary` | root persistent flag | names the executable helmfile invokes; run during `helmexec` init, before any chart resolution |
| `--post-renderer` | the `diff` subcommand itself | names an executable helm invokes |
| `--args`, `--diff-args` | the `diff` subcommand | raw argument pass-through into `helm` |
| `-f`, `--file`, `--state-values-file` | root / subcommand | read a path — but see the correction below before treating this like the rows above |

Being a *root persistent* flag is what makes the first one reachable:
Cobra merges parent persistent flags into a subcommand's flag set, so it
parses **after** the operation name, which is where the attacker-controlled
tail lands. `plugin-helmfile/tasks.md` records the opposite assumption —
"global flag, must precede the subcommand" — and that assumption is what
left this open. The second entry reaches the same outcome without
depending on persistent-flag behaviour at all, so the fix cannot rest on
disproving the first.

These names are from helmfile's public CLI reference at the pinned
version, not privileged information; they are written down here because a
blocklist or allowlist cannot be authored without them.

**Correction: `-f` does not belong with the rows above it.** An earlier
draft of this entry listed it as a danger alongside the
executable-naming flags, and that was wrong. `-f` is *the* mechanism for
pointing helmfile at a state file that is not at the default name in the
working directory, which real repositories routinely need — a repository
whose state files sit at its root, or which keeps one file per
environment, cannot run at all without it. Blanket-blocking it would
refuse legitimate configuration rather than an attack.

The distinction that matters is not the flag but **where the value comes
from**. A path supplied by repository configuration is reviewed alongside
the code it selects; a path typed into a pull request comment is not. That
is a source rule, not a flag rule, and it is the reason this slice has to
decide *who may supply arguments* rather than just *which arguments
exist*. The executable-naming flags in the rows above are different in
kind: they grant code execution regardless of who supplies them.

**Authorization does not help.** `target.go` requires write permission
only when the Operation is not the Plugin's plan Operation, and Helmfile's
plan Operation is `diff` — so the *least*-privileged trigger is exactly
the one that carries arguments.

**Prior art**: Atlantis ships `--blocked-extra-args`, defaulting to
`-chdir,--chdir,-plugin-dir,--plugin-dir`, for precisely this class.

**Configured defaults are the other half of the same rule.** Many
repositories cannot run at all without arguments: where a Project's state
file is not at the default name in its working directory, helmfile needs
`-f` on *every* invocation, and making a human type that into every
comment is both hostile and unreviewable. So this slice decides two
things, not one — which arguments a *comment* may carry, and which a
*repository* may configure. Three decisions, taken 2026-09-19:

**Repository-supplied, never server-side.** A state file path describes
the repository's own layout, so an operator pinning it centrally would be
describing someone else's directory structure. It belongs in
`turnip.yaml` and nowhere else — unlike `runner.serviceAccount`, where the
Server supplies a default and the repository overrides it.

**Not gated, but jailed.** Naming a file inside your own repository grants
nothing: turnip chooses the flag, the repository chooses only which of its
own files that flag names. By turnip's own rule — gates belong on what
*grants capability* — this needs no `TURNIP_ALLOWED_OVERRIDES` entry. What
it needs is containment, so that `../../../../../etc/passwd` does not
resolve.

The jail is the **workspace**, not the Project directory. A repository
with per-environment directories and one shared state file at its root is
a legitimate layout, and confining to the Project directory would refuse
it; escaping the clone is the line that matters, because that is where pod
internals begin. Enforce it twice — at validation on the Server, so the
failure arrives as a comment naming the offending value, and again in the
Runner before invoking, since the Runner receives this through an
environment variable and should not trust it.

A lexical check (`filepath.IsLocal` on the cleaned, workspace-relative
path) is proportionate. Its limit, stated rather than glossed: a symlink
committed inside the repository could point outside it, and only symlink
resolution would catch that — but the same commit could run arbitrary code
anyway, so the lexical check is not the weakest link. This is the same
invariant the Backlog records for `Project.directory`; they should share
one helper.

**A typed key, not a raw list.** `with.file` beside the existing
`with.environment`, with the Plugin translating each into the right flag
in the right position. turnip picks the flag; the repository supplies only
a value — which is what makes the previous decision safe by construction,
since there is no way to smuggle an executable-naming flag through a field
whose only use is a path. Requirement 18.3 set this precedent already with
`workspace` and `backendConfig` for Terraform, and `With` is *already* a
free-form `map[string]string` that reaches the Plugin, so this needs no
schema change at all — only another key read in
`internal/plugin/helmfile.go`.

An Atlantis-style raw `extra_args` list was considered and rejected for
now: it is precisely the surface this slice exists to restrict, and since
`turnip.yaml` is read from the pull request's own head commit it is no
more trusted than a comment. If a need appears that a typed key cannot
express, it goes through this policy rather than around it.

**`--helm-binary` is gated, not banned.** Refusing it outright would be
wrong — there are real reasons to point helmfile at a helm build other
than the one the tool image ships, a merged-but-unreleased fix being the
obvious one. So it belongs in `TURNIP_ALLOWED_OVERRIDES` beside
`runner.serviceAccount`: refused by default, available to an operator who
has decided their repositories may do it. That is the same judgement
`runner.serviceAccount` already encodes — the setting grants capability,
so a gate sits on it, but the capability is legitimate.

Note what this does *not* cover: running a forked **helmfile** rather than
a forked **helm**. That is an image question, not a flag question, and no
flag reaches it — see the Backlog.

**Delivers**: validation of trailing arguments on the **Server**, before
the Job is built, so a refusal reaches the pull request rather than
failing opaquely in a pod. An allowlist per tool is preferable to a
blocklist — a blocklist has to be re-audited every time a tool adds a
flag, which is the same maintenance trap turnip rejected for tool
versions. A plausible Helmfile allowlist to start from is `-l`/`--selector`,
`--set`, `--context` and `--concurrency`: enough for the scoped-diff
workflow Slice 20 is built around, and nothing that names a path. Also to
decide: whether trailing arguments should require write permission even on
a plan, since today only the Operation *name* is authorized, never its
arguments.

**Relationship to Slice 20**: they compose, and neither substitutes for
the other. Slice 20 makes the plan the only Operation that accepts
arguments, which shrinks the surface but does not close it — a plan still
takes them, and the plan is the lowest-privilege trigger there is.

**Rated HIGH** in the 2026-09-19 security review, with the caveat that
turnip's only current deployment is a private repository where every
commenter already has repository access.

---

### Slice 23: Authorize on Permission Level, Not Call Success

**Goal**: Make the collaborator gate test what GitHub actually answered.

**What's wrong today**: `Authorizer.IsCollaborator`
(`internal/github/authorize.go`) discards the permission string and
returns true whenever the API call merely *succeeds*. It asks
`GET /repos/{owner}/{repo}/collaborators/{username}/permission`, whose
documented values include `none` — and `none` is a value in a **200
response body**, which cannot be observed on a 404.

**The premise is written down, and it is wrong.**
`github-integration/design.md` justifies the single cached call with the
claim that "GitHub's API returns 404 for a non-collaborator on that
endpoint". That 204/404 behaviour belongs to the *other* endpoint,
`/collaborators/{username}` — which this codebase already implements
correctly as `Client.IsCollaborator` and never calls from the Authorizer.
The tests encode the same assumption, which is why they pass: the
non-collaborator fixture is an *error*, and nothing asserts that a `none`
permission yields false.

**What it reaches**: the collaborator check is the only gate on the
comment path, and the write-permission check is skipped for plan
Operations. Apply, sync and unlock stay protected. A plan does not — and a
plan carries both project selection (any Project in `turnip.yaml`, not
only ones the pull request touched) and trailing arguments, which is where
this compounds with Slice 22.

**Scope honestly**: latent on a private repository, where anyone able to
comment already has read access and would pass a correct check anyway. It
matters on the first public installation, and it is an unambiguous
violation of global Requirement 16.1/16.2 either way.

**Delivers**: the Authorizer asking the endpoint that answers the
question; rejection of `none` and of unrecognized values; the design
decision *corrected* rather than quietly edited, since a wrong premise
left in place is what produced this; and regression tests for the `none`
case in both `authorize_test.go` and `comment_test.go`.

**Do not fix it with a rank comparison.** On a public repository the
permission endpoint may report `read` for any user at all, since public
repositories grant universal pull access — so `>= read` would still admit
the attacker. The fix is the endpoint, not the threshold.

**And do not let `Client.IsCollaborator` be tidied away before this
lands.** A dead-code sweep flags it as test-only and correctly so: nothing
in production calls it today. But it is the *right* implementation — the
204/404 endpoint that actually answers "is this person a collaborator" —
and this slice's entire fix is to start calling it. Deleting it as dead
would remove the correct code and leave the broken code in place, which is
the worst available outcome. Recorded here because the finding and the fix
live in different places, and whoever runs the sweep may not be whoever
reads this slice.

---

### Slice 24: How the Runner Receives Its GitHub Token

**Goal**: Stop the GitHub App installation token being readable by anything
that can read a Pod.

**What's wrong today**: `internal/jobs/build.go` hands the token to the
clone container as a literal environment value —
`corev1.EnvVar{Name: "TURNIP_GITHUB_TOKEN", Value: op.GitHubToken}` — and
turnip creates no Kubernetes Secret anywhere. A search of the Job path
finds no `secretKeyRef` and no `EnvFrom`; the only secret the codebase
handles is the Server's own webhook secret, read from the Server's
environment.

**The consequence is a collapsed privilege boundary.** A literal env value
is a field of the Job and Pod objects, so the token is readable by anyone
who can `get pod` or `get job` in the Runner's namespace, shows up in
`kubectl describe` and `kubectl get -o yaml`, and sits in etcd in
cleartext unless the cluster has encryption-at-rest configured for that
resource. Pod-read is granted far more freely than secret-read —
monitoring agents, debugging access, operators — so a `secretKeyRef` would
put the credential behind a permission an operator grants deliberately,
where today it rides one they grant casually.

**The existing intent is right and must survive the fix.** The code
comment records that only the clone reads the token, "so the container
that runs the tool has no business carrying it", and the token is
correctly absent from the main container's environment. That scoping is
sound; it is the *delivery mechanism* that leaks, not the placement.

**Three surfaces carry the same token, and this slice should own all
three** rather than fixing the one that happens to be in front of us:

| Surface | Where | Who can read it |
|---|---|---|
| Job/Pod spec env value | `internal/jobs/build.go` | anything with pod or job read in the namespace; etcd |
| `.git/config` in the workspace | `internal/runner/clone.go`'s `git remote add` | any code running in the tool container — recorded in Slice 15 |
| `OperationStart` gRPC message | `internal/runner/reporter.go` | plaintext pod-to-pod traffic, on the clone-failure path only |

**Delivers**: a delivery mechanism that keeps the credential out of a
widely-readable API object. Three candidates, and they are not
interchangeable:

| # | Option | Closes pod-read | Closes tool-container read | Available today |
|---|---|---|---|---|
| A | per-Job Secret via `secretKeyRef`, deleted with the Job | yes | no | yes |
| B | fetched over gRPC instead of handed over | yes | no | **no** — see below |
| C | never persisted: credential helper or `http.extraHeader` in the clone step | n/a | **yes** | yes |

**C is not an alternative to A or B** — it addresses where the token ends
up rather than how it arrives, and it is the only one of the three that
closes a surface reachable from a pull request. A and B both defend
against someone who already has cluster access. Do C regardless of which
delivery option wins.

**B is blocked on Slice 25.** The existing RPC is client-streaming
(`rpc ExecuteOperation(stream ExecuteOperationRequest) returns
(ExecuteOperationResponse)`), so there is no server-to-client channel to
push a credential down — that part is only a proto change. The real
obstacle is that the endpoint authenticates nothing, so a credential-fetch
call could only be keyed on the operation id, which is itself a plaintext
env value on the pod (`TURNIP_OPERATION_ID`). That trades "pod-read yields
the token" for "pod-read yields the id, which yields the token" — an
indirection, not a fix. Slice 25 is what makes B coherent.

**Recommended order**: C first (smallest, and the only one a pull request
can reach), then A as the delivery change, with B as the end state once
Slice 25 lands.

**Free either way, and larger than the credential.** `OperationStart`
declares twelve fields; the Server reads exactly one of them —
`payload.Start.GetOperationId()` at `internal/rpc/server.go:83`. The other
eleven, `github_token` among them, are populated by
`internal/runner/reporter.go` on every stream open and discarded on
arrival. Deleting the token field is the security-relevant part; deleting
the rest is the reason the message exists at all being re-examined.

Note the reporter re-sends the whole message on **every** reconnect, and
`reportOnce` retries on a fifteen-minute budget — so this is not a
one-off cost, and it is the same payload that carries the credential.
Verified by counting the proto's fields against the Server's reads, not
inherited from a report.

**On the RBAC objection to A**: adding `secrets: create/delete` to the
Server's Role looks like an escalation and mostly is not — anything that
can create a Pod in a namespace can already mount any Secret in that
namespace into it, so Job-create already implies Secret-read. The genuine
costs are lifecycle: an `ownerReference` so the Secret dies with the Job,
and a decision about orphans if garbage collection misses one.

**What does not help**: the token already being short-lived. A GitHub App
installation token expires in about an hour, but the exposure window *is*
the Job's lifetime — precisely the interval in which the token is live. Expiry
bounds the damage afterwards; it does not reduce the exposure.

**Provenance**: the 2026-09-19 security review rejected a related finding
about the token crossing plaintext gRPC, and in doing so observed that the
Pod spec is the cheaper read — "retrievable without touching the network
at all". That observation, not the rejected finding, is what this slice
acts on.

---

### Slice 25: Authenticating the Runner to the Server

**Goal**: Let the Server know *which* Runner it is talking to, so an
Operation's results can only come from the Pod that actually ran it.

**What's wrong today**: `internal/rpc.NewServer` is a bare
`grpc.NewServer()` — no credentials, no interceptors — and the Runner
dials with `insecure.NewCredentials()`. The Server identifies an Operation
solely by the `operation_id` the client sends, which it looks up in Redis
(`MarkStarted`, `ClaimForResult`). So the id is a **bearer capability**,
and it is handed to the Pod as a plaintext environment value
(`TURNIP_OPERATION_ID`, `internal/jobs/build.go`) — readable by anything
with pod-read in the namespace.

**This is worth doing on its own merits, not just as a prerequisite.** Any
Pod that can reach the Server and knows an id can stream log lines and a
*final result* for that Operation. That means a forged success carrying
attacker-chosen output posted to the pull request, and a Lock released as
though an apply had completed. It is the one gap that lets a third party
write to a pull request's record of what happened.

**The mechanism**: an audience-scoped projected ServiceAccount token.
`internal/jobs/build.go` adds a `ServiceAccountToken` projection to the
Pod's existing volumes with an explicit `audience` and a short expiry; the
Runner sends it as gRPC metadata; the Server validates it with a
`TokenReview`, checking that audience.

**Why the explicit audience matters**: every Pod already automounts a
token at `/var/run/secrets/kubernetes.io/serviceaccount/token`, but its
audience is the API server. Sending *that* to the Server would let the
Server replay it and act as the Runner's ServiceAccount. An
audience-scoped token is worthless anywhere but here.

**The check binds to the Pod, not the ServiceAccount — and that is the
decision worth recording, because the obvious rule is wrong.** turnip lets
an operator, and where `TURNIP_ALLOWED_OVERRIDES` permits a repository,
choose `runner.serviceAccount`. A rule like "the SA must be
`turnip-runner`" breaks the moment anyone uses that feature, and where a
repository chooses the SA it would let the repository influence which
identities the Server accepts.

The SA-agnostic rule is: *is this token from the one Pod belonging to the
Job created for this Operation?* Every piece already exists:

| Step | Mechanism |
|---|---|
| Operation → Job | `turnip.ivan.vc/operation-id` label (`internal/jobs/labels.go`) |
| Job → Pod | `batch.kubernetes.io/job-name` selector (`internal/jobs/status.go`) |
| exactly one Pod | `BackoffLimit: 0` on every Runner Job |
| permission to look | `pods` get/list, already in the Server's Role |

Nothing about the ServiceAccount enters the decision, so the override
feature stays orthogonal. **No change to anyone's ServiceAccount is
needed** either: the projection is a field of the Pod spec turnip already
writes, not of the SA object. The resemblance to EKS Pod Identity/IRSA —
where the SA genuinely *is* annotated — is misleading, because that is a
cloud identity system rather than a Kubernetes-native one.

**Open question to settle before implementing**: whether `TokenReview`
returns the Pod name and uid in `UserInfo.Extra` for bound tokens on the
Kubernetes versions turnip targets. If it does, the binding is exact. If
not, the fallback is for the Runner to send its Pod name via the downward
API, with the Server checking the token's audience and SA, that the named
Pod is the Operation's Pod, and that the Pod's SA matches the token's —
weaker, since a co-resident Pod running as the same SA could claim another
Pod's name, but still SA-agnostic and far stronger than an unauthenticated
id.

**Where it goes**: a `grpc.StreamInterceptor`, not a check inside the
handler. `NewServer` takes no options today, so there is exactly one place
to add it, and it then fails closed for any RPC added later.

**Encryption is a separate axis.** One-way TLS — the Server presents a
certificate, the Runner verifies it — encrypts the channel without any
client PKI. Full mTLS would authenticate too, but a client certificate
delivered by environment value or Secret is itself a credential with the
delivery problem Slice 24 exists to solve, and issuing per-Job
certificates needs something to authenticate the request, which lands back
on this token. SPIFFE/SPIRE resolves that properly, at the cost of a large
new infrastructure dependency turnip does not otherwise require.

**Amendments**: Slice 5 `grpc-runner` (the service gains an interceptor
and a metadata contract, and `NewServer`'s signature changes), Slice 6 for
the Server-side wiring, and `deploy/base/role.yaml` for one new verb —
`create` on `authentication.k8s.io/tokenreviews`.

**Unblocks Slice 24's option B**, which is circular without it.

---

### Slice 26: Real-Time Operation Output

**Goal**: Let someone watching a long plan or apply see what it is doing
while it runs, instead of waiting for the comment at the end.

**What already exists, and is the reason this is smaller than it sounds**:
the Runner already streams every log line to the Server over the existing
`ExecuteOperation` RPC, and `HandleLog` already receives each one. The
Server simply drops them. Nothing needs to be added to the Runner, the
proto, or the Job to get the data flowing — it is already flowing.

turnip also already has both primitives this needs. `internal/orchestrator/notify.go`
publishes to a per-Operation Redis channel and subscribes to it from
another replica, which is exactly the fan-out shape live output requires.
And `cmd/server/main.go` already serves an HTTP mux (`/healthz`,
`/readyz`, `/metrics`) that an `/operations/{id}` route can join. Slice 29
moves the webhook off `/` beforehand, so this slice inherits a mux where
`/` is no longer a catch-all and route precedence needs no special care.

**What is genuinely missing**: Server-side accumulation. The Runner's
256 KiB ring buffer is for *stream reconnection* — resending what a
dropped connection may have lost — not for viewer backfill. A browser
opening mid-apply needs the output so far, and no component holds it on
the Server side today.

**Prior art, and turnip can beat it.** Atlantis shipped this as
"Real-time logs" in v0.18.0 (PR #1937): `GET /jobs/{job-id}` serves an
xterm.js page, `GET /jobs/{job-id}/ws` the websocket, and the user reaches
it through the commit status **Details** link rather than the comment
body. Its mid-run semantics name a real hazard — `addChan` drains the
buffered output into the new channel *before* registering the receiver,
specifically so backfill cannot interleave with live lines — though the
transport decision below removes the need to solve it by hand.

What should **not** be copied is where it is stored. Atlantis keeps job
output in per-process maps, so a browser routed to a replica that did not
run the job gets `invalid key`; its HA support (Redis locking, external
plan stores) explicitly does not extend to logs, and its deployment
manifests still ship `replicas: 1`. turnip is stateless by construction,
so accumulating into a capped Redis stream per Operation gives it
multi-replica streaming that Atlantis cannot offer.

**Nor should its security defaults be copied.** Atlantis's
`--websocket-check-origin` defaults to `false` and web auth is off unless
switched on, so by default any page a user visits can open the job
websocket and read plan output — mitigated only by job ids being UUIDs.
Plan output routinely discloses infrastructure detail, so viewer
authorization is a design question here, not a later hardening pass. It
belongs with the Backlog's stated-security-model entry.

**Transport and storage are decided.** Output accumulates in one Redis
stream per Operation — `XADD` per line, `XREAD BLOCK 0 STREAMS <key> <id>`
to follow it — and reaches the browser over server-sent events rather than
a websocket.

These are one decision rather than two, because what makes them fit is a
shared cursor. An SSE `id:` field carries the stream entry ID; the
browser's `EventSource` replays it as `Last-Event-ID` on automatic
reconnect; the Server resumes the `XREAD` from exactly that entry. The
resume position lives in the stream rather than in a replica's memory, so
a reconnect landing on a *different* replica continues where it left off.
That is the property Atlantis structurally cannot offer, and it is why
this is worth doing differently rather than porting their design.

It also disposes of the ordering hazard noted above instead of
re-solving it. One `XREAD` cursor that reads history and then blocks for
more is a single ordered sequence — there is no separate backfill path to
interleave with live lines, so the bug `addChan` exists to prevent cannot
be written here.

*Alternative considered*: a capped list plus pub/sub. *Rejected because*
pub/sub is fire-and-forget — a subscriber that connects late or drops
briefly loses lines with no way to ask for them again — so it needs the
list for backfill regardless, and then a hand-written seam between "what
I read from the list" and "what arrived live". That seam is precisely the
interleaving bug above, reintroduced by the storage choice.

*Alternative considered*: websockets, as Atlantis uses. *Rejected
because* this output flows one way only, and SSE reconnects and replays
its cursor without application code having to manage either.

**This sets a Redis version floor of 5.0**, where streams were introduced.
The documentation states no floor at all today, so it should be recorded
once — alongside Slice 27's AUTH/TLS work — rather than rediscovered per
slice.

**Sizing is arithmetic, not a worry.** Capping each stream with
`XADD ... MAXLEN ~` at the Runner's own 256 KiB buffer bound costs about
that per in-flight Operation, so N concurrent Operations cost roughly
N × 256 KiB until retention expires them. Worth stating because that cap
is also half the retention answer below.

**Still open**:

- **Retention.** Atlantis clears on pull request close and loses
  everything on restart. `MAXLEN` plus an `EXPIRE` on the stream key is
  the obvious turnip shape, but the cap interacts with how much scrollback
  is worth keeping.
- **Who may view.** GitHub identity would be the principled answer and is
  the most work; a signed, expiring URL in the check run is the cheap one.
- **Entry point.** The check run's Details link is where Atlantis puts it
  and where a reader already looks; the consolidated comment is the
  alternative.

**This raises what an unauthenticated Redis exposes.** Locks and plan
artifacts already live there; streaming output adds the full text of every
Operation, which is the most disclosure-heavy content turnip handles.
Slice 27 is the mitigation, and is a prerequisite in practice even though
the dependency column does not say so.

**This is why `HandleLog`'s unread parameter and the Runner's ring buffer
must not be tidied away** — a dead-code sweep flags both, and deleting
either forecloses this slice. The note in `result.go` says so at the site.

---

### Slice 27: Authenticated and Encrypted Redis

**Goal**: Let turnip connect to a Redis/Valkey that requires AUTH, TLS, or
both — so that adopting turnip stops being a reason to weaken the
datastore it depends on.

**The problem is currently written down as advice.**
`docs/deployment.md` does not merely omit authentication; it instructs
operators to remove it. A managed instance "works only if you disable its
AUTH/in-transit-encryption requirement, or put an unauthenticated proxy in
front of it". That is the deployment guide telling a reader to turn off
encryption in transit on ElastiCache or Memorystore in order to run this
project. The instruction is accurate about today's code, which is what
makes it worth fixing rather than rewording.

**The blast radius is exactly one process**, which is what makes this
slice small. `cmd/server/main.go:49` holds the only
`redis.NewClient` in the tree, and `internal/runner` contains no Redis
reference at all. No Redis credential is threaded into a Job, an env var
on a Runner Pod, or anything else that a pod executing repository-supplied
IaC can read. This is therefore *not* another instance of the problem
Slices 24 and 25 exist to solve — it touches the Server alone.

**Decision 1 — TLS is inferred from the URL scheme rather than a separate
flag.** `TURNIP_REDIS_ADDR` gains the ability to be a URL:
`rediss://host:6379` connects over TLS, `redis://host:6379` does not, and
a bare `host:port` behaves exactly as it does today. go-redis v9.22.0
already implements precisely this — `ParseURL` accepts both schemes
(`options.go:674`) and installs a `tls.Config` when the scheme is `rediss`
(`options.go:706`) — so this is a library feature to adopt, not behaviour
to hand-roll.

The bare form must keep working, and not only for compatibility's sake:
`ParseURL` rejects a schemeless value outright, and
`deploy/overlays/kind/kustomization.yaml:32` ships
`TURNIP_REDIS_ADDR=redis:6379`. So the address is parsed as a URL only
when it carries a scheme, and treated as an `Addr` otherwise.

*Alternative considered*: an explicit `TURNIP_REDIS_TLS=true`.
*Rejected because* it is a second source of truth for a fact the address
already states, and the two can disagree.

*The tradeoff this accepts*: a one-character typo — `redis://` where
`rediss://` was meant — silently yields a plaintext connection, and
"did I actually get TLS?" is no longer answerable by reading the config.
The slice compensates by making the resolved transport **observable
rather than inferred twice**: the Server logs the transport it actually
negotiated at startup, and the readiness surface reports it. A log line
saying `redis: connected addr=... tls=false auth=none` is what turns a
silent typo into a visible one.

**Decision 2 — the URI may carry a username; it may never carry a
password.** A username in the URI is the natural place for it and needs
no variable of its own (`redis://turnip@host:6379`), which is why this
slice adds no `TURNIP_REDIS_USERNAME`. A password there must be refused
at startup, and the reason is specific to turnip's own manifests rather
than a general principle: `TURNIP_REDIS_ADDR` is delivered as a
`configMapGenerator` literal in every overlay that sets it
(`deploy/overlays/kind/kustomization.yaml:32`,
`deploy/overlays/release/kustomization.yaml:29`, and the note at
`deploy/base/kustomization.yaml:11`). A password embedded in that value
therefore lands in a **ConfigMap, not a Secret** — a different RBAC verb,
a different audit story, and a value that turnip itself would echo into
the startup log line Decision 1 just introduced.

`ParseURL` will read `rediss://user:pass@host` into `Options.Password`
without complaint (`options.go:686`), so silence here would quietly defeat
the split. The startup error should name the alternative, not merely
refuse.

**Decision 3 — the password arrives by value or by path, with the path
preferred.** `TURNIP_REDIS_PASSWORD` and `TURNIP_REDIS_PASSWORD_PATH`,
following the shape `internal/orchestrator/config.go:107-116` already
established for `TURNIP_GITHUB_PRIVATE_KEY` / `..._PATH`, down to naming
both in one `missing` entry when neither is set. The path form is the
recommended one for the same reason it matters for the private key: a
mounted Secret file is not readable by anything holding pod-read, which is
the exposure Slice 24 exists to close.

**Decision 4 — `?skip_verify=true` must be refused explicitly.** This is
the decision that would otherwise be made by accident. `ParseURL` honours
a `skip_verify` query parameter and writes it straight into
`TLSConfig.InsecureSkipVerify` (`options.go:882`). Adopting `ParseURL`
wholesale therefore ships a certificate-verification escape hatch that
appears nowhere in turnip's own code or documentation — the exact option
this slice would otherwise have deliberately declined to add, arriving
through the back door. An escape hatch that disables verification tends to
become permanent in someone's cluster, and one that is undocumented
becomes permanent *and* invisible.

Refusing is better than silently forcing it back to `false`: a
configuration that does not do what it plainly says is its own failure
mode. Private CAs are served instead by an explicit CA bundle path
(`TURNIP_REDIS_CA_PATH`), which solves the legitimate case — an internal
certificate authority — without solving it by not checking.

Usefully, `ParseURL` already errors on query keys it does not recognize
(`options.go:887`), so turnip needs no typo guard of its own; only the
keys it recognizes but turnip should not accept need handling.

**What else adopting `ParseURL` quietly brings**, and should be decided
rather than inherited: a database number from the URL path (`/3`, and a
`?db=` override), and roughly twenty connection-tuning parameters —
`dial_timeout`, `pool_size`, `max_retries`, and so on
(`options.go:850-876`). These are arguably a gain, since turnip exposes no
pool tuning today. But they mean `TURNIP_REDIS_ADDR` stops being an
address and becomes a tuning surface, in a ConfigMap, unmentioned in any
documentation. The slice should either accept them deliberately and
document them, or restrict the accepted query keys to an allowlist.

**Documentation is part of the slice, not a follow-up.**
`docs/deployment.md`'s prerequisite 2 (lines 13-28) has to be rewritten
rather than amended — its current text is an instruction to disable a
security control, and it is the first thing a prospective operator reads.

**Record the Redis version floor while this is open.** The documentation
states none today. ACL usernames need Redis ≥ 6 (`AUTH user pass`);
password-only AUTH works on 5. Slice 26 has since settled on streams,
which need ≥ 5.0, so the floor is worth stating once, here, rather than
rediscovered per slice.

---

### Slice 28: Seeing and Dropping Locks Without Hunting for the PR

**Goal**: List every held Lock on one page, and release one from there —
instead of opening pull requests one at a time to work out which of them
is holding the Project you want. Dropping a Lock posts a comment on the
pull request that held it, so the release is visible to whoever was
relying on it rather than silently changing the world underneath them.

**Prior art**: Atlantis's `/locks` page, which does exactly this and
comments on the affected pull request when a lock is discarded. Worth
copying in shape.

**The data is already there, and needs no schema change.** This is the
reason the slice is small. `lockKey` is `"lock:" + projectKey`
(`internal/lock/redis.go:26`) and `projectKey` is `owner/repo/project`
(`internal/orchestrator/execute.go:20`), so the key *itself* carries
everything needed to identify a row. The stored `LockData` already holds
`PRNumber`, `PullRequestURL`, `LockedAt`, `LockedBy`, `HasPlan` and
`PlanSummary`. A lock table renders from a `SCAN` plus an `MGET` with no
new fields written anywhere.

**This is the consumer those fields were kept for.** The dead-code sweep
flagged `LockStatus.LockedAt`, `LockedBy`, `HasPlan` and `PlanSummary` as
having no production reader, and they were deliberately kept because
`redis-lock-manager` Requirement 4.3 specifies them — "ahead of their
consumer, not left behind by one", as the note on the type says. This
slice is that consumer, which retires the note.

**What is genuinely missing is enumeration.** `LockManager` has no method
that lists locks — every existing method takes a `projectKey` the caller
already knows. This slice adds one. It must use `SCAN`, not `KEYS`:
`KEYS` blocks the server for the length of the keyspace, which on a shared
Redis punishes every other tenant for turnip's admin page.

**`ReleaseLock`'s signature does not fit an administrative drop.** It
takes a `prNumber` and refuses with `ErrLockedByOtherPR` when the holder
is someone else — correct for `/turnip unlock`, typed inside the pull
request that holds the Lock, but an operator dropping a stuck Lock from a
list is not acting on behalf of a pull request. Either the caller passes
the holder's number read back from the Lock (a read-then-act with a
lost-update window if the Lock changes hands in between) or the interface
grows an explicit force path. Deciding which is part of this slice, and
the CAS script already in `internal/lock/scripts.go` is the tool for doing
it without the window.

**Authorization already has an answer here — reuse it rather than invent
one.** `handleUnlock` (`internal/orchestrator/comment.go:142`) requires
`HasWritePermission` on the repository before releasing anything. A drop
from a web page must clear the same bar, which has a sharp consequence
for Slice 26's open "who may view" question: a signed, expiring URL is a
defensible answer for *reading* log output, and is **not** an acceptable
answer for a mutating control. Anyone holding the link could drop any
Lock. If this slice and Slice 26 share a UI surface, the identity
mechanism has to be chosen for this one. Whatever it turns out to be, its
endpoints belong under the `/github/` prefix Slice 29 establishes — an
OAuth callback at `/github/oauth/callback`, not loose at the root.

**Posting the comment needs a token that no webhook supplied.** Every
comment turnip writes today happens inside a webhook flow carrying an
installation ID. A drop initiated from a browser has no such event, so
the Server must resolve the installation for `owner/repo` itself before
it can authenticate the post. That is a small piece of new plumbing, but
it is new, and it is easy to miss when scoping this as "just a page".

**The webhook moves off the root path first.** Slice 29 does that on its
own, so neither UI slice has to carry the migration, and so this page can
be separated from the webhook at the network layer rather than sharing a
route with it.

**Open questions**:

- **Read-only first?** Listing Locks is useful on its own and carries none
  of the authorization weight of dropping one. Shipping the list before
  the button is a defensible split, and would let Slice 26 and this share
  a viewer without waiting on a mutation story.
- **What a stale Lock even means.** Slice 18 revisits holding a Lock when
  there is nothing to apply; if it lands first, some of what this page
  exists to clean up stops occurring.

---

### Slice 29: Move the Webhook Off the Root Path

**Goal**: Give the webhook an explicit path of its own, so that `/` stops
being a catch-all and so the webhook can be separated from everything else
turnip serves.

**Why now, specifically.** The path lives in each GitHub App's own
settings, so moving it is a manual change per installation. There is
exactly one installation today, which makes that change a single settings
edit. This cost only ever grows, and it grows silently: a missed update
means GitHub's deliveries 404 with nothing on turnip's side to notice —
no failed webhook, no error, just an automation that quietly stops
running. Spending it at one installation is the cheapest this will ever
be.

**The real reason is network separation, not tidiness.** The webhook has
to be publicly reachable, because GitHub delivers to it from the internet.
The surfaces Slices 26 and 28 add — live operation output, and a control
that drops Locks — should not be. While both live under `/`, an Ingress
cannot tell them apart: path rules are what Ingress controllers route on,
and method-based routing is something most of them do poorly or not at
all. Splitting the webhook onto its own path turns "expose the webhook
publicly, keep the UI internal" into an ordinary Ingress rule instead of a
controller-specific trick.

**A secondary win worth having anyway.** `mux.Handle("/", ...)`
(`cmd/server/main.go:85`) hands *every* unmatched path to the webhook
handler, which then rejects it for failing signature verification. A
typo'd probe or a stray scan surfaces as a webhook authentication error
rather than a plain 404, which is misleading in exactly the logs someone
reads when debugging a webhook.

**The scope is small and fully enumerable**, which is the other reason to
do it standalone:

| What | Where |
|---|---|
| The only mount point | `cmd/server/main.go:85` |
| Published webhook URL | `docs/configuration.md:452` |
| Deployment guide references | `docs/deployment.md:39`, `:165` |

Nothing else moves. No test asserts the route — `internal/github`'s
webhook tests construct the handler directly and never go through the mux
— and the deploy manifests reference only `/healthz` and `/readyz`, which
are probe paths and unaffected. A gitignored internal note also mentions
the URL and should be updated alongside.

`docs/configuration.md:452` needs rewriting rather than adjusting: it
currently states the root path emphatically, as "the root path (`/`), not
`/webhook` or any other sub-path", which is the sort of sentence a reader
trusts precisely because it anticipates the mistake.

**Decisions this slice makes**:

- **The path name is decided: `/github/webhook`.** Namespacing by forge
  rather than by resource (`/webhook`) costs nothing today and avoids a
  second published-URL migration if turnip ever accepts deliveries from
  GitLab, Gitea, or anything else. Forge-first also gives every *other*
  forge-specific endpoint a home rather than scattering them at the root:
  Slice 28 needs a GitHub identity to authorize a Lock drop, and an OAuth
  callback belongs at `/github/oauth/callback`.

  Worth being explicit about what this does and does not buy. The path is
  the cheap part of supporting a second forge; signature verification is
  not — GitLab authenticates deliveries with a secret-token header rather
  than GitHub's HMAC, so each forge needs its own verification path. This
  is a hedge against renaming a published URL later, not a claim that the
  rest of multi-forge support is designed for.
- **Whether `/` keeps accepting deliveries during a transition.**
  Recommendation: break cleanly. A compatibility shim's entire value is
  letting many installations migrate on their own schedule, and there are
  no many. A shim kept past its purpose becomes the code nobody is willing
  to delete because nobody can prove it is unused.
- **What `/` serves afterwards.** A 404 today; a UI index once Slice 26 or
  28 lands. Deciding now only commits to the first.

**This is an operator action as well as a code change.** Deploying it
without updating the GitHub App's Webhook URL stops deliveries, so the two
have to happen together — which is the whole argument for doing it while
"the operator" is one person with one App.

**Ordering**: before Slices 26 and 28. Both want a route on this server,
and doing this first means neither of them has to carry a migration
alongside its actual feature.

---

### Slice 30: Scheduling — Concurrency and Execution Order

**Goal**: Let a repository control how its Operations are scheduled — how
many run at once, and which must finish before others start. turnip offers
neither today: `executeTargets` starts a goroutine per Target and waits
for all of them (`internal/orchestrator/execute.go:30-48`), with no cap
and no ordering.

**The motivating scale is Terraform workspaces**, and it is worth being
precise about why, because the obvious worry is the wrong one.

A workspace is already expressible: `with.workspace` is a documented
Terraform setting (`docs/configuration.md:165`) carried in `Project.With`
(`internal/config/config.go:61`). So N workspaces are N Projects. Each has
its own `name`, therefore its own `projectKey`, therefore its own Lock;
and each Runner is a separate Pod with a separate clone. **Nothing
contends.** Two workspaces of one configuration hold independent state and
are safe to run together.

What fans out is *cost*. A repository with a dozen environments turns one
comment into a dozen simultaneous Kubernetes Jobs, each pulling providers
and talking to an API with its own rate limits. The reason to serialise is
cluster capacity and provider throttling, not correctness — which also
means the control belongs where capacity is known.

**Strand 1 — a concurrency cap, not a boolean.** Atlantis spells this
`parallel_plan` / `parallel_apply`. A maximum-concurrent-Operations
integer is strictly more expressive and subsumes both, since 1 is
serialisation. Where it is configured is a real decision: cluster capacity
is the operator's knowledge (a Server-side setting), while which Projects
may safely overlap is the repository's (turnip.yaml). Most likely both,
with the operator's value acting as a ceiling the repository cannot raise.

*Where the cap is **enforced** is a separate decision from where it is
configured, and the two obvious answers are not equivalent.* turnip can
hold back Job creation itself (a semaphore in `executeTargets`), or create
every Job and let Kubernetes admit them gradually through a ResourceQuota
or a queueing controller.

**The second breaks the timeout sweep, and it is worth spelling out
because it would be discovered the hard way.** `StartDeadline` is stamped
when the Operation Record is created — `time.Now().Add(o.startTimeout)` at
`internal/orchestrator/execute.go:204` — which is *before* the Job exists.
The clock therefore starts when turnip decides to run something, not when
the cluster admits it. `sweepOnce`
(`internal/orchestrator/sweep.go:32`) claims every Record past that
deadline which never reported a start, and `reportTimeout` completes its
check run as **failed**, posts a failure result, and deliberately does not
release the Lock (`internal/orchestrator/sweep.go:89-92`).

So a Job queued behind a Kubernetes-side cap for longer than the start
timeout is reported as a failure, strands its Lock, and may then still run
— because nothing cancels it. `timeoutDiagnostic` already renders exactly
this shape ("Pod still %s after 5 minutes",
`internal/orchestrator/sweep.go:112`), which shows the condition is
recognisable; it is simply classified as failure today, correctly, because
today nothing queues Jobs on purpose.

That makes an in-process semaphore the cheaper enforcement point. Choosing
Kubernetes admission instead is defensible, but it requires making
`StartDeadline` relative to admission rather than creation, which is a
change to the sweep's contract and not a configuration detail.

*One caveat on scope, whichever is chosen.* A semaphore in `executeTargets`
caps concurrency **per replica, per event**: the Server is stateless and
horizontally scaled, so two comments landing on two replicas each get
their own budget. A genuinely global cap needs the count to live in Redis
— which is the same state question Strand 3 raises, and the reason these
two strands are one slice rather than two.

**Strand 2 — execution order groups.** Lower-numbered groups run to
completion before higher ones; Projects within a group still run together.

The evidence for prioritising the integer form: a survey of a real
multi-repository Atlantis deployment found ordering declared on **353 of
387 projects** — near-universal, in every case to make one foundational
project finish before its dependants start. That same deployment used
`depends_on` nowhere, and used the `workflows` concept (which turnip
deliberately omits) for nothing at all. Ordering is the larger gap, and
the integer form is both simpler to schedule and evidently sufficient.

This was deliberately kept out of `project-schema-v1alpha2`: the schema
half is trivial and the behaviour is not, and adding the key first would
ship a field that parses and does nothing — the failure this platform has
already been bitten by twice.

**Strand 3 — the hard part, which is neither of the above.** Both strands
lengthen the life of the detached goroutine `HandleIssueComment` spawns.
Eight Projects run in parallel take as long as the slowest; serialised
they take the sum. The Server is stateless by construction, so a replica
that restarts mid-run already loses the run and lets the sweep time its
Operations out — ordering introduces no new *class* of failure, but it
multiplies the window, and an ordered run interrupted halfway leaves
earlier groups applied and later groups not.

That is a deliberate decision to take, not a detail to discover: either
accept it and say so plainly in the documentation, or move scheduling
state into Redis where a surviving replica could resume it. The second is
a much larger slice than the first.

**This amends global Requirement 17.1**, which currently reads that the
Server "SHALL execute Operations for all Projects in parallel" — written
when parallel was the only scheduling turnip had. Rewritten in place per
the convention that the global spec carries no amendment markers, with the
amendment recorded here.

**Open questions**:

- **What an ordered group does when an earlier Project fails.** Atlantis
  makes this configurable (`abort_on_execution_order_fail`); a default
  that continues into dependants after a foundational failure is hard to
  defend, but aborting silently strands work too.
- **Whether a cap applies per repository or per Server.** A per-repository
  cap does not protect a cluster from ten repositories each staying under
  their own limit.

---

### Slice 31: Runner Pod Placement — Node Selectors and Tolerations

**Goal**: Let an operator place Runner Pods on a node pool chosen for the
job — larger nodes for a heavy state refresh, a pool whose egress
addresses a provider allowlists, or on-demand capacity where the default
pool is spot. A selector steers the Pod; a toleration admits it to a pool
reserved behind a taint.

**What is missing**: `RunnerSpec` carries `serviceAccount` and `env` and
nothing else (`internal/config/config.go:79-91`), and `internal/jobs` sets
no scheduling field anywhere. The Backlog's Azure Workload Identity entry
needs the same pod template opened for *metadata*; this slice opens it for
*scheduling*, and whichever lands first pays for the seam.

**Decision 1 — Server configuration, not `turnip.yaml`, to begin with.**
turnip.yaml is read from the pull request's own head commit, so anything
settable there is settable by whoever opens the pull request. Placement is
exactly the class of setting where that matters, so the first form is
operator-controlled, matching the precedent the Azure entry sets for pod
labels.

**Decision 2 — the two fields do not classify alike, and the slice records
why even while both are operator-only.** The rule is the one
`runner.serviceAccount` established: gate what grants capability, leave
alone what does not.

| Field | Grants capability? |
|---|---|
| `tolerations` | **Yes.** A toleration is precisely what admits a Pod to a tainted pool an operator reserved for something else |
| `nodeSelector` | **Usually not.** Labels are not access control; without a matching toleration a selector can only make scheduling *fail*, not reach anywhere new |
| `nodeSelector`, on a cluster granting identity per node pool | **Yes.** With EKS node roles or GKE node service accounts, landing on a pool *is* a credential grant |

That last row is why this is written down now rather than when someone
asks for a per-Project override: the answer depends on a property of the
cluster, not of turnip, so the gate cannot be decided once and forgotten.

**Decision 3 — an unschedulable Pod must not be discovered by timeout.**
A selector matching no node leaves the Pod Pending; after `o.startTimeout`
the sweep claims it, completes the check run as failed, and deliberately
leaves its Lock held (`internal/orchestrator/sweep.go:89-92`). So today a
mistyped label costs five minutes and strands a Lock, and the pull request
is told only "Pod still Pending after 5 minutes".

That is survivable but poor, and this slice is what makes it *likely* —
before it, nothing turnip wrote could render a Pod unschedulable.
Detecting `PodScheduled=False` with reason `Unschedulable` and failing
fast with the scheduler's own message is the obvious answer; it is real
work, and belongs here rather than being left for whoever hits it.

**Encoding**: tolerations are structured (`key`, `operator`, `value`,
`effect`, `tolerationSeconds`), so JSON is the natural form and matches
what the Azure entry reasons its way to for annotations. A node selector
is a flat label map where values cannot contain commas, so `k=v,k=v` would
also work — but one encoding for both is worth more than saving a few
characters on the simpler field.

**Out of scope**:

- **Affinity** — Backlog, by scope decision. Node, pod and anti-pod
  affinity with required and preferred forms is a large permanent surface,
  and a selector plus tolerations covers the motivating case.
- **Per-Project overrides.** Decision 2 records the gating rule this would
  need; applying it is a later slice, and interacts with the Backlog's
  top-level `runner:` block, where list-valued fields raise a merge
  question (append or replace) that neither `serviceAccount` nor `env`
  answers.
- **Pod labels and annotations** — the Azure Workload Identity entry.

---

### Slice 32: Refuse a Closed Pull Request, Plan a Reopened One

**A bug, not a feature.** Commenting `/helmfile diff` on a closed pull
request runs it — plans, locks, creates a Job, executes. Nothing stops
it, because turnip has no concept of a pull request being open or closed.

**What's missing**: `github.PullRequest` carries `Number`, `HeadSHA`,
`BaseRef`, `HeadRef` and `Draft`, and `GetPullRequest` maps only those.
GitHub's payload offers `state`, `merged`, `merged_at` and `closed_at`;
turnip reads none of them. `HandleIssueComment` has no state guard at any
point — it authorizes the commenter, fetches the configuration at the head
SHA, resolves Targets and executes.

**Consequence 1 — a Lock nothing will ever release.** Lock release on
close happens in `handlePRClosed`, and that event has already fired by the
time someone comments on a closed pull request. There is no second close
event. So a plan taken afterwards acquires a Lock with **no remaining
lifecycle event to discharge it**: successful apply and pull-request close
are the only other releases, and both are out of reach. It blocks every
other pull request from planning that Project until a human runs
`/turnip unlock`.

This is adjacent to, but not covered by, Slice 15's Requirement 2.3. That
one keeps the `closed` path working so Locks are not stranded *at* close;
this strands one *after* close, which nothing guards.

**Consequence 2 — applying changes that were rejected.** `/turnip apply`
alone is refused, because close released the Lock and an apply needs one
carrying a plan. But `diff` then `apply` re-acquires, re-plans and
applies. The Runner merges base into head, so on a pull request closed
**without merging** that deploys precisely the changes someone declined to
merge. On a merged pull request it is close to inert, since head is
already in base — the rejected case is the dangerous one.

**Scope is the comment path only.** `HandlePullRequest` autoplans on
`opened`, `synchronize` and `ready_for_review`, and routes `closed` to
Lock release; every other action falls to `default: return nil`. So the
automatic path cannot reach a closed pull request, and the whole gap is in
`HandleIssueComment`.

**Deliberately different from drafts.** Slice 19 decided a draft "changes
when turnip acts on its own, never what it can be asked to do" — a draft
is unfinished work whose author may legitimately want a plan. A closed
pull request is finished or abandoned, and the cleanup that follows it has
already run. The reasoning that makes a draft's comment trigger valid is
exactly what makes a closed one's invalid.

**Delivers**: pull-request state on `PullRequest`, mapped from both the
webhook payload and `GetPullRequest`, and a refusal of every Operation —
plan and mutating alike — on a pull request that is not open. No
distinction between merged and closed-unmerged: both are closed, and the
Lock-lifecycle argument applies to each.

**Sequencing with Slice 15.** Both slices add a mapped field to
`PullRequest` from the same two payloads (`head.repo` there, `state`
here), so whichever lands second is materially cheaper — the mapping in
`GetPullRequest` and in `pullRequestWebhookEvent` is touched once either
way. Worth doing adjacently.

**Open question**: silent or commented. Slice 15 chose silence because the
requester may be an attacker. Here the requester is almost certainly a
colleague who commented on the wrong tab, and there is no attack to starve
of feedback — so a reply naming the reason is probably right. Worth
deciding explicitly rather than inheriting Slice 15's answer.

**`reopened` is in scope, after all.** It is handled nowhere
(`default: return nil`), so reopening a pull request triggers no plan
until someone pushes. Initially deferred as "a behaviour addition inside a
bug fix", then pulled in: the slice's subject is a pull request's *state*,
and refusing a closed one while ignoring a reopened one covers half of it.
A reopened pull request plans the Projects its changes match, exactly as
an opened one does — which is what adding the action to the existing arm
produces, draft guard included.

**A note on what Slice 18 changed here.** The stranded-Lock consequence
above is narrowed but not closed: a failed plan now releases its Lock, and
a plan finding nothing releases for a tool that is inert without changes.
A *successful* plan still holds, and Helmfile is never inert because
`sync` acts regardless of the diff — so the ordinary case still strands a
Lock that only `/turnip unlock` can clear.

---

### Slice 33: Show What Ran and With What Scope

**Goal**: Make the pull request say what turnip executed, and with what
scope, so that neither has to be reconstructed from configuration at that
commit.

**What's missing today, and they are two separate silences.**

The first is scope. Slice 20 made a plan record its arguments
(`lock.PlanRecord.Args`) and every Mutating_Operation replay them, refusing
arguments of its own. The scope is therefore durable state on the Lock,
outliving the comment that produced it. But the summary line renders
`<project> · diff · +0 ~1 -0` whether the plan examined one release or the
whole Project, and the footer offers a bare `/turnip apply` beneath "holds
locks on `<project>` until applied or released". That text is stale rather
than unsafe — Slice 17 wrote it before Slice 20 existed, and Slice 20
changed what "until applied" means without revisiting it.

The second is the command. `helmfile.go:39` injects `--environment <env>`
from the Project's `config`; the author never typed it and the comment
never shows it. The output block opens on the tool's first line of stdout,
with no record of which binary ran, at which version, in which directory.

**Delivers**: a per-Project scope marker on the summary line, a footer that
describes a scoped apply accurately, and an Execution_Transcript written
into the Operation's own output — provenance, the resolved command, and an
exit-and-duration trailer — emitted from the shared command-execution seam
at the moment each command runs.

**Emitted at the seam, not per Plugin, and not server-side.** Putting it in
`execCommand` means a Plugin cannot forget it, and a Plugin added later is
covered by existing code. Emitting into the output stream rather than
carrying the command back in `OperationResult` means no proto change, and
means a transcript stays correct for a Plugin that runs several commands —
Helmfile runs one, Terraform will run `init` then `plan`.

**Why this is a slice and not an amendment.** Slice 20 named this hazard in
its own requirements and then removed it by prevention rather than display,
deferring nothing; the display gap is not a defect it left behind. And the
work needs a new field on `ProjectResult` plus plumbing from the Operation
record to the renderer, spans two completed slices' files, and carries real
decisions of its own — where every existing amendment in this repo is small
and mechanical.

**Closes an exposure that predates it.** `Output` is interpolated into a
code fence with no escaping (`comment.go:407`). Content that terminates the
fence early renders as markdown inside a comment authored by turnip. Adding
a line built from trigger-supplied tokens widens that, so the slice hardens
both.

**Adjacent Backlog entries.** "Resolving a floating tool version" asks
where a concrete version would come from once floating tags are allowed;
this slice builds the surface that would report it, and is satisfiable
today because `resolveVersion` always returns a concrete version. The
entry on a Project directory escaping the workspace shares this slice's
path-stripping helper but is a validation change, not a display one.

## Backlog (not yet sliced)

Recorded so they aren't rediscovered the hard way. None of these has a
spec directory, and none is scheduled.

### Matching Projects by directory rather than name

Slice 21 globs over Project *names*, which covers a repository that names
its Projects for their paths — `gcp/project`, `aws/project` — with no
second addressing mechanism and no code beyond the pattern matching it
already performs. That is why this is a note rather than a slice: naming a
Project for its directory is the cheaper answer and needs nothing built.

It remains a real gap for a repository that names Projects something other
than their directory. `Project.Directory` is not addressable at all, so
`env/gcp/*` selects nothing however the files are laid out.

Two findings worth keeping if it is ever picked up:

- **Atlantis' `-d` spelling cannot be copied.** `indexOfArgStart`
  (`internal/github/parser.go:101`) stops Project names at the first
  `-`-prefixed token and hands the rest to the tool — a rule chosen in
  Slice 20 and documented at `docs/usage.md:68` — so `-d env/gcp/*` would
  reach the tool as arguments and select nothing. A positional,
  path-shaped selector needs no parser change at all, which is the shape
  Slice 21's patterns already use.
- **Names and directories overlap rather than partition.** A Project's
  name defaults to its directory (`internal/config/parse.go:129`), so one
  token can be both a valid name and a valid path pattern. Any
  directory-matching feature has to state precedence, and "exact name
  first" carries a trap: adding a Project can then silently change what an
  existing pattern selects.

### Contain `Project.directory` to the workspace

`internal/config/validate.go` checks only that `directory` is non-empty.
The value becomes the tool subprocess's working directory through
`filepath.Join(dir, cfg.ProjectDir)` in `internal/runner/run.go`, and
`filepath.Join` *cleans* `..` segments without *containing* them — joining
the workspace root with `../../etc` yields `/etc`. Nothing in the
repository applies `filepath.IsLocal` or any equivalent check.

**Deliberately not a security slice.** The 2026-09-19 review examined this
and rejected it as a vulnerability, correctly: `turnip.yaml` is read from
the pull request's own head commit, so anyone who can set `directory` can
already run code in the Runner pod by other means, and the escape on its
own yields "no state file found" rather than any file's contents. It grants
an attacker nothing they do not already have.

**Worth doing as robustness anyway**, for two reasons that have nothing to
do with attackers. A Project pointing outside the repository fails with a
confusing tool error naming a path nobody wrote, instead of a clear
configuration error on the pull request. And `stripWorkspacePath` stops
matching once the working directory leaves the workspace, so absolute
pod-internal paths appear in the comment where repository-relative ones
should.

Shape: reject `filepath.IsAbs`, and anything where
`!filepath.IsLocal(filepath.Clean(p.Directory))`, during validation — so
the failure arrives when the file is written rather than when a Job runs.
A two-line check plus tests, restoring the invariant that a Project
addresses something inside its own repository.

**Slice 22 needs the identical invariant** for its `with.file` key, jailed
to the workspace rather than to the Project directory. Whichever lands
first should expose the helper the other uses — two independent
containment checks is how they drift apart.

### Running a tool build turnip does not ship

`internal/jobs/versions.go` hardcodes one image template per tool —
helmfile's is `ghcr.io/helmfile/helmfile:v%s` — and `uses: <tool>@<version>`
substitutes only the version into it. There is no per-Project image field
in the schema, and the Server's only image setting,
`TURNIP_RUNNER_IMAGE`, names turnip's own Runner image rather than any
tool's. So a build that is not one of the vendor's published releases is
not expressible anywhere in turnip today.

The motivating case is real and was hit in practice: needing a fix that is
merged upstream but not yet released, during a period when the vendor's
releases were infrequent. The only workaround is to wait for the vendor's
cadence — which is the bottleneck the per-tool vendor-image design was
adopted to remove, reappearing one layer down.

**Distinct from the `--helm-binary` gate in Slice 22**, and the two are
easy to confuse. That flag selects the *helm* binary helmfile shells out
to; it cannot change which *helmfile* runs. A forked helmfile is an image
question, and no argument policy reaches it.

**Gating is not optional here, and an allowed-overrides entry is not
enough.** An arbitrary image is arbitrary code running with the Runner's
identity — the plainest capability grant turnip could offer. Note why this
is stronger than `runner.serviceAccount`'s gate: permitting that field
lets a repository choose among the ServiceAccounts that *happen to exist
in one namespace*, which is bounded by what an operator already created.
Permitting a free-form image string is unbounded — anything published
anywhere. So the shape should be a **Server-side allowlist of permitted
images**, with a repository at most selecting from it, rather than a
boolean that turns "any image" on.

**It also needs an explicit security disclosure, and that is a
requirement of the feature, not documentation polish.** The warning has to
say plainly that enabling this runs code turnip did not build and cannot
vouch for, with the Runner's cloud and cluster credentials, and that doing
so is the operator's decision and the operator's risk. Default off,
refused unless deliberately enabled, and documented as designed-to-be-
dangerous.

The point of writing it down is not ceremony. A feature that grants code
execution *by design* attracts vulnerability reports unless the project
has already said, in public and in advance, that this is the intended
behaviour and where the line sits. That requires somewhere to say it —
which turnip does not have yet. See the Backlog entry below on a stated
security model; it should land **before** this feature, not with it.

Adjacent to this Backlog's floating-tool-version entry, which asks which
*version* resolves rather than which image supplies it.

### A stated security model, so opt-in danger is not reported as a vulnerability

turnip has **no `SECURITY.md`** — not at the root, not under `.github/`,
not in `docs/`. The only security prose in the repository is two
paragraphs in `docs/configuration.md` about helmfile `prepare` hooks
running during a plan.

The gating convention itself is in good shape: both
`TURNIP_ALLOWED_OVERRIDES` paths are documented, each explains *why* its
gate exists, and the default permits nothing. What is missing is the other
half — nothing anywhere says what it means for an operator to **open** one
of those gates. `runner.serviceAccount`'s documentation explains why the
gate is there; it never says that turning it on is a deliberate
acceptance of risk rather than a supported-and-safe configuration.

**This is needed already, not when some future feature lands.**
`runner.serviceAccount` ships today and can be enabled today. Slice 22
adds `--helm-binary` on the same pattern, the custom-image entry above
adds a third, and Slice 15 leaves open whether an operator may ever opt
into fork pull requests. That is a category, not a series of one-offs.

What it should contain:

- **The threat model in one paragraph**: `turnip.yaml` and the IaC code
  are read from the pull request's own head commit, and the tool executes
  in a Pod holding cloud and cluster credentials. Anyone who can get code
  into a pull request that turnip plans can run it.
- **The list of opt-ins that grant code execution by design**, each named,
  each default-off, each with what enabling it costs.
- **What turnip does consider a vulnerability** — a gate that fails open,
  a default that grants more than documented, a leak reachable with no
  opt-in at all — so a reporter can tell the difference without having to
  re-derive the reasoning.

**Prior art**: Atlantis documents `--allow-fork-prs` with an explicit
SECURITY WARNING saying that enabling it lets anyone who can open a pull
request cause arbitrary code to run, and keeps a security page framing the
whole threat model. That is the shape — the warning is only load-bearing
because there is a stated model behind it.

Worth doing before turnip has users rather than after the first report
arrives, and it is mostly writing rather than code.

### Azure Workload Identity needs Runner *pod* labels

turnip selects the Runner pod's ServiceAccount
(`TURNIP_RUNNER_SERVICE_ACCOUNT`, optionally overridden per Project), and
for AWS EKS Pod Identity and GCP Workload Identity that is the whole job:
both attach the identity association to the ServiceAccount object itself,
which an operator creates and turnip only references by name. GCP even
allows the target service account to live in another project, so no
second step is needed there.

Azure differs in one respect: its mutating admission webhook injects
nothing at all unless the **pod** carries the label
`azure.workload.identity/use: "true"`. Identity selection itself stays on
the ServiceAccount (the `azure.workload.identity/client-id` annotation,
which explicitly supports one ServiceAccount referencing many
identities), so that part needs nothing from turnip.

The gap is that `internal/jobs.BuildJob` sets labels on the *Job's*
ObjectMeta and gives the pod template no ObjectMeta at all — so turnip
sets no pod labels or annotations whatsoever today.

Likely shape: an operator-controlled passthrough (Server configuration,
not `turnip.yaml`, since nothing here should be PR-editable) applying
labels and annotations to the pod template. JSON-encoded rather than
comma-separated: label values can't contain commas, but annotation values
are arbitrary strings and can — Azure's own
`azure.workload.identity/skip-containers` is semicolon-separated. A
reserved prefix should stop an operator silently overwriting the keys
turnip uses for correlation.

### Runner Pod affinity

Slice 31 adds `nodeSelector` and `tolerations`, which together cover the
dedicated node-pool case: a selector steers, a toleration admits. Affinity
is deliberately left out of it.

`affinity` is node affinity, pod affinity and pod anti-affinity, each with
a required and a preferred form. Supporting it wholesale means embedding a
sizeable piece of the Kubernetes API in turnip's own configuration surface
and tracking it as that API moves — a large, permanent cost for capability
nothing has yet asked for.

Worth picking up when something needs what a selector cannot express:
spreading Runners across zones, or keeping two Runners off one node.
(Topology spread constraints may be the better answer to the first of
those, and are a much smaller surface — worth comparing before assuming
affinity is the tool.)

The gating question Slice 31 settles applies here unchanged: a node
affinity term is capability-granting in exactly the circumstance a node
selector is, which is a cluster that grants identity per node pool.

### `whenModified` cannot see through a symlink

Matching is textual, and it happens before any clone exists. The Server
asks GitHub which files a pull request changed and glob-matches those path
strings (`projectMatches`, `internal/config/match.go`); nothing in
`internal/config` touches a filesystem or a repository tree.

git makes that consequential twice over. A symlink is a blob of mode
120000 whose content is its target path, so editing the target never
modifies the link and the link's own path never appears in the diff. And
git stores a symlinked directory as a *single* entry rather than expanding
it. So where `env/prod/modules` links to `shared/modules`, editing
`shared/modules/main.tf` produces exactly one changed path —
`shared/modules/main.tf` — and a Project whose patterns cover only
`env/prod/**` matches nothing and never replans.

The Runner clones with real `git` (`internal/runner/clone.go`), so the
link resolves normally at execution. The gap is entirely in *selection*:
by the time a filesystem exists, the decision not to run has been made.

**The workaround costs one line and should be documented rather than
engineered around** — name both paths:

```yaml
whenModified:
  - "env/prod/**"
  - "shared/modules/**"
```

**The asymmetry that makes this a trap rather than an inconvenience.** A
symlink is itself a tracked entry, so *adding or removing* one changes a
path inside the linking directory and does trigger that Project. Only
*edits to the link's target* go unnoticed. A layout of this shape
therefore works on the day it is built and silently stops noticing a
shared component the first time someone edits it — for exactly those
Projects whose patterns nobody remembered to extend. It fails by omission,
producing no error and no output to read.

**The shape this arises in is not exotic.** One Project per environment,
each environment directory linking several shared component directories,
is a natural way to stop a pull request planning every environment at
once — which is the problem `whenModified` exists to solve. It needs one
pattern per linked component per Project, and the count grows with
environments times components.

**What resolving it automatically would take.** turnip would fetch the
repository tree at the head SHA, find every mode-120000 entry, read each
target blob, and then answer the *reverse* question: is this changed path
reachable through a link covered by some Project's patterns? That is a
recursive tree call per event — cacheable per head SHA — plus resolution
logic that has to handle links to links, links pointing outside the
repository, and links whose targets do not exist.

**The cost this removes is boilerplate, and that is the stronger argument
for it.** Adding one shared component to one environment means three
edits: create the link, register it with the tool, and extend the
Project's patterns. The first two fail loudly — a missing link or an
unregistered file breaks on the next run. The third fails silently. So
turnip owns exactly one third of the boilerplate and it is the third that
produces no error when forgotten, which is a poor split to leave in place.

*A warn-only variant is not the cheap escape it first appears.* To report
that a linked directory is covered by no pattern, turnip must still find
the link and read its target — the same tree walk and the same blob
reads. What it buys is not a smaller mechanism but a weaker obligation: a
warning may be best-effort and skipped when an API call fails, whereas
resolution that silently changes which Projects run has to be reliable, or
it trades one invisible failure for another.

*What is genuinely cheap, and available now, is generating the
configuration.* A repository whose Projects are derived by walking its own
tree — emitting each Project's patterns from the links actually present —
makes the third edit a build artefact rather than a thing to remember.
That is the established answer in this ecosystem (the Terragrunt and
Atlantis world generates its configuration for the same reason), it needs
nothing from turnip, and it should be the recommendation until resolution
exists.

Where a tool-native mechanism exists it sidesteps the question entirely:
Terraform's `source = "../../shared/modules"` is the idiomatic answer for
modules, though it has no counterpart for a helmfile that simply lives in
a shared directory.

### Expose the workspace path as a variable rather than a literal

Slice 12 fixes the Runner's workspace at `/turnip/src`, which repositories
may reference from committed configuration. That is fine while turnip has
no releases and one consumer, and relocating the path is a release-gated
change.

It stops being fine with several consumers: a version bump reaches the
operator who changes the image tag, but the breakage lands in other
teams' repositories, which didn't choose the upgrade. The established fix
is to make a variable the interface rather than the literal path —
GitHub documents `GITHUB_WORKSPACE`, GitLab documents `CI_PROJECT_DIR`,
both precisely so the path underneath can move.

The obstacle is that turnip launches the tool without a shell, so a value
like `${TURNIP_WORKSPACE_DIR}/...` arrives literally; supporting it means
expanding a placeholder when applying a Project's `env`. Small, but only
worth building when a second repository onboards or the path needs to
move. Woodpecker shows the cost of leaving it too long: having made the
workspace configurable after plugins had already hardcoded it, it now
carries a permanent exception — "Plugins will always have the workspace
base at `/woodpecker`".

### Resolving a floating tool version

`uses: terraform@latest` is rejected, because a floating tag breaks the
guarantee locking exists to provide: plan on Monday against 1.9.5, apply
on Thursday against 1.10.0, and the binary that runs is not the one that
produced the approved plan. Locking cannot help, because the drift is in
the tool rather than in the plan.

Two mechanisms together make it viable, and neither requires turnip to
talk to a registry — which is what had made this look expensive. Set
`imagePullPolicy: Always`, but only where the tag is actually floating, so
pinned versions keep running from the node's cache rather than paying a
manifest fetch on every Job; the kubelet then does the resolving. Then
record the version that actually ran in the Redis Lock alongside the plan
data, and build the apply Job from that concrete version rather than
resolving `latest` a second time.

The open question is how turnip learns which version ran. The cheapest
answer is the tool-provisioning initContainer writing `<tool> version`
into the shared tools volume it already populates — it is running the
vendor image, and the Runner already mounts that volume.

A related but distinct idea, worth weighing against it rather than
alongside: derive the version from the IaC code's own constraint, as
Atlantis does by reading Terraform's `required_version`. That keeps the
code as the source of truth and resolves deterministically at plan time,
but only one of the three tools has such a constraint to read. Note that
`required_version` constrains rather than selects, so it cannot be
combined with `latest` — it would turn a drifting version into a hard
failure rather than a correct choice.

Worth doing when a consumer asks to stop pinning, not before: the
concrete-version path has to keep working either way.

### Per-repository server-side configuration

turnip's Server configuration is global: one value applies to every
repository it serves. Several settings would be better expressed per
repository — whether to fetch submodules, whether fork pull requests are
ever permitted, which overrides a given repository may take — and each
time one appears, the choice is between a global default that is wrong for
someone and a repository-side setting that the repository's own authors
control.

Atlantis solves this with a server-side `repos.yaml` keyed by repository
id (a literal or a regex), carrying per-repository `allowed_overrides`,
`allow_custom_workflows`, and workflow selection. That is the shape to
copy if this is picked up.

Deliberately not built for any single setting: introducing a whole
configuration surface to express one tri-state value is disproportionate,
and each setting so far has had a defensible global default plus a
repository-level override. Worth revisiting when a third or fourth
setting genuinely needs per-repository policy, or when an operator serves
repositories they do not own.

A configuration UI is further out still and is not implied by this:
turnip has no UI, no settings persistence, and no authentication for one.

### A top-level `runner:` block, merged into each Project's

`TURNIP_RUNNER_SERVICE_ACCOUNT` carries a Server-wide default today, and a
Project overrides it with `runner.serviceAccount`, gated through
`TURNIP_ALLOWED_OVERRIDES` (Slice 13). `runner.env` is per-Project only,
and deliberately ungated. What is missing in both cases is the layer
between the Server and the Project: a repository stating its shared Runner
configuration once, with each Project stating only its delta.

Slice 16 introduces the first repository-scoped block, `clone:`, so the
shape to copy already exists — a top-level `runner:` beside it.

**Merging is field-dependent, and that is the design work.**
`serviceAccount` is a scalar: a Project that sets one wins outright, and
the repository's value is a default beneath it — three layers, with the
Server's at the bottom. `env` is a map and must merge *per key*, because a
Project setting one variable should not silently drop every shared one,
which is exactly what replacing the map wholesale would do.

**The gating asymmetry is inherited, not redesigned.**
`runner.serviceAccount` is gated because it grants capability: it picks an
identity, and turnip.yaml is read from the pull request's own head commit,
so whoever opens the pull request chooses its contents. `runner.env`
grants nothing the repository does not already have, which is why it is
ungated. A repository-level block changes neither judgment, but it does
mean the gate has to apply per field rather than to the block as a whole.

**It weakens the case for workspaces and workflows further.** The survey
behind turnip's decision not to grow a workflows concept found Atlantis
workflows are overwhelmingly argument carriers — repositories reach for
them, and for additional `workspace` entries, largely to avoid repeating
boilerplate across near-identical projects. Letting shared configuration
be stated once removes that pressure at its source. One boundary worth
keeping explicit, though: this addresses the *configuration-repetition*
reason for declaring many projects, not Terraform workspaces as a
state-separation mechanism — a different thing wearing the same name, and
nothing here replaces it.

If it is picked up, Slice 16's Decision 8 is the seam: `Target` already
has to carry repository-scoped configuration to the execution path, and
this would be the second such block after `clone:` — which settles whether
that field should be named for one block or carry the repository's
configuration generally.

### Authenticating to a cluster turnip is not running in

The Runner authenticates to Kubernetes with its Pod's ServiceAccount —
in-cluster configuration, which is exactly right when the IaC targets the
cluster the Runner runs in, and useless otherwise. Nothing today produces
a kubeconfig for anywhere else.

For an external cluster something has to mint credentials *before* the
tool runs. On EKS that is `aws eks update-kubeconfig`, which needs the AWS
CLI present in the container and a Pod identity permitted to call it
(Pod Identity/IRSA already selects that identity through
`runner.serviceAccount`).

Two shapes, neither chosen:

- **A general pre-command**, the Atlantis-shaped option and the one first
  suggested. Flexible, and it covers providers turnip has never heard of.
  But it is arbitrary code read from the pull request's own head commit,
  so it needs the same gating argument `runner.serviceAccount` has — and
  by the principle that gates belong on what *grants* capability, running
  arbitrary commands plainly grants it.
- **A declarative kubeconfig step** turnip performs itself from named
  inputs (cluster, region, role). No arbitrary execution and nothing new
  to gate, but it only ever covers the providers turnip teaches itself.

**It collides with Slice 14.** Under run-in-image the container running
the tool is the vendor's own image, which for helmfile is Alpine and does
not carry the AWS CLI. Any design here that assumes it can install
packages into that image is assuming something turnip deliberately gave
up when it stopped building tool images.

Not urgent while the pilot targets the cluster turnip itself runs in,
which is also the case that needs no credentials at all.

## Notes

- Slices 1–5 can be developed in parallel once Slice 0 is complete
- Each slice should be merged to `main` before starting dependent slices
- The global spec remains the source of truth for cross-cutting concerns
- Individual slice specs may refine or add detail beyond the global spec
