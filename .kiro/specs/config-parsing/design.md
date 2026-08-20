# Design Document: Config Parsing & Project Matching (Slice 1)

## Overview

This slice implements `internal/config`: a pure library (no I/O beyond
accepting bytes) that turns `turnip.yaml` content into a validated `Config`
value and matches `Project`s against a list of modified file paths. It has no
dependency on GitHub, Redis, gRPC, or the plugin system — callers (Slice 4/6)
own fetching the file and acting on the result.

The package replaces the placeholder `internal/config/doc.go` created in
Slice 0 with real types and functions, matching the `Project` shape already
sketched in the global design doc's `ProjectMatcher` section.

## Package Layout

```
internal/config/
  config.go      // Config, Project types + exported tool constants
  parse.go        // Parse(data []byte) (*Config, error), applyDefaults(*Config)
  validate.go     // internal validate(*Config) error, called from Parse
  match.go        // MatchProjects(projects []Project, modifiedFiles []string) []Project
  errors.go       // ParseError, ValidationError, ValidationErrors
  doc.go          // updated package doc (no longer "Slice 1" placeholder)
```

One file per concern keeps parse/validate/match independently testable and
keeps `errors.go` reusable by all three without import cycles.

## Dependencies

Two new dependencies are added (both currently absent or indirect-only in
`go.mod`):

- **`go.yaml.in/yaml/v3`** (promote from indirect to direct) for YAML
  decoding. It's already pulled in transitively (via `sigs.k8s.io/yaml`,
  which itself migrated to it), it's a drop-in of `gopkg.in/yaml.v3`'s API,
  and using it directly avoids adding a second, redundant YAML dependency
  (`gopkg.in/yaml.v3` is also present but only as a transitive dependency of
  other modules).
- **`github.com/bmatcuk/doublestar/v4`** (new) for glob matching, including
  `**` recursive patterns. This is the library the global design doc names
  explicitly as the intended implementation for `WhenModified` matching.

## Data Model

```go
package config

type Config struct {
    Version  int       `yaml:"version"`
    Projects []Project `yaml:"projects"`
}

type Project struct {
    Name         string            `yaml:"name"`
    Directory    string            `yaml:"directory"`
    Tool         string            `yaml:"tool"`
    WhenModified []string          `yaml:"whenModified"`
    Config       map[string]string `yaml:"config"`
}

const (
    ToolTerraform = "terraform"
    ToolPulumi    = "pulumi"
    ToolHelmfile  = "helmfile"
)
```

This mirrors the `Project` struct already documented in the global design
(`design.md` "Project Matcher" section) field-for-field, so downstream slices
that were designed against that sketch don't need to change.

## API Surface

```go
// Parse unmarshals and validates turnip.yaml content in one call.
// It returns *Config only when parsing AND validation both succeed;
// otherwise it returns a nil *Config and a non-nil error.
func Parse(data []byte) (*Config, error)

// MatchProjects returns the subset of projects whose WhenModified patterns
// match at least one of modifiedFiles. It never returns an error: pattern
// syntax is validated eagerly in Parse (see "Eager pattern validation"
// below), so by the time a *Config exists, its patterns are known-valid.
func MatchProjects(projects []Project, modifiedFiles []string) []Project
```

`Parse` validating internally (rather than exposing a separate `Validate`
step the caller must remember to call) directly serves Requirement 2's goal
of catching misconfiguration early — there's no way to obtain a `*Config`
that hasn't been validated.

### Name defaulting

Before validation runs, `Parse` defaults each Project's `name` to its
`directory` value when `name` is omitted (Requirement 1.6). `directory` is
itself required, so it's always available as a fallback identifier by the
time defaulting runs — a Project can only end up with an empty `name` if
`directory` is *also* empty, in which case the "directory is required"
validation error fires as usual and no separate "name is required" error is
needed. This is why Requirement 2.3's required-field check no longer lists
`name`: a defaulted name is never treated as missing by validation.

Defaulting happens before the duplicate-name check, so two Projects that
both omit `name` and declare the same `directory` still produce a
duplicate-name `ValidationError` (Requirement 2.4) — defaulting doesn't
create an exemption from uniqueness.

### Error types

```go
// ParseError wraps a YAML syntax error, preserving line/column
// information from the underlying decoder when available.
type ParseError struct {
    Line    int    // 0 if unknown
    Column  int    // 0 if unknown
    Message string
}
func (e *ParseError) Error() string

// ValidationError describes one problem with one project.
type ValidationError struct {
    ProjectRef string // project name if known, else "projects[<index>]"
    Field      string // e.g. "tool", "name", "whenModified[1]"
    Message    string
}
func (e *ValidationError) Error() string

// ValidationErrors aggregates every ValidationError found in a single
// Parse call (Requirement 2.5: don't stop at the first problem).
type ValidationErrors []*ValidationError
func (e ValidationErrors) Error() string // joins each error on its own line
```

