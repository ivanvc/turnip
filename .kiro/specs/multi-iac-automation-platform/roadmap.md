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
| 23 | Authorize on Permission Level, Not Call Success | `collaborator-authorization` | Complete | Slice 4 |
| 24 | How the Runner Receives Its GitHub Token | `runner-token-delivery` | Complete | Slices 5, 6, 25 |
| 25 | Authenticating the Runner to the Server | `runner-authentication` | Complete | Slices 5, 6 |
| 26 | Real-Time Operation Output | `operation-output-stream` | Not Started | Slices 5, 6 |
| 27 | Authenticated and Encrypted Redis | `redis-tls-auth` | Not Started | Slices 3, 10 |
| 28 | Seeing and Dropping Locks Without Hunting for the PR | `lock-admin-ui` | Not Started | Slices 3, 4 |
| 29 | Move the Webhook Off the Root Path | `webhook-path` | Complete | Slices 4, 10 |
| 30 | Scheduling: Concurrency and Execution Order | `operation-scheduling` | Not Started | Slices 6, 7 |
| 31 | Runner Pod Placement: Node Selectors and Tolerations | `runner-pod-placement` | Not Started | Slices 5, 13 |
| 32 | Refuse a Closed Pull Request, Plan a Reopened One | `closed-pull-requests` | Complete | Slices 4, 6 |
| 33 | Show What Ran and With What Scope | `execution-provenance` | Complete | Slices 2, 17, 20 |
| 34 | What a Pull Request Must Satisfy Before an Apply | `apply-requirements` | Not Started | Slices 4, 6, 20 |
| 35 | What the Checks List Says | `check-run-titles` | Not Started | Slices 6, 33, 37 |
| 36 | A Blocked Operation Blocks the Merge, Visibly | `check-run-refusals` | Not Started | Slices 6, 32, 35, 37 |
| 37 | One Check Branch Protection Can Require | `aggregate-check-run` | Complete | Slices 6, 17 |
| 38 | Encrypting the Runner-to-Server Channel | `runner-server-tls` | Not Started | Slice 25 |
| 39 | Runner Pod Resources | `runner-resources` | Not Started | Slices 5, 13 |
| 40 | Bounding How Long a Runner Runs | `runner-timeouts` | Not Started | Slices 5, 6 |
| 41 | Runner Pods Run Without Disruption | `runner-disruption` | Not Started | Slices 5, 39, 40 |
| 42 | The Workspace on a Per-Runner Volume | `runner-workspace-volume` | Not Started | Slices 12, 39 |

## Slice Details

### Slice 0: Project Scaffolding (`project-scaffolding`)

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

### Slice 1: Config Parsing & Project Matching (`config-parsing`)

**Goal**: Parse `turnip.yaml` and determine which projects should trigger based on file changes.

**Delivers**:
- YAML schema definition and parser
- Project configuration validation
- Glob pattern matching for `whenModified` rules
- Property tests for round-trip parsing and pattern matching

**Global requirements covered**: 1, 2, 18

---

### Slice 2: Plugin System & Helmfile Plugin (`plugin-helmfile`)

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

### Slice 3: Redis Lock Manager (`redis-lock-manager`)

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

### Slice 4: GitHub Client & Webhook Handler (`github-integration`)

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

### Slice 5: gRPC & Runner (`grpc-runner`)

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

### Slice 6: Server Orchestration (`server-orchestration`)

**Goal**: Wire all components together into the complete webhook-to-operation flow.

**Delivers**:
- PR event handler (opened, synchronized, closed, merged)
- Comment event handler (trigger detection, authorization, execution)
- Operation orchestration (parallel execution, result collection)
- Lock acquisition → runner creation → result → comment/check flow
- Integration tests for end-to-end workflows

**Global requirements covered**: 4, 5, 6, 17, 19, 20

---

### Slice 7: Terraform & Pulumi Plugins (`terraform-pulumi-plugins`)

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

### Slice 8: Structured Logging (`structured-logging`)

**Goal**: Replace unstructured `log.Printf`/`fmt.Fprintf` calls with
`log/slog`-based structured, leveled logging.

**Delivers**:
- `internal/logging` package (`TURNIP_LOG_LEVEL` parsing, `log/slog` setup)
- Every existing Server/Runner log call site rewritten to structured logging

**Global requirements covered**: non-functional

---

### Slice 9: Metrics & Health Endpoints (`metrics`)

**Goal**: Give the Server an HTTP observability surface.

**Delivers**:
- `/healthz`/`/readyz` endpoints (and the `http.ServeMux` routing change
  they require in `cmd/server/main.go`)
- Prometheus metrics (`/metrics`) — webhook, Operation, lock, and Runner
  Job Start Latency signals
- A Grafana dashboard (JSON) for those metrics

**Global requirements covered**: non-functional

---

### Slice 10: Deployment — Kustomize & Release Images (`deployment-kustomize`)

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

### Slice 11: HA Validation & Documentation (`ha-validation`)

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

### Slice 12: Runner Workspace & Project Environment (`runner-workspace-environment`)

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

### Slice 13: Project Schema v1alpha2 (`project-schema-v1alpha2`)

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

### Slice 14: Per-Tool Provisioning (`tool-provisioning`)

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

### Slice 15: Refuse Fork Pull Requests (`fork-pull-requests`)

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

### Slice 16: Cloning Submodules (`clone-submodules`)

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

### Slice 17: Pull Request Comment Output (`comment-output`)

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

### Slice 18: The Lock's Lifecycle as a State Machine (`lock-release-rules`)

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

### Slice 19: Draft Pull Requests (`draft-pull-requests`)

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

### Slice 20: Apply Exactly What Was Planned (`plan-scoped-apply`)

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

### Slice 21: What a Bare Command Targets (`project-selection`)

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

### Slice 22: Refuse Tool Arguments That Name an Executable (`tool-argument-policy`)

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

### Slice 23: Authorize on Permission Level, Not Call Success (`collaborator-authorization`)

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

**Correction made** (2026-09-21): `github-integration/design.md`'s
"Single cached call, not two" paragraph is corrected in place, with the
original claim quoted rather than deleted — a wrong premise left in place
is what produced the defect, and the next person to economise on an API
call would read the same sentence. Recorded there and here, per the
convention the global spec uses.

Also recorded there: the two claims this entry makes about *which values*
the permission endpoint returns — `none` for a non-collaborator, `read`
for any user on a public repository — were checked while fixing this and
**neither was confirmed**. The defect does not depend on either. Treating
a 200 as the answer is wrong whatever the body says, which is why the fix
is the endpoint and not a threshold on the string.

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

### Slice 24: How the Runner Receives Its GitHub Token (`runner-token-delivery`)

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

**Four surfaces carry the same token, and this slice should own all
four** rather than fixing the one that happens to be in front of us:

