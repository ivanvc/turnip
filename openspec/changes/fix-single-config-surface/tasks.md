# Tasks

**The checkpoint that matters is task 4.1.** The render tests prove what
Kustomize produces, not that Kubernetes accepts it: that a pod mounting
the whole `turnip-server-secrets` Secret and reading two keys from it into its
environment starts, that `enableServiceLinks: false` leaves no
`TURNIP_*` Service variables behind, and that the Server then logs no
`unknown settings` warning. Only a real cluster shows those, and
`make test-kind` builds one.

After wave 2, a review on the main model reads the whole change against
its deltas and design, and a fix pass applies what it finds, before the
checkpoint.

## 1. Manifests

- [x] 1.1 `deploy/base`: the `turnip-server-config` generator gets
  `options.disableNameSuffixHash: true` (design Decision 1); the
  Deployment sets `enableServiceLinks: false`, reads both `secretKeyRef`
  entries from `turnip-server-secrets` in place of `turnip-github-app`, and
  mounts the whole Secret read-only at `/etc/turnip/secrets` (Decisions
  2 and 3), with a comment where it references the Secret stating its
  name, its two required keys, and the mount for the rest. The kind and
  release overlays' comments name `turnip-server-secrets`, and the release
  overlay's comment shows setting values in the ConfigMap and
  restarting rather than `kubectl set env`. Verify: `test/manifests`
  checks, for the base and both overlays, the fixed ConfigMap name,
  `enableServiceLinks` false, the Secret name in both references and the
  volume, and the read-only mount; its fixtures generate `turnip-server-secrets`
  (with a `redis-password` key in one) and check the generated name in
  both references and the volume, and a merged setting keeping the
  fixed name; `make manifests-check`
  - _Deltas: deployment (Kustomize base, Overlays and release manifest)_
