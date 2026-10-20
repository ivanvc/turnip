# Tasks

**The checkpoint that matters is task 4.1.** The render tests prove
what Kustomize produces, not that Kubernetes accepts it: that the
RoleBinding in one namespace really grants a ServiceAccount from
another, that `$(POD_NAMESPACE)` expands in the Server's environment,
and that a Runner-namespace Pod resolves the Server's full name. Only a
real cluster shows those, and `make test-kind` builds one.

## 1. The setting and its default (Server)

- [x] 1.1 `internal/orchestrator` configuration reads
  `TURNIP_RUNNER_NAMESPACE` into a field renamed `RunnerNamespace`,
  drops it from the required settings, and no longer reads
  `TURNIP_K8S_NAMESPACE`; an unset value stays empty (design Decision
  5). `docs/operating/server-settings.md` replaces the
  `TURNIP_K8S_NAMESPACE` row with a `TURNIP_RUNNER_NAMESPACE` row
  (optional; default the Server's own namespace; preset `turnip-runners`
  by `deploy/base`) and rewrites the paragraph about installing outside
  `default` to point at the deployment page's namespace section. Verify:
  `config_test.go` covers the value read, unset leaving it empty with
  no error, and `TURNIP_K8S_NAMESPACE` alone being ignored; the
  settings Reference test passes
  - _Deltas: server (Settings loader, Settings index)_
- [x] 1.2 `cmd/server` fills an empty `RunnerNamespace` from the
  namespace of the same `clientcmd` deferred loading config it builds
  its client from, before passing it to `jobs.NewClient` and
  `runnerauth.New`; a failure to resolve it stops startup with an error
  naming `TURNIP_RUNNER_NAMESPACE`. The `server starting` log line's
  `namespace` field becomes `runner_namespace` (the resolved value) and
  gains `runner_server_addr` (design Decision 4). Verify: unit tests
  with a temporary `KUBECONFIG` (a context naming namespace `dev`
  resolves `dev`; a context naming none resolves `default`; a set
  value is used as is without consulting the loader)
  - _Deltas: server (Settings loader), runner-job (One Job per
    Operation), runner-security (Every RPC is authenticated and bound to
    one Operation)_

## 2. Manifests

- [x] 2.1 `deploy/transformers/namespaces` (a kustomization whose one
  resource is the `ReplacementTransformer` of design Decision 1), and
  `deploy/base`: `namespace: turnip-system`, the transformer under
  `transformers:`, `TURNIP_RUNNER_NAMESPACE=turnip-runners` in the
  ConfigMap with no `TURNIP_RUNNER_SERVER_ADDR` there, the Deployment's
  `env` gaining `POD_NAMESPACE` (Downward API) followed by
  `TURNIP_RUNNER_SERVER_ADDR=turnip-server.$(POD_NAMESPACE).svc:9090`
  (Decision 3), the binding subjects naming `turnip-system`, and the
  ClusterRoleBinding's comment corrected (Kustomize does not rename
  that subject; the transformer does). New comments explain why the
  transformer exists and why an operator lists it again. Verify: a new
  Go test package `test/manifests`, skipped when `kubectl` is not on
  `PATH`, renders `deploy/base`, `deploy/overlays/release` and two
  fixtures under `test/manifests/testdata` (the documented operator
  kustomization of Decision 2 with local paths; and one renaming the
  namespaces to `ops` and `ops-runners`), and checks each resource's
  namespace and the subjects against design's "Where each resource
  lands" table, the ConfigMap holding no address, and the two `env`
  entries in order; `make manifests-check` passes
  - _Deltas: deployment (Kustomize base, Server RBAC, Overlays and
    release manifest)_
