# Tasks

**The checkpoint that matters is task 4.1.** The unit tests run the
plugin against a fake command runner and the command runner against a
scripted shell, so neither shows that Helmfile itself accepts the flags
where turnip puts them: global flags before the operation, with the
values as written. Only the real Helmfile binary shows that.

After wave 1, a review on the main model reads the change against its
deltas and design, and a fix pass applies what it finds, before the
checkpoint.

## 1. The command runner (`internal/plugin/command.go`)

- [x] 1.1 The command runner takes warning lines as an input (design
  Decision 2) and appends each as an annotation line right after the
  two header lines, before it releases the tool's first output. The
  fakes in `helmfile_test.go` and `property_test.go` follow the new
  signature. Verify: `command_test.go`: with two warnings, the record
  opens with the two header lines, then both warnings in the order
  given, then the tool's lines, for a tool that writes the moment it
  starts; with none, the record is exactly as before
  - _Deltas: plugins (Execution transcript and interleaved output)_

## 2. The Helmfile plugin (`internal/plugin/helmfile.go`)

- [x] 2.1 One ordered table of the `with:` keys the plugin reads and
  their flags (design Decision 1) builds the arguments (`--no-color`,
  then each set key's flag and value in the table's order, then the
  operation and extra arguments) and the warnings: one line per key not
  in the table, sorted, reading `with: ignored unexpected key <key>
  (supported: environment, file, kube-context, kubeconfig)`, the
  supported list taken from the table. Verify: one test per scenario of
  the `plugin-helmfile` delta (environment set or omitted, extra
  arguments after the operation, kube context, file and kubeconfig on a
  `sync`, a misspelled key, an unknown key, an empty value, no keys);
  the warnings passed to the command runner in sorted order; a failed
  run's standard-error-only output still holds the warning among its
  leading annotation lines
  - _Deltas: plugin-helmfile (Helmfile command line), configuration
    (with passthrough), plugins (Execution transcript and interleaved
    output)_

## 3. Documentation

- [x] 3.1 `docs/plugins/helmfile.md`: the four keys in a table with
  their flags and an example, that global flags reach every
  sub-helmfile, the warning line, and when to use `with.kubeconfig` or
  `KUBECONFIG` in `runner.env` (proposal Decision 3), and to set a kube
  context in one place only. `docs/using/turnip-yaml.md`'s `with:` entry
  points there. `docs/using/troubleshooting.md` explains the warning
  line. Verify: `make docs-check`; the examples match the delta's
  scenarios
  - _Deltas: plugin-helmfile (Helmfile command line), configuration
    (with passthrough)_

## 4. Checkpoint: the real Helmfile

- [x] 4.1 With Helmfile 1.8.1 (pinned for the pilot's own environments;
  `ASDF_HELMFILE_VERSION=1.8.1`), a minimal helmfile in a temporary
  directory, and a kubeconfig file holding one context, run the
  argument vector the plugin builds for `with: {environment: default,
  file: <that helmfile>, kube-context: <that context>, kubeconfig:
  <that file>}` with a subcommand that needs no cluster
  (`helmfile ... build`): Helmfile accepts the four flags before the
  operation and loads the named file. Record the command and its
  output here

## 5. Final checkpoint

- [x] 5.1 `go build ./...`, `gofmt -l ./internal ./cmd ./test` clean,
  `go vet ./...`, `make test`, `make lint` pass
- [x] 5.2 Every scenario in the deltas maps to a test or a doc change;
  `openspec validate fix-helmfile-with-keys --strict` passes;
  `openspec/roadmap.md` shows the change as implemented
- [x] 5.3 An intermittent failure is recorded here with its full output,
  not rerun away

## Repository owner

- Review and commit
- Build and deploy the Runner image from this commit (the Server is
  unchanged); a Project's `with.kube-context` then reaches Helmfile,
  and an unexpected key shows its warning line in the next plan
- Archive this change after `runner-timeouts`: its `plugins` delta is
  written on that change's version

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "name": "Command runner and docs", "model": "Sonnet", "effort": "medium", "tasks": ["1.1", "3.1"] },
    { "id": 1, "name": "Helmfile plugin", "model": "Sonnet", "effort": "medium", "tasks": ["2.1"] },
    { "id": 2, "name": "Real Helmfile", "model": "Sonnet", "effort": "low", "tasks": ["4.1"] },
    { "id": 3, "name": "Final checkpoint", "model": "Opus", "effort": "medium", "tasks": ["5.1", "5.2", "5.3"] }
  ]
}
```

## Deviations recorded at implementation

- 2026-11-05: Task 1.1. The command runner takes the warnings as a
  `[]string` parameter beside the idle timeout; each is written in the
  `@@ turnip: ... @@` form before the reader releases the tool's first
  output.
- 2026-11-05: Task 2.1. The failed-run test uses the fake command
  runner, which writes the warning line itself; the real ordering is
  covered by task 1.1's tests.
- 2026-11-05: Task 3.1. "Reaching a cluster" in the Helmfile page now
  selects the context with `with.kube-context` rather than the
  helmfile's own `kubeContext`.
- 2026-11-05: Review fixes. (1) `make lint` failed: `misspell` flagged
  the deliberate `enviroment` in the misspelled-key test, now one
  constant with a lint exemption; and `testifylint` rejected an
  assertion built from a composite literal. (2) The developer guide's
  Helmfile command line was stale. (3) The "Reaching a cluster" example
  now sets `with.kube-context` as its sentence says.
- 2026-11-05: Task 4.1. With Helmfile 1.8.1, `helmfile --no-color
  --environment default --file <helmfile> --kube-context ctx
  --kubeconfig <kubeconfig> build` exited 0 and loaded the named file;
  its output showed `helmDefaults.kubeContext: ctx`, so the context
  reaches every Helm call. A nonexistent `--file` path failed with
  `specified state file ... is not found`, so the flag is honored.

