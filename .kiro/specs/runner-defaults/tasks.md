# Implementation Plan: Runner Settings Shared Across Projects (Slice 43)

## Overview

The whole mechanism lives in `internal/config`: the top-level key, each
level validated against its own path, and the merge driven by the rules
table, recording where `serviceAccount` came from. Everything downstream
already reads `Project.Runner`, so the orchestrator only changes the
refusal's wording.

**The checkpoint that matters is task 3.** A merge that is right in
`internal/config` is worthless if a reader downstream sees the Project as
written. The tests that count go through `Parse` into the gate and the
Job, never through a hand-built Project with the merged value already in
it.

Tasks in the same wave of the dependency graph (at the end) touch
disjoint files and run in parallel. The tree builds after every wave.

## Tasks

- [x] 1. The top-level block (`internal/config`)
  - [x] 1.1 `Config.Runner RunnerSpec` under the key `runner`, beside
    `clone`; the schema version unchanged
  - [x] 1.2 Validation of the top-level `env` by the rules a Project's
    uses, reported once at `runner.env["X"]` against the file
    (`configFileRef`), in the same pass as every other violation
  - [x] 1.3 The merge rules table, keyed by `RunnerSpec` field:
    `serviceAccount` replaced, `env` merged per key; the merge reads it
  - [x] 1.4 The merge as `Parse`'s last step, after validation, writing
    the Effective_Runner into each `Project.Runner` and recording where
    its `serviceAccount` came from (Project, top level, or neither) in a
    field that is not part of the YAML
  - [x] 1.5 Tests: a file without the key parses as today; each field at
    neither, either and both levels, with the recorded source; a Project
    overriding one variable keeps the rest; `X: ""` and `X:` set an empty
    value that overrides the top level's; an unknown key inside the
    top-level block is rejected; a top-level violation reported once
    beside a Project's own
  - [x] 1.6 The rules test: walks `RunnerSpec`'s fields and fails for any
    with no rule; run also over a test-only struct with an unlisted
    field, asserting it is reported
  - [x] 1.7 The configuration round-trip property test compares a parsed
    Project's `Runner` against the merge of what was generated
  - _Requirements: 1.1–1.3, 2.1, 2.2, 3.1–3.4, 4.1–4.3, 6.1–6.3_

- [x] 2. The refusal names its source (`internal/orchestrator`)
  - [x] 2.1 `ServiceAccountNotPermittedError` carries the source;
    `resolveServiceAccount` reads it from the Project; the message says
    "requested" for the Project's own block and "inherits … from the
    top-level `runner:` block" for the top level, with the fix unchanged
  - [x] 2.2 Still a Configuration_Refusal with the setting
    `runner.serviceAccount`, so the Project_Check Title is unchanged
  - _Requirements: 2.3, 5.1–5.4_

- [x] 3. Checkpoint: every reader sees the Effective_Runner
  - [x] 3.1 Tests from `turnip.yaml` bytes through `Parse`:
    - a top-level `serviceAccount` refused when not permitted, the
      message naming the top-level block and the Title unchanged
    - permitted, it reaches the Job's Pod
    - a Project's own value wins over the top level's
    - a top-level `env` variable reaches the tool container, beside the
      Project's own, with the Project's winning on a clash
    - the automatic plan and a comment-triggered Operation both see it
  - [x] 3.2 `go build ./...`, `go test -race ./...`, golangci-lint (v2)
    pass
  - _Requirements: 7.1, 7.2_

- [x] 4. Documentation
  - [x] 4.1 `docs/configuration.md`: the top-level `runner:` block in the
    schema table and example; each field's rule; that the top level is
    for what every Project uses, with anchors for settings only some
    share; that a Project cannot remove a shared variable, only override
    it; that a relative path in a shared `env` value resolves in each
    Project's own directory; that the `runner.serviceAccount` gate
    applies at either level
  - [x] 4.2 The EKS recipe sets `KUBECONFIG` once at the top level as
    `/turnip/src/.turnip/kubeconfig`
  - [x] 4.3 `SECURITY.md`: the `runner.serviceAccount` opt-in covers the
    top-level block too
  - _Requirements: 8.1–8.5_

- [x] 5. Final checkpoint
  - Build, tests and lint pass; every acceptance criterion maps to a task
  - Roadmap status updated
  - Any intermittent test failure is recorded in the roadmap Backlog's
    intermittent-failure entry, with the full output kept, rather than
    rerun and dropped
  - Deviations recorded at implementation:
    - The refusal says "from the top-level runner: block" without the
      backticks design.md shows, matching the existing message, which
      renders `runner.serviceAccount` unquoted too.
    - The merge copies every merged map, so Projects inheriting the same
      top-level `env` never share one map (tested by
      `TestParse_InheritedEnvIsNotShared`). A *replaced* field is assigned
      as is, which is only safe for scalars; the comment on
      `mergeReplaced` says a slice adding a map or slice field under that
      rule must copy it.
    - `mergeRunner` panics on a field with no rule or an unknown rule, a
      guard beside the rules test rather than a second check of it. An
      `env` set at neither level stays nil, so a file without the block
      parses exactly as before.
    - The rules test also asserts each rule names a real `RunnerSpec`
      field and that a per-key field is a map.
    - The EKS recipe now generates the kubeconfig from the repository
      root into `.turnip/kubeconfig` and says to commit it there, so the
      file matches the absolute `KUBECONFIG` the top-level block sets.
      `docs/configuration.md` also gained a YAML anchor example for
      settings only some Projects share, as task 4.1 asks.

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1.1", "1.2", "1.3", "1.4", "1.5", "1.6", "1.7"] },
    { "id": 1, "tasks": ["2.1", "2.2"] },
    { "id": 2, "tasks": ["3.1", "3.2"] },
    { "id": 3, "tasks": ["4.1", "4.2", "4.3"] },
    { "id": 4, "tasks": ["5"] }
  ]
}
```