| Surface | Where | Who can read it |
|---|---|---|
| Job/Pod spec env value | `internal/jobs/build.go` | anything with pod or job read in the namespace; etcd |
| `.git/config` in the workspace | `internal/runner/clone.go`'s `git remote add` | any code running in the tool container — recorded in Slice 15 |
| reported output | `internal/runner/clone.go`'s error paths, through the Runner's stream | anyone who can read the pull request the comment lands on |
| `OperationStart` gRPC message | `internal/runner/reporter.go` | plaintext pod-to-pod traffic, on the clone-failure path only |

The third was missed by an earlier draft of this entry and is the widest
audience of the four — a credential in an error message travels into a
pull request comment. It is closed today, and only, by `clone.go`'s
`redact`/`redactArgs`. It is listed because this slice rewrites the
delivery path those two functions guard.

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

**Decided 2026-09-21: B, and A is never built.** The earlier reading here
was "C first, then A, with B as the end state once Slice 25 lands" — which
would have built the per-Job Secret, its `ownerReference` lifecycle and
its RBAC, and then deleted all of it when B arrived.

Whether A is needed at all is decided by Slice 25's mechanism, not by this
slice. Slice 25 uses an audience-scoped **projected ServiceAccount
token**: kubelet mounts it, nothing lands in the Pod spec, and there is no
credential to deliver. Every other authentication mechanism — mTLS
certificates, a per-Operation bearer token — creates the delivery problem
A exists to solve, and so would have justified A. The projection does not.

There is one installation and no urgency, so the right move is the end
state rather than a patch with a known expiry date. **Slice 25 lands
first**, then this slice fetches the credential over the channel 25
authenticates.

**Still done here, and unblocked by anything**: C, because it is the only
surface a pull request can reach; deleting `github_token` from
`OperationStart`; and narrowing what the token can do in the first place —
see below.

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

**Narrow the token, which no entry previously said.**
`GenerateInstallationToken` returns `c.itr.Token(ctx)` — the **full**
installation token: every repository the App is installed on, every
permission it holds, for about an hour. `ghinstallation.Transport` already
carries an `InstallationTokenOptions` field taking `Repositories` and
`Permissions`; turnip simply never sets it.

Minting `{Repositories: [...], Permissions: {contents: read}}` reduces
what a leak is worth on **all four** surfaces at once and depends on
nothing — not on Slice 25, not on the delivery mechanism. It is the only
mitigation here that also helps against the surface a pull request can
reach, short of never persisting the token.

**But it is not "the one repo", and an earlier draft of this entry said
it was.** `internal/runner/submodules.go` authenticates submodule fetches
with the *same* token, via `url.<authenticated>.insteadOf` rewrites, and
`TURNIP_CLONE_SUBMODULES` defaults to `top-level`. A single-repository
token would therefore break every repository whose submodules live in
sibling private repos — and Requirement 1.3's "fail rather than fall back"
would make that a hard failure, on exactly the repositories using the
feature. The scope has to follow what the clone actually fetches.

The top level is enumerable before minting: the Server mints the token
before it creates the Job, and it can already read a file from the
repository at the Operation's commit — that is how `turnip.yaml` arrives —
so `.gitmodules` at the same ref costs one more call. Under `recursive`
the nested set is only discoverable by cloning, so completeness has a
boundary, and Requirement 1.4 states the invariant instead: narrowing must
never be the reason a clone fails.

Verified against the vendored library and against the submodule code
rather than assumed.

**Provenance**: the 2026-09-19 security review rejected a related finding
about the token crossing plaintext gRPC, and in doing so observed that the
Pod spec is the cheaper read — "retrievable without touching the network
at all". That observation, not the rejected finding, is what this slice
acts on.

**Shipped 2026-09-22.** What landed, and two things this entry did not
anticipate:

- **A Runner Pod now carries no GitHub credential at all.** git asks
  turnip's own binary, running as a git credential helper, which fetches
  one over the channel Slice 25 authenticates at the moment git needs it.
  The request message is empty: which Operation's credential to return is
  decided by the authenticated identity, so the cross-repository theft a
  naive fetch endpoint would allow is unreachable rather than forbidden.
- **The token is scoped** to the repositories the clone will fetch, with
  `contents: read`. Under `recursive` the repository scope stays wide,
  because nested submodules are not enumerable before cloning them, and
  narrowing must never be why a clone stops working.
- **There was a fourth surface**, missed by the first three drafts of
  this entry: a credential in an error message reaches a pull request
  comment, which is a wider audience than any of the other three. It was
  held closed only by `clone.go`'s `redact`/`redactArgs`.
- **And there was a fifth token placement.** `submoduleConfigEnv` put the
  token in `GIT_CONFIG_VALUE_n` environment values of the git
  subprocess — readable through `/proc`. Both it and `embedToken` are
  gone; redaction went with them, because the Runner process no longer
  holds a credential to redact against.

**Amendments made**: Slice 25 gains a unary interceptor — it had
installed only the stream one, so a unary RPC was reachable with no
authentication, and its own probe test was streaming and never noticed.
Slice 2 `clone-submodules` loses the credential from its rewrites.

**Still open, recorded here rather than fixed**: a finished Runner Pod
lingers for the Job TTL of 15 minutes. It no longer holds a credential,
so this is now only about Pod garbage collection rather than exposure.

**Order within the slice, and its relationship to Slice 38.** Requirement
1 (narrowing) depends on nothing and goes first. That is not just
convenience: it decides what Requirement 2 puts on the wire. Slice 25
landed authentication without encryption, so B moves the credential from
the Pod spec — where pod-read and etcd reach it, passively and at rest —
onto a plaintext channel, where capturing it needs node access or
`CAP_NET_RAW`. That is a net improvement against the realistic adversary,
and it is a much easier one to defend once the thing crossing the wire is
a single-repository `contents: read` token rather than a key to the whole
installation.

**Slice 38 (`runner-server-tls`) is therefore listed as a preference, not
a dependency.** The residual it closes is real and worth naming: without
server verification a Runner *asks an unauthenticated peer for a
credential*, so an attacker able to win routing to the Server's Service
name becomes a participant rather than an observer. Requirement 1 bounds
what they collect; Slice 38 is what stops them collecting it. If 38 is
already scheduled when this slice starts, do it first.

---

### Slice 25: Authenticating the Runner to the Server (`runner-authentication`)

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
the Server-side wiring, and the deployment for one new verb — `create` on
`authentication.k8s.io/tokenreviews`. No `secrets` grant: that belonged to
the certificate work now in Slice 38.

**That verb cannot go in `deploy/base/role.yaml`**, as an earlier draft of
this entry said. `TokenReview` is a cluster-scoped resource, so RBAC for
it requires a **ClusterRole and ClusterRoleBinding** — conventionally by
binding the built-in `system:auth-delegator`. turnip is namespaced-Role
only today, so this is a genuine privilege increase for an operator to
accept, and the first cluster-scoped grant turnip asks for. Worth stating
plainly rather than discovering during deployment.

