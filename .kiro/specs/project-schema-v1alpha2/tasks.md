# Implementation Plan: Project Schema v1alpha2 (Slice 13)

## Overview

Ordered so the data model lands first, then the parse pipeline that fills
it, then the consumers — because every fixture in the repository breaks the
moment `Project` changes, and doing that alongside a behavioral change
would make a failing test ambiguous between the two causes.

Discovery (Requirement 8) is deliberately last among the code changes: it
lives in a different package from everything else and shares no state with
it, so it can land or be reverted independently.

## Tasks

- [x] 1. Data model
  - [x] 1.1 Reshape `Project` in `internal/config/config.go`
    - `Uses`, `With`, `Runner` replace `Tool`, `Config`, `Env`; add `RunnerSpec`
    - Add `Tool`/`ToolVersion` with `yaml:"-"` (Decision 1)
    - `SupportedSchemaVersion` becomes `v1alpha2`
    - _Requirements: 1.1, 2.1, 3.1, 3.2, 3.3, 6.1_

- [x] 2. Parse pipeline
  - [x] 2.1 Three decode passes in `internal/config/parse.go` (Decision 2)
    - Pass 1 lenient → `*ParseError` on failure; pass 2 strict only after
      `schemaVersion` is known good, so its errors are unambiguously
      unknown keys
    - Map each `yaml.TypeError` entry to a `ValidationError`; never surface
      the decoder's own wording, which names Go types
    - _Requirements: 5.1, 5.2, 5.3_
  - [x] 2.2 Decompose `uses` in `applyDefaults`
    - Split on the first `@`; strip a leading `v`; leave `Tool`/`ToolVersion`
      set for everything downstream
    - _Requirements: 1.1, 1.5, 1.7_

- [x] 3. Validation in `internal/config/validate.go`
  - [x] 3.1 Move the `schemaVersion` check out of `validate` so `Parse` can
    report it alone, before the strict pass
    - _Requirements: 5.3, 6.1, 6.2_
  - [x] 3.2 Validate `uses`: required, known tool, well-formed version
    - Version shape moves here from `internal/jobs` (Decision 4) — config is
      a leaf package and `jobs` imports it, so the regex cannot be borrowed
    - A floating tag such as `latest` is not well-formed
    - _Requirements: 1.2, 1.3, 1.6_
  - [x] 3.3 Point env validation at `Runner.Env`; drop `config.serviceAccount`
    - _Requirements: 3.4_

- [x] 4. Checkpoint - config package is self-consistent
  - `go build ./internal/config/...` and its tests pass. A failure here is
    about the schema alone; no consumer has changed yet.

- [x] 5. Consumers
  - [x] 5.1 `internal/jobs/build.go`
    - `project.Config["version"]` → `project.ToolVersion`; the marshaled
      tool config → `project.With`; `project.Env` → `project.Runner.Env`
    - `TURNIP_TOOL_CONFIG` stops carrying `version`/`serviceAccount`
    - _Requirements: 2.5_
  - [x] 5.2 `internal/jobs/versions.go`
    - Keep default selection and image-tag formatting; the malformed-version
      branch becomes defensive now that `config` rejects earlier
    - _Requirements: 1.6_
  - [x] 5.3 Overrides in `internal/orchestrator`
    - `TURNIP_ALLOWED_OVERRIDES` replaces the boolean in `config.go`,
      `orchestrator.go`, `execute.go` and `cmd/server/main.go`
    - `resolveServiceAccount` reads `Runner.ServiceAccount`; its error names
      the path and the variable that would permit it
    - _Requirements: 4.1, 4.2, 4.3, 4.4_
  - [x] 5.4 `deploy/base/kustomization.yaml`
    - Outside 5.3's scope as written, which named only the Go files; found
      by sweeping for the old variable rather than by listing consumers
    - The base ConfigMap still sets
      `TURNIP_RUNNER_SERVICE_ACCOUNT_ALLOW_FROM_CONFIG`, which the Server no
      longer reads — harmless at runtime, but an operator copying it would
      believe they had gated something they had not
    - _Requirements: 4.4_

- [x] 6. Fixture migration
  - [x] 6.1 Every `turnip.yaml` fixture in the repository
    - Found by grepping, not by listing — the same undercount bit Slice 12.
      Expect `internal/config` (4 files), `internal/github/client_test.go`,
      `internal/orchestrator` (several), `internal/jobs/build_test.go`
    - _Requirements: 6.4_

