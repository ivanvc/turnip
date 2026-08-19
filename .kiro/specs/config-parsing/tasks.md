# Implementation Plan: Config Parsing & Project Matching (Slice 1)

## Overview

This plan implements `internal/config` per `design.md`: a pure library with
no I/O beyond accepting bytes. Tasks are ordered so that dependencies and
error types (needed by everything else) land first, followed by the data
model, then `Parse`/`validate` (which depend on the data model and error
types), then `MatchProjects` (independent of `Parse`), then the package doc
update, then tests (unit, then property).

## Tasks

- [x] 1. Add dependencies
  - [x] 1.1 Promote `go.yaml.in/yaml/v3` to a direct dependency and add `github.com/bmatcuk/doublestar/v4`
    - Add `go.yaml.in/yaml/v3` to the `require` block in `go.mod` (moving it out of the indirect block)
    - Add `github.com/bmatcuk/doublestar/v4` as a new direct dependency
    - Run `go mod tidy` and verify it produces no further changes (exit 0, no diff)
    - _Requirements: 1.1, 3.4_

- [x] 2. Implement error types
  - [x] 2.1 Create `internal/config/errors.go`
    - Define `ParseError` struct (`Line int`, `Column int`, `Message string`) with `Error() string`
    - Define `ValidationError` struct (`ProjectRef string`, `Field string`, `Message string`) with `Error() string`
    - Define `ValidationErrors []*ValidationError` with `Error() string` joining each error on its own line
    - Verify compilation with `go build ./internal/config/...`
    - _Requirements: 1.5, 2.2, 2.3, 2.4, 2.5_

- [x] 3. Implement data model
  - [x] 3.1 Create `internal/config/config.go`
    - Define `Config` struct: `Version int` (yaml:"version"), `Projects []Project` (yaml:"projects")
    - Define `Project` struct: `Name string`, `Directory string`, `Tool string`, `WhenModified []string`, `Config map[string]string` — with matching yaml tags (`name`, `directory`, `tool`, `whenModified`, `config`)
    - Define exported constants `ToolTerraform = "terraform"`, `ToolPulumi = "pulumi"`, `ToolHelmfile = "helmfile"`
    - Verify compilation with `go build ./internal/config/...`
    - _Requirements: 1.2, 1.3, 1.4, 2.1, 4.1, 4.3_

- [x] 4. Checkpoint - Verify types and dependencies
  - Ensure `go build ./internal/config/...` succeeds and `go mod tidy` produces no changes. Ask the user if questions arise.

- [x] 5. Implement validation
  - [x] 5.1 Create `internal/config/validate.go`
    - Implement internal `validate(*Config) error` accumulating every problem into one `ValidationErrors` rather than returning on first failure
    - Check each project has non-empty `name`, `directory`, `tool`
    - Check `tool` is one of `ToolTerraform`, `ToolPulumi`, `ToolHelmfile`; error names the offending project and invalid value
    - Check no two projects share the same `name`; error identifies the duplicate name
    - Check each `whenModified` entry is a syntactically valid glob via `doublestar.ValidatePattern`
    - Return `nil` if no errors accumulated, else the populated `ValidationErrors`
    - _Requirements: 2.1, 2.2, 2.3, 2.4, 2.5_

- [x] 6. Implement parsing
  - [x] 6.1 Create `internal/config/parse.go`
    - Implement `Parse(data []byte) (*Config, error)` using `go.yaml.in/yaml/v3` to unmarshal into `*Config`
    - On YAML syntax error, return `nil` and a `*ParseError` populated with line/column when the decoder provides it (no partial `Config` returned)
    - On successful unmarshal, call internal `validate`; if it returns a non-nil error, return `nil` and that error
    - Return the populated `*Config` and `nil` error only when parsing and validation both succeed
    - Verify compilation with `go build ./internal/config/...`
    - _Requirements: 1.1, 1.2, 1.3, 1.4, 1.5, 2.5, 4.1, 4.2_