The alternative is validating the token's signature locally against the
API server's JWKS, which avoids the cluster-scoped grant at the cost of
more code and more ways to get signature validation subtly wrong. Weigh it
in the design; do not inherit this note as the decision.

**Unblocks Slice 24's option B**, which is circular without it: the
Server can now tell which Pod is calling. Note that B should also wait
for **Slice 38** — reporting a result to an unverified peer risks a
forged result, while *fetching a GitHub token* from one hands a
repository credential to whoever answered.

**Settled during implementation** (2026-09-22). Three things this entry
left open, recorded here because a reader arriving at the entry should
not have to reconstruct them from the slice's design:

- **The open question is answered: `TokenReview` does return the Pod's
  uid**, in `UserInfo.Extra` under
  `authentication.kubernetes.io/pod-uid`, so the binding is exact and
  the weaker Pod-name fallback was not built. The cost is a version
  floor: the extra exists from v1.29 but is feature-gated until v1.32,
  and a gate turnip cannot detect from a response is not a version it
  can claim to support. A cluster that answers without the extra gets a
  refusal naming the version, not a relaxed check.
- **A narrow ClusterRole ships instead of binding
  `system:auth-delegator`.** The conventional grant also carries
  `subjectaccessreviews`, which turnip never creates: it asks the
  cluster *who is this*, never *may they do X*. The privilege increase
  this entry asks an operator to accept is therefore `create` on
  `tokenreviews` and nothing else. Local JWKS validation was not
  revisited — the cluster-scoped grant is one narrow verb, and signature
  validation has more ways to be subtly wrong.
- **"Encryption is a separate axis" survived, after briefly not.** The
  slice's requirements made TLS its Requirement 4, and it was designed
  and implemented — generation into a Secret on first boot, replicas
  converging through the API server's own compare-and-swap with no
  leader election. It was then split back out into **Slice 38
  (`runner-server-tls`)** on 2026-09-22, because the argument for
  merging them proved too much: it equates an attacker with network
  position with anything holding pod-read, and everything in this slice
  demonstrably holds over an unencrypted transport. What shipped here is
  authentication alone.

---

### Slice 26: Real-Time Operation Output (`operation-output-stream`)

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

### Slice 27: Authenticated and Encrypted Redis (`redis-tls-auth`)

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

### Slice 28: Seeing and Dropping Locks Without Hunting for the PR (`lock-admin-ui`)

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

### Slice 29: Move the Webhook Off the Root Path (`webhook-path`)

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

### Slice 30: Scheduling — Concurrency and Execution Order (`operation-scheduling`)

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

### Slice 31: Runner Pod Placement — Node Selectors and Tolerations (`runner-pod-placement`)

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

### Slice 32: Refuse a Closed Pull Request, Plan a Reopened One (`closed-pull-requests`)

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

### Slice 33: Show What Ran and With What Scope (`execution-provenance`)

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

### Slice 34: What a Pull Request Must Satisfy Before an Apply (`apply-requirements`)

**Goal**: Let an operator require that a pull request has been approved,
and can actually be merged, before turnip will change anything.

**What's missing today**: turnip admits a mutating Operation on two facts —
the commenter has write permission, and the Lock is in `StatePlanReady`.
It asks nothing about whether anyone *approved* the change, or whether the
pull request could even be merged. A single collaborator can plan and
apply their own pull request with no second pair of eyes, which for most
repositories is the opposite of why they run a review process.

**What Atlantis has** (researched 2026-09-21,
`runatlantis.io/docs/command-requirements`): three requirements, per
command — `approved` ("approved by at least one person other than the
author"), `mergeable` ("prevents applies unless a pull request is able to
be merged"), and `undiverged` (merge checkout strategy only: "prevents
applies if there are any changes on the base branch since the most recent
plan"). All are **opt-in**; none is default. A repository's own
`atlantis.yaml` cannot set them unless the server-side config lists them
in `allowed_overrides`.

**Delivers**: `TURNIP_APPLY_REQUIREMENTS`, comma-separated, defaulting to
empty — which is today's behaviour and Atlantis's default. `approved` and
`mergeable` are in scope; `undiverged` is not, because it needs a base
commit the Lock does not record.

**Operator-side only, and that breaks the existing pattern deliberately.**
`runner.serviceAccount` and `clone.submodules` are gateable through
`TURNIP_ALLOWED_OVERRIDES` because an operator may have reason to let a
repository choose and the worst case is scoped. A requirement is different
in kind: its whole purpose is to constrain the pull request, and the pull
request supplies `turnip.yaml`. Offering it as a gateable path is a trap —
an operator who adds it to the list has silently disabled the control for
anyone who can edit that file, and nothing in the mechanism hints that
this key is unlike its neighbours. `knownOverridePaths` stays at two.

**Avoids Atlantis's deadlock by construction.** Do not use GitHub's
`mergeable_state` (`clean`/`blocked`/`behind`/`unstable`): it folds in
required status checks, which is why Atlantis ships
`--gh-allow-mergeable-bypass-apply` — "enable ability to use `mergeable`
mode with required apply status check". If turnip's own `turnip` check
(Slice 37) is required by branch protection — and by design it cannot pass
until every plan is applied — the pull request cannot be `clean` until an
apply runs and the apply will not run until it is `clean`.

Use the **`mergeable` boolean**, which reports conflicts only. Then
`mergeable` means "no merge conflict", `approved` means "someone signed
off", and the two compose without turnip asking GitHub a question whose
answer includes turnip's own. A well-posed question rather than an escape
hatch from a badly-posed one.

**Where the gate sits**: once per Trigger Command in the comment path,
beside the collaborator check. Not per Target, which would fire a
per-pull-request API call once for every Project on a fleet. Sufficient
because a mutating Operation only ever arrives by comment — the automatic
path plans only.

**`unlock` is outside the gate**, structurally rather than by exception:
`comment.go:159` handles it before Target resolution and it is never a
Plugin Operation. Gating it would be wrong anyway — releasing a Lock is
how someone recovers from a pull request that cannot satisfy the
requirements.

**Cost per requirement**: `approved` needs one new client call
(`GET /pulls/{n}/reviews`) plus a not-the-author test. `mergeable` is
nearly free, since `GetPullRequest` already fetches the field — but GitHub
computes it asynchronously, so it is `null` on a fresh pull request and
needs a bounded retry or a "not yet known" refusal.

**Deferred**: `undiverged` needs the base SHA recorded at plan time, which
the Lock does not carry; it is also the cheap, deadlock-free approximation
of the plan-freshness check the Backlog records as accepted. Per-repository
requirements need the Backlog's per-repository server configuration.

---

### Slice 37: One Check Branch Protection Can Require (`aggregate-check-run`)

**Goal**: Give an operator one check run name that always reports, so that
requiring turnip is possible at all.

**What's wrong today**: turnip creates one check run per (Project,
Operation), named `turnip/<project>/<operation>`. That is right for detail
and unusable for branch protection, because the set of Projects differs
per pull request. Mark `turnip/web/diff` required and any pull request
that does not touch `web` never reports it — and blocks forever with
nothing wrong. There is no name today that an operator can safely require.

