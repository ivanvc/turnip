# Design Document: Runner Workspace & Project Environment (Slice 12)

## Overview

Three changes, related by the fact that the first is what makes the second
usable:

1. The Runner's filesystem becomes **predictable** — `/turnip/src` and
   `/turnip/tools` instead of a randomly-named `os.MkdirTemp` directory
   and a top-level `/tools`.
2. A Project may declare **environment variables**, applied to the
   IaC_Tool subprocess.
3. turnip.yaml's version field becomes **`schemaVersion: v1alpha1`**,
   enforced rather than parsed and discarded.

Nothing here is cloud-aware. turnip arranges a filesystem and an
environment; what a repository puts in them — an AWS profile file, a GCP
credential configuration, nothing at all — stays the repository's
business. The motivating case (a Runner assuming a second IAM role,
which AWS can only express through a config file the tool must be
pointed at) is a *consequence* of these mechanisms, not a feature of
them. It appears once, at the end, as a worked example.

## Decisions

### Decision 1: the Workspace is a Config field with a temp-directory fallback, not a new seam

`Config` gains `WorkspaceDir`, populated from `TURNIP_WORKSPACE_DIR` the
same way `ToolsDir` is populated from `TURNIP_TOOLS_DIR`. When it is
empty, the Runner creates a temporary directory as it does today.

Production always sets it, from the Job spec. Everything that doesn't —
`runWith`'s existing tests, a local `go run` — keeps today's behavior
with no changes, because `testRunConfig()` simply never sets the field.

*Alternative considered*: a `workspaceProvider func() (dir string, cleanup
func(), err error)` seam threaded into `runWith`, matching the existing
`cloner` and `pluginSelector` seams. *Rejected because* `runWith` already
carries four seams, and a seam earns its place when *policy* varies —
"ensure empty", "reuse for caching" — not when a value varies. Revisit if
a cache mount at a known path ever arrives, which the fixed layout makes
possible.

### Decision 2: two `emptyDir` volumes, not one mounted at `/turnip`

| Volume | Mounted at | initContainer | Runner |
|---|---|---|---|
| `tools` | `/turnip/tools` | read-write | read |
| `workspace` | `/turnip/src` | **not mounted** | read-write |

Mounting each at its own path creates the `/turnip` parent implicitly;
nothing needs to create it.

*Alternative considered*: one volume mounted at `/turnip`, with the
initContainer writing to a `tools` subdirectory. *Rejected because* it
would give the tool-provisioning initContainer write access to the clone
path before the clone happens. The isolation costs one extra volume
declaration.

### Decision 3: a Project's environment is set on the Job spec, and the Runner does nothing

`BuildJob` already receives the `config.Project`, so it appends each of
`project.Env`'s pairs to the Runner container's `env` list, after turnip's
own variables. The kubelet starts the Runner with them already present,
and the tool subprocess inherits them because `execCommand` leaves
`cmd.Env` nil — the same inheritance `PATH` already relies on.

The Runner needs no code at all for this: no transport encoding, no
parsing, no `os.Setenv` loop, and no tests for any of it. The variables
are also visible in `kubectl describe job`, which is where someone
debugging "why didn't the tool see my profile?" will look first.

**Values are escaped on the way in.** Kubernetes expands `$(VAR)`
references inside container environment values, resolving them against
entries defined *earlier* in the same list and leaving unresolvable ones
as literal text without failing the container. Requirement 2.6 promises
values reach the tool byte-for-byte, so every `$` is doubled: the
documented escape is `$$`, and "escaped references are never expanded,
regardless of whether the referenced variable is defined or not".

*Alternative considered*: the Server encodes `project.Env` as JSON in one
variable — mirroring `TURNIP_TOOL_CONFIG`, which already carries
`project.Config` — and the Runner parses it and calls `os.Setenv`.
*Rejected because* it is a transport, a parser and a loop to arrive at
exactly the state Kubernetes will hand us for free.

*Alternative considered*: add `Env map[string]string` to
`plugin.ExecuteOptions` and have `execCommand` set `cmd.Env`. *Rejected
because* it changes the Plugin interface for every tool to serve one
caller, and a non-nil `cmd.Env` means reconstructing the entire
environment rather than adding to it.

*Alternative considered*: skip the escaping, so a Project could write
`$(TURNIP_WORKSPACE_DIR)/.turnip/aws-config` and have Kubernetes resolve
it — the placeholder expansion this slice otherwise declines to build.
*Rejected because* it silently rewrites values that merely happen to
contain `$(`, and an unresolvable reference survives as literal text
rather than erroring, which is a bad failure to debug. It also
contradicts Requirement 2.6. Worth revisiting only if referencing the
workspace from `env` turns out to be something people actually want.

