# Requirements Document: Config Parsing & Project Matching (Slice 1)

## Introduction

This slice delivers the `internal/config` package: a library that parses a
repository's `turnip.yaml` into a typed configuration, validates it, and
determines which Projects are affected by a set of modified files. It is
consumed by later slices (Slice 6: Server Orchestration) but does not itself
talk to GitHub, Redis, or plugins — those are wired up elsewhere.

This slice implements Requirements 1, 2, and 18 from the global spec
(`.kiro/specs/multi-iac-automation-platform/requirements.md`), scoped to the
parsing/validation/matching library only.

## Glossary

(Inherited from the global spec glossary.)

- **Config**: The parsed, validated in-memory representation of a `turnip.yaml` file.
- **Project**: A configuration unit within Config defining a directory, IaC tool, whenModified rules, and tool-specific config.
- **WhenModified_Rule**: A glob pattern string that determines if a Project should be triggered by a set of modified files.

## Requirements

### Requirement 1: Parse turnip.yaml into a typed Config

**User Story:** As a platform developer, I want to parse a `turnip.yaml` document into a typed Go structure, so that other components can access project definitions without re-parsing YAML themselves.

#### Acceptance Criteria

1. THE Config Parser SHALL accept `turnip.yaml` content as raw bytes (the caller is responsible for fetching the file; fetching from GitHub is out of scope for this slice)
2. THE Config Parser SHALL parse a top-level `version` field
3. THE Config Parser SHALL parse a `projects` list, where each entry has `name`, `directory`, `tool`, `whenModified`, and an optional `config` map
4. FOR EACH parsed Project, THE Config Parser SHALL preserve the directory, tool type, and WhenModified_Rule patterns exactly as declared
5. IF the input bytes are not valid YAML, THEN THE Config Parser SHALL return a structured error identifying that the document failed to parse (including the underlying line/column when available), without posting to GitHub or otherwise reporting externally
6. IF a Project's `name` is omitted, THEN THE Config Parser SHALL default its `name` to the Project's `directory` value

### Requirement 2: Validate parsed configuration

**User Story:** As a platform developer, I want invalid `turnip.yaml` configurations rejected with actionable errors, so that misconfigurations are caught before any operation is attempted.

#### Acceptance Criteria

1. THE Config Parser SHALL validate that each Project specifies a supported IaC_Tool: `terraform`, `pulumi`, or `helmfile`
2. IF a Project specifies an unsupported or missing `tool` value, THEN THE Config Parser SHALL return a structured validation error naming the offending project and the invalid value
3. IF a Project is missing a required field (`directory` or `tool`), THEN THE Config Parser SHALL return a structured validation error naming the missing field and the project (by index or name, whichever is available)
4. IF two Projects declare the same `name` — including names defaulted from `directory` per Requirement 1.6 — THEN THE Config Parser SHALL return a structured validation error identifying the duplicate name
5. THE Config Parser SHALL return all detectable validation errors from a single parse call rather than stopping at the first one, where practical

### Requirement 3: Selective project matching via WhenModified rules

**User Story:** As a developer, I want Projects to be matched only when relevant files change, so that unrelated IaC operations don't run on every PR.

#### Acceptance Criteria

1. THE Project Matcher SHALL accept a list of Projects and a list of modified file paths
2. FOR EACH Project, THE Project Matcher SHALL evaluate its WhenModified_Rule patterns against the modified file paths
3. IF any modified file matches at least one of a Project's WhenModified_Rule patterns, THEN THE Project Matcher SHALL include that Project in the matched result
4. THE Project Matcher SHALL support glob patterns including `**` for recursive directory matching (e.g., `infrastructure/vpc/**/*.tf`)
5. IF no Projects match the modified files, THEN THE Project Matcher SHALL return an empty result (the decision to skip execution entirely is made by the caller, e.g. Slice 6)
6. THE Project Matcher SHALL treat WhenModified_Rule patterns as matching against repository-root-relative paths, consistent with how GitHub reports modified file paths

### Requirement 4: Tool-specific configuration passthrough

**User Story:** As a developer, I want to specify tool-specific settings per Project, so that Terraform workspaces, Pulumi stacks, and Helmfile environments can be configured declaratively.

#### Acceptance Criteria

1. THE Config Parser SHALL support an optional `config` map of string key/value pairs within each Project definition
2. THE Config Parser SHALL preserve `config` entries without interpreting or validating tool-specific keys (e.g., it does not require `workspace` to be present for `terraform` projects)
3. THE parsed Project structure SHALL expose `config` so that later slices (Plugin System, Server Orchestration) can pass it through to plugin execution unchanged

## Out of Scope

- Fetching `turnip.yaml` from a repository (GitHub API access) — Slice 4
- Posting error comments to a PR when configuration is missing or invalid — Slice 4 / Slice 6 (this slice only returns structured errors for a caller to act on)
- Retrieving the list of modified files from a PR — Slice 4
- Validating tool-specific `config` keys/values against each tool's actual requirements (e.g., that a Terraform workspace name is valid) — deferred to the respective plugin (Slice 2, Slice 7) or left unvalidated
- Any business logic beyond parsing, validation, and glob matching (no orchestration, no plugin invocation)
