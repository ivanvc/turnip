# Design

## Context

The Helmfile plugin (`internal/plugin/helmfile.go`) builds Helmfile's
arguments from one `with:` key, `environment`, and ignores the rest of
the Project's `with:` map (`ExecuteOptions.Config`). The command runner
(`execCommand` in `internal/plugin/command.go`) writes the record's two
opening annotation lines right after the tool starts, while the reader
holds the tool's first output back until they are in, so they always
come first.

The change has two parts: three more keys become flags, and a key the
plugin does not read gets a warning line right after the command line.
The second part has to go through the command runner, since only it
can place a line between the header and the tool's first output.

## Decisions

### 1. One table drives the flags and the warning

The plugin holds one ordered table of the `with:` keys it reads and the
global flag each becomes:

| Key | Flag |
|---|---|
| `environment` | `--environment` |
| `file` | `--file` |
| `kube-context` | `--kube-context` |
| `kubeconfig` | `--kubeconfig` |

The arguments are `--no-color`, then each key of the table set to a
non-empty value, in the table's order, as `<flag> <value>`, then the
operation and the extra arguments, as today. The warning's list of
supported keys is the same table's keys, so adding a fifth key later
(`--namespace`, say) is one row, and the warning cannot fall out of
step with what is actually read.

Every key of `with:` not in the table is unexpected. The plugin sorts
them, so the record is the same on every run, and renders one line for
each: `with: ignored unexpected key <key> (supported: environment, file,
kube-context, kubeconfig)`. An unexpected key is warned about whatever
its value, an empty one included: the point is that the key is not
read, not what it holds. A known key set to an empty value is neither
passed nor warned about.

- **Alternative considered:** a pass-through of any `with:` key named
  like a Helmfile flag. **Rejected because:** any key would then reach
  Helmfile's command line, including ones that change what it does in
  ways a reviewer of `turnip.yaml` cannot see (`--helm-binary`,
  `--interactive`); a short list read deliberately keeps the command
  line legible, and the backlog lists the next candidates.

### 2. The command runner writes the warnings after the header

The command runner gains the warning lines as an input, and appends
them as annotation lines right after the two it writes today, before it
releases the tool's first output. They are part of the record's leading
annotation lines, so every output a plugin selects keeps them (the
`plugins` capability's "Output chosen by the tool"), including a failed
Helmfile run whose output is its standard error only. They also reach
the Runner's log and the live stream with the rest of the record.

The input is plain lines, not a Helmfile notion: the command runner
knows nothing of `with:`, and a later plugin can use it for any warning
about its configuration.

- **Alternative considered:** the plugin appending the lines to the
  record itself. **Rejected because:** before the command runner is
  called they would come ahead of the header, and after it they would
  land among, or after, the tool's own lines; only the command runner
  knows the moment between the two.
- **Alternative considered:** a notice raised by the Server when it
  reads `turnip.yaml` (the proposal's Decision 1 alternative).
  **Rejected for now because:** it needs the Server to learn each
  plugin's keys and a new path for warnings into the comment; the
  transcript line sits where the missing flag was noticed. Kept in the
  backlog.

### 3. Values are passed as written

turnip does not resolve, check or rewrite a value: a relative `file` or
`kubeconfig` is resolved by Helmfile against the Project's directory,
where it runs, and a path under `/turnip/...` (the repository's Secret
mount, the checkout) works as it does for `KUBECONFIG` today. A value
that is wrong fails in Helmfile, with Helmfile's message, in the same
transcript.

## Documentation

`docs/plugins/helmfile.md` lists the four keys in a table with their
flags and an example, says global flags reach every sub-helmfile, shows
the warning line, and says when to prefer `with.kubeconfig` and when
`KUBECONFIG` in `runner.env` (the proposal's Decision 3).
`docs/using/turnip-yaml.md` keeps its `with:` entry short and points
there. `docs/using/troubleshooting.md` gains the warning line: what it
means, and that the operation still ran without that key.

## Verification

| What | How |
|---|---|
| The command line | `helmfile_test.go`: one case per scenario of the `plugin-helmfile` delta (environment, kube context, file and kubeconfig on a `sync`, extra arguments after the operation, an empty value, no keys at all) |
| The warning | an unexpected key and a misspelled one produce the line, keys sorted, the supported list matching the table; no line when every key is known; the operation still runs |
| Its place in the record | `command_test.go`: with warnings given, the record opens with the two header lines, then the warnings, then the tool's lines, even when the tool writes at once; with none, the record is exactly as today |
| Its place in the output | a failed Helmfile run keeps the warning among the leading annotation lines in its standard-error-only output |
| A real Helmfile | `helmfile --no-color --kube-context x --kubeconfig y --file z diff` is accepted by Helmfile 1.8.1's argument parser (the flags exist and come before the operation) |

## Risks

- **Projects whose `with:` already held other keys** start showing a
  warning line in every run. That is the point; the line names the key
  and the run is otherwise unchanged.
- **A kube context set twice**, in `with.kube-context` and in the
  helmfile's own `helmDefaults.kubeContext`: Helmfile decides which
  wins, not turnip. The docs say to set it in one place.