**Delivers** (settled 2026-09-23; spec in `aggregate-check-run/`):

- **One check named `turnip`**, reporting a verdict on the pull request
  rather than the outcome of a command. Operations are named in each
  tool's own vocabulary (`diff`/`plan`/`preview`, `apply`/`sync`/`up`), so
  any role-named aggregate would borrow one tool's word; a verdict needs
  none. A stronger verdict later arrives as a setting, not a second name.
- **What it asserts: every plan this pull request made has been carried
  out** — applied, or found to have nothing to apply. turnip never applies
  on merge and a merge releases the Locks, so a plan-only gate would let a
  pull request merge with its plans unapplied and nothing left to apply
  them.
- **A per-pull-request, per-commit record** in Redis of each Project's
  latest Outcome, derived from the `lock.Event` each Operation already
  produces. Not read off the Lock: a Lock is released for reasons that
  mean opposite things (applied, nothing to apply, a first plan that
  failed, an unlock), and a Lock another pull request holds was never
  this one's. Unlocking does not satisfy the check.
- **Absent until the first apply** (a required check shows GitHub's
  "Expected", which blocks without being red); then in progress while
  anything awaits its apply; `success` when all are done; `failure` only
  for something the author must fix — a failed apply, an invalid
  `turnip.yaml`, an affected Project whose tool has no Plugin; `skipped`
  when nothing is affected. A Project waiting on another pull request's
  Lock blocks, and is never red.
- **Per-Project checks renamed** to `turnip/<operation>/<project>`, so
  every `diff` or every `sync` lists together. Breaking, and pre-1.0.
- **HA without a lock**: any instance writes its Project's field, reads
  the whole record, and publishes; after publishing it re-reads the
  record's version and publishes again if it moved.

**First of the three check-run slices**, and nothing blocks it. Slices 35
and 36 both follow: 35 so its title sweep covers the check this one adds,
36 because blocking a merge visibly is only meaningful once an operator
has a check they can require.

**Why Slice 36 depends on it.** That slice makes a blocked Operation block
the merge visibly, which only means something if there is a check an
operator can actually require. Without this one, a blocked Project's
`queued` check blocks only if that Project's own name was required — which
is the fragile arrangement described above.

---

### Slice 35: What the Checks List Says (`check-run-titles`)

**Goal**: Make a check run's one line worth reading.

**What's wrong today**: turnip already computes the informative string and
then puts it where it takes a click to see. A completed plan sets
`Title: "success"` and `Summary: "add: 1, change: 4, destroy: 2"`, so the
checks list reads `turnip/diff/web — success`, which says nothing the ✅
does not. The good string is one field away.

The timeout path wastes it most: `timeoutDiagnostic` already builds
exactly the condensed reason this wants — "Job X: container stuck
(ImagePullBackOff)" — and the title says `timed out`.

**Delivers**: a shared formatter for the title, reusing the one-line shape
Slice 33 settled for the comment's summary line, applied at the four sites
that set one (`execute.go:198`, `execute.go:284`, `result.go:93`,
`sweep.go:104`).

| State | Today | After |
|---|---|---|
| plan with changes | `success` | `+1 ~4 -2` |
| plan with none | `success` | `no changes` |
| scoped plan | `success` | `+0 ~1 -0, -l name=api` |
| timed out | `timed out` | the diagnostic already built |
| failed | `failure` | `exit N`, or `ErrorMessage` when set |

**The name is an identity, not a label.** None of this may move into the
check run's *name*: branch protection's required-status-checks match on
it, so `turnip` and `turnip/<operation>/<project>` (Slice 37) have to
stay stable. A name carrying
change counts would mint a new required check on every run and never
satisfy protection. Titles are free to change precisely because nothing
matches on them.

**Deliberately not attempting a failure *reason*.** The comparison that
prompted this — Prow reporting "Pod scheduling timeout" — flatters turnip:
Prow knows its own failure modes, where turnip's failures are mostly "the
tool exited non-zero" with the reason somewhere in the output. Guessing
and being wrong is worse than `exit 1`, because a confident wrong summary
in a list is what people act on without clicking. Per-Plugin extraction is
the only thing that would reach Prow's quality and is a `ActsWithoutChanges`-shaped
concept for whoever wants it.

**Follows Slice 37, deliberately.** This slice is a sweep: every site
that sets a title moves onto one formatter. Run before Slice 37 and it
sweeps four sites, after which Slice 37 adds a fifth — the aggregate
check — that its author has to remember to format the same way. Run after,
and the sweep covers everything that exists. The dependency is about the
gap left behind rather than about code that will not compile.

**Confirm before implementing**: the `output.title` length cap, for
truncation. Not asserted here because it was not checked.

---

### Slice 36: A Blocked Operation Blocks the Merge, Visibly (`check-run-refusals`)

**Goal**: Make a refused Operation visible in the checks list **without**
letting the pull request merge unplanned.

**The governing rule**: turnip must never report a conclusion that allows
a pull request to merge when its infrastructure has not been planned.

That rules out `neutral` and `skipped`, which GitHub counts as satisfying
a required check — *"Required status checks must have a `successful`,
`skipped`, or `neutral` status before collaborators can make changes to a
protected branch."* Reporting either for "we could not plan this" means
the change merges unplanned and unapplied, which defeats gating on turnip
at all.

**An earlier draft of this entry proposed exactly that, and was wrong.**
It treated "blocked indefinitely" as the defect. Blocking is correct; the
defect is only that the reason is invisible. Recorded rather than quietly
replaced, because "make the red thing go away" is the instinct that
produced it and will produce it again.

**What's wrong today**: every refusal inside `executeOne` returns before
`CreateCheckRun` (`execute.go:194`), so a plan blocked by another pull
request's Lock produces a comment section — ❌ with "locked by PR #5" and
a link — and nothing in the checks list. The pull request is correctly
blocked and gives no indication why, several scrolls below the comment
that explains it.

**Delivers**: a check run in a **non-terminal status** for a blocked
Operation. `status` and `conclusion` are separate fields; a check run
reported as `queued` with no conclusion blocks a required check, appears
in the list carrying its title, and claims nothing false. When the Lock
frees and a plan runs, a conclusion supersedes it.

`failure` was considered and rejected for this case: it blocks and is
visible, and it says the change is broken — sending the author to debug
their own diff when the obstacle is somebody else's pull request.

**Refusals are not one kind of thing**, and the split is the substance:

