# Design Document: Plugin System & Helmfile Plugin (Slice 2)

## Overview

This slice implements `internal/plugin`: the `Plugin` interface every IaC
tool satisfies, plus the first implementation of it, `HelmfilePlugin`. A
Plugin's only job is to translate `(operation, ExecuteOptions)` into a
subprocess invocation and translate the subprocess's stdout/stderr/exit code
back into an `ExecuteResult`. It has no dependency on gRPC, Kubernetes, or
GitHub — Slice 5 (Runner) calls `Execute` after cloning a repo and cd-ing
into the Project directory; Slice 6 (Server Orchestration) is what decides
*which* Plugin and Project to use.

The package replaces the placeholder `internal/plugin/doc.go` created in
Slice 0 with the real interface, types, and the Helmfile implementation.

## Package Layout

```
internal/plugin/
  plugin.go        // Plugin interface, ExecuteOptions, ExecuteResult, ChangeSummary
  command.go        // command execution seam (commandRunner type + default os/exec-based runner)
  helmfile.go        // HelmfilePlugin implementing Plugin
  helmfile_parse.go  // parseChangedReleases(output string) int
  errors.go          // UnsupportedOperationError
  doc.go             // updated package doc
```

One file per concern, matching the pattern established in
`internal/config`. `command.go` is split out specifically so
`helmfile_test.go` can inject a fake runner instead of shelling out to a
real `helmfile` binary (see "Testing Strategy" below).

## Dependencies

No new external dependencies. The default command runner uses only
`os/exec` and `context` from the standard library.

## Data Model

Mirrors the global design doc's Plugin section
(`multi-iac-automation-platform/design.md:184-228`) field-for-field, so
downstream slices designed against that sketch don't need to change:

```go
package plugin

import "context"

// Plugin defines the unified interface for all IaC tools.
type Plugin interface {
    Name() string
    GetOperations() []string
    GetPlanOperation() string
    GetApplyOperation() string
    Execute(ctx context.Context, operation string, opts ExecuteOptions) (*ExecuteResult, error)
}

type ExecuteOptions struct {
    WorkingDir string
    Config     map[string]string
    ExtraArgs  []string
    PlanData   []byte
}

type ExecuteResult struct {
    Output        string
    ChangeSummary ChangeSummary
    PlanData      []byte
    ExitCode      int
    Error         error
}

type ChangeSummary struct {
    Add     int
    Change  int
    Destroy int
}
```

### Reconciling `GetOperations()` with global Requirement 13

Global Requirement 13.2 lists only `"diff"`, `"apply"`, `"sync"` as
Helmfile's supported operations, but 13.7-13.9 describe a `Destroy`
operation that executes `helmfile destroy` when "explicitly requested" —
worded as if Destroy is invoked the same way as the other three, not as a
flag layered on `"apply"` (contrast with Terraform's `-destroy` flag on
`plan`, Requirement 6). The global design doc's Property 22 settles this by
example: *"Diff should execute `helmfile diff`, Sync should execute
`helmfile sync`, Apply should execute `helmfile apply`, and Destroy should
execute `helmfile destroy`"* — describing four directly-invokable
operations, not three-plus-a-flag.

This slice's Requirement 3.2 therefore includes `"destroy"` in
`GetOperations()`. *Alternative considered*: treat destroy as an
`ExtraArgs`-driven variant of `"apply"`, mirroring Terraform. Rejected
because it contradicts Property 22's explicit per-operation phrasing and
would require `Execute` to special-case `ExtraArgs` content to pick a
binary subcommand — string-sniffing arguments to decide behavior is exactly
the kind of implicit branching the unified interface is meant to avoid
(Requirement 1.8 / global Requirement 3.5).

### `PlanData` is unused by Helmfile

Unlike Terraform (`terraform plan -out=<file>`, later applied from that
exact file) or Pulumi, Helmfile has no persistable plan artifact distinct
from re-running `diff`. `ExecuteOptions.PlanData` and
`ExecuteResult.PlanData` exist on the struct because the interface is
shared across tools, but `HelmfilePlugin.Execute` ignores
`opts.PlanData` on input and always returns `nil` for
`ExecuteResult.PlanData`. `"apply"` re-diffs against live cluster state
rather than applying a frozen plan. Slice 3/6 (locking, orchestration) must
not assume every tool round-trips a non-empty `PlanData`.

## Command Execution Seam

