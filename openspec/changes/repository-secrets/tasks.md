# Tasks

**The checkpoint that matters is task 5.1.** Unit tests on the Pod spec
can all pass while the kubelet does something else with it. Only a real
cluster shows that a non-root process can read a listed key, that an
unlisted key is not there, and that a missing key keeps the Pod
`Pending` without starting any container, which is what the start
timeout's hint relies on. It also answers the design's open question
(the waiting reason such a Pod reports). `make test-kind` runs it
against a kind cluster.

Wave 0 adds the field, the repository id and the Secret's name side by
side; wave 1 builds on the field (merge, validation, the volume) and
writes the documentation; waves 2 and 3 wire the id through the
orchestrator, then add the hint, which needs the id on the Operation
Record.

## 1. The field (`internal/config`)

- [x] 1.1 `RunnerSpec.Secrets`: a list type whose only zero value is
  absent, so `secrets: []` stays distinct from an absent list through
  YAML marshaling and the Operation Record's JSON; verify with parse
  tests for absent, empty and non-empty lists at both levels
- [x] 1.2 A merge rule for replaced lists that copies the winning list,
  and its `runnerMergeRules` entry (`runner.go`, `runner_test.go`); verify
  `TestRunnerMergeRules_CoverEveryField` passes, a test shows two
  Projects inheriting the top-level list do not share it, and a rapid
  property (tagged `Feature: repository-secrets`) shows the effective
  list is the Project's when present (even empty), else the top level's
  - _Deltas: configuration (Runner settings at two levels)_
- [x] 1.3 Key validation: the character set, the 253 limit, `.` and a
  leading `..`, and a key listed twice, with the spec's messages, and a
  top-level problem reported once against `turnip.yaml` (`validate.go`,
  `validate_test.go`); verify table
  tests covering the spec's scenarios, and a test checking the rule
  against `k8s.io/apimachinery`'s `IsConfigMapKey` over a corpus and a
  rapid property (a test-only import)
  - _Deltas: configuration (Repository Secret keys)_
- [x] 1.4 The configuration round-trip property generates absent, empty
  and non-empty `secrets` lists at both levels (`property_test.go`);
  verify
  `TestProperty_ConfigurationRoundTrip` passes
- [x] 1.5 `docs/using/turnip-yaml.md`: the `runner.secrets` Reference
  row, and a `secrets` bullet under "`runner`: where it runs" (keys of
  the repository's Secret, the files under `/turnip/repository-secrets`,
  pointing `runner.env` at one, the replace rule and `[]`, that every
  listed key is readable by any pull request's plan, and a link to the
  operator page); verify `TestTurnipYAMLReference_MatchesSchema` and
  `make docs-check` pass

## 2. The repository id (`internal/github`)

- [x] 2.1 `Repository.ID`, read from the payload of `pull_request` and
  `issue_comment` events; verify webhook tests assert the id for both

## 3. The Pod (`internal/jobs`) and the operator documentation

- [x] 3.1 `RepositorySecretName(repositoryID int64) string` and
  `OperationParams.RepositoryID`; verify a unit test of the name
- [x] 3.2 `BuildJob` adds the `repository-secrets` volume (items exactly
  the listed keys, each at a path equal to its key, mode `0444`, not
  optional) mounted read-only at `/turnip/repository-secrets` in the
  `runner` container only, under both provisioning strategies; no volume
  for an absent or empty list; keys listed with a zero repository id
  return an error; verify unit tests for each, including that no init
  container mounts it
  - _Deltas: runner-job (Repository Secret keys as files, Fixed paths
    under /turnip)_
- [x] 3.3 `docs/operating/runner-access.md`: the "Cloud credentials"
  section rewritten around the three routes (the ServiceAccount's
  identity, the repository's Secret, what the tool's committed
  configuration reaches), with no claim that access comes only through
  the ServiceAccount; a section on the repository's Secret (its name
  from the repository id and how to look the id up, the Runner
  namespace, creating it with `kubectl create secret generic`, keys as
  files, Kubernetes' 1 MiB Secret limit, no Server setting); the warning
  that a Runner ServiceAccount allowed to read or create Secrets in that
  namespace reaches every repository's Secret; the note that the
  namespace also holds turnip's own Secret until
  `split-server-runner-namespaces`; verify `make docs-check`
  - _Deltas: runner-access (The repository's Secret, Access turnip does
    not provide)_
- [x] 3.4 `docs/plugins/helmfile.md`, beside the existing hook warning:
  a mounted key is readable during `diff` through a `prepare` or
  `cleanup` hook and through `exec` or `readFile` templating, and can be
  printed into the results comment (read by everyone who can read the
  repository) and the Runner's log; blocking hooks does not close it;
  select the context with `kubeContext` and never write to a mounted
  kubeconfig, which is read-only; verify `make docs-check`