| Refusal | Means | Reports |
|---|---|---|
| locked by another pull request | **not yet** — succeeds once that merges | `queued`, reason and remedy in the title |
| unsupported tool, refused override | **not without changing this pull request** | `failure` — the configuration really is wrong |
| no plan recorded, on a mutating Operation | the author asked for the wrong thing | nothing; an apply is not the gate |

A `queued` check sits until something re-triggers the plan. turnip does
not re-plan when another pull request's Lock frees, so the title has to
say what the reader must do — "locked by PR #5; re-plan once it merges" —
rather than leaving them watching a spinner that will never turn.

**Never created for a pull request that is not open.** Check runs attach
to a *commit*, not a pull request, so on a merge-commit strategy the head
SHA becomes an ancestor of the base branch and a check run created there
writes into the base branch's history for a run that never happened.

The rule is about **creation**, not update: one created while the pull
request was open must still be updated with its outcome, or an Operation
whose pull request closes mid-run leaves a check stuck in progress
forever.

This holds structurally rather than by a guard — check runs are
per-Target, and the whole-trigger refusals (a fork, Slice 15; a closed
pull request, Slice 32; a non-collaborator, Slice 23) return before any
Target is resolved. The slice's job is to keep that true while adding
check runs to per-Target refusals, not to add a state test.

**Depends on Slice 35** for the title formatter. Not because a refusal
title cannot be written without it — it plainly can — but because a
refusal reason rendered by different code from every other title is how
the two drift, and a check whose title read `queued` would recreate the
problem this slice is fixing.

**Depends on Slice 37 for its point to land.** Per-Project check names
are fragile as *required* checks: if `turnip/diff/web` is required, a pull
request that does not touch `web` never reports it and blocks forever,
with no Lock involved. Blocking the merge visibly only means something
once there is a check an operator can actually require, which is Slice
37's `turnip`. That check already keeps a Lock-blocked Project from
passing and never reports it red; what this slice adds is the reason, on
the Project's own check.

---

### Slice 38: Encrypting the Runner-to-Server Channel (`runner-server-tls`)

**Goal**: Encrypt the gRPC channel Runners report over, and let a Runner
verify it reached the real Server.

**Split out of Slice 25 on 2026-09-22.** That slice's requirements made
encryption its Requirement 4, on the argument that a bearer token on a
plaintext channel makes the authentication decorative. The argument
proves too much: it equates "an attacker with network position could
capture the credential" with "anything holding pod-read can read it", and
those differ by a wide margin. Slice 25 holds entirely over an
unencrypted transport — its interceptor suite runs on
`insecure.NewCredentials()` — so tying it to an unresolved certificate
question was holding up the part that was settled.

This restores what this entry said before that: **encryption is a
separate axis.**

**What is exposed until this lands**: plan and diff output and log lines
are readable by anything able to capture pod-to-pod traffic, which needs
node access or `CAP_NET_RAW` rather than a namespaced RBAC grant. The
Runner_Token is capturable on the same terms — audience-scoped to turnip,
expiring in minutes, and good only for writing to one Operation whose
output the captor can already read. Stated in `docs/deployment.md` rather
than left to inference.

**This is a real prerequisite for Slice 24's option B**, more than Slice
25 alone is. Reporting results to an unverified peer risks a forged
result; *fetching a GitHub token* from an unverified peer hands a
repository credential to whoever answered. Slice 24 should wait for this,
not merely for authentication.

**The open question, unsettled deliberately: where does the certificate
come from?** Four options — turnip generates once, turnip generates and
rotates the leaf, cert-manager, or the operator supplies one by hand —
with the constraint that **cert-manager is not installed on the cluster
turnip is piloted against**. `runner-server-tls/requirements.md` carries
the comparison and a survey of how cert-manager's own webhook, OLM,
ingress-nginx, Argo CD and etcd-operator each solved it, so it is not
re-derived.

**Two findings worth not losing.** Nobody surveyed hand-writes the PKI —
etcd-operator calls `transport.SelfCert`, cert-manager publishes
`webhook-lib/authority` — and nobody picked a ten-year expiry; the
converged numbers are a 365-day CA with short leaves, or OLM's two years
with regenerate-and-redeploy.

**And a defect the first implementation had, independent of provenance**:
it read the certificate once at boot into a static
`tls.Config.Certificates` and stamped a fixed CA into every Job at
`Orchestrator` construction. Invisible at a ten-year expiry; a recurring
total outage at any renewal interval, which a restart appears to fix.
Requirement 2 of the slice exists to prevent it.

### Slice 39: Runner Pod Resources (`runner-resources`)

**Goal**: Give every Runner Pod CPU, memory and ephemeral-storage requests
and limits, with each request equal to its limit, so the scheduler
reserves what a Job actually uses and a cluster autoscaler has a real
number to scale on.

**What is missing**: `internal/jobs` sets no `resources` on any container,
so every Runner Pod is BestEffort. The scheduler places it as though it
needs nothing, the autoscaler never adds a node on its account, and under
node pressure it is the first Pod the kubelet evicts. An evicted Runner is
not retried (`BackoffLimit` is 0, correctly) and, once started, is not
noticed either — Slice 40 covers that; this slice keeps it from being
evicted for want of a size.

**Why request equals limit.** Mirroring the two puts the Pod in the
Guaranteed QoS class — last in line for node-pressure eviction — and
makes what the scheduler reserved the same as what the Job can consume. A
Runner is a CI job: bursting past its request buys a little speed, and
the price is being evicted or OOM-killed from a node that was never sized
for it.

**Ephemeral storage is not covered by QoS, so it is its own resource.**
The QoS class looks only at CPU and memory. Under disk pressure the
kubelet ranks Pods by how far their disk use exceeds their
*ephemeral-storage request* — and a Runner with none is always over it,
so it is evicted first however its CPU and memory are set. The workspace
(`/turnip/src`) is an `emptyDir` on the node's disk, and it is where
Terraform puts `.terraform` — modules and providers — so this is the
resource that grows once Terraform Projects are onboarded. A request
lets the scheduler avoid a nearly full node and the autoscaler provision
disk; the equal limit makes a Runner that outgrows it fail loudly (the
kubelet evicts it) rather than fill the node for every other Pod on it.
The Pod-level limit counts the `emptyDir` volumes, the containers'
writable layers and their logs together.

**Configuration, in two layers.**

| Layer | Form | Role |
|---|---|---|
| Server environment | one quantity per resource, e.g. `TURNIP_RUNNER_CPU`, `TURNIP_RUNNER_MEMORY`, `TURNIP_RUNNER_EPHEMERAL_STORAGE` | the operator's default for every Runner |
| `turnip.yaml` | `runner.resources: {cpu: …, memory: …, ephemeralStorage: …}` on a Project | a Project that needs more (a large state refresh, many providers) or less |

One value per resource, not a request and a limit: the premise is that
they are equal, and two fields invite setting them apart.