**A door this leaves open**: Kubernetes' `env` entries support
`valueFrom` with `secretKeyRef` and `configMapKeyRef`. If turnip ever
supports a value sourced from a Secret, this design extends to it; the
Runner-applied alternative would need a parallel mechanism invented from
scratch.

### Decision 4: no compatibility machinery for the previous schema

`Parse` uses `yaml.Unmarshal`, which silently ignores unknown keys, so a
file still carrying `version: 1` simply has that line dropped and then
fails because `schemaVersion` is absent. That error is clear and
actionable, and it is the whole behavior: turnip carries **no** code to
recognize the old field.

A tombstone field bound to `yaml:"version"` would buy a marginally better
message — naming the rename rather than the missing key. *Rejected
because* it is compatibility machinery for a schema that was never
released, living in the parser indefinitely to describe something turnip
no longer accepts. `alpha` exists so that pre-1.0 breaking changes are
paid for by consumers updating their files, not by the codebase carrying
shims.

*Alternative also considered*: `yaml.Decoder` with `KnownFields(true)`,
rejecting every unrecognized key. *Rejected because* it is a broader
behavioral change than this slice's scope — it would reject files
carrying harmless extra keys — and it would produce a generic
unknown-field error anyway, so it does not serve this purpose either. It
deserves its own decision rather than arriving as a side effect of a
rename.

### Decision 5: schema-version problems reuse `ValidationError` with a file-level reference

`ValidationError` is project-scoped: `ProjectRef`, `Field`, `Message`,
where `ProjectRef` is a project name or `projects[<index>]`. A schema
version belongs to the file, not to a project.

Rather than introduce a second error type, `ProjectRef` carries the file
itself (`turnip.yaml`), giving `config: turnip.yaml: schemaVersion:
unsupported version "v0"; this turnip supports "v1alpha1"`.

*Alternative considered*: a distinct `FileValidationError` type, which
would require `ValidationErrors` to become `[]error` rather than
`[]*ValidationError`. *Rejected because* that ripples through every
`errors.As` call site and test in `internal/config` and
`internal/orchestrator` to produce an identical message for the reader.

## Data Model

`internal/config`:

```go
type Config struct {
    SchemaVersion string    `yaml:"schemaVersion"`
    Projects      []Project `yaml:"projects"`
}

type Project struct {
    Name         string            `yaml:"name"`
    Directory    string            `yaml:"directory"`
    Tool         string            `yaml:"tool"`
    WhenModified []string          `yaml:"whenModified"`
    Config       map[string]string `yaml:"config"`
    Env          map[string]string `yaml:"env"`
}
```

`Env` is deliberately separate from `Config`: `Config` is tool-specific and
read by Plugins (`environment`, `version`, `serviceAccount`), while `Env`
is passed to the process and never interpreted.

`internal/runner`:

```go
type Config struct {
    // ... existing fields ...

    // WorkspaceDir is where the repository is cloned. Empty means the
    // Runner creates (and removes) a temporary directory instead.
    WorkspaceDir string
}
```

### Accepted schema versions

| Value | Status |
|---|---|
| `v1alpha1` | the only accepted value |
| anything else | rejected, naming what was found and what is supported |
| absent | rejected; no default is applied |

## Job Spec Changes

| | Before | After |
|---|---|---|
| Tools mount | `/tools` | `/turnip/tools` |
| Workspace | `os.MkdirTemp` inside the container | `/turnip/src`, an `emptyDir` |
| Env vars set | `TURNIP_TOOLS_DIR` | `TURNIP_TOOLS_DIR`, `TURNIP_WORKSPACE_DIR`, plus the Project's `env` (escaped) |
| Volumes | one (`tools`) | two (`tools`, `workspace`) |

`/tools` is internal — the constant lives in `internal/jobs`, the Runner
learns the value at runtime, and neither `docs/` nor `deploy/` names it —
so moving it costs a constant and one test literal.

## Runner Startup Sequence

A Project's variables arrive with the container rather than being applied
mid-flight, so there is no ordering to get wrong. Separation rests on
names instead: the Runner reads only `TURNIP_*`, and Requirement 2.3
forbids a Project from setting anything in that namespace.

