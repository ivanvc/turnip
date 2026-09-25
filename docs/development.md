# Developing turnip

Notes for contributors. Build, test and lint commands, and the
spec-driven workflow under `.kiro/specs/`, are in [`CLAUDE.md`](../CLAUDE.md).

## Adding a tool

Everything turnip knows about a tool lives in that tool's Plugin, and the
Plugins turnip ships are listed in one place. Adding a tool is three steps,
all inside `internal/plugin`; nothing else in the tree names a tool.
[`internal/plugin/helmfile.go`](../internal/plugin/helmfile.go) is the
worked example.

### 1. Write the Plugin

Implement `plugin.Plugin` (`internal/plugin/plugin.go`):

| Method | Returns |
|---|---|
| `Name()` | the name repositories write in `uses:` and in trigger comments (`/<name> <operation>`) |
| `GetOperations()` | the tool's own operation names, not a shared vocabulary (helmfile: `diff`, `apply`, `sync`) |
| `GetPlanOperation()`, `GetApplyOperation()` | which of those plans and which applies |
| `ActsWithoutChanges()` | whether any operation besides the plan can change infrastructure when the plan found nothing (true for helmfile, because of `sync`) |
| `Provisioning()` | how the tool reaches a Runner Job; see step 2 |
| `Execute()` | runs one operation by shelling out to the tool's CLI |

`Execute` goes through the `commandRunner` seam in `command.go` rather
than calling `os/exec` directly, so the Plugin's tests can fake the
subprocess without a real binary or cluster (see `helmfile_test.go`).

### 2. Declare its provisioning

`Provisioning()` returns a `provisioning.Spec` (`internal/provisioning`):

- **`Image`**: the tool's image repository, **fully qualified** with its
  registry host (`ghcr.io/helmfile/helmfile`, not `helmfile/helmfile`),
  and with no tag. The tag is the version the repository writes in
  `uses:`, appended exactly as written, so `helmfile@v1.7.4` runs
  `ghcr.io/helmfile/helmfile:v1.7.4`. A Plugin never declares a default
  version.
- **`Strategy`**: one of two, both implemented once in `internal/jobs`:
  - **`CopyOut`**: an init container copies the tool's binary out of
    `Image` onto a shared volume, and turnip's own Runner image runs it.
    Right for a tool that is a single self-contained binary. Set
    **`BinaryPath`** to where that binary lives inside `Image`.
  - **`RunInImage`**: `Image` itself is the Job's main container, with
    turnip's runner binary copied in beside it, so the tool runs with its
    image's own `PATH`, environment and `HOME`. Right for a tool that
    needs helpers or plugins its image provides (helmfile needs `helm`,
    helm-diff and `sops`). Leave `BinaryPath` empty.

A new tool picks a strategy; it never builds Kubernetes objects itself.
If neither fits, the new strategy belongs in `internal/provisioning` and
`internal/jobs`, not in the Plugin.

### 3. Register it

Add the constructor to the list in
[`internal/plugin/registry.go`](../internal/plugin/registry.go). That one
line is what makes the tool exist everywhere:

- `turnip.yaml` validation accepts it in `uses:` (and a tool with no
  Plugin is a validation error listing the registered ones);
- trigger comments accept `/<name> <operation>`;
- the Server and the Runner both find the Plugin through `Registry()`.

`registry_test.go`'s `TestRegistry_EveryPluginDeclaresItsProvisioning`
runs against every registered Plugin and fails unless its `Spec` is
complete: a known `Strategy`, a fully qualified `Image` with no tag, and
a `BinaryPath` exactly when the strategy is `CopyOut`. It exists so a
half-declared Plugin fails in `go test` rather than in a Runner Job, after
a Lock is held and a check is running.

Finally, update [`configuration.md`](configuration.md)'s "Tool support"
table.