- [x] 2.2 `make test-kind` and its tests use two namespaces: the
  Makefile creates `turnip-system` and `turnip-runners`, puts the
  Secret and `test/load/redis.yaml` in `turnip-system`, and waits on
  the Deployment there; the kind overlay's comment shows the Secret
  created with `-n turnip-system`. The tests read
  `TURNIP_TEST_SERVER_NAMESPACE` and `TURNIP_TEST_RUNNER_NAMESPACE`
  (defaults `turnip-system` and `turnip-runners`) in place of
  `TURNIP_TEST_NAMESPACE`, and check: the Server can create Jobs and get
  Pods in the Runner namespace; it can do neither in the Server
  namespace; it cannot get or list Secrets in either; a Pod in the
  Runner namespace reaches `turnip-server.turnip-system.svc:9090`; the
  Server's startup log reports `runner_namespace=turnip-runners` and
  `runner_server_addr=turnip-server.turnip-system.svc:9090`; and the
  repository Secret test creates its Secret in the Runner namespace.
  Verify: `go vet -tags kind ./test/load/...` compiles; the run itself
  is task 4.1
  - _Deltas: deployment (Server RBAC), runner-access (The Runner
    namespace holds nothing of the Server's, The repository's Secret)_

## 3. Operator documentation

- [x] 3.1 `docs/operating/deployment.md`: "Choosing a namespace"
  becomes the two namespaces: why they are separate (a Runner's grant
  in its namespace no longer reaches `turnip-github-app` or the Server
  Pod); that the operator creates both first; the operator
  kustomization of design Decision 2 with both its `namespace:` and
  transformer lines presented as required, and why; renaming either
  namespace; that the Runner's address follows the Server namespace
  and is changed by patching the Deployment's `env`, not the ConfigMap;
  and that leaving `TURNIP_RUNNER_NAMESPACE` unset (outside the base)
  puts Runners beside the Server and what that exposes. Quick install
  creates both namespaces and runs `kubectl set env` with
  `-n turnip-system`. `docs/operating/troubleshooting.md` gains a
  Forbidden error on Job creation, pointing at a missing transformer
  line or a mismatched `TURNIP_RUNNER_NAMESPACE`. Verify:
  `make docs-check`; the documented kustomization matches the fixture
  of task 2.1 apart from the remote paths
  - _Deltas: deployment (Kustomize base, Overlays and release
    manifest), runner-access (The Runner namespace holds nothing of the
    Server's)_
- [x] 3.2 `docs/operating/runner-access.md` and
  `docs/getting-started/operators.md` name the Runner namespace and
  `TURNIP_RUNNER_NAMESPACE`: the Runner ServiceAccount, its grants and
  repository Secrets are created in the Runner namespace; "Grants that
  reach the GitHub credential" now says separation removes that
  exposure, which returns only when the two namespaces are the same.
  `docs/plugins/helmfile.md`'s RBAC example names the Runner namespace.
  Verify: `make docs-check`; `git grep TURNIP_K8S_NAMESPACE` finds it
  only under `openspec/` (changes, and the capability specs until this
  change is archived) and in the `config_test.go` case proving the old
  name is ignored
  - _Deltas: runner-access (The Runner namespace holds nothing of the
    Server's, The repository's Secret)_

## 4. Checkpoint: a real cluster

- [x] 4.1 `make test-kind` passes. A failure here that the render tests
  did not catch is recorded below with its output, and the render test
  that should have caught it is added

## 5. Final checkpoint

- [x] 5.1 `go build ./...`, `gofmt -l ./internal ./cmd ./test` clean,
  `go vet ./...`, `make test`, `make lint`, `make manifests-check` pass
- [x] 5.2 Every scenario in the deltas maps to a test or a doc change;
  `openspec validate split-server-runner-namespaces --strict` passes;
  `openspec/roadmap.md` shows the change as implemented
- [x] 5.3 An intermittent failure is recorded here with its full output,
  not rerun away

## 6. Revision (2026-10-19): one namespace by default

The base preset `TURNIP_RUNNER_NAMESPACE=turnip-runners`, so a first
install failed until a second namespace existed. The owner asked for
the defaults alone to run turnip; proposal, specs and design were
revised first (design Decisions 1 and 2).

- [x] 6.1 `deploy/base` drops the preset and lists no transformer;
  `deploy/transformers/namespaces` keeps only the subject replacement;
  a new `deploy/transformers/runner-namespace` includes it and moves the
  Role and RoleBinding to `TURNIP_RUNNER_NAMESPACE`; the kind overlay
  separates `turnip-runners` through it. Verify: the render test covers
  base and release (one namespace, no `TURNIP_RUNNER_NAMESPACE`), kind,
  and fixtures `operator`, `renamed` (Server namespace renamed, Runners
  beside it) and `separated`; `make manifests-check`
  - _Deltas: deployment (Kustomize base, Server RBAC, Overlays and
    release manifest)_
- [x] 6.2 The operator docs describe one namespace by default, renaming
  with `namespaces`, and separating (recommended) with
  `runner-namespace`, with what sharing exposes. Verify:
  `make docs-check`; the documented kustomizations match the fixtures
  - _Deltas: deployment (Kustomize base), runner-access (The Runner
    namespace holds nothing of the Server's)_
- [x] 6.3 `make test-kind` passes with no check skipped, then the final
  checkpoint (5.1, 5.2) is rerun

## Repository owner

- Review and commit
- In the pilot cluster, to separate the Runner namespace: create it,
  move into it the Runner ServiceAccount, its grants (including the
  cloud identity binding it uses) and each repository's
  `turnip-repository-<id>` Secret, merge `TURNIP_RUNNER_NAMESPACE`, and
  list `deploy/transformers/runner-namespace` in the kustomization
  (or, staying in one namespace, list `deploy/transformers/namespaces`
  and move nothing)
- Deploy the Server through Helmfile, and check the rendered Role and
  RoleBinding land where intended with subjects in the Server namespace
  (design Risks: chartify's release namespace); then run a plan
- Once a plan runs, archive this change

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1.1", "2.1", "3.1", "3.2"] },
    { "id": 1, "tasks": ["1.2", "2.2"] },
    { "id": 2, "tasks": ["4.1"] },
    { "id": 3, "tasks": ["5.1", "5.2", "5.3"] },
    { "id": 4, "tasks": ["6.1", "6.2"] },
    { "id": 5, "tasks": ["6.3"] }
  ]
}
```

## Deviations recorded at implementation

- 2026-10-17: Task 3.2's check reworded. As written it could not pass:
  `openspec/specs/` keeps the old name until this change is archived,
  and task 1.1's own test of the ignored old name has to set it.
- 2026-10-17: Task 2.1 renders five directories, not four: the kind
  overlay was added. `POD_NAMESPACE` and the address are the first two
  `env` entries, ahead of the Secret references.
- 2026-10-17: Task 1.1 links the settings rows to
  `deployment.md#choosing-namespaces`, the heading task 3.1 created
  (it was bold text before, with no anchor).
- 2026-10-17: Task 3.2 also rewrote the "turnip's own Secret" bullet in
  `runner-access.md`, which said "until split-server-runner-namespaces
  lands", and the namespace step of `operators.md`.
- 2026-10-17: Review fixes. (1) The `TURNIP_RUNNER_SERVER_ADDR` row of
  `server-settings.md` still described a ConfigMap preset; it now says
  the Deployment's `env` sets it and that a ConfigMap value is ignored.
  (2) The docs said a missing transformer line leaves the RoleBinding
  subject stale; a render shows that only happens when the Server
  namespace is renamed (then token reviews fail too), and both
  `deployment.md` and `troubleshooting.md` now say so. (3) The operator
  and renamed fixtures gained a `secretGenerator`, and the render test
  checks the Secret lands in the Server namespace and that the
  Deployment references it. (4) `kubectl scale` in "Scaling up" and
  `kubectl set env` in the release overlay's comment gained
  `-n turnip-system`.
- 2026-10-19: Found at the pilot deploy. Helmfile's chartify builds a
  Kustomize chart in two passes: it renders the base to a file, then
  applies the release's `transformers:` to that file. By the second pass
  the ConfigMap is already `turnip-server-config-<hash>`, so
  `runner-namespace` fails with "nothing selected by
  ConfigMap/turnip-server-config:data.TURNIP_RUNNER_NAMESPACE"
  (reproduced locally), and a later edit to the ConfigMap no longer
  changes its name, so the Server does not restart on it. The design's
  Risks entry on chartify is confirmed. The pilot works around it with
  patches selected by regex name and a direct namespace patch on the
  Role and RoleBinding; a supported Helmfile path belongs to
  `fix-single-config-surface`.
