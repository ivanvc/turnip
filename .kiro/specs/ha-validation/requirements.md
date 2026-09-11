# Requirements Document: HA Validation & Documentation (Slice 11)

## Introduction

This slice proves — not just asserts by design — that turnip's stateless,
multi-instance HA design actually holds under real concurrency and a real
deployment, and documents the finished platform for a new operator.

This is the last of four slices splitting what was originally scoped as a
single, too-broad "HA, Observability & Deployment" roadmap entry: **8.
Structured Logging** (`structured-logging`) → **9. Metrics & Health
Endpoints** (`metrics`) → **10. Deployment: Kustomize & Release Images**
(`deployment-kustomize`) → **11. HA Validation & Documentation** (this
slice). Per `roadmap.md`'s dependency column, this slice depends on Slice
6 (Server Orchestration), Slice 9 (`metrics` — the `/readyz` endpoint
this slice's test harness and documentation both use), and Slice 10
(`deployment-kustomize` — Requirement 1's `kind`-cluster variant deploys
that slice's overlay). This is deliberately the last slice in the split:
everything it validates or documents needs to already exist.

This slice implements the global spec's Requirement 19 (High Availability
Server Deployment) and Properties 34-37 from `design.md`
(`.kiro/specs/multi-iac-automation-platform/design.md`), plus the
"Testing Strategy" section's "Performance Testing" subsection. Every
deployable/testable criterion below is validated against Helmfile
Projects exclusively — the only Plugin this codebase currently
implements (Slice 2) — deliberately, so this slice's delivery isn't
gated on Slice 7 (Terraform & Pulumi Plugins).

## Glossary

(Inherited from the global spec glossary.)

- **Load Test**: An automated test simulating a burst of concurrent
  webhook events against a running Server (or Server fleet), per the
  global design's "Performance Testing" section (100 concurrent events).

## Requirements

### Requirement 1: Stateless Server Validation and Multi-Instance Concurrent-Load Testing

**User Story:** As a platform operator, I want proof that N Server
replicas behind the Kustomize base's Service produce identical, race-free
results to one replica, so that scaling out in production is a safe
operation, not an untested assumption.

#### Acceptance Criteria

1. THE test suite SHALL include an integration test running ≥2 in-process
   Server instances sharing one **real** Redis instance (not `miniredis`,
   whose single-threaded event loop can hide real concurrency bugs),
   processing an interleaved sequence of webhook events across both
   instances, and asserting identical outcomes to a single-instance run
   of the same sequence — validating global Properties 34 and 35
2. THE test suite SHALL include a test firing concurrent plan-lock
   acquisition attempts for the same Project Key from multiple simulated
   Server instances and asserting exactly one succeeds — extending
   `internal/lock`'s existing single-instance-caller coverage of Property
   36 to genuinely concurrent multi-instance callers
3. Requirement 1.1/1.2's tests SHALL run as part of the default `go test
   -race ./...` suite — these are core correctness properties worth
   checking on every commit, not gated behind a manual step; CI SHALL
   provide a real Redis instance for them (a new service container)
4. THE test suite SHALL include a Load Test simulating 100 concurrent
   webhook events (per the global design's "Performance Testing" section)
   against a real Redis and Helmfile-tooled Projects, asserting no
   deadlock, no lock-contention outcome other than the expected
   "already locked" rejections, and completion within a documented time
   bound
5. THE Load Test and Requirement 1.1/1.2's multi-instance scenarios SHALL
   also be runnable against `deployment-kustomize`'s `kind` overlay
   deployed to a real `kind` cluster, exercising the actual Kubernetes
   RBAC and networking path — not only application logic in-process
6. IF the `kind`-cluster variant of this Requirement fails to complete
   within a documented time bound (a resource- or scheduling-bound
   `kind` cluster is expected to be slower than the in-process variant),
   THEN that failure SHALL be reported distinguishably from a
   correctness failure (Properties 34-36 violated), so a flaky/slow CI
   runner doesn't get conflated with an actual HA regression
7. Requirement 1.4's Load Test and Requirement 1.5's `kind`-cluster
   variant SHALL NOT be wired into the default `go test -race ./...`
   suite — they are real infrastructure validation, appropriately more
   expensive than the rest of this codebase's test suite, and are
   runnable on demand instead (e.g. `make test-kind`)

### Requirement 2: Documentation

**User Story:** As a new platform operator, I want a README, deployment
guide, and troubleshooting doc, so I can stand up turnip without reading
its source code first.

#### Acceptance Criteria

1. THE repository's README SHALL describe what turnip does, its
   Server/Runner architecture, and link to `deployment-kustomize`'s base/
   overlays as the primary deployment path
2. THE repository SHALL include a deployment guide covering:
   prerequisites (Redis/Valkey, a GitHub App, a Kubernetes cluster), the
   values an overlay must supply, and how to verify a successful
   deployment via `/healthz`/`/readyz` (`metrics`, Slice 9)
3. THE repository SHALL include a troubleshooting section translating the
   failure modes the global design's "Error Handling" section already
   documents (lock contention, clone failure, Job timeout, GitHub API
   errors) into operator-facing symptoms and remediation — not a
   restatement of the design doc's developer-facing framing
4. Documentation SHALL be written last, once `structured-logging`,
   `metrics`, `deployment-kustomize`, and this slice's own Requirement 1
   have real, working artifacts to describe

## Out of Scope

- Any acceptance criterion whose validation requires a Terraform or
  Pulumi Plugin (per-tool load-test scenarios) — deferred until Slice 7
  lands; Requirement 1 exercises Helmfile Projects only
- Structured logging, metrics, health endpoints, Kustomize manifests,
  release images — delivered by `structured-logging`, `metrics`, and
  `deployment-kustomize` (Slices 8-10); this slice validates and
  documents them, it doesn't build them
- Autoscaling (HPA) configuration for the Server Deployment — multi-
  instance behavior is validated at a fixed replica count (Requirement
  1); making replica count itself dynamic is a separate, later concern
- Wiring Requirement 1.5's `kind`-cluster test into the default CI
  pipeline's every-commit path (Requirement 1.7) — a follow-up decision
  for whoever owns CI cost/time tradeoffs