Callers that need structured access (e.g. Slice 6 formatting a PR comment)
use `errors.As(err, &config.ValidationErrors{})` or
`errors.As(err, &(*config.ParseError)(nil))` rather than string-matching.

## Validation Rules (Requirement 2)

Run in this order inside `Parse`, after name defaulting and accumulating
into one `ValidationErrors` rather than returning on first failure:

1. Each project has non-empty `directory`, `tool`. (`name` is not checked
   directly — see "Name defaulting" above.)
2. `tool` is one of `terraform`, `pulumi`, `helmfile`.
3. No two projects share the same `name` (post-defaulting).
4. **Eager pattern validation** (design addition, not itemized in
   `requirements.md` but within Requirement 2's stated intent of catching
   misconfiguration early): each `whenModified` entry is a syntactically
   valid glob per `doublestar.ValidatePattern`. This is what lets
   `MatchProjects` stay error-free and match the global design's
   `ProjectMatcher` interface signature exactly — bad glob syntax is a
   config problem, caught at parse time, not a matching-time concern.
   *Alternative considered*: validate patterns lazily inside `MatchProjects`
   and skip/log bad ones. Rejected because it would let a typo'd pattern
   silently never match instead of surfacing as a config error, and it would
   force `MatchProjects` to return `([]Project, error)`, diverging from the
   documented interface.

A YAML syntax error (malformed document) short-circuits before validation
runs — there's no partial `Config` to validate.

## Matching Semantics (Requirement 3)

```go
func MatchProjects(projects []Project, modifiedFiles []string) []Project {
    var matched []Project
    for _, p := range projects {
        for _, pattern := range p.WhenModified {
            for _, f := range modifiedFiles {
                if ok, _ := doublestar.Match(pattern, normalize(f)); ok {
                    matched = append(matched, p)
                    goto next // move to next project once one file matches
                }
            }
        }
    next:
    }
    return matched
}
```

- `normalize` strips a leading `./` and converts `\`-style separators to `/`,
  since patterns are always written and compared as repo-root-relative,
  forward-slash paths (Requirement 3.6) — matching what GitHub's compare API
  already returns, so in practice normalization is a no-op safety net rather
  than a required conversion.
- Patterns are matched independently per project; a file matching one
  project's pattern has no effect on other projects.
- Order of the returned slice follows the input `projects` order (stable),
  so callers get deterministic output for consolidated comments later.

## Edge Cases

| Case | Behavior |
|---|---|
| `projects:` is absent or empty | `Parse` succeeds with `Config.Projects == nil`; `MatchProjects` returns `nil` |
| `name` omitted, `directory` present | `Project.Name` defaults to `Project.Directory` (Requirement 1.6) |
| `name` and `directory` both omitted | `ValidationError` on `directory` only; no separate "name is required" error |
| `version` missing or not `1` | Parsed as-is, not rejected — no requirement pins the schema to a single version number, and rejecting unknown versions here would require a Slice 1 change every time the schema version changes. Semantic version-gating, if ever needed, belongs to the caller |
| Duplicate project names | `ValidationError` per Requirement 2.4 |
| Unknown `tool` value | `ValidationError` listing the three valid values |
| Malformed YAML (bad indentation, wrong types) | `ParseError` with line/column when the decoder provides one |
| `whenModified` pattern with invalid glob syntax | `ValidationError` at parse time (see "Eager pattern validation") |
| Modified file list is empty | `MatchProjects` returns `nil` (no projects can match) |
| `config` map absent | `Project.Config` is `nil`; downstream code must treat `nil` and empty map identically |

## Testing Strategy

Per the global spec's dual testing approach (`design.md` "Testing Strategy"):

- **Unit tests** (target 90% coverage, matching the global doc's "Project
  matcher" line item): one file per source file above, covering each
  validation rule individually, YAML syntax errors, and glob edge cases
  (`**`, character classes, no-match, multi-pattern-single-file).
- **Property tests** using `pgregory.net/rapid` (originally `gopter`;
  migrated 2026-08, see `tasks.md`), ≥100 iterations, tagged per the global
  convention:
  - `// Feature: multi-iac-automation-platform, Property 1: Configuration Round-Trip` — generate a random valid `Config`, marshal with `go.yaml.in/yaml/v3`, `Parse` it back, assert equality.
  - `// Feature: multi-iac-automation-platform, Property 2: Tool Validation Rejects Invalid Tools` — generate configs with a random non-empty string as `tool`; assert rejection unless it's one of the three valid values.
  - `// Feature: multi-iac-automation-platform, Property 3: WhenModified Pattern Matching` — generate a project with random glob patterns and a random file list containing at least one match; assert the project is present in `MatchProjects`' result.

## Backward Compatibility

N/A — this is new functionality with no prior consumers; `internal/config`
currently contains only the Slice 0 placeholder `doc.go`, which this slice
replaces outright.
