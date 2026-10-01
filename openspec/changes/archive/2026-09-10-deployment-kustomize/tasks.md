# Implementation Plan: Deployment — Kustomize & Release Images (Slice 10)

## Overview

This plan lands Decision 3's code change first (`internal/jobs.OperationParams.RunnerImage`
threaded from a new `TURNIP_RUNNER_IMAGE` env var through
`internal/orchestrator` into `cmd/server/main.go`), since it's the only part
of this slice that touches Go source and every later manifest references the
env var name it defines. It then builds the Kustomize tree from design.md's
Decision 1 (`deploy/base` → `deploy/overlays/kind` → the optional
`deploy/components/grafana-dashboard`), and finishes with Decision 2's
`goreleaser` config and release workflow, which depends on nothing else in
this plan (only on the two existing `build/*/Dockerfile`s from Slice 0) and
can be built in parallel with the manifests once task 1 is done.

`deploy/base` (task 3) must exist before `deploy/overlays/kind` (task 4) and
`deploy/components/grafana-dashboard` (task 5), since both build on the base
via `resources: [../../base]`. Task 5 has no dependency on task 4 beyond
both needing task 3 first. The CI manifest-validation step (task 6) needs
task 4 done (it builds and validates the `kind` overlay specifically). Task
7 (`goreleaser`) is independent of tasks 3-6 and only needs task 1's env var
name (`TURNIP_RUNNER_IMAGE`) to already be decided, which it is as soon as
task 1.3 lands.

## Tasks

