# Requirements Document: Plugin System & Helmfile Plugin (Slice 2)

## Introduction

This slice delivers the `internal/plugin` package: the unified Plugin
interface that every IaC tool implements, and the first concrete
implementation of it — the Helmfile plugin. Later slices depend on this
package (Slice 5: gRPC & Runner invokes `Execute`; Slice 6: Server
Orchestration selects a plugin by tool type and passes it the modified
Project's config) but this slice does not itself talk to gRPC, Kubernetes,
or GitHub — it only runs the tool's CLI as a subprocess and returns a
standardized result.

This slice implements Requirements 3 and 18 (partially — the plugin-facing
half of tool-specific config propagation) and Requirement 13 from the
global spec (`.kiro/specs/multi-iac-automation-platform/requirements.md`),
scoped to the Plugin interface and the Helmfile implementation only.

## Glossary

(Inherited from the global spec glossary.)

- **Plugin**: A Go value implementing the unified `Plugin` interface for one
  IaC tool (Terraform, Pulumi, or Helmfile — only Helmfile is implemented in
  this slice).
- **Operation**: One of the tool-native subcommands a Plugin exposes (e.g.,
  `diff`, `apply`, `sync`, `destroy` for Helmfile). Unlike the global spec's
  glossary entry, this slice does not force operations into a standardized
  `plan`/`apply`/`destroy` vocabulary — each tool's own operation names are
  used directly, matching Requirement 3.2's intent.
- **Change Summary**: The add/change/destroy counts extracted from a
  completed Operation's output.

## Requirements

### Requirement 1: Unified Plugin interface

**User Story:** As a platform developer, I want a single Plugin interface
that all IaC tools implement, so that later slices (Runner, Server
Orchestration) can invoke any tool without tool-specific branching.

#### Acceptance Criteria

1. THE Platform SHALL define one `Plugin` Go interface that every IaC tool implementation satisfies
2. THE Plugin interface SHALL define `Name() string`, returning the tool's identifier (`"terraform"`, `"pulumi"`, or `"helmfile"`)
3. THE Plugin interface SHALL define `GetOperations() []string`, returning the tool-native operation names the Plugin supports
4. THE Plugin interface SHALL define `GetPlanOperation() string`, returning the operation name used for planning (e.g., `"diff"` for Helmfile)
5. THE Plugin interface SHALL define `GetApplyOperation() string`, returning the operation name used for applying (e.g., `"apply"` for Helmfile)
6. THE Plugin interface SHALL define `Execute(ctx context.Context, operation string, opts ExecuteOptions) (*ExecuteResult, error)`, running one of the operations returned by `GetOperations()`
7. IF `Execute` is called with an operation not present in that Plugin's `GetOperations()`, THEN THE Plugin SHALL return a structured error identifying the unsupported operation, without invoking any external command
8. THE Platform SHALL NOT introduce separate Executor, Adapter, or Workflow abstractions alongside Plugin (Requirement 3.5)

### Requirement 2: Standardized execution options and results

**User Story:** As a platform developer, I want `Execute`'s inputs and
outputs standardized across tools, so that later slices can treat every
Plugin identically regardless of which tool it wraps.

#### Acceptance Criteria

1. THE `ExecuteOptions` struct SHALL carry `WorkingDir string`, `Config map[string]string`, `ExtraArgs []string`, and `PlanData []byte`
2. THE `ExecuteResult` struct SHALL carry `Output string`, `ChangeSummary ChangeSummary`, `PlanData []byte`, `ExitCode int`, and `Error error`
3. THE `ChangeSummary` struct SHALL carry `Add int`, `Change int`, and `Destroy int`
4. THE Plugin SHALL run the tool's subprocess with its working directory set to `opts.WorkingDir`
5. THE Plugin SHALL append `opts.ExtraArgs` to the invoked command's arguments
6. THE Plugin SHALL populate `ExecuteResult.ExitCode` with the subprocess's actual exit code, including on failure
7. IF the subprocess exits non-zero, THEN THE Plugin SHALL still return a non-nil `*ExecuteResult` (with `Output`, `ExitCode`, and `Error` populated) rather than only a bare error, so that callers can report the tool's own output

### Requirement 3: Helmfile Plugin implementation

**User Story:** As a developer, I want to use Helmfile, so that I can
manage Kubernetes applications declaratively through the same PR-driven
workflow as other IaC tools.

#### Acceptance Criteria

1. THE Platform SHALL provide a Helmfile Plugin implementing the Plugin interface, with `Name()` returning `"helmfile"`
2. THE Helmfile Plugin SHALL expose `"diff"`, `"apply"`, `"sync"`, and `"destroy"` as its supported operations
3. THE Helmfile Plugin's `GetPlanOperation()` SHALL return `"diff"`
4. THE Helmfile Plugin's `GetApplyOperation()` SHALL return `"apply"`
5. WHEN the `"diff"` operation is executed, THE Helmfile Plugin SHALL run `helmfile diff`
6. WHEN the `"apply"` operation is executed, THE Helmfile Plugin SHALL run `helmfile apply`
7. WHEN the `"sync"` operation is executed, THE Helmfile Plugin SHALL run `helmfile sync`
8. WHEN the `"destroy"` operation is executed, THE Helmfile Plugin SHALL run `helmfile destroy`
9. THE Helmfile Plugin SHALL parse `helmfile diff` output to derive a count of changed releases and report it via `ExecuteResult.ChangeSummary`
10. IF `helmfile diff` output cannot be parsed for a change count, THEN THE Helmfile Plugin SHALL return a zero-valued `ChangeSummary` and still return the raw `Output` and a nil `Error` (a parsing shortfall is not an execution failure)

### Requirement 4: Helmfile environment configuration

**User Story:** As a developer, I want to specify a Helmfile environment
per Project, so that the same `turnip.yaml` project can target
`staging`/`production`/etc. without duplicating project definitions.

#### Acceptance Criteria

1. IF `opts.Config` contains an `"environment"` key, THEN THE Helmfile Plugin SHALL pass it as `helmfile --environment <value>` (or equivalent) to the subprocess for every operation
2. IF `opts.Config` does not contain an `"environment"` key, THEN THE Helmfile Plugin SHALL invoke `helmfile` without an explicit environment flag, deferring to Helmfile's own default
3. THE Helmfile Plugin SHALL ignore `opts.Config` keys other than `"environment"` without error (forward compatibility with keys other tools use)

## Out of Scope

- Terraform and Pulumi plugins — Slice 7
- Wiring `Execute` into gRPC calls from the Runner process — Slice 5
- Selecting which Plugin to use for a given Project (`tool` → Plugin lookup) and passing `turnip.yaml`'s `config` map into `ExecuteOptions.Config` — Slice 6 (this slice only defines what the Plugin does once it receives that map)
- Persisting or retrieving `PlanData` via Redis locks — Slice 3 (this slice only defines the field on `ExecuteOptions`/`ExecuteResult`; Helmfile itself has no separate plan artifact to persist — see design.md)
- Making the `helmfile` binary available to the Runner at execution time — resolved as a per-tool Kubernetes initContainer using Helmfile's own official versioned image (see `multi-iac-automation-platform/design.md`'s "Tool Binary Provisioning" section), owned by Slice 5, not Slice 0 or this slice
- Validating that a Kubernetes cluster/kubeconfig is reachable before running — a connection failure surfaces as a normal non-zero exit code and populated `Output`/`Error`, per Requirement 2.7