- [x] 1.2 The two-pass case of design Decision 5 in `test/manifests`:
  render `deploy/base`, write it into a temporary directory beside a
  kustomization listing it as a resource and applying an
  `ImageTagTransformer`, a `PatchTransformer` setting
  `TURNIP_REDIS_ADDR` and `TURNIP_RUNNER_NAMESPACE=ops-runners` in
  `turnip-server-config`, and the `runner-namespace` replacement read
  from `deploy/transformers/runner-namespace`, then render that. Verify:
  the ConfigMap keeps its name and holds both values, the Deployment
  still references it and runs the new image, and the Role and
  RoleBinding are in `ops-runners`
  - _Deltas: deployment (Kustomize base: "Transformers applied to the
    rendered base", "Choosing the Server image")_
- [x] 1.3 `make test-kind` and its tests: the Makefile creates
  `turnip-server-secrets` with `webhook-secret`, `private-key` and a
  `redis-password` key; the tests check the Server's log has no
  `unknown settings` line. Verify: `go vet -tags kind ./test/load/...`;
  the run itself is task 4.1
  - _Deltas: deployment (Kustomize base: "A Secret without the optional
    keys"), server (Settings index: "Every variable known")_

## 2. The unknown-setting warning (Server)

- [x] 2.1 `internal/orchestrator`: `SettingNames()` as design Decision 4
  states it, in the settings index's order. The docs reference test
  asserts it equals the names it records `ConfigFromEnv` reading, so
  the list, the code and the settings Reference cannot drift. Verify:
  `go test ./internal/orchestrator/ -run Reference`
  - _Deltas: server (Settings index)_
- [x] 2.2 `cmd/server`: once settings load, compare the environment's
  `TURNIP_*` names with `SettingNames()` and log one warning,
  `unknown settings`, naming the rest in order, leaving out the
  Service-variable shapes the `server` delta lists. Verify: unit tests
  for an unknown name (`TURNIP_RUNNER_TIMEOUT`), a setting no longer
  read (`TURNIP_K8S_NAMESPACE`), every name known (no warning), and
  `TURNIP_REDIS_PORT`, `TURNIP_REDIS_SERVICE_HOST` and
  `TURNIP_SERVER_PORT_9090_TCP_ADDR` left out
  - _Deltas: server (Settings index)_

## 3. Documentation

- [x] 3.1 `docs/operating/deployment.md`: the Kustomize steps generate
  `turnip-server-secrets`; a new "Installing with Helmfile" section gives the
  one release of design Decision 5 and the Secret beside it; quick
  install patches the ConfigMap and creates `turnip-server-secrets` instead of
  `kubectl set env`; Redis's password is a key of `turnip-server-secrets` with
  `TURNIP_REDIS_PASSWORD_PATH=/etc/turnip/secrets/redis-password`; and
  every place a setting or secret changes ends with
  `kubectl rollout restart deployment/turnip-server` (Decision 6).
  Verify: `make docs-check`; the Kustomize and Helmfile snippets match
  the fixtures of tasks 1.1 and 1.2 apart from remote refs
  - _Deltas: deployment (Kustomize base, Overlays and release manifest)_
- [x] 3.2 The other pages naming the Secret or a settings change:
  `docs/operating/server-settings.md` (the introduction states the
  restart and the startup warning; the Redis rows point at the mounted
  Secret), `docs/operating/runner-access.md`,
  `docs/development/README.md`, `docs/getting-started/operators.md` and
  `docs/operating/troubleshooting.md` (the `CreateContainerConfigError`
  entry names `turnip-server-secrets`). Verify: `make docs-check`;
  `git grep turnip-github-app` finds it only under `openspec/`
  - _Deltas: deployment (Kustomize base), runner-access (both
    requirements), server (Settings index)_

## 4. Checkpoint: a real cluster

- [x] 4.1 `make test-kind` passes with every check run and none skipped
  (`GOFLAGS="-v -count=1"`). A failure the render tests did not catch is
  recorded below with its output, and the render test that should have
  caught it is added

## 5. Final checkpoint

- [x] 5.1 `go build ./...`, `gofmt -l ./internal ./cmd ./test` clean,
  `go vet ./...`, `make test`, `make lint`, `make manifests-check` pass
- [x] 5.2 Every scenario in the deltas maps to a test or a doc change;
  `openspec validate fix-single-config-surface --strict` passes;
  `openspec/roadmap.md` shows the change as implemented
- [x] 5.3 An intermittent failure is recorded here with its full output,
  not rerun away

## 6. Revision (2026-11-03): no ConfigMap in the base

Proposal Decisions 3 to 5 and design Decisions 1 and 5 to 8 replace the
fixed-name ConfigMap the earlier tasks built. After wave 6, a review on
the main model reads the revision against the deltas and design, and a
fix pass applies what it finds, before the checkpoint.

- [x] 6.1 Manifests: `deploy/base` drops its `configMapGenerator`; the
  Deployment's `env` gains `TURNIP_RUNNER_NAMESPACE: $(POD_NAMESPACE)`
  after `POD_NAMESPACE`, and the comment at its `envFrom` states the
  ConfigMap's contract; `deploy/transformers/runner-namespace` copies
  the Role's namespace to the RoleBinding and to that `env` entry (still
  including `../namespaces`); the kind overlay generates its ConfigMap
  and separates `turnip-runners` with a Role namespace patch under
  `transformers:`; the release overlay sets only the Server image.
  Verify: `test/manifests` checks the base (no ConfigMap or Secret, the
  `env` entry), every fixture generating both objects with the
  generated names wired in, the separated fixture (with `namespace:`)
  and the two-pass case moving the Role, RoleBinding and the Server's
  `TURNIP_RUNNER_NAMESPACE` together; `make manifests-check`
  - _Deltas: deployment (Kustomize base, Overlays and release manifest),
    runner-access (The Runner namespace holds nothing of the Server's)_
- [x] 6.2 Server defaults: `ConfigFromEnv` defaults `TURNIP_HTTP_ADDR`
  and `TURNIP_GRPC_ADDR` to `:8080` and `:9090` and no longer requires
  them; `docs/operating/server-settings.md` updates both rows, says the
  base sets `TURNIP_RUNNER_SERVER_ADDR` and `TURNIP_RUNNER_NAMESPACE`
  itself (a ConfigMap value has no effect), and in its introduction
  when a restart is needed. Verify: `ConfigFromEnv` tests for the
  defaults and the required list; the settings Reference test
  - _Deltas: server (Settings loader, Settings index)_
- [x] 6.3 Operator docs: `docs/operating/deployment.md` (the Kustomize
  steps generate the ConfigMap; "Choosing namespaces" separates with
  the Role patch; the Helmfile recipe as design Decision 5; quick
  install creates both objects; restarts as Decision 8),
  `docs/getting-started/operators.md`, `docs/operating/runner-access.md`
  (separating the Runner namespace), `docs/operating/troubleshooting.md`
  (a missing ConfigMap) and `docs/development/README.md` if it shows
  the old ConfigMap. Verify: `make docs-check`; the snippets match the
  fixtures of task 6.1 apart from remote refs
  - _Deltas: deployment (Kustomize base, Overlays and release manifest)_
- [x] 6.4 `make test-kind` passes with every check run and none
  skipped (`GOFLAGS="-v -count=1"`), the kind overlay now generating
  its ConfigMap and separating the Runner namespace the new way
- [x] 6.5 The pilot: the `jaws` module (settings as a ConfigMap release
  beside the Secret's; the Role namespace patch and the
  `runner-namespace` transformer in the `turnip` release) and the
  private guide in `docs/internal/`, rendered through the two-pass
  build against the revised base
- [x] 6.6 Final checkpoint: `go build ./...`, `gofmt`, `go vet ./...`,
  `make test`, `make lint`, `make manifests-check`; every scenario of
  the revised deltas maps to a test or a doc change;
  `openspec validate fix-single-config-surface --strict` and
  `openspec validate runner-timeouts --strict` pass

## Repository owner

- Review and commit
- In the pilot: rename the GitHub App Secret to `turnip-server-secrets`, adding
  the Redis password as its `redis-password` key; move every setting
  into one patch of `turnip-server-config` and set
  `TURNIP_REDIS_PASSWORD_PATH=/etc/turnip/secrets/redis-password`; drop
  `turnip-server-overrides`, the `envFrom` patch, the Redis Deployment
  patch and the pattern-matched ConfigMap patch; list the
  `runner-namespace` transformer for `turnip-jobs`
- Deploy, check the Server's log has no `unknown settings` line, and
  run a plan; then archive this change

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "name": "Base and settings list", "model": "Sonnet", "effort": "medium", "tasks": ["1.1", "2.1"] },
    { "id": 1, "name": "Two-pass render, kind, warning", "model": "Sonnet", "effort": "medium", "tasks": ["1.2", "1.3", "2.2"] },
    { "id": 2, "name": "Documentation", "model": "Sonnet", "effort": "medium", "tasks": ["3.1", "3.2"] },
    { "id": 3, "name": "Real cluster", "model": "Sonnet", "effort": "low", "tasks": ["4.1"] },
    { "id": 4, "name": "Final checkpoint", "model": "Opus", "effort": "medium", "tasks": ["5.1", "5.2", "5.3"] },
    { "id": 5, "name": "Revision: manifests and defaults", "model": "Sonnet", "effort": "medium", "tasks": ["6.1", "6.2"] },
    { "id": 6, "name": "Revision: docs", "model": "Sonnet", "effort": "medium", "tasks": ["6.3"] },
    { "id": 7, "name": "Revision: real cluster", "model": "Sonnet", "effort": "low", "tasks": ["6.4"] },
    { "id": 8, "name": "Revision: pilot and final checkpoint", "model": "Opus", "effort": "medium", "tasks": ["6.5", "6.6"] }
  ]
}
```

## Deviations recorded at implementation

- 2026-10-20: Task 1.2. Kustomize refuses an absolute path to a
  transformer directory, so the two-pass test copies
  `deploy/transformers/runner-namespace/runner-namespace.yaml` into its
  temporary directory at test time and lists it as a file; it still
  reads what ships.
- 2026-10-20: Task 2.1. `SettingNames()` follows the settings index's
  order, the plain form before its `_PATH` form; the settings
  Reference's row order differs and was not treated as authoritative.
- 2026-10-20: Task 3.2. `troubleshooting.md` had no
  `CreateContainerConfigError` entry; "The Server Pod does not start"
  was added, and the review moved the unknown-settings bullet out of it
  into its own entry, since that warning never stops the Pod.
- 2026-10-20: Review fixes. (1) The Helmfile recipe's `chart:` used
  Kustomize's `//` remote-base form, which Helmfile rejects
  (`repo github.com not found`); it is now Helmfile's
  `git::https://...turnip.git@deploy/base?ref=...` form, rendered with
  helmfile 1.5.2 against a local copy of `deploy/`. (2) A troubleshooting
  bullet had unbalanced backticks and did not quote the kubelet's
  message; rewritten. (3) The unknown-settings bullet moved, as above.