- [x] 3.5 `SECURITY.md`: the repository's Secret in the opt-ins table
  (creating the Secret is the opt-in, no Server setting; any
  collaborator's pull request can read every listed key at plan time);
  verify `make docs-check`

## 4. Wiring and the hint (`internal/orchestrator`)

- [x] 4.1 The repository id carried from the event to dispatch, written
  to the Operation Record and passed to `BuildJob`; keys listed with a
  zero id refuse the Operation through the `Runner Job could not be
  built` path; verify orchestrator tests: the Job built for a Project
  with keys mounts from `turnip-repository-<id>`, and the refusal
- [x] 4.2 The sweep's hint: when the record's Project lists keys and
  the Pod is `Pending`, the Project's output is the diagnostic, a
  newline, and the spec's hint line; the check's title is the diagnostic
  alone; verify tests for a `Pending` Pod with keys, a `Pending` Pod
  without keys, and a Pod in another phase with keys
  - _Deltas: operation-lifecycle (Start timeout and sweep)_
- [x] 4.3 `docs/operating/troubleshooting.md`: the hint, what it means,
  and how to list a Secret's keys without printing their values; verify
  `make docs-check`

## 5. Checkpoint: the kubelet mounts what the Pod spec says

- [x] 5.1 A kind test in `test/load` (tag `kind`, run by
  `make test-kind`): a Secret with two keys; a Pod carrying the volume
  and `runner` mount taken from `BuildJob`'s output for a Project listing
  one of them, running as a non-root user, reads the listed key and finds
  no file for the other; a Pod listing a key the Secret lacks stays
  `Pending` for 60 seconds without starting any container; the test logs
  the waiting reason that Pod reports
- [x] 5.2 The waiting reason from 5.1 is recorded as the answer in
  `design.md`'s Open Questions, and in the troubleshooting page if it
  names one; verify `make docs-check`

## 6. Final checkpoint

- [x] 6.1 `go build ./...`, `gofmt -l ./internal ./cmd ./test` clean,
  `go vet ./...`, `make test`, `make lint` and `make test-kind` pass
- [x] 6.2 Every scenario in the four deltas maps to a test or a doc
  change; `openspec validate repository-secrets --strict` passes; no
  changed file names the pilot repository
- [x] 6.3 An intermittent failure is recorded with full output, not
  rerun away

## Repository owner

- Review and commit
- For each repository that needs one: create `turnip-repository-<id>` in the
  Runner namespace with its keys, list them in `runner.secrets`, and
  point `runner.env` at `/turnip/repository-secrets/<key>`
- Deploy, run a plan for a Project that lists a key, and confirm the
  tool reaches its cluster; then the change is archived

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1.1", "2.1", "3.1"] },
    { "id": 1, "tasks": ["1.2", "1.3", "1.4", "1.5", "3.2", "3.3", "3.4", "3.5"] },
    { "id": 2, "tasks": ["4.1", "4.3", "5.1"] },
    { "id": 3, "tasks": ["4.2", "5.2"] },
    { "id": 4, "tasks": ["6.1", "6.2", "6.3"] }
  ]
}
```

## Deviations recorded at implementation

- The repository id reaches dispatch through the `github.Repository`
  value that already travels with every Target, not a new Target field
  as design Decision 5 first said; the design now says so. On the
  comment path it is the webhook's own repository, never the pull
  request fetched separately (tested on both trigger paths).
- The review found the Runner namespace never reached the hint in the
  deployed Server (it was an option `cmd/server` did not pass). The fix
  pass removed the option: `New` reads the namespace from the Jobs
  client, which is created with it, so the two cannot disagree
  (`TestNew_RunnerNamespaceComesFromTheJobsClient`,
  `TestClient_NamespaceIsWhereJobsAreCreated`).
- The review found the docs understated the exposure as "every key a
  Project lists"; a pull request edits `turnip.yaml` and can list any key
  of its repository's Secret. `runner-access.md`, `helmfile.md` and
  `turnip-yaml.md` now say so.
- The checkpoint answered the open question: `PodInitializing`, so a
  missing Secret or key is titled `Job <name>: container stuck
  (PodInitializing)`; both troubleshooting pages use that title. The
  test Pods carry an init container that mounts nothing, as Runner Pods
  do, which is what produces that reason. Only a missing key was tested,
  not a missing Secret.
- `make test-kind` first failed before any test ran: Docker Hub rejected
  the stale login saved in `~/.docker/config.json` while pulling the kind
  node image. It passed with `DOCKER_CONFIG` pointed at an empty
  directory (anonymous pulls). Its `go test -timeout` is raised from 180s
  to 360s for the 60 second Pending check.
- An invalid key is not also reported as a duplicate.
