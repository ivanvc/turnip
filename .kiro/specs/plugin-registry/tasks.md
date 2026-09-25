# Implementation Plan: One Place to Add a Tool (Slice 45)

## Overview

A refactor with three deliberate breaking changes, so the first task pins
today's behavior before anything moves: a comparison test of the Job
`BuildJob` builds for a Helmfile Project. Every later wave has to keep it
passing, with exactly one intended difference (`TURNIP_TOOL_VERSION`
carrying the `v` as written).

Then the new vocabulary (`internal/provisioning`, the registry), then each
package that held its own copy of tool knowledge, each in isolation, then
the orchestrator that wires them together.

**The checkpoint that matters is task 9.** Waves 2 and 3 change
signatures across packages (`config.Parse`, `github.ParseTriggers`,
`jobs.OperationParams`), so the repository does not build between them.
Task 9 is the first point the whole tree builds and the pinned Job is
checked against the real wiring rather than a test harness.

## Waves

Tasks in a wave touch disjoint packages and run in parallel; a wave starts
when the previous one is done. Until wave 3 lands, agents build and test
only their own package, since callers elsewhere are updated later.

| Wave | Tasks | Packages |
|---|---|---|
| 1 | 1 (pin today's Job) and 2 (provisioning, registry) | `internal/jobs` tests; `internal/provisioning`, `internal/plugin` |
| 2 | 4 (jobs), 5 (config), 6 (trigger parser), 7 (runner) | `internal/jobs`; `internal/config`; `internal/github`; `internal/runner` |
| 3 | 8 (orchestrator and server wiring), then checkpoint 9 | `internal/orchestrator`, `cmd/server` |
| 4 | 10 (docs) | `docs/`, `README.md` |
| 5 | review against the requirements, fixes, then checkpoint 11 | as findings require |

## Tasks

- [x] 1. Pin today's Job
  - [x] 1.1 A test that builds the Job for a Helmfile Project at `1.7.4`
    with today's code and asserts its images, init containers, commands,
    volumes and environment in full
  - [x] 1.2 Written so that wave 2 can change only its input (the Project
    becomes `v1.7.4`, and the Spec is passed in) and the one expected
    difference, `TURNIP_TOOL_VERSION`
  - _Requirements: 6.1_

- [x] 2. The shared vocabulary
  - [x] 2.1 `internal/provisioning`: `Strategy` (`CopyOut`, `RunInImage`)
    and `Spec{Strategy, Image, BinaryPath}`, with doc comments saying what
    each strategy does to a Job; no Kubernetes imports
  - [x] 2.2 `Provisioning() provisioning.Spec` on the Plugin interface;
    the Helmfile Plugin returns `{RunInImage, "ghcr.io/helmfile/helmfile", ""}`
  - [x] 2.3 `internal/plugin/registry.go`: `Registry() map[string]Plugin`,
    returning a fresh map, listing the Helmfile Plugin
  - [x] 2.4 A test that every registered Plugin's Spec has a known
    Strategy, a fully qualified Image, and a BinaryPath exactly when the
    Strategy is CopyOut
  - _Requirements: 1.1, 2.1, 2.2, 2.4, 6.3_

- [x] 3. Checkpoint: the vocabulary compiles and nothing uses it yet
  - `go build ./...`, `go test -race ./...`, golangci-lint (v2) pass

- [x] 4. `internal/jobs` receives the Spec
  - [x] 4.1 `OperationParams` gains `Provisioning provisioning.Spec`;
    `BuildJob` switches on its Strategy and composes the image as
    `Spec.Image + ":" + version`, the version used verbatim
  - [x] 4.2 Delete `versions.go`: the table, `resolveVersion`,
    `UnrecognizedToolError` and `UnrecognizedVersionError`
  - [x] 4.3 Task 1's test passes with its input updated and the one
    expected difference; update other `internal/jobs` tests only where
    they named the deleted code
  - _Requirements: 2.2, 2.3, 4.3, 4.4, 6.1_

- [x] 5. `internal/config` receives the tool names
  - [x] 5.1 `Parse(data []byte, tools []string)`; remove the tool
    constants and `isSupportedTool`; an unknown tool and a missing `uses:`
    list the registered names
  - [x] 5.2 `uses:` requires `@<version>`; `splitUses` stops stripping `v`;
    the version check accepts an optional leading `v` and still rejects
    floating tags; `IsWellFormedVersion` becomes unexported
  - [x] 5.3 `SupportedSchemaVersion = "v1alpha3"`; a `v1alpha2` file is
    rejected on its own
  - [x] 5.4 Tests: unknown tool with names listed; bare `uses:`; `v` kept;
    `v1alpha2` rejected alone; every existing fixture moved to `v1alpha3`
    with versions stated
  - _Requirements: 3.1, 3.3, 3.4, 4.1, 4.2, 4.4, 4.5, 5.1, 5.2_

- [x] 6. The trigger parser receives the tool names
  - [x] 6.1 `ParseTriggers(body string, tools []string)`; `knownTools`
    becomes `turnip` plus the names passed in
  - [x] 6.2 Tests: a registered tool is a trigger; an unregistered one is
    skipped as someone else's command
  - _Requirements: 3.2_

- [x] 7. The Runner uses the registry
  - [x] 7.1 `selectPlugin` becomes a lookup in `plugin.Registry()`; an
    unregistered tool still fails fast
  - _Requirements: 1.2_

- [x] 8. The orchestrator wires it together
  - [x] 8.1 `NewPluginRegistry` returns `plugin.Registry()`; the
    orchestrator keeps taking the map as a parameter, so its tests keep
    injecting fakes
  - [x] 8.2 Pass the registered names, sorted, to `config.Parse`
    (`configfetch.go`) and `github.ParseTriggers` (`comment.go`)
  - [x] 8.3 Fill `OperationParams.Provisioning` from the Target's Plugin
  - [x] 8.4 Delete the unsupported-tool path per the design's table
    (`planTargetsFor`'s second return, `recordUnsupported` and its notice,
    `OutcomeUnsupported`, its verdict branch and summary line,
    `unsupportedTitle`, `executeOne`'s refusal) and its tests; a missing
    Plugin in `executeOne` is returned as a programming error
  - [x] 8.5 Keep the lookups for dispatched Operations (`result.go:75`,
    `sweep.go:91`, `lockstate.go:16`, `comments.go:49`, `execute.go:93`)
    and their tests unchanged
  - [x] 8.6 `cmd/server` builds with the new signatures
  - [x] 8.7 The orchestrator's `turnip.yaml` fixtures move to `v1alpha3`
    with versions stated (`pullrequest_test.go`, `comment_test.go`,
    `aggregate_wiring_test.go`, `ha_test.go`, `configfetch_test.go`), in
    this task, because they stop parsing the moment task 5 lands
  - **Deviations recorded:**
    - An optional `buildJob` field on the Orchestrator (nil means
      `jobs.BuildJob`). Once the version check moved to parse time,
      nothing reachable could make `BuildJob` fail, so the tests of the
      "Runner Job could not be built" refusal (Slice 36) had no way to
      reach it. The field keeps that refusal tested rather than deleting
      its tests.
    - Two removed-path tests were rewritten rather than deleted, to cover
      what replaced them: `TestExecuteOne_MissingPluginIsAnInternalError`
      and `TestAutomaticPlan_UnregisteredToolIsAnInvalidConfig`.
    - `ProjectEntry.Tool` was removed too: the unsupported summary line
      was its only reader.
  - _Requirements: 1.2, 3.1, 3.2, 3.5, 3.6_

- [x] 9. Checkpoint: the whole tree, with the real wiring
  - `go build ./...`, `go test -race ./...`, golangci-lint (v2) pass
  - Task 1's pinned Job passes through the orchestrator's own path, not
    only through `BuildJob` directly
  - No reference remains to `ToolHelmfile`, `ToolTerraform`, `ToolPulumi`,
    `isSupportedTool`, `knownTools`, `toolImages`, `resolveVersion`,
    `selectPlugin`'s switch, or `OutcomeUnsupported`. (`knownTools`
    survives by name as the parser's helper that builds `turnip` plus the
    names passed in, which is what the design describes; the fixed set is
    gone.)
  - _Requirements: 1.3, 6.1, 6.2_

- [x] 10. Documentation
  - [x] 10.1 A new `docs/development.md`, for contributors, opening with
    an "Adding a tool" section: write the Plugin, declare its
    provisioning, register it in `internal/plugin/registry.go`. The
    README's documentation table gains a row pointing to it; the README
    itself stays an overview
  - [x] 10.2 `docs/configuration.md`: `v1alpha3` everywhere; `uses:`
    always names a version; the version is the image tag as written
    (`helmfile@v1.7.4`); no mention of a default version; tool support
    says the accepted tools are the ones this Server has Plugins for
  - [x] 10.3 `docs/troubleshooting.md`'s examples moved to `v1alpha3`
    with versions stated. (`docs/internal/` is private notes and out of
    scope; `internal/github/client_test.go`'s `v1alpha2` is file content
    passed through a fetch test, and stays.)
  - _Requirements: 5.3, 7_

- [x] 11. Final checkpoint
  - Build, tests and lint pass; every acceptance criterion maps to a task
  - Roadmap status updated
  - Any intermittent test failure is recorded in the roadmap Backlog's
    intermittent-failure entry, with the full output kept, rather than
    rerun and dropped. None occurred: three full `go test -race ./...`
    runs passed
  - **Coverage note**: of the lookups kept by Requirement 3.6, only
    `lockstate.go` has a test for a tool no longer registered;
    `result.go`, `sweep.go`, `comments.go` and the refusal closure in
    `execute.go` have none. The code paths are unchanged by this slice, so
    the gap predates it