- 2026-10-20: Noted, not changed. The fixer found that a clean clone of
  `https://github.com/ivanvc/turnip.git` is not this checkout's history
  (whose `origin` is `ivanvc/ares`); every remote-base URL in the docs
  assumes the public repository is `ivanvc/turnip`. That predates this
  change and is the owner's to confirm.
- 2026-10-23: The Secret renamed from `turnip-server` to
  `turnip-server-secrets` after implementation, to mirror
  `turnip-server-config` (proposal Decision 2). Renamed only where it
  names the Secret: the Deployment, Service, ServiceAccount, Role and
  RoleBinding keep `turnip-server`. Rechecked with the render tests,
  `make manifests-check`, `make docs-check` and `make test-kind`.
- 2026-11-04: Task 6.1. The Role patch is a JSON 6902 `replace` of
  `/metadata/namespace`; a strategic merge patch naming the new
  namespace does not select the Role. The kind overlay gained
  `namespace: turnip-system` (its generated ConfigMap otherwise had
  none) and `TURNIP_RUNNER_IMAGE` for the `:dev` Runner image it loads,
  since the base's `:latest` placeholder is gone. The separated fixture
  carries its own copy of the Role patch.
- 2026-11-04: Task 6.2. `DefaultHTTPAddr` and `DefaultGRPCAddr` are
  exported constants.