- [x] 1. Thread `RunnerImage` through `internal/jobs` and `internal/orchestrator` (Decision 3)
  - [x] 1.1 Amend `internal/jobs/build.go`
    - Add `RunnerImage string` to `OperationParams`
    - In `BuildJob`, resolve the runner container's image as
      `op.RunnerImage`, falling back to the existing `runnerImage` constant
      when `op.RunnerImage == ""` (Backward Compatibility) — drop the
      constant's `// TODO: This should point to the released version.`
      comment now that it's resolved, keep the constant itself as the
      fallback value
    - _Requirements: 2.4_
  - [x] 1.2 Update `internal/jobs/build_test.go`
    - Add `TestBuildJob_RunnerImageFromParams`: `testParams()` +
      `RunnerImage: "ghcr.io/ivanvc/turnip-runner:v1.2.3"`, assert the
      runner container's `Image` field matches
    - Add `TestBuildJob_RunnerImageFallsBackToDefault`: `testParams()` with
      `RunnerImage` left as `""` (zero value), assert the runner
      container's `Image` field equals today's hardcoded constant value
    - _Requirements: 2.4_
  - [x] 1.3 Amend `internal/orchestrator/config.go`
    - Add `RunnerImage string` to `Config`, read from `env("TURNIP_RUNNER_IMAGE")`
    - Not added to the required/`missing` list — an unset value is a valid
      zero value that `BuildJob`'s fallback (task 1.1) already handles,
      matching `MinimizeOutdatedPlanComments`'s already-optional treatment
    - _Requirements: 2.4_
  - [x] 1.4 Update `internal/orchestrator/config_test.go`
    - `TestConfigFromEnv_RunnerImageDefaultsEmpty`: omit
      `TURNIP_RUNNER_IMAGE`, assert `cfg.RunnerImage == ""` and `err == nil`
    - `TestConfigFromEnv_RunnerImageRoundTrip`: set
      `TURNIP_RUNNER_IMAGE=ghcr.io/ivanvc/turnip-runner:v1.2.3`, assert it
      round-trips onto `cfg.RunnerImage`
    - _Requirements: 2.4_
  - [x] 1.5 Amend `internal/orchestrator/orchestrator.go`
    - Add a `runnerImage string` field to `Orchestrator` and a trailing
      `runnerImage string` parameter to `New(...)` (after
      `runnerServerAddr`, following that field's existing precedent),
      assigned straight through in the returned `&Orchestrator{...}`
    - _Requirements: 2.4_
  - [x] 1.6 Update `internal/orchestrator/orchestrator_test.go`'s `New(...)` call site (line ~26)
    - Add a trailing runner-image argument (e.g.
      `"ghcr.io/ivanvc/turnip-runner:test"`) to match the new arity
    - _Requirements: 2.4_
  - [x] 1.7 Amend `internal/orchestrator/execute.go`'s `executeOne`
    - Add `RunnerImage: o.runnerImage` to the `jobs.OperationParams{...}`
      literal passed to `jobs.BuildJob`
    - _Requirements: 2.4_
  - [x] 1.8 Amend `cmd/server/main.go`'s `run(cfg)`
    - Add `cfg.RunnerImage` as the trailing argument to the existing
      `orchestrator.New(...)` call
    - _Requirements: 2.4_

- [x] 2. Checkpoint - Verify the Go changes compile and pass
  - Confirmed: `go build ./...` and
    `go test ./internal/jobs/... ./internal/orchestrator/... ./cmd/...` both
    pass.

- [x] 3. Build the Kustomize base (Decision 1)
  - [x] 3.1 Create `deploy/base/serviceaccount.yaml`
    - A `ServiceAccount` named `turnip-server`
    - _Requirements: 1.1_
  - [x] 3.2 Create `deploy/base/role.yaml` and `deploy/base/rolebinding.yaml`
    - `Role` named `turnip-server`: one rule for `apiGroups: ["batch"]`,
      `resources: ["jobs"]`, `verbs: [create, delete, get, list, watch]`
      (matching `internal/jobs/client.go`'s `Create`/`Delete` and
      `status.go`'s `Get`/implicit watch-by-poll usage) and one rule for
      `apiGroups: [""]`, `resources: ["pods"]`, `verbs: [get, list]`
      (matching `status.go`'s `pods.List`) — no other verbs or resources
    - `RoleBinding` named `turnip-server` binding that `Role` to the
      `turnip-server` `ServiceAccount`, same namespace
    - _Requirements: 1.1_
  - [x] 3.3 Create `deploy/base/deployment.yaml`
    - One `Deployment`, container `server`, image
      `ghcr.io/ivanvc/turnip-server:latest` (overlay-overridden per
      Requirement 1.2/Decision 3), `serviceAccountName: turnip-server`
    - `envFrom` a `configMapRef` named `turnip-server-config` (task 3.5)
      for every non-secret value; `env` with `secretKeyRef`s against a
      Secret named `turnip-github-app` for `TURNIP_GITHUB_WEBHOOK_SECRET`
      and `TURNIP_GITHUB_PRIVATE_KEY` (Requirement 1.2's "never committed
      as plain YAML" — the base only references the Secret by name, an
      overlay provides it)
    - `readinessProbe`: `httpGet` `/readyz` on the HTTP container port,
      `periodSeconds: 5`; `livenessProbe`: `httpGet` `/healthz` on the same
      port, `periodSeconds: 5` (Requirement 1.4)
    - Container `ports`: the HTTP port (readiness/liveness/`/metrics`) and
      the gRPC port
    - _Requirements: 1.1, 1.2, 1.4_
  - [x] 3.4 Create `deploy/base/service.yaml`
    - One `Service` (`ClusterIP`) named `turnip-server`, exposing the HTTP
      and gRPC ports, selecting the Deployment's pod labels
    - _Requirements: 1.1_
  - [x] 3.5 Create `deploy/base/kustomization.yaml`
    - `resources:` listing `deployment.yaml`, `service.yaml`,
      `serviceaccount.yaml`, `role.yaml`, `rolebinding.yaml`
    - `configMapGenerator:` one generator named `turnip-server-config`
      with sane non-secret defaults as `literals:` — `TURNIP_LOG_LEVEL=info`,
      `TURNIP_HTTP_ADDR=:8080`, `TURNIP_GRPC_ADDR=:9090`,
      `TURNIP_K8S_NAMESPACE=default`, `TURNIP_RUNNER_IMAGE=` (empty, so
      `BuildJob`'s constant fallback from task 1.1 applies until an
      overlay sets a real value), `TURNIP_RUNNER_SERVER_ADDR=turnip-server:9090`,
      `TURNIP_MINIMIZE_OUTDATED_PLAN_COMMENTS=false` — deliberately no
      `TURNIP_REDIS_ADDR` default (Requirement 1.3: no value the base can
      sanely default to without implying a bundled Redis); every overlay
      must supply it via `configMapGenerator` `behavior: merge`
    - No `images:` transformer and no `TURNIP_GITHUB_APP_ID`/Secret
      values here — those are overlay-level (Requirement 1.2), left to
      task 4
    - _Requirements: 1.1, 1.2, 1.3_

- [x] 4. Build the `kind` example overlay
  - [x] 4.1 Create `deploy/overlays/kind/kustomization.yaml`
    - `resources: [../../base]`
    - `images:` transformer setting the `turnip-server` image's `newTag`
      (the `kind`-loaded local build tag, e.g. `dev`)
    - `replicas:` patch setting the Server Deployment to 1
    - `configMapGenerator:` with `behavior: merge` against
      `turnip-server-config`, supplying `TURNIP_REDIS_ADDR` (pointing at
      whatever Redis Service the `kind` test setup provides — matching
      Requirement 1.3, this overlay assumes it exists, doesn't create it)
      and `TURNIP_GITHUB_APP_ID`
    - References the pre-existing `turnip-github-app` Secret by name only
      (Requirement 1.2) — does not generate one; the overlay's own
      `kustomization.yaml` comments the exact `kubectl create secret
      generic turnip-github-app --from-literal=...` an operator runs
      before `kubectl apply -k` (Requirement 1.5's "no manual post-apply
      step" is about the manifests themselves, not this one prerequisite
      external input)
    - _Requirements: 1.2, 1.5_
  - [x] 4.2 Verify with `kubectl kustomize deploy/overlays/kind`
    - Confirmed: builds cleanly, produces exactly one Deployment, Service,
      ServiceAccount, Role, RoleBinding, ConfigMap, image tag overridden to
      `dev`, `TURNIP_REDIS_ADDR`/`TURNIP_GITHUB_APP_ID` merged in.

- [x] 5. Package the Grafana dashboard component
  - [x] 5.1 Create `dashboards/kustomization.yaml` and `deploy/components/grafana-dashboard/kustomization.yaml`
    - `dashboards/kustomization.yaml`: `configMapGenerator:` one generator
      named `turnip-grafana-dashboard`, `files: [turnip.json]` (the
      `metrics` slice's existing file, packaged verbatim, not redefined),
      labeled `grafana_dashboard: "1"` via `options.labels` — placed here
      rather than under `deploy/` because Kustomize sandboxes a
      `configMapGenerator`'s `files:` source to its own directory
      (discovered during implementation; see design.md's amended note)
    - `deploy/components/grafana-dashboard/kustomization.yaml`:
      `apiVersion: kustomize.config.k8s.io/v1alpha1`, `kind: Component`,
      `resources: [../../../dashboards]` — pulls in the already-generated,
      already-labeled ConfigMap; a `resources:` entry isn't subject to the
      same sandboxing as a generator's `files:` source
    - _Requirements: 1.6_
  - [x] 5.2 Verify with `kubectl kustomize` against a scratch overlay
    - A throwaway `kustomization.yaml` with
      `resources: [../../base]` + `components: [../../components/grafana-dashboard]`
      builds cleanly and includes the labeled ConfigMap. Confirmed.

- [x] 6. Checkpoint - Manifest validation in CI
  - [x] 6.1 Add a step to `.github/workflows/ci.yml`
    - After the existing `Proto freshness` step: install `kubectl`
      (`azure/setup-kubectl@829323503d1be3d00ca8346e5391ca0b07a9ab0d # v5.1.0`)
      for `kubectl kustomize`, then run `kubectl kustomize
      deploy/overlays/kind | go run
      github.com/yannh/kubeconform/cmd/kubeconform@latest -strict -summary`
    - Switched from the originally planned `kubectl apply --dry-run=client
      -f -` to `kubeconform` — confirmed during implementation that
      `kubectl apply`'s REST discovery contacts the API server
      unconditionally, even under `--dry-run=client`, so it cannot run
      without a real reachable cluster at all (see design.md's amended
      Testing Strategy note). `kubeconform` validates against Kubernetes'
      published OpenAPI schemas offline, which is what "without needing a
      real cluster" actually requires; task 4/5's real-cluster validation
      stays `ha-validation`'s job either way
    - _Requirements: 1.1, 1.2, 1.4, 1.5_
  - [x] 6.2 Run the same command locally to confirm it passes before relying on CI
    - Confirmed: 6 resources valid, 0 invalid. Also confirmed kubeconform
      catches a real error (a deliberately broken test manifest with a
      malformed `replicas` field and a missing `containers` list failed
      validation as expected).

- [x] 7. `goreleaser` config and release workflow (Decision 2)
  - [x] 7.1 Create `.goreleaser.yaml`
    - `builds:` two entries (`server`, `runner`), `main: ./cmd/server` /
      `./cmd/runner`, `env: [CGO_ENABLED=0]`, `goos: [linux]`,
      `goarch: [amd64, arm64]`, `ldflags: ["-s -w"]` — matching the
      existing Dockerfiles' own build flags
    - `dockers:` two entries (`turnip-server`, `turnip-runner`), each
      `use: buildx`, `dockerfile: build/server/Dockerfile` /
      `build/runner/Dockerfile` (existing, Slice 0, multi-stage
      source-based builds — reused as-is rather than switched to
      goreleaser's prebuilt-binary-`COPY` convention, since the
      Dockerfiles already do their own `go build` and need no
      goreleaser-supplied artifact), `image_templates:
      ["ghcr.io/ivanvc/turnip-server:{{ .Tag }}",
      "ghcr.io/ivanvc/turnip-server:latest"]` (and the `-runner`
      equivalent), `build_flag_templates` for OCI labels. Since the
      Dockerfiles' own `COPY . .` needs the full source tree, and
      goreleaser's per-image build context otherwise contains only the
      matched `builds:` artifact, each entry also sets `extra_files:
      [go.mod, go.sum, cmd, internal, proto]` — discovered necessary by
      running the config, not something the design sketch called out
    - `checksum:`/`changelog:` sections using goreleaser's conventional-
      commit defaults (Requirement 2.2's "changelog generation" point)
    - `goreleaser check` flags `dockers:`/`docker_manifests:` as
      soft-deprecated in favor of `dockers_v2` — left as `dockers:` since
      it's still fully functional and better documented; not a blocker
    - _Requirements: 2.1, 2.2, 2.3_
  - [x] 7.2 Create `.github/workflows/release.yml`
    - Trigger: `push: tags: ["v*"]`
    - Steps: checkout (fetch-depth 0 for changelog), `docker/setup-buildx-action`,
      `docker/login-action` against `ghcr.io` using `secrets.GITHUB_TOKEN`,
      `goreleaser/goreleaser-action@f06c13b6b1a9625abc9e6e439d9c05a8f2190e94 # v7.2.3`
      with `args: release --clean`
    - _Requirements: 2.1, 2.3_
  - [x] 7.3 Checkpoint - Verify the config without publishing
    - `goreleaser release --snapshot --clean --skip=publish` (goreleaser
      v2.17.1, already installed locally) succeeded end-to-end: both
      binaries cross-compiled (linux/amd64, linux/arm64) and both Docker
      images built. Confirmed `docker run
      ghcr.io/ivanvc/turnip-server:v0.0.0` starts and fails with the
      expected "missing required environment variable(s)" error — proof
      the image's binary is the real one, not a stub. Snapshot images and
      `dist/` cleaned up afterward, nothing committed.

- [x] 8. Final checkpoint - Full verification
  - Confirmed: `go build ./...` succeeds, `go test -race ./...` passes
    repo-wide (all packages `ok`), `gofmt -l .` is empty, `go mod tidy`
    produces no diff, and the real golangci-lint v2 passes with 0 issues
    (`go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
    run ./...`). Re-ran `kubectl kustomize deploy/overlays/kind | go run
    github.com/yannh/kubeconform/cmd/kubeconform@latest -strict -summary`
    (6/6 valid) and `goreleaser release --snapshot --clean --skip=publish`
    (succeeded, both images built) together as a final pass; snapshot
    artifacts and images cleaned up afterward — `git status` shows no
    stray files.

- [x] 9. Amendment - Drop the `RunnerImage` backward-compatibility fallback
  - Tasks 1.1/1.3 originally gave `internal/jobs.BuildJob` a fallback to a
    hardcoded `runnerImage` constant when `OperationParams.RunnerImage` was
    unset, framed as backward compatibility. Turnip has no released
    version and no external caller of this code yet, so that framing was
    unearned complexity — removed per user feedback.
  - [x] 9.1 `internal/jobs/build.go`: delete the `runnerImage` constant and
    the fallback branch; the Runner container's `Image` is `op.RunnerImage`
    directly. `OperationParams.RunnerImage`'s doc comment no longer
    mentions a fallback.
  - [x] 9.2 `internal/jobs/build_test.go`: `testParams()` now sets a
    default `RunnerImage`; deleted `TestBuildJob_RunnerImageFallsBackToDefault`
    (no fallback left to test).
  - [x] 9.3 `internal/orchestrator/config.go`: `TURNIP_RUNNER_IMAGE` moved
    into `ConfigFromEnv`'s required/`missing` list, same as every other
    required value.
  - [x] 9.4 `internal/orchestrator/config_test.go`: `envMap`'s defaults
    gained `TURNIP_RUNNER_IMAGE`; deleted
    `TestConfigFromEnv_RunnerImageDefaultsEmpty` (no longer a valid state);
    added `TestConfigFromEnv_MissingRunnerImage`.
  - [x] 9.5 `deploy/base/kustomization.yaml`: `TURNIP_RUNNER_IMAGE`'s
    `configMapGenerator` literal changed from empty (relying on the code
    fallback) to a real default
    (`ghcr.io/ivanvc/turnip-runner:latest`, matching the Server image's own
    default tag) — an overlay still overrides it, just with no code-level
    escape hatch backing it up.
  - [x] 9.6 `design.md` amended in place: Decision 3, the Testing Strategy
    bullet, and the Backward Compatibility section all rewritten to drop
    the fallback framing.
  - [x] 9.7 Re-verified: `go build ./...`, `go test -race ./...`,
    `gofmt -l .` (empty), `go mod tidy` (no diff), golangci-lint v2
    (0 issues), and `kubectl kustomize deploy/overlays/kind` confirmed
    `TURNIP_RUNNER_IMAGE` resolves to the real default value end-to-end.

- [x] 10. Amendment - Downloadable per-release install manifest (Decision 4)
  - User feedback: `docs/deployment.md` was telling operators to
    hand-copy the local-testing `kind` overlay as if it were a production
    starting point, instead of shipping a ready-to-apply release
    artifact.
  - [x] 10.1 Create `deploy/overlays/release/kustomization.yaml`
    - `deploy/base` + `images:`/`TURNIP_RUNNER_IMAGE` overrides, both
      left as literal `:latest` placeholders for CI to substitute
    - Verified: `kubectl kustomize deploy/overlays/release` builds
      cleanly with no `TURNIP_REDIS_ADDR`/`TURNIP_GITHUB_APP_ID` in the
      rendered ConfigMap (as intended — Requirement 1.3)
    - _Requirements: 1.2, 1.3_
  - [x] 10.2 Amend `.github/workflows/release.yml`
    - New "Render install manifest" step (needs `kubectl`, so
      `azure/setup-kubectl` added here too), running *before* the
      `goreleaser-action` step: `sed`-substitutes the two `:latest`
      placeholders for `${GITHUB_REF_NAME}`, then `kubectl kustomize
      deploy/overlays/release > dist-manifests/turnip-install.yaml`
    - Discovered by testing: a single `sed 's/:latest/:$TAG/'` pattern
      only matches the `TURNIP_RUNNER_IMAGE` literal
      (`turnip-runner:latest`, no space) — it silently misses the
      `images:` transformer's `newTag: latest` (space after the colon),
      leaving the Server's own image tag un-substituted. Needed two
      separate `sed` rules; caught by actually rendering and grepping the
      output, not by config validation alone
    - `dist-manifests/` (not goreleaser's own `dist/`) — goreleaser
      cleans `dist/` at the start of every run, confirmed empirically,
      which would silently delete a file placed there ahead of time
    - _Requirements: 2.1, 2.3_
  - [x] 10.3 Amend `.goreleaser.yaml`
    - Added `release.extra_files: [{glob: dist-manifests/turnip-install.yaml}]`
      — goreleaser's own documented mechanism for attaching arbitrary
      files to a release, no new dependency
    - `goreleaser check` validates the schema; the actual upload only
      happens on a real publish (a live GitHub Release), which
      `--snapshot`/`--skip=publish` both skip — verified as far as this
      environment allows (file exists at the exact glob path by the time
      goreleaser would run; config schema valid) without a real release
    - _Requirements: 2.1, 2.3_
  - [x] 10.4 Add `.gitignore`
    - None existed; added one covering `/bin/`, `/dist/`, `/dist-manifests/`
      — the latter two are new build-artifact directories this amendment
      introduces, and `/bin/` was an existing gap worth closing while
      touching this
  - [x] 10.5 Rewrite `docs/deployment.md`
    - New primary "Installing a release" section:
      `kubectl apply -f https://github.com/ivanvc/turnip/releases/download/vX.Y.Z/turnip-install.yaml`
      (or `/releases/latest/download/...`), then `kubectl set env` for
      `TURNIP_REDIS_ADDR`/`TURNIP_GITHUB_APP_ID` and creating the
      credentials Secret
    - `deploy/overlays/kind/` demoted to a "Local development" section,
      explicitly not a production starting point; hand-maintaining a live
      overlay demoted to an opt-in "Alternative" section (kept, not
      deleted — legitimate for GitOps users), pointing at
      `deploy/overlays/release/` as its starting point, with the
      "values an overlay must supply" reference table moved there
    - Verified the `kubectl apply -k https://github.com/...?ref=...`
      remote-Kustomize syntax against a known-public repo
      (`kubernetes-sigs/kustomize`) before publishing it, rather than
      guessing the URL form
    - Confirmed the public repo really is `github.com/ivanvc/turnip`
      (matching the Go module path and every `ghcr.io` image name)
      despite this working copy's `git remote origin` pointing at a
      differently-named repo — asked rather than guessed, given
      contradicting evidence
    - _Requirements: 2.1, 2.2_

- [x] 11. Amendment - Declarative Kustomize install, replacing imperative post-apply steps
  - User feedback, pointing at `deploy/base/deployment.yaml`'s
    `secretKeyRef`/`configMapRef` lines: task 10's "Quick install" flow
    still told operators to `kubectl set env`/`kubectl create secret`
    *after* applying — imperative patch-on-top, not a declarative install.
    Asked for the pattern
    [nfs-subdir-external-provisioner uses](https://github.com/kubernetes-sigs/nfs-subdir-external-provisioner#with-kustomize):
    a user-authored `kustomization.yaml` referencing the project's base as
    a remote resource, generating everything (including the credentials
    Secret) in one `kubectl apply -k .`.
  - [x] 11.1 Verified the exact mechanics before documenting them
    - Confirmed `kubectl kustomize`'s double-slash remote-base syntax
      (`github.com/OWNER/REPO//path?ref=TAG`) against a known-public repo
    - Built and rendered the full worked example locally (a scratch
      overlay referencing `deploy/base` with `configMapGenerator`
      `behavior: merge` + a `secretGenerator` using `files:` for both the
      webhook secret and the private key) — confirmed Kustomize's
      generator-hash-suffix mechanism automatically rewrites the base's
      static `secretKeyRef`/`configMapRef` names to match what gets
      generated, so nothing needs hand-patching; validated the rendered
      output with `kubeconform` (7/7 resources valid, including the new
      Secret)
  - [x] 11.2 Rewrote `docs/deployment.md`'s install section
    - New `## Installing turnip` with two subsections: `### With
      Kustomize (recommended)` (the four-step worked example above,
      crediting nfs-subdir-external-provisioner's docs as the pattern
      this follows) and `### Quick install (single file)` (task 10's
      original `turnip-install.yaml` + imperative-command flow, kept as a
      documented option for anyone who'd rather not write Kustomize, but
      no longer the primary recommendation)
    - Removed the now-redundant "Alternative: maintain your own overlay"
      section entirely (task 10.5) — superseded by the new primary
      section, which covers the same ground more thoroughly
    - _Requirements: 2.1, 2.2_
  - [x] 11.3 Fixed the now-stale cross-reference inside
    `deploy/overlays/release/kustomization.yaml`'s own comment (pointed at
    the deleted "Alternative" section) and `docs/configuration.md`'s
    cross-reference (pointed at the renamed "Installing a release"
    heading)

## Notes

- No new Go dependency beyond what's already in `go.mod` — `goreleaser` is
  a CI-installed binary, never imported.
- `deploy/base` never contains an `images:` transformer or any secret
  material — every environment-specific value is overlay-level
  (Requirement 1.2), enforced by task 3.5 shipping no `TURNIP_REDIS_ADDR`
  default at all rather than a placeholder that could be forgotten.
- Task 1's `New(...)` arity change (task 1.5) has exactly one test call
  site (`orchestrator_test.go`, task 1.6) and one production call site
  (`cmd/server/main.go`, task 1.8) — both land together so the tree never
  sits in a non-compiling state between them.
- Actually applying `deploy/overlays/kind` to a real `kind` cluster is
  `ha-validation`'s concern (Slice 11) — tasks 4.2/5.2/6 here only prove
  the manifests are well-formed and internally consistent via `kubectl
  kustomize`/`kubeconform`, never a live cluster.
- The Grafana dashboard's generator lives at `dashboards/kustomization.yaml`,
  not under `deploy/`, and CI validates manifests with `kubeconform` rather
  than `kubectl apply --dry-run=client` — both are deviations from the
  original design.md sketch, discovered during implementation and now
  reflected in design.md's amended notes (see Decision 1's Grafana-dashboard
  paragraph and the Testing Strategy section).

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1.1", "1.3"] },
    { "id": 1, "tasks": ["1.2", "1.4", "1.5", "1.7"] },
    { "id": 2, "tasks": ["1.6", "1.8"] },
    { "id": 3, "tasks": ["3.1", "3.2", "3.3", "3.4"] },
    { "id": 4, "tasks": ["3.5", "7.1"] },
    { "id": 5, "tasks": ["4.1", "5.1", "7.2"] },
    { "id": 6, "tasks": ["4.2", "5.2", "7.3"] },
    { "id": 7, "tasks": ["6.1"] },
    { "id": 8, "tasks": ["6.2"] }
  ]
}
```