**No default in code or in the kustomize manifests** — by decision, not
omission. The right size depends on the tool, the size of the state and
the node shapes of the cluster; any number turnip picked would be wrong
for most deployments while *looking* chosen. Unset means what it means
today: no `resources` at all. `docs/configuration.md` documents the
variables and the `turnip.yaml` field beside the other Runner settings;
`docs/deployment.md` explains why an autoscaled cluster needs them set
and gives starting points per tool — marked as starting points, not
defaults.

**Open questions for the slice's requirements:**

- **Is a Project's value gated?** `turnip.yaml` is read from the pull
  request's head commit, so a Project value is settable by whoever opens
  the pull request. Resources grant no *access* — by Slice 31's rule
  ("gate what grants capability") that argues for ungated — but they do
  grant *cost*: `cpu: 64` makes an autoscaler buy a node. Either put it
  behind `TURNIP_ALLOWED_OVERRIDES` like `serviceAccount`, or leave it
  open and document a namespace `LimitRange` as the ceiling, which
  Kubernetes enforces at admission with no turnip code.
- **Which containers.** The clone and tool-provisioning init containers
  run before the main one, and a Pod's effective request is the larger of
  its biggest init container and its main container. Setting the same
  values on all of them is the simple answer, and costs nothing because
  init containers do not run alongside the main one.
- **Validation.** A malformed quantity should fail at Server start (the
  environment) and at `turnip.yaml` validation (a Project), with the
  latter reported on the pull request like any other config error — not
  discovered when the Job is rejected by the API server.
- **Unschedulable.** A size no node can satisfy leaves the Pod Pending.
  Slice 31's Decision 3 (fail fast on `Unschedulable` rather than by
  timeout) applies unchanged; whichever slice lands first builds it.

**Relation to other entries**: Slice 41 relies on this slice for
node-pressure eviction. Slice 42 moves the workspace off the node's disk
when an operator opts in, after which the ephemeral-storage value only
has to cover the containers' own use. The Backlog's top-level `runner:` block would
need to say whether `resources` merges per field or replaces as a whole.

### Slice 40: Bounding How Long a Runner Runs (`runner-timeouts`)

**Goal**: A Runner that goes silent, hangs or dies after it started is
stopped and reported as a failure the ordinary way, instead of holding
its check run, its Lock and its node until something else gives up.
Three pieces: an idle timeout the Runner enforces, an absolute deadline
Kubernetes enforces, and detection of a started Runner whose Job ended
without a result. Sizing is Slice 39's and eviction Slice 41's; this
slice is only about time.

**What happens today.** Once an Operation's first line reaches the
Server, nothing looks at output timing again: the Runner and the plugins
have no idle or overall timeout, and the sweep only claims Operations
that never started. A silent Runner — slow or hung, indistinguishable —
leaves the check run "in progress", the Lock held and the Pod running.
At 24 hours (`operationTTL`) the Redis record expires and the dispatching
Server instance posts "waiting for result: context deadline exceeded",
without moving the Lock; if that instance restarted meanwhile, nothing is
posted at all. The Pod runs until its process exits on its own.

**The tools cannot be relied on to bound themselves.** Terraform has no
run-wide timeout — resource timeouts are per provider — though an apply
prints `Still creating... [10s elapsed]` and is rarely silent for long.
The silent cases are elsewhere:

| Tool | Silent while |
|---|---|
| Helm / Helmfile | `--wait` until its own `--timeout`; rendering a large chart; hooks |
| Terraform | waiting on a state lock (`-lock-timeout`); provider downloads; a provider call with no resource timeout |
| Pulumi | a hung plugin or policy pack |

**Decision 1 — the Runner enforces it.** It sees every line as it is
produced, it is alive when the tool is not, and it needs no Server
bookkeeping — which fits a stateless Server better than a Server-side
watchdog would. After N minutes with no line on either stream, it stops
the tool and reports a failure whose message says so. Because that is an
ordinary result, the Lock transition, check run and comment all follow
the existing path; a stopped mutating Operation takes the same "failed
part-way, plan is stale" edge any failed apply does.

**Decision 2 — what counts as output.** A line on the tool's stdout or
stderr resets the clock. turnip's own `@@ turnip:` lines do not — they
say nothing about whether the tool is alive. The transcript's trailer
records the stop, so the comment says what happened where the exit code
would be.

**Decision 3 — stop gracefully, then forcefully.** `exec.CommandContext`
sends `SIGKILL` on cancellation, and the Runner forwards no signal to the
tool today. Killing Terraform outright leaves its state lock held — the
next plan then fails on a lock nobody owns. So the Runner sends an
interrupt first, waits a grace period, and only then kills
(`exec.Cmd.Cancel` and `WaitDelay`). The same handling belongs on the
Runner's own `SIGTERM` — sent by the kubelet on eviction and on this slice's
deadline — which today terminates the Runner without telling the tool.

**Precedent**: CI systems bound silence rather than, or as well as,
duration — CircleCI's `no_output_timeout`, 10 minutes by default.

**Decision 4 — an absolute deadline, enforced by Kubernetes.** The idle
timeout needs a working Runner; a Runner that is itself wedged cannot
report anything. `activeDeadlineSeconds` on the Job, from a Server setting
(e.g. `TURNIP_RUNNER_DEADLINE`), is the backstop, and Kubernetes enforces
it even if every Server instance has restarted. It must be comfortably
larger than the idle timeout, or it fires first and the comment loses the
clearer "no output" message. Slice 41's PodDisruptionBudget depends on
it: without a deadline, a hung Runner holds its node against drains.

**Decision 5 — a started Runner whose Job ended is noticed.** A Runner
killed by its deadline, evicted, or OOM-killed sends no result, and today
nothing would notice: the sweep claims only Operations that never
started. The sweep's scan extends to started Operations whose Job has a
`Failed` condition (e.g. reason `DeadlineExceeded`) or whose Pod is gone,
and finalizes them as failures with that diagnostic — the way it already
finalizes a start timeout, through the same claim so a late result and
the sweep cannot both finalize. This is what removes the 24-hour hang,
rather than merely making it rarer.

Cleanup stays with the Job's `ttlSecondsAfterFinished`: the Server never
deletes a Runner Job, and a deadline-killed Job is *finished*, so the TTL
now reaches the case it could not before.

**Open questions for the slice's requirements:**

- **Defaults or not?** Both the idle timeout and the deadline are Server
  settings. Slice 39's rule is no default for sizing values, where any
  default is wrong for someone; these are closer to safety bounds, and
  without a deadline Slice 41's PDB can block drains indefinitely.
  Recommendation: generous defaults (the deadline in hours), documented
  as safety bounds rather than sizing choices. For the owner to decide.
- **Per-Project override.** A Project whose Helm releases legitimately
  wait longer than the Server's value needs more. Ungated by Slice 31's
  rule (it grants nothing), but it lets a pull request lengthen how long
  it can hold a node.
