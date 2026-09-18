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
| 15 | Refuse Fork Pull Requests | `fork-pull-requests` | Not Started | Slice 6 |
| 16 | Cloning Submodules | `clone-submodules` | Complete | Slice 5 |

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

**Deliberately not scheduled now**: the only deployment is a single
private test repository with no forks in play, so this is a real gap
without being a present risk. Prioritising it over the Helmfile MVP would
be fixing the wrong thing first.

**Open when this is picked up**: whether a refusal is silent or commented
(silence gives an attacker no feedback; a comment stops a legitimate fork
contributor wondering why nothing happened), and whether an operator may
ever opt in for a genuinely credential-free public project. Neither was
explored — both are decisions for whoever takes this.

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

### Flaky reporter reconnect test — or a real dropped-log bug

`TestReporter_ReportReconnectsAndResendsFullBufferWithResumedTrue` fails
intermittently. It asserts that the entire buffered log history is resent
after a dropped connection, and sometimes observes two lines where three
were produced.

Confirmed pre-existing rather than introduced by any recent slice: it
reproduces at `12f77c3` and passes repeatedly against the working tree, so
it is timing-dependent rather than a regression.

Worth resolving because the two explanations differ in seriousness. If the
test is racing its own fixture, it is noise that will eventually be
dismissed as "just the flaky one" and stop being read. If the reporter
genuinely resends a partial buffer, then log lines are silently lost when a
Runner reconnects mid-operation — and the pull request would show a plan
missing lines with nothing to indicate anything went missing, which is the
worse failure mode because it looks like success.

Reproduce with `go test ./internal/runner/ -run
TestReporter_ReportReconnectsAndResendsFullBufferWithResumedTrue -count=20`.
Start by deciding which of the two it is; only then decide whether the fix
belongs in the reporter or the test.

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