- 2026-11-04: Task 6.3. The Helmfile recipe's Secret release reads its
  values with `requiredEnv`; `docs/development/README.md` needed no
  change.
- 2026-11-04: Review fixes. (1) The Helmfile recipe used templates in a
  file named `helmfile.yaml`, which Helmfile v1 does not render; it is
  now `helmfile.yaml.gotmpl`, checked with helmfile 1.8.1. (2) The
  settings Reference still described base presets; `TURNIP_RUNNER_IMAGE`
  is plainly required, and the ConfigMap is "the one you create". (3)
  The deployment page's wrong-transformer cases were rendered and
  corrected: a Role patch with `namespaces` leaves the RoleBinding and
  the Server's namespace behind (Forbidden naming the Server
  namespace); a Role patch under `patches:` does not fail at all, and
  Runners silently run beside the Server (a troubleshooting entry says
  so). (4) The two-pass test's `TURNIP_RUNNER_NAMESPACE` check could
  pass vacuously; it now requires the entry. (5) Leftover "merge into
  `turnip-server-config`" wording, the Secret missing from "Choosing
  namespaces", an old Runner-namespace comment in the Helmfile page,
  and a now-dead `turnip-runner:latest` substitution in
  `.github/workflows/release.yml`.
- 2026-11-04: Task 6.5. The pilot already ran the first version
  (`jaws` `5907a3f7`, turnip `2ae354e`), so its ConfigMap belongs to the
  `turnip` Helm release. The private guide hands it to the new
  `turnip-server-config` release with a `kubectl annotate` before the
  sync (release name, and `helm.sh/resource-policy: keep` so the
  `turnip` release's upgrade does not delete it), and drops the keep
  annotation after.