```mermaid
sequenceDiagram
    participant K as Kubelet
    participant R as Runner
    participant P as Plugin
    participant T as Tool subprocess

    K->>R: start with TURNIP_* env plus the Project's env, from the Job spec
    R->>R: ConfigFromEnv (reads only TURNIP_*, which a Project cannot set)
    R->>R: PATH = ToolsDir + PATH
    alt WorkspaceDir set
        R->>R: use it; do not remove afterwards
    else empty
        R->>R: MkdirTemp; remove on exit
    end
    R->>R: clone into the workspace
    R->>P: Execute(operation, opts)
    P->>T: exec, inheriting the Runner's environment
    T-->>P: output, exit code
    P-->>R: ExecuteResult
    R->>R: strip workspace path from reported output
```

## API Surface

`internal/runner` gains one unexported helper. Its signature is the
contract; the cleanup return is what encodes Decision 1's asymmetry
between a directory the Runner made and one it was handed:

```go
func resolveWorkspace(dir string) (path string, cleanup func(), err error)
```

`internal/config` gains validation only — no new exported surface.

## Errors

| Condition | Message shape |
|---|---|
| Unsupported `schemaVersion` | `turnip.yaml: schemaVersion: unsupported version "v0"; this turnip supports "v1alpha1"` |
| Missing `schemaVersion` | `turnip.yaml: schemaVersion: required` |
| Reserved env name | `<project>: env["TURNIP_TOOL"]: names beginning with "TURNIP_" are reserved` |
| Reserved `PATH` | `<project>: env["PATH"]: PATH is reserved; the tools directory is prepended to it at startup` |

All are `*ValidationError` values inside `ValidationErrors`, so a file with
several problems reports them together (Requirement 2.5's convention).

## Edge Cases

| Case | Behavior |
|---|---|
| `schemaVersion` wrong *and* projects invalid | All reported together. A reader may see project errors that are artifacts of reading an unknown shape; accumulating is still preferred over hiding real problems behind one error (Requirement 4.8) |
| `env` empty or absent | No variables applied; identical to today |
| `env` value is an empty string | Applied as an empty value, not skipped — `FOO=` is meaningful to some tools |
| `env` value contains `$(` | Escaped to `$$(`, so the tool receives it verbatim and Kubernetes performs no substitution |
| Two projects with different `env` | Unrelated: each Operation is its own Job, its own pod, its own Runner process |
| `WorkspaceDir` set to a path that doesn't exist | Created. A missing *mount* is a pod that never starts, which the existing start-timeout sweep reports |
| Workspace non-empty at start | Cannot happen with an `emptyDir` per pod; not defended against |

## Testing Strategy

- **`internal/config`**: table-driven validation tests for each error in
  the table above; a round-trip test that `env` survives parsing; a
  property test extension confirming any name beginning with `TURNIP_` is
  rejected regardless of suffix.
- **`internal/jobs`**: the Job carries two volumes with the expected mount
  paths; the initContainer mounts only `tools`; both new env vars are set;
  a Project's `env` appears on the Runner container with every `$`
  doubled, and a value containing no `$` is unchanged.
- **`internal/runner`**: `resolveWorkspace` with a given directory (used,
  not removed) and without one (created, removed).
- No new dependency, and no test requires a real cluster.

## Backward Compatibility

The schema change is breaking by design, which is what `v1alpha1`
announces. Every turnip.yaml must gain `schemaVersion: v1alpha1` and drop
`version: 1`. An unmigrated file fails because `schemaVersion` is absent;
turnip carries no code to recognize the old field, by choice (Decision 4).

With no releases cut and one repository using turnip, the migration is a
two-line edit in one file plus the fixtures listed in Requirement 4.10.

The `/tools` → `/turnip/tools` move is invisible outside the Job spec: the
Runner has always learned that path from `TURNIP_TOOLS_DIR` rather than
assuming it.

## Worked Example (not a turnip feature)

To show the mechanisms are sufficient, and to make clear where turnip's
involvement stops. A repository commits an AWS profile file and points at
it:

```yaml
schemaVersion: v1alpha1
projects:
  - name: project
    directory: environments/project
    tool: helmfile
    env:
      AWS_CONFIG_FILE: /turnip/src/.turnip/aws-config
      AWS_PROFILE: target
      AWS_SDK_LOAD_CONFIG: "1"
```

turnip sets three variables and clones into a known path. The AWS SDK
does the rest — including refreshing the assumed role, which is why this
is preferable to injecting credentials. `AWS_SDK_LOAD_CONFIG` is present
because Go SDK v1 ignores the shared config file without it, silently.

turnip does not validate any of these names, know what a profile is, or
contain an AWS dependency. The same three-variable shape serves GCP's
credential configuration; Azure typically needs no variables at all,
since its identity is selected by ServiceAccount annotation.