- **Warn before stopping?** A line in the output at, say, half the limit
  ("no output for N minutes") would make a slow-but-alive run legible in
  the live view (Slice 26) before it is stopped.

**Relation to other entries**: Slice 41 depends on this slice's deadline
for its PDB. The start-timeout sweep this slice extends is Slice 6's
(Requirement 8).

### Slice 41: Runner Pods Run Without Disruption (`runner-disruption`)

**Goal**: A Runner is a CI job — once it starts, it runs to completion on
the node it started on. Nothing turnip can influence should evict it
partway through a plan or apply.

**What is missing**: `internal/jobs` sets no priority and no annotations
on a Runner, and nothing in `deploy/` guards Runner Pods against
eviction. An evicted Runner is not retried (`BackoffLimit` is 0 — a
retried apply is exactly the drift locking prevents), so every eviction
is a failed Operation.

**The scheduler never relocates a running Pod.** kube-scheduler places a
Pod once. A running Runner is disrupted only by being *evicted*, and each
source of eviction has its own control — there is no single setting:

| Source | Control | Where |
|---|---|---|
| Scheduler preemption — a higher-priority Pod needs room | Runner priority at least that of what it could be preempted for | this slice: operator-named `priorityClassName` |
| Node-pressure eviction by the kubelet | Guaranteed QoS: requests equal to limits | Slice 39 |
| Autoscaler scale-down or consolidation | autoscaler annotations | this slice: always set |
| `kubectl drain`, managed node upgrades, descheduler | a PodDisruptionBudget | this slice: one static PDB in `deploy/base` |
| A Runner that hangs, holding all of the above | a Job deadline | Slice 40 |
| Spot interruption, node failure | none in the Pod spec | out of scope — Slice 31's node selector places Runners on on-demand capacity |

**1 — Priority, named by the operator.** Preemption is the one
disruption the scheduler itself performs, and it honours
PodDisruptionBudgets only on a best-effort basis. A Runner at the default
priority of 0 is a candidate whenever something higher cannot be placed.
The Server sets `priorityClassName` from `TURNIP_RUNNER_PRIORITY_CLASS`;
the PriorityClass itself is cluster-scoped and the operator's to create,
so turnip neither ships one nor defaults the name. Server-only — a
Project choosing its own priority is choosing whom it may preempt.

**2 — Autoscaler annotations, always set.** Every Runner Pod carries:

| Annotation | Read by |
|---|---|
| `cluster-autoscaler.kubernetes.io/safe-to-evict: "false"` | Kubernetes cluster-autoscaler, on scale-down |
| `karpenter.sh/do-not-disrupt: "true"` | Karpenter, on consolidation and drift — it does not read the former |

Never configurable: no operator gains anything from letting an autoscaler
remove a Runner mid-apply to reclaim a node minutes sooner, and on a
cluster running neither autoscaler they are inert. They overlap with the
PDB below, and are kept anyway: cluster-autoscaler reads `safe-to-evict`
before it considers a node at all, which is cheaper than planning a
scale-down and having the PDB refuse it.

**3 — One static PodDisruptionBudget, not one per Runner.** Drains and
managed node upgrades go through the Eviction API, which honours only
PDBs. There is no `minAvailable` on a Pod or a Job — only a PDB carries
one — but it need not be created per Operation. `deploy/base` ships one
PDB selecting `app.kubernetes.io/name: turnip-runner`:

- **`minAvailable` set to a large sentinel integer.** Eviction is allowed
  only while healthy Pods exceed `minAvailable`, so a number no deployment
  reaches refuses every voluntary eviction of a running Runner. The
  natural `maxUnavailable: 0` is not accepted for Job Pods: Kubernetes
  restricts a PDB over Pods whose controller has no scale subresource to
  an integer `minAvailable`. The manifest carries a comment saying why the
  number is what it is.
- **`unhealthyPodEvictionPolicy: AlwaysAllow`**, so a Runner stuck
  Pending or crash-looping never blocks a drain — only a running one does.
- **The Pod template carries the selector label.** Today it is on the Job
  only (`build.go`), and a PDB selects Pods.

*Alternative considered*: a PDB per Operation (`minAvailable: 1`,
selecting the Operation ID, owned by the Job so it is collected with it).
*Rejected because* it needs RBAC for `policy/poddisruptionbudgets`, an
API call and a failure path per Operation, and buys nothing the static
one does not.

Managed providers respect a PDB only up to a provider-specific limit
during upgrades, then fail the upgrade or force the eviction. The PDB
delays and signals; it is not a guarantee, and `docs/deployment.md` says
so.

**Depends on Slice 40.** A hung Runner is healthy as far as the PDB is
concerned, so without Slice 40's Job deadline the PDB would hold it on its
node — blocking drains — until the provider forces the upgrade. The PDB
must not ship before that deadline does.

**Open question**: a priority no node can satisfy leaves the Pod Pending;
Slice 31's Decision 3 (fail fast on `Unschedulable`) applies, as it does
to Slice 39's sizes.

**Relation to other entries**: Slice 31 opens the same pod template for
scheduling fields, and the Backlog's Azure Workload Identity entry opens
it for metadata — the annotations here are the first metadata turnip
sets, so whichever of the three lands first builds that seam.

### Slice 42: The Workspace on a Per-Runner Volume (`runner-workspace-volume`)

**Goal**: Let an operator put a Runner's workspace on a volume of its own
— created with the Pod, sized for it, deleted with it — instead of the
node's disk, for when Terraform's modules and providers make a workspace
heavier than a node should carry.

`emptyDir` stays the default and a fully supported configuration, not a
stopgap: sized by Slice 39's ephemeral-storage value, it is the right
answer for most deployments. The volume is an option an operator turns
on, never a migration every deployment is expected to make.

**Principle: every Runner starts empty and leaves nothing behind.** A run
depends on nothing a previous run left, and no storage outlives the run
that used it. This is the property the slice must not trade away, and the
reason for the one alternative it rejects outright (below).

**What is there today**: the workspace (`/turnip/src`) is an `emptyDir`
on the node's disk, as are the small `tools` and `bin` volumes. No tool
cache location is set (`HOME`, `HELM_CACHE_HOME`, `PULUMI_HOME`,
`TF_PLUGIN_CACHE_DIR`), so Helm's repository cache, Pulumi's plugins and
anything else a tool keeps under its home directory land on the
container's writable layer — also the node's disk. Slice 39's
ephemeral-storage value makes all of it schedulable and bounded; this
slice is for when bounding it on the node is the wrong answer.

**Decision 1 — a generic ephemeral volume, opted into by the operator.**
`volumes[].ephemeral.volumeClaimTemplate`: Kubernetes creates the claim
with the Pod and deletes it with the Pod, so there is nothing for turnip
to clean up and nothing that grows across runs.