- [x] 7. Implement project matching
  - [x] 7.1 Create `internal/config/match.go`
    - Implement `MatchProjects(projects []Project, modifiedFiles []string) []Project`
    - For each project, evaluate its `WhenModified` patterns against `modifiedFiles` using `doublestar.Match`, including the project once any pattern matches any file
    - Implement internal `normalize(path string) string` stripping a leading `./` and converting `\`-style separators to `/`
    - Preserve input `projects` order in the returned slice (stable, deterministic)
    - Return `nil` when `projects` or `modifiedFiles` is empty, or when nothing matches
    - Verify compilation with `go build ./internal/config/...`
    - _Requirements: 3.1, 3.2, 3.3, 3.4, 3.5, 3.6_

- [x] 8. Update package documentation
  - [x] 8.1 Replace the Slice 0 placeholder in `internal/config/doc.go`
    - Replace the "Slice 1" placeholder doc comment with one describing the package's actual purpose (parsing, validating, and matching `turnip.yaml` projects)
    - Verify `go build ./...` still succeeds for the whole module
    - _Requirements: (documentation, no direct requirement)_

- [x] 9. Checkpoint - Verify library compiles end-to-end
  - Ensure `go build ./...` succeeds and `go vet ./internal/config/...` reports no issues. Ask the user if questions arise.

- [x] 10. Write unit tests
  - [x] 10.1 Write `internal/config/parse_test.go`
    - Cover valid YAML round-trip through `Parse`
    - Cover malformed YAML producing a `*ParseError` with line/column when available
    - Cover `projects:` absent/empty parsing successfully with `Config.Projects == nil`
    - Cover `version` missing or non-`1` parsing as-is (not rejected)
    - _Requirements: 1.1, 1.2, 1.3, 1.4, 1.5_

  - [x] 10.2 Write `internal/config/validate_test.go`
    - Cover missing required field (`name`, `directory`, `tool`) each producing a `ValidationError` naming the field and project
    - Cover unsupported/missing `tool` value producing a `ValidationError` listing the three valid values
    - Cover duplicate project names producing a `ValidationError` identifying the duplicate
    - Cover multiple simultaneous violations all appearing in one `ValidationErrors` from a single `Parse` call
    - Cover invalid `whenModified` glob syntax producing a `ValidationError` at parse time
    - _Requirements: 2.1, 2.2, 2.3, 2.4, 2.5_

  - [x] 10.3 Write `internal/config/match_test.go`
    - Cover a project matching when a modified file matches one of its patterns
    - Cover `**` recursive glob patterns (e.g. `infrastructure/vpc/**/*.tf`)
    - Cover no projects matching when no modified file matches any pattern (empty result)
    - Cover multiple patterns on one project, only one of which matches
    - Cover independence: a file matching one project's pattern doesn't affect other projects
    - Cover deterministic ordering following input `projects` order
    - Cover path normalization (leading `./`, backslash separators)
    - Cover empty `modifiedFiles` list returning `nil`
    - _Requirements: 3.1, 3.2, 3.3, 3.4, 3.5, 3.6_

  - [x] 10.4 Write `internal/config/config_test.go`
    - Cover `config` map round-tripping through `Parse` unchanged (no interpretation of keys/values)
    - Cover `Project.Config` being `nil` when the `config` map is absent
    - _Requirements: 4.1, 4.2, 4.3_

- [x] 11. Checkpoint - Verify unit tests pass
  - Ensure `go test ./internal/config/...` passes. Ask the user if questions arise.

- [x] 12. Write property tests
  - [x] 12.1 Add `gopter` as a direct test dependency
    - Add `github.com/leanovate/gopter` to `go.mod`
    - Run `go mod tidy` and verify it produces no further changes
    - _Requirements: (testing infrastructure, no direct requirement)_

  - [x] 12.2 Write `internal/config/property_test.go`
    - `// Feature: multi-iac-automation-platform, Property 1: Configuration Round-Trip` — generate a random valid `Config`, marshal with `go.yaml.in/yaml/v3`, `Parse` it back, assert equality; ≥100 iterations
    - `// Feature: multi-iac-automation-platform, Property 2: Tool Validation Rejects Invalid Tools` — generate configs with a random non-empty string as `tool`, assert rejection unless it's one of the three valid values; ≥100 iterations
    - `// Feature: multi-iac-automation-platform, Property 3: WhenModified Pattern Matching` — generate a project with random glob patterns and a random file list containing at least one match, assert the project is present in `MatchProjects`' result; ≥100 iterations
    - _Requirements: 1.1, 1.2, 1.3, 1.4, 2.1, 2.2, 3.1, 3.2, 3.3, 3.4_

- [x] 13. Final checkpoint - Full verification
  - Ensure `go build ./...` compiles, `go test -race ./internal/config/...` passes including property tests, `go mod tidy` produces no changes, and `golangci-lint run ./internal/config/...` passes. Ask the user if questions arise.

- [x] 14. Default Name from Directory when omitted
  - [x] 14.1 Implement `applyDefaults` in `internal/config/parse.go`
    - Add internal `applyDefaults(*Config)` that sets each Project's `Name` to its `Directory` when `Name` is empty
    - Call `applyDefaults` in `Parse`, after unmarshal and before `validate`
    - Remove the "name is required" check from `internal/config/validate.go` (a Project can only end up with an empty `Name` when `Directory` is also empty, which the existing "directory is required" check already catches)
    - _Requirements: 1.6, 2.3_

  - [x] 14.2 Update tests
    - Add test: `name` omitted, `directory` present → `Name` defaults to `Directory`'s value
    - Add test: explicit `name` is not overridden by defaulting
    - Add test: two projects with the same `directory` and no `name` still produce a duplicate-name `ValidationError`
    - Add test: `name` and `directory` both omitted → only a `directory` `ValidationError`, referencing the project by index
    - Remove the "missing name" case from the missing-required-field test (no longer an error on its own)
    - _Requirements: 1.6, 2.3, 2.4_

  - [x] 14.3 Checkpoint - Verify tests pass with full coverage
    - Ensure `go test -race ./internal/config/...` passes and coverage remains at 100%

## Notes

- No GitHub, Redis, gRPC, or plugin-system dependencies are introduced — this
  slice is a pure library, matching the design's stated scope.
- `errors.go` and `config.go` come first because `validate.go`, `parse.go`,
  and the test files all depend on the types they define.
- `match.go` has no dependency on `parse.go`/`validate.go` and could be built
  in parallel with them, but is sequenced after for readability; the
  dependency graph below reflects the true parallel opportunity.
- Coverage target is 90%, per the global design doc's line item for the
  project matcher, applied here to the whole package.

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1.1"] },
    { "id": 1, "tasks": ["2.1"] },
    { "id": 2, "tasks": ["3.1"] },
    { "id": 3, "tasks": ["5.1", "7.1"] },
    { "id": 4, "tasks": ["6.1"] },
    { "id": 5, "tasks": ["8.1"] },
    { "id": 6, "tasks": ["10.1", "10.2", "10.3", "10.4"] },
    { "id": 7, "tasks": ["12.1"] },
    { "id": 8, "tasks": ["12.2"] }
  ]
}
```
