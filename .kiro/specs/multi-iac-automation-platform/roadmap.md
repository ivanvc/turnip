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
| 13 | Project Schema v1alpha2 | `project-schema-v1alpha2` | Not Started | Slice 12 |

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

## Backlog (not yet sliced)

Recorded so they aren't rediscovered the hard way. None of these has a
spec directory, and none is scheduled.

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

### Cross-project execution ordering

turnip executes every matched Project in parallel (global Requirement
17.1) and offers no way to say one must finish before another starts.

This looked minor until it was measured. A survey of a real
multi-repository Atlantis deployment found ordering declared on **353 of
387 projects** — near-universal — in every case to make one foundational
project complete before the projects that depend on it start. By contrast
the same survey found the `workflows` concept, which turnip deliberately
does not implement, doing no real work at all. Ordering is the larger gap
of the two, and was the one nobody had written down.

Atlantis spells it two ways: `execution_order_group`, an integer bucket
where lower runs first, and `depends_on`, naming specific projects. The
surveyed deployment used the integer form exclusively and `depends_on`
nowhere — worth weighing, since the integer form is far simpler to
schedule and evidently sufficient in practice.

Deliberately left out of `project-schema-v1alpha2`: the schema half is
trivial, but the behaviour is not — `executeTargets` would need to run
groups in sequence while keeping projects within a group parallel, and
decide what an ordered group does when an earlier project fails. Adding
the key before the behaviour would ship a field that parses and does
nothing, which is the failure this platform has now been bitten by twice.

## Notes

- Slices 1–5 can be developed in parallel once Slice 0 is complete
- Each slice should be merged to `main` before starting dependent slices
- The global spec remains the source of truth for cross-cutting concerns
- Individual slice specs may refine or add detail beyond the global spec