```go
// commandRunner abstracts subprocess execution so tests can substitute a
// fake without invoking a real helmfile binary.
type commandRunner func(ctx context.Context, dir, name string, args []string) (stdout, stderr []byte, exitCode int, err error)

// execCommand is the default commandRunner, backed by os/exec.
func execCommand(ctx context.Context, dir, name string, args []string) (stdout, stderr []byte, exitCode int, err error)
```

`HelmfilePlugin` holds a `run commandRunner` field, defaulting to
`execCommand` when constructed via `NewHelmfilePlugin()`. Tests construct a
`HelmfilePlugin` with a fake `run` directly (unexported field, same
package) to assert on the exact command and arguments without requiring
`helmfile` to be installed or a Kubernetes cluster to be reachable.

`execCommand` distinguishes two failure shapes:
- The subprocess ran and exited non-zero: return its actual stdout,
  stderr, and exit code, with `err == nil` — this is a normal tool failure,
  not an execution error (Requirement 2.7).
- The subprocess could not be started at all (binary not found, working
  directory invalid): return `exitCode == -1` and a non-nil `err` — this
  *is* an execution error, since there is no tool output to report.

## Helmfile Plugin

```go
func NewHelmfilePlugin() *HelmfilePlugin

func (p *HelmfilePlugin) Name() string             { return "helmfile" }
func (p *HelmfilePlugin) GetOperations() []string   { return []string{"diff", "apply", "sync", "destroy"} }
func (p *HelmfilePlugin) GetPlanOperation() string  { return "diff" }
func (p *HelmfilePlugin) GetApplyOperation() string { return "apply" }

func (p *HelmfilePlugin) Execute(ctx context.Context, operation string, opts ExecuteOptions) (*ExecuteResult, error)
```

`Execute`:
1. Validates `operation` is one of `GetOperations()`; if not, returns
   `(nil, &UnsupportedOperationError{...})` without calling `p.run`.
2. Builds `args := []string{operation}` (`helmfile <operation>`, since the
   operation name *is* the Helmfile subcommand name for all four ops —
   no translation table needed, unlike a tool where the interface's
   operation name might diverge from the CLI verb).
3. If `opts.Config["environment"]` is non-empty, prepends
   `--environment <value>` (a **global** Helmfile flag that must precede
   the subcommand, per `helmfile --help`'s flag placement — confirmed
   against the installed `helmfile 1.7.4` binary's `--help` output during
   design).
4. Appends `opts.ExtraArgs`.
5. Runs via `p.run(ctx, opts.WorkingDir, "helmfile", args)`.
6. Builds `ExecuteResult`:
   - `Output`: stdout, with stderr appended if non-empty (so failures
     that only write to stderr are still visible to the PR comment).
   - `ExitCode`: as returned.
   - `Error`: nil for a completed-but-nonzero-exit subprocess (Requirement
     2.7); non-nil only when `p.run` itself returned an error (couldn't
     start the process).
   - `ChangeSummary`: populated only when `operation == "diff"`, via
     `parseChangedReleases` (see below); zero-valued for `apply`/`sync`/`destroy`
     (those operations don't emit the same diff-shaped output, and the
     global design doesn't ask for a live change count from them —
     Requirement 13.6/13.10 only ask for `diff` output parsing).
   - `PlanData`: always `nil` (see "PlanData is unused by Helmfile" above).

## Output Parsing (`helmfile diff`)

Helmfile's `diff` subcommand (via the `helm-diff` plugin it shells out to
internally) prints, per release it examines, a header line of the form:

```
Comparing release=<name>, chart=<chart>
```

followed by the diff body for that release when one exists, or nothing
before the next `Comparing release=` line (or EOF) when the release is
unchanged. `parseChangedReleases` counts headers followed by at least one
non-blank line of diff body before the next header:

```go
var comparingReleaseRe = regexp.MustCompile(`^Comparing release=(\S+),`)

func parseChangedReleases(output string) int {
    lines := strings.Split(output, "\n")
    changed := 0
    inChangedBody := false
    sawBodyLine := false
    for _, line := range lines {
        if comparingReleaseRe.MatchString(line) {
            if inChangedBody && sawBodyLine {
                changed++
            }
            inChangedBody = true
            sawBodyLine = false
            continue
        }
        if inChangedBody && strings.TrimSpace(line) != "" {
            sawBodyLine = true
        }
    }
    if inChangedBody && sawBodyLine {
        changed++
    }
    return changed
}
```

