# Proposal

## Why

A Helmfile Project's `with:` has one key with an effect, `environment`,
passed as `--environment`; every other key is accepted and silently
ignored (the `plugin-helmfile` capability says so outright). Onboarding
two environments that share one kubeconfig, a Project set
`with: {environment: staging, kube-context: <context>}`: the context was
dropped without a word, the transcript showed
`helmfile --no-color --environment staging diff` with no
`--kube-context`, and with no current context in the kubeconfig Helm
had no cluster chosen. Nothing said the key was ignored, so the only
clue was a flag missing from a command line. The same silence would
hide a typo such as `enviroment`, which plans the default environment
instead of the one named.

Found 2026-11-05 on the pilot.

## What Changes

- The Helmfile plugin passes three more `with:` keys as Helmfile's own
  global flags, beside `environment`:

  | Key | Helmfile flag | What it selects |
  |---|---|---|
  | `environment` | `--environment` | the Helmfile environment (unchanged) |
  | `file` | `--file` | the helmfile to load, relative to the Project's directory as Helmfile resolves it |
  | `kube-context` | `--kube-context` | the kubeconfig context |
  | `kubeconfig` | `--kubeconfig` | the kubeconfig file |

  Helmfile applies global flags to every sub-helmfile, so a Project
  whose helmfile only lists others gets them everywhere, which a
  `helmDefaults` entry in that helmfile would not
- A `with:` key the plugin does not read is no longer silent: the
  transcript gets an annotation line right after the command line,
  naming each unexpected key and the keys the plugin supports, for
  example `@@ turnip: with: ignored unexpected key kube-contxt
  (supported: environment, file, kube-context, kubeconfig) @@`. The
  operation still runs: a warning, not a refusal
- The docs list the keys, and say how `with.kubeconfig` relates to
  `KUBECONFIG` in `runner.env`

## Decisions for review

1. **Warn in the transcript, from the plugin.** The plugin is the only
   part of turnip that knows which `with:` keys mean something (the
   configuration library is plugin-agnostic by design), and the
   transcript is where a reader looks at the command that ran, next to
   the line that would otherwise have been missing a flag. Alternative:
   a notice at the top of the results comment, raised by the Server
   when it reads `turnip.yaml`: visible before opening the Project's
   output, but it needs the Server to ask each plugin for its keys,
   and a new path for warnings into the comment. Left for later if the
   transcript line proves too easy to miss.
2. **Warn, not refuse.** As asked: an unexpected key is reported and
   the operation runs. Refusing would also catch it, at the cost of a
   failed plan for a key that may be harmless.
3. **`with.kubeconfig` and `KUBECONFIG` both remain.** The flag is
   Helmfile's and reaches Helmfile and the Helm it runs; `KUBECONFIG`
   in `runner.env` reaches every process in the Runner (hooks,
   `kubectl`, the AWS CLI's `get-token`). The docs recommend
   `KUBECONFIG` when anything besides Helmfile and Helm must reach the
   cluster, and say Helmfile's flag wins for Helmfile when both are
   set.

## Capabilities

### New Capabilities

### Modified Capabilities
- `plugin-helmfile`: the command line takes `--file`, `--kube-context`
  and `--kubeconfig` from `with:`; an unexpected key is warned about in
  the transcript instead of silently ignored
- `configuration`: `with passthrough` says which plugin decides what an
  unknown key does, and that the Helmfile plugin warns
- `plugins`: the record's leading annotation lines include a tool's
  warnings about the Project's configuration, after the command line

## Impact

`internal/plugin` (`helmfile.go` and its tests),
`docs/plugins/helmfile.md`, `docs/using/turnip-yaml.md` (the `with:`
entry). The Runner image changes; the Server does not.

Out of scope, kept in the backlog:
- Other Helmfile global flags as `with:` keys (`--namespace`,
  `--state-values-file`, `--state-values-set`), added the same way when
  a repository needs them
- A results-comment notice for unexpected keys (Decision 1's
  alternative)

## Dependencies

- **Archive after `runner-timeouts`,** which also modifies "Execution
  transcript and interleaved output" (`plugins`); this change's delta is
  written on its version.