- [x] 7. Checkpoint - schema change is complete end to end
  - `go build ./...` and `go test -race ./...` pass. Discovery has not
    changed yet, so a failure here is the schema or a consumer.

- [x] 8. Discovery
  - [x] 8.1 `internal/orchestrator/configfetch.go` and `errors.go`
    - `.turnip/config.yaml` then `turnip.yaml`; `.github/turnip.yaml` gone
    - Both messages derive their path list from one exported slice so they
      cannot drift (Decision 7)
    - _Requirements: 8.1, 8.2, 8.3, 8.4, 8.5, 8.6_

- [x] 9. Tests
  - [x] 9.1 `internal/config`: schema
    - `uses` forms (bare, `@version`, `@vVersion`, empty version, unknown
      tool, `latest`); `with`/`runner` optional; reserved env names
    - A document using anchors and merge keys parses under strict decoding —
      pins Decision 3 against a future refactor
    - A `v1alpha1` document reports exactly one error — pins Requirement 5.3
    - Unknown keys: several reported together; unknown keys *inside* `with`
      accepted, inside `runner` rejected
    - _Requirements: 1.2-1.6, 2.6, 3.5, 5.1-5.5, 6.2_
  - [x] 9.2 `internal/jobs`: `ToolVersion` and `With` reach the Job spec, and
    `TURNIP_TOOL_CONFIG` no longer carries turnip's own keys
    - _Requirements: 2.5_
  - [x] 9.3 `internal/orchestrator`: overrides allowed/refused by path;
    discovery order, and that only two content requests are made when no
    configuration exists
    - _Requirements: 4.3, 8.1, 8.6_

- [x] 10. Documentation
  - [x] 10.1 `docs/configuration.md` and `docs/troubleshooting.md`
    - `uses`/`with`/`runner`, the recognized-keys list, unknown keys as
      errors, `TURNIP_ALLOWED_OVERRIDES` and its default, and both accepted
      file locations
    - _Requirements: 7.1-7.5_

- [x] 11. Final checkpoint - full verification
  - `go build ./...`, `go test -race ./...`, `go mod tidy` clean,
    `gofmt -l .` clean, and the real golangci-lint v2 via
    `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest run ./...`

- [x] 12. Gate only `runner.serviceAccount` (amendment)
  - User-flagged in review: `uses` was in the default override set, which
    raised the question of what gating it would even mean — without it, a
    Project cannot name a tool at all
  - The implementation had answered that by gating only the field's
    optional half, the pinned version, and documenting the reinterpretation
    at the call site. The honest conclusion is the opposite: an override is
    a repository replacing a value the *Server* also supplies, and turnip
    has no Server-side tool to fall back to, so `uses` was never an
    override. Atlantis leaves `terraform_version` out of
    `allowed_overrides` for the same reason
  - Removed `overrideUses`, `checkVersionOverride`,
    `VersionOverrideNotPermittedError` and the call in `executeOne`;
    `runner.serviceAccount` is now the only known path and the default
    permits nothing, which also removes the wart that there had been no way
    to express the empty set
  - Amended Requirement 4.2 and rewrote 4.5, replaced Decision 6's table
    with the alternative-considered note, and dropped the now-redundant
    literal from `deploy/base/kustomization.yaml`
  - _Requirements: 4.1, 4.2, 4.3, 4.5_

## Notes

- **No new dependencies.** Strict decoding is `yaml.Decoder.KnownFields`,
  already present.
- **Two behaviors were verified experimentally before design, not
  assumed**: YAML merge keys survive `KnownFields(true)`, and the proposed
  struct shape round-trips with `omitempty` on a nested struct and
  `yaml:"-"` on derived fields. Task 9.1 pins both so they stay true.
- **The migration cannot be staged.** No file value is accepted by both the
  deployed Server and the new one, so the consuming repository's file and
  the Server roll together. This differs from the `v1alpha1` migration and
  is the thing most likely to be assumed rather than read.
- **Coverage target**: 80% for touched packages, consistent with prior
  slices.

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1.1"] },
    { "id": 1, "tasks": ["2.1", "2.2", "3.1"] },
    { "id": 2, "tasks": ["3.2", "3.3"] },
    { "id": 3, "tasks": ["5.1", "5.2", "5.3"] },
    { "id": 4, "tasks": ["6.1"] },
    { "id": 5, "tasks": ["8.1"] },
    { "id": 6, "tasks": ["9.1", "9.2", "9.3"] },
    { "id": 7, "tasks": ["10.1"] }
  ]
}
```