This maps the result into `ChangeSummary.Change` (Helmfile doesn't
distinguish "added" vs. "modified" releases in its header format the way
Terraform's plan output enumerates per-resource actions); `Add` and
`Destroy` stay `0` for Helmfile.

**This heuristic is a best-effort reading of `helm-diff`'s conventional
header format, not a documented, versioned output contract** — Helmfile has
no `--output json` mode for `diff` as of the `1.7.4` binary checked during
this design (`--output string` on `diff` only forwards to the diff
plugin's own text-formatting options, e.g. `simple`). Requirement 3.10
exists specifically so a future Helmfile/helm-diff release that reshapes
this header doesn't turn a parsing miss into a reported execution failure —
worst case, the PR comment shows a `0/0/0` change summary alongside the
full raw `Output`, which still contains the real diff for a human to read.

## Errors

```go
// UnsupportedOperationError is returned when Execute is called with an
// operation the Plugin does not support.
type UnsupportedOperationError struct {
    Plugin    string   // e.g. "helmfile"
    Operation string   // the invalid operation requested
    Supported []string // GetOperations()'s result, for the error message
}
func (e *UnsupportedOperationError) Error() string
```

## Edge Cases

| Case | Behavior |
|---|---|
| `Execute` called with an operation not in `GetOperations()` | `(nil, *UnsupportedOperationError)`, no subprocess started |
| `helmfile` binary not found in `$PATH` | `(nil, err)` from `p.run`; `Execute` returns `(nil, err)` — no `ExecuteResult` to populate since nothing ran |
| Subprocess starts but exits non-zero (e.g. cluster unreachable, chart error) | Non-nil `*ExecuteResult` with real `Output`, real `ExitCode`, `Error == nil` |
| `opts.Config` is `nil` or has no `"environment"` key | No `--environment` flag added; Helmfile uses its own default |
| `opts.Config` has other tool's keys (e.g. `"workspace"`) mixed in | Ignored silently — only `"environment"` is read |
| `helmfile diff` produces output `parseChangedReleases` can't recognize | `ChangeSummary{}` (all zero), `Output` still populated, `Error` still nil |
| `opts.ExtraArgs` is `nil` | No extra arguments appended; equivalent to an empty slice |

## Testing Strategy

Per the global spec's dual testing approach and this package's coverage
target (90%, "Plugin implementations" per the global design doc's Testing
Strategy section):

- **Unit tests**: one file per source file above.
  - `plugin_test.go`: `UnsupportedOperationError` message formatting.
  - `command_test.go`: `execCommand` against real trivial commands (`echo`,
    a script that exits non-zero, a nonexistent binary) to verify the
    stdout/stderr/exitCode/err contract without needing `helmfile` itself.
  - `helmfile_test.go`: constructs `HelmfilePlugin` with a fake
    `commandRunner`, asserts the exact `name`/`args`/`dir` passed for each
    of the four operations, with and without `Config["environment"]`, and
    with `ExtraArgs`.
  - `helmfile_parse_test.go`: `parseChangedReleases` against hand-written
    fixture strings covering zero changed releases, one, multiple, and a
    release header with no body (unchanged).
- **Property tests** using `gopter`, ≥100 iterations, tagged per the global
  convention:
  - `// Feature: multi-iac-automation-platform, Property 4: Plugin Result Structure Completeness` — for a random operation among `GetOperations()` and random `ExecuteOptions`, using a fake `commandRunner` that returns randomized stdout/stderr/exitCode, assert the returned `*ExecuteResult` is non-nil and its `Output`/`ExitCode` fields are always populated (never silently dropped).
  - `// Feature: multi-iac-automation-platform, Property 22: Helmfile Plugin Command Execution` — for each of `"diff"`, `"sync"`, `"apply"`, `"destroy"`, assert `Execute` invokes `helmfile <operation>` (with `--environment` correctly placed when configured), using a fake `commandRunner` to capture the call instead of running a real subprocess.

No integration tests against a real Kubernetes cluster or real `helmfile`
binary are part of this slice — that belongs to Slice 11 (Integration
Testing) in the global roadmap, which already plans a `kind`-based
environment.

## Backward Compatibility

N/A — this is new functionality with no prior consumers;
`internal/plugin` currently contains only the Slice 0 placeholder
`doc.go`, which this slice replaces outright. The `Plugin` interface itself
is designed to be extended by Slice 7 (Terraform, Pulumi) without change —
those plugins implement the same five methods against the same
`ExecuteOptions`/`ExecuteResult`/`ChangeSummary` types.
