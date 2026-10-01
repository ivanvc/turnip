# Requirements Document: Deployment — Kustomize & Release Images (Slice 10)

## Introduction

This slice makes turnip actually deployable: Kustomize manifests for the
Server (Deployment, Service, RBAC), and a real release pipeline replacing
the `:latest` placeholder both container images have carried since Slice
0.

This is one of four slices splitting what was originally scoped as a
single, too-broad "HA, Observability & Deployment" roadmap entry: **8.
Structured Logging** (`structured-logging`) → **9. Metrics & Health
Endpoints** (`metrics`) → **10. Deployment: Kustomize & Release Images**
(this slice) → **11. HA Validation & Documentation** (`ha-validation`).
Per `roadmap.md`'s dependency column, this slice depends on Slice 6
(Server Orchestration) **and Slice 9** (`metrics`): Requirement 1.4's
`readinessProbe`/`livenessProbe` wire to the `/readyz`/`/healthz`
endpoints Slice 9 adds — this slice cannot be meaningfully completed
before that one. **`ha-validation` (Slice 11) depends on this slice**:
its `kind`-cluster test variant needs a real, deployable overlay to test
against.

This slice implements no specific numbered global requirement — it's a
non-functional, production-readiness concern.

## Glossary

(Inherited from the global spec glossary.)

## Requirements

### Requirement 1: Kubernetes Manifests via Kustomize

**User Story:** As a platform operator, I want to deploy the Server and
Runner to a real cluster via Kustomize, so that production deployment
doesn't require hand-written, drifting manifests — and without needing a
templating tool beyond what `kubectl` already ships.

#### Acceptance Criteria

1. THE repository SHALL provide a Kustomize base deploying the Server as
   a Kubernetes Deployment, a Service in front of it, and the RBAC
   (ServiceAccount/Role/RoleBinding) the Server needs to create and
   manage Runner Jobs in its namespace — exactly the permissions
   `internal/jobs.Client` uses (`batch/v1` Jobs, `core/v1` Pods for
   status), least-privilege, no more
2. THE base SHALL be parameterized for per-environment values —
   replica count, the Server/Runner image and tag (Requirement 2), the
   Redis/Valkey address, GitHub App credentials, and the Kubernetes
   namespace Runner Jobs are created in — via overlay-level
   `configMapGenerator`/`patches`, not values hardcoded into the base;
   GitHub App credentials SHALL be referenced as an existing Secret name
   (an overlay-provided `secretGenerator` or a pre-existing Secret), never
   committed as plain YAML
3. THE base SHALL NOT include a Redis/Valkey Deployment — `redis.address`
   is supplied by an overlay as an external dependency, matching the
   global design's "single source of truth" framing and keeping turnip's
   own manifests from owning a stateful dependency's lifecycle
4. THE base Server Deployment SHALL configure `readinessProbe` against
   `/readyz` and `livenessProbe` against `/healthz` (endpoints `metrics`,
   Slice 9, already provides), so a rolling update never routes traffic
   to an instance that can't reach Redis yet
5. THE repository SHALL include at least one example overlay (e.g.
   `deploy/overlays/kind/`) that is applyable against a `kind` cluster via
   `kubectl apply -k` with no manual post-apply step, demonstrating the
   base is actually consumable and not just theoretically parameterized
6. THE repository SHALL provide an optional Kustomize component
   provisioning the Grafana dashboard `metrics` (Slice 9, Requirement 3)
   defines, via a ConfigMap labeled `grafana_dashboard: "1"` — an
   operator opts in by including that component in their own overlay,
   rather than the base assuming a specific Grafana deployment method it
   doesn't otherwise control

### Requirement 2: Versioned Release Images and Build Pipeline

**User Story:** As a platform operator, I want a real, reproducible
version tag on the images I deploy, so a rollback or an audit can point
at an exact build rather than a moving `:latest` tag.

#### Acceptance Criteria

1. THE Server and Runner container images SHALL be built and tagged with
   a version derived from the release (a git tag / semantic version) —
   not `:latest`, the placeholder `internal/jobs/build.go`'s
   `runnerImage` constant has carried since Slice 0 (see that constant's
   own `TODO` comment)
2. THE build pipeline SHALL evaluate `goreleaser` for building, tagging,
   and pushing both images as part of a release, per the global
   roadmap's note — this criterion's acceptance is a documented decision
   (adopt, or explicitly reject with a reason), not a mandate to adopt it
   unconditionally
3. IF `goreleaser` is adopted, THEN its configuration SHALL produce both
   the Server and Runner images from one release trigger (a git tag
   push), so the two images can never end up at different versions from
   the same release
4. `internal/jobs/build.go`'s `runnerImage` constant SHALL become
   configurable (a `BuildJob` parameter or package-level value the Server
   sets from its own config, sourced from the overlay's environment
   values) rather than a hardcoded string — a deployed Server SHALL create
   Runner Jobs at its own matching release version, not an independently
   hardcoded one

## Out of Scope

- Structured logging — `structured-logging` (Slice 8)
- Health/readiness endpoints, metrics, the dashboard's own JSON
  definition — `metrics` (Slice 9); this slice only packages that
  dashboard for deployment (Requirement 1.6)
- Multi-instance/load testing (including the `kind`-cluster variant that
  deploys this slice's overlay to validate it) and documentation —
  `ha-validation` (Slice 11)
- A managed/bundled Redis or Prometheus/Grafana deployment as part of
  turnip's own manifests — the base takes them as external dependencies
  (Requirement 1.3), consistent with the global design's "single source
  of truth... external Redis" framing
- Autoscaling (HPA) configuration for the Server Deployment