| Setting | Form | Effect |
|---|---|---|
| Server environment | `TURNIP_RUNNER_WORKSPACE_STORAGE_CLASS` | set: the workspace is an ephemeral volume of that class; unset: `emptyDir`, as today |
| Server environment | `TURNIP_RUNNER_WORKSPACE_SIZE` | the claim's size, required when a storage class is set — a Server that has one without the other refuses to start |
| `turnip.yaml` | a repository-level workspace size | a repository whose clone and providers need more (or less) than the Server's value |

No default size in code or kustomize, for Slice 39's reason: the right
number depends on the repository.

**Decision 2 — tool caches follow the workspace.** A volume that holds
the clone but not the caches solves half the problem. The Runner points
each tool's cache into the workspace volume (a directory beside the
clone, not inside it, so it is never mistaken for repository content):
`HOME`, `HELM_CACHE_HOME`, `PULUMI_HOME`, `TF_PLUGIN_CACHE_DIR`. On
`emptyDir` this changes where on the node's disk they go; on a volume it
takes them off the node. The cache still dies with the Pod — it exists
to avoid a second download *within* a run, not across runs.

**What it costs, which is why it is opt-in:**

- **Start latency.** Every Runner waits for a claim to be provisioned and
  attached — often tens of seconds on cloud block storage — and that
  counts against the start timeout (Slice 6, Requirement 8).
- **A cluster dependency.** A StorageClass with dynamic provisioning and
  `volumeBindingMode: WaitForFirstConsumer`, so a zonal disk is created
  in the zone the Pod was scheduled to rather than pinning the Pod to
  wherever the disk landed.
- **Density.** Nodes limit how many volumes can be attached at once,
  which caps how many Runners a node can run regardless of CPU and
  memory.

**Rejected: a cache shared across Runners** (a long-lived volume of
providers and modules). It is the obvious way to save downloads, and it
is rejected, not deferred:

- It breaks the principle above — a run would depend on what earlier
  runs left, and two runs of the same commit could behave differently.
- It grows without bound. Needing to keep enlarging a long-lived volume
  is one of the operational costs turnip exists to remove, not
  reintroduce.
- It thrashes and contends: concurrent Runners writing one cache need
  locking the tools do not all provide, and eviction policy becomes
  turnip's problem.
- It is shared between pull requests: what one pull request's run puts
  in the cache, the next one executes.

**Open questions for the slice's requirements:**

- **Repository or Project?** The workspace holds the whole clone, which
  argues for a repository-level size; a Project with unusually many
  providers argues for per-Project. Interacts with the Backlog's
  top-level `runner:` block.
- **Gated?** The same question as Slice 39's resources: a size grants
  cost, not access.
- **Does the start timeout need to know?** If provisioning routinely takes
  a large share of it, either the timeout grows when a volume is in use,
  or the documentation says to size it accordingly.

**Relation to other entries**: Slice 39's ephemeral-storage value still
applies — to the containers' writable layers, logs and the small `tools`
and `bin` volumes — and can shrink once the workspace is on a volume.
Slice 12 owns the workspace's layout; this slice changes where it lives,
not what is in it.

---

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

### A Helmfile apply does not verify its plan is still fresh — accepted

Terraform replays a plan file; Helmfile has no artifact, so its "plan" is
the arguments that produced the diff and the apply re-renders at run time.
Slice 20 made the *scope* replayable, not the content.

Slice 18 closed the half that moves most: dispatching a plan invalidates a
stored one, so an apply cannot cross a commit, a failed apply, or a
timeout. What remains unguarded is narrower:

- **Resolution drift** — a floating chart version, a `^1.2` range, or a
  remote values source can move with the commit unchanged.
- **Cluster drift** — the diff compares desired against live, so the live
  side can move between plan and apply for reasons unrelated to the
  change.

**Considered and declined** (2026-09-21): re-diffing at apply time and
comparing against the stored diff. Four shapes were weighed — comparing
the changed-release set rather than the text, fingerprinting
`helmfile template` output, pinning resolved chart versions and values
checksums, and showing the fresh diff without blocking.

Declined because the residual risk is accepted, not because the mechanism
would not work. Three costs drove it: comparing diff output is unreliable
(a chart calling `randAlphaNum` without a `lookup` guard renders
differently every time, so those releases would report stale forever);
blocking on cluster drift blocks on other people's legitimate changes,
which is what apply exists to reconcile; and every variant pays an extra
render at apply time, on top of the one `helmfile apply` already performs
internally — three renders where a fleet-shaped repository feels each one.

**What would change the answer**: a chart resolving to a different version
between plan and apply, or an apply visibly deploying something the
reviewed diff did not describe. If that happens, pinning the inputs is the
shape to reach for rather than comparing the outputs — its failure message
names a cause ("chart api moved from 1.2.3 to 1.2.4") where a diff
comparison only reports a difference.

**If it is ever picked up**, it belongs on the Plugin rather than in the
orchestrator: Terraform replays an artifact and Helmfile would need a
fingerprint, which is the same shape as Slice 18's `ActsWithoutChanges` —
a Plugin declaring a property of its own tool, so Slice 7 fills a seam
instead of reopening a decision.

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

### A failed Operation's output loses the transcript's highlighting

Slice 33's Decision 8 says a failure renders in the same `diff` fence as
a success. The code never did: `fencesFor` (`comment.go`) still chooses
`diff` only on success, and
`TestBuildConsolidatedComment_FailureKeepsAPlainFence` pins the plain
fence, so the spec and the code disagree about a behaviour both claim.

It matters more since the Slice 33 amendment. The transcript's
`@@ turnip: … @@` lines are highlighted as hunk headers *only inside a
`diff` fence*, so a failed run — the one a reader scrutinises hardest,
and the one whose trailer carries the non-zero exit — is the one where
turnip's lines blend into the tool's.

The plain fence has a real reason, recorded in that test: a failure's
body is often an error message, and diff highlighting colours any
column-0 `-` red. Decision 8's answer was that the same YAML can appear
on success, where the red is already accepted. Neither side has been
weighed against the other since the transcript changed what the fence
is for.

Options, none chosen:

- **One `diff` fence for both**, as Decision 8 states. Consistent, and
  the transcript is always highlighted; an error line that starts with
  `-` is coloured as a removal.
- **Keep the plain fence** and amend Decision 8 to say so, accepting
  that a failure's transcript is not highlighted.
- **Fence per stream** is not an option while the output is one
  interleaved record (Slice 33, Requirement 8) — splitting it back by
  stream is exactly what that amendment undid.

Small either way; what it needs is the decision, then Decision 8 and the
test brought into agreement with it.

## Notes

- Slices 1–5 can be developed in parallel once Slice 0 is complete
- Each slice should be merged to `main` before starting dependent slices
- The global spec remains the source of truth for cross-cutting concerns
- Individual slice specs may refine or add detail beyond the global spec
