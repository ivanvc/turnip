# Implementation Plan: Running a Tool Image the Operator Approves (Slice 44)

## Overview

The security model goes first, as Requirement 1 asks: it is written before
the feature it warns about, and it covers the opt-ins that already ship.

Then the vocabulary (the Plugin's tag globs, the Catalog and its Entries),
then each package in isolation, then the orchestrator wiring them
together.

**The checkpoint that matters is task 8.** Two things are only testable
once the orchestrator is wired: that an apply runs the digest its plan
recorded, not the tag re-resolved, and that a plan whose digest could not
be recorded refuses its apply. Both are the point of the slice, and both
look fine in package tests that never cross from a plan's result to the
apply that follows it.

Tasks in the same wave of the dependency graph (at the end) touch
disjoint files and run in parallel. Waves 1 and 2 change signatures across
packages (`config.Parse`, `jobs.OperationParams`, `lock.PlanRecord`), so
until wave 2 lands the tree does not build, and each task builds and tests
only its own package. Task 11 is the owner's, after implementation, and is
not in the graph.

## Tasks

- [x] 1. The security model
  - [x] 1.1 `SECURITY.md` at the repository root: the threat model in one
    paragraph; every opt-in that grants capability by design
    (`runner.serviceAccount` and `clone.submodules` through
    `TURNIP_ALLOWED_OVERRIDES`, and `TURNIP_ALLOWED_IMAGES`), each
    default-off, with what enabling it costs; what turnip does consider a
    vulnerability (a gate that fails open, a default granting more than
    documented, a leak reachable with no opt-in); reporting through
    GitHub's private vulnerability reporting, with no email address
  - _Requirements: 1.1–1.5_

- [x] 2. The vocabulary
  - [x] 2.1 `ImageTags() []string` on the Plugin interface: the Tag_Spec
    globs its Alias allows. Helmfile returns `v*.*.*` and `sha256:*`. Add
    it to every test fake implementing Plugin (`internal/runner/run_test.go`,
    `internal/orchestrator/target_test.go`)
  - [x] 2.2 The registry test (Slice 45) also requires at least one glob
    per Plugin
  - [x] 2.3 In `internal/config`: `Entry{Tool, Image, Glob}` and
    `Catalog{Aliases, Allowed}`, per the design, and a function parsing
    one `tool:image@glob` line against the registered Tool_Names, with
    tests for every start-refusal case in the design. Nothing uses them
    yet
  - _Requirements: 3.1, 3.2, 4.2_

- [x] 3. Resolving `uses:` in `internal/config`
  - [x] 3.1 `Parse(data []byte, catalog Catalog)`, replacing the `tools`
    slice; the Tool_Names come from the Catalog
  - [x] 3.2 Resolution per the design's table, in order: no `@`; a pasted
    `:tag` (error shows the corrected line); unqualified non-Alias; a
    non-`sha256` or malformed digest; no matching Entry (names the image
    and `TURNIP_ALLOWED_IMAGES`). An Alias tries only its own Entries; an
    Image_Reference tries every Entry for that image. Matching with
    `path.Match`
  - [x] 3.3 A resolved Project carries `Tool` from the Entry, `Image`, and
    `ToolVersion` as the Tag_Spec verbatim
  - [x] 3.4 Delete `isWellFormedVersion` and its "floating tags are not
    accepted" error
  - [x] 3.5 Tests: one per resolution row; the vendor image in full vs
    the Alias; `helmfile@latest` refused while the vendor's `latest` in
    full is allowed by an Entry; each glob example in Requirement 4.3;
    existing fixtures updated to build a Catalog
  - _Requirements: 2.1–2.6, 2.8, 3.5, 4.3, 4.6, 4.7_

- [x] 4. `internal/jobs` builds from a finished reference
  - [x] 4.1 `OperationParams` gains `ImageRef` and `PullAlways`; `BuildJob`
    uses `ImageRef` as given and sets `imagePullPolicy` from `PullAlways`
    (Always, or IfNotPresent) on the container running the tool
  - [x] 4.2 `JobStatus` gains the main container's `ImageID`
  - [x] 4.3 `pin_test.go` still passes with `ImageRef`
    `ghcr.io/helmfile/helmfile:v1.7.4` and `PullAlways` true; its only new
    expectation is the pull policy
  - _Requirements: 2.7, 6.4_

- [x] 5. `lock.PlanRecord` carries the digest
  - [x] 5.1 `ImageDigest string` on `PlanRecord`, stored and returned with
    the rest of the plan
  - _Requirements: 6.1_

- [x] 6. The Server setting and the orchestrator wiring
  - [x] 6.1 `TURNIP_ALLOWED_IMAGES` parsed in `internal/orchestrator/config.go`
    beside `TURNIP_ALLOWED_OVERRIDES`; the Catalog built from it and each
    registered Plugin's `Provisioning().Image` and `ImageTags()`; the
    Server refuses to start per the design, naming every offending Entry
  - [x] 6.2 Pass the Catalog to `config.Parse`
  - [x] 6.3 Compose `ImageRef` and `PullAlways` for each Job: tag or
    digest for a plan; `image@<recorded digest>` for a Mutating_Operation.
    A Project whose image is not its Alias's runs `RunInImage` whatever
    its Plugin's Strategy
  - [x] 6.4 At a plan's result, read the Pod's `ImageID` through
    `jobs.Status` and record its `sha256` suffix on the `PlanRecord`; if it
    cannot be read, record none
  - [x] 6.5 A Mutating_Operation whose `PlanRecord` has no digest refuses
    ("the image this plan ran could not be recorded; re-plan"), as a
    refusal of the `other` kind (Slice 36): no check created, record
    unchanged
  - [x] 6.6 `timeoutDiagnostic`: an image-pull failure for a
    Mutating_Operation running a recorded digest says the digest may no
    longer exist and to re-plan
  - [x] 6.7 The orchestrator's fixtures build a Catalog; `cmd/server`
    builds
  - _Requirements: 3.3, 3.4, 4.1, 4.4, 4.5, 4.8, 5.1, 6.1–6.3_

- [x] 7. Checkpoint: the whole tree
  - `go build ./...`, `go test -race ./...`, golangci-lint (v2) pass
  - No reference remains to `isWellFormedVersion`

- [x] 8. Checkpoint: the digest round trip
  - [x] 8.1 An orchestrator test: a plan runs with `uses:` naming a tag;
    its result records the digest the fake Pod reports; the apply's Job
    runs `image@<that digest>` with IfNotPresent, even after the fake Pod
    starts reporting a different digest for the same tag
  - [x] 8.2 A plan whose digest could not be read: the apply refuses with
    the re-plan message, creates no Job and leaves the record unchanged
  - _Requirements: 6.1–6.3_

- [x] 9. Documentation and deployment
  - [x] 9.1 `docs/configuration.md`: the `uses:` forms (Alias and full
    reference, one exact Tag_Spec, used verbatim); each Alias's allowed
    Tag_Specs; `TURNIP_ALLOWED_IMAGES` in the Server settings table with
    examples (`helmfile:ghcr.io/org/helmfile-aws@*`,
    `helmfile:ghcr.io/org/helmfile-aws@latest`,
    `helmfile:ghcr.io/helmfile/helmfile@latest` for the vendor's own);
    the pasted `:tag` form next to its correction; what a custom image
    must provide; pulling from private registries (node credentials or
    `imagePullSecrets` on the Runner's ServiceAccount); each opt-in linking
    to `SECURITY.md`
  - [x] 9.2 The EKS recipe: an image `FROM` helmfile's with the AWS CLI
    added, the Access_List Entry allowing it, and no "not usable in turnip
    yet" warning
  - [x] 9.3 `deploy/base/kustomization.yaml`: a comment that
    `TURNIP_ALLOWED_IMAGES` is deliberately unset, beside the one for
    `TURNIP_ALLOWED_OVERRIDES`; `docs/deployment.md` names the setting
  - _Requirements: 5.2, 7.1, 7.2, 8.1–8.3_

- [x] 10. Final checkpoint
  - Build, tests and lint pass; every acceptance criterion maps to a task
  - Roadmap status updated
  - Any intermittent test failure is recorded in the roadmap Backlog's
    intermittent-failure entry, with the full output kept, rather than
    rerun and dropped

- Deviations recorded at implementation:
    - Resolution gained a row the design's table lacked: a Tag must be an
      exact OCI tag, so `helmfile@v*.*.*` cannot match its own Alias glob
      (Requirement 2.4). A review found it; the design table now has it
    - A pasted `:tag` with no `@` at all (`ghcr.io/org/x:1.7.4`) gets the
      corrected-line error rather than "names no version"
    - sha256 hex must be lowercase, as OCI requires
    - The digest is recorded only from a repository digest
      (`repo@sha256:…`); a bare `sha256:…` imageID is a local image ID
      and counts as unreadable
    - `jobs.JobStatus` also gained `ToolWaiting`, so the re-plan
      diagnostic fires only when the tool container's pull fails, not
      turnip's own Runner image
    - Signatures: `ConfigFromEnv(getenv, plugins)`, `orchestrator.New`
      takes the Catalog, `Catalog.ToolNames()` added

- [ ] 11. After implementation — the repository owner's step
  - Enable GitHub's private vulnerability reporting in the repository's
    settings (Security → Private vulnerability reporting), which
    `SECURITY.md` points to

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1.1", "2.1", "2.2", "2.3"] },
    { "id": 1, "tasks": ["3.1", "3.2", "3.3", "3.4", "3.5", "4.1", "4.2", "4.3", "5.1"] },
    { "id": 2, "tasks": ["6.1", "6.2", "6.3", "6.4", "6.5", "6.6", "6.7"] },
    { "id": 3, "tasks": ["7"] },
    { "id": 4, "tasks": ["8.1", "8.2"] },
    { "id": 5, "tasks": ["9.1", "9.2", "9.3"] },
    { "id": 6, "tasks": ["10"] }
  ]
}
```
