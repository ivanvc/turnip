# Design

## Context

See `proposal.md` for why, and the deltas for the behavior. As the code
stands:

- **Counting happens on the Runner, in the plugin.**
  `HelmfilePlugin.Execute` counts a `diff`'s changed releases from the
  record's standard output (`parseChangedReleases`) and an `apply`'s or
  `sync`'s tables from its standard error (`parseReleaseTables`). The
  counts travel to the Server in the result as three numbers.
- **"Counts not reported" is decided on the Server, for mutating
  operations only.** `HandleResult` asks the plugin
  `MutationChangesReported(output)` for a successful operation that is
  not the plan, and marks the result `Unreported`; the comment and the
  check then read "succeeded". For Helmfile the answer comes from the
  transcript: an older banner, or a log level above info on the command
  line turnip annotates (`argsSilenceTables`).
- **A plan's Lock is decided from its counts.** `lockEventFor` keeps a
  plan applicable when any count is non-zero or the tool acts without
  changes, and releases the Lock otherwise. It does not know whether
  the counts were read.
- **The summary Helmfile prints for a removal** (v1.8.1, `pkg/app/run.go`)
  goes through its logger to standard error, after the diffs:
  `Affected releases are:` followed by one line per release,
  `  <name> (<chart>) DELETED` or `UPDATED`, sorted, then a blank line.
  `UPDATED` lines appear only with Helmfile's `--detailed-exitcode`.
  With `--no-color`, which turnip always passes, `DELETED` carries no
  color codes.

## Goals / Non-Goals

**Goals**
- A plan that removes a release reports it as destroyed
- A plan whose removals cannot be seen says so instead of "no changes"
- An unreadable plan never gives up its Lock as if it had nothing to apply

**Non-Goals**
- Detecting Helmfile's log level set through the image's environment
  (as for the apply tables today)
- Which next steps a plan offers (`fix-apply-suggested-with-lock`)

## Decisions

### 1. The removals are parsed beside the release tables

A new parser in `helmfile_parse.go` reads the record's standard error:
each `Affected releases are:` line opens a summary, each following line
of the form `  <name> (<chart>) DELETED` counts one destroyed release,
and the summary ends at the first line that is neither blank nor of
that form. Summaries are summed. `UPDATED` lines end nothing and count
nothing. Color codes are stripped first, as the table parser does,
though `--no-color` means there are none.

`Execute` adds the count to the `diff`'s `Destroy`, beside the changed
count it already reads from standard output. Reading only standard
error is what keeps the same text inside a manifest from counting, as
it does for the apply tables.

- **Alternative considered:** run `helm list` or `helmfile list` after
  the diff to find releases to remove.
- **Rejected because:** it is a second Helmfile run, against the
  cluster, for what Helmfile already prints.

### 2. "Counts reported" becomes a question about any operation

The plugin method is generalized:

```go
// ChangesReported reports whether a successful operation's counts are
// meaningful, given its transcript. False renders "succeeded".
ChangesReported(operation, output string) bool
```

It replaces `MutationChangesReported(output)`. `HandleResult` asks it
for every successful operation, the plan included. Helmfile answers as
before for `apply` and `sync`, and for `diff` answers false when the
command line sets the log level above info (the same
`argsSilenceTables`), since the summary is then not printed. Another
tool's plugin answers true for its plan unless it has a reason not to.

- **Alternative considered:** decide on the Runner, where the plugin
  counts, and send a flag in the result.
- **Rejected because:** it adds a field to the protocol for a decision
  the Server already makes from the same transcript, and two places
  would then decide it.

### 3. An unreported plan is applicable

`lockEventFor` is given whether the counts were reported, and keeps a
plan whose counts were not as applicable, as it does one with changes.
For Helmfile nothing moves (it acts without changes, so its Lock is
kept either way), but the rule is the `plugins` delta's and holds for
any tool.

## Verification

| What | How |
|---|---|
| The summary parser | tests for one removal, two, two summaries (summed), `UPDATED` lines (not counted), a summary followed by other log lines, color codes, and the summary's text on standard output (not counted) |
| The `diff` counts | `Execute` with a record holding diffs on standard output and a summary on standard error reports both; the delta's scenarios |
| Quiet plans | `ChangesReported("diff", ...)` is false for `-q`, `--quiet` and `--log-level warn`, true for `--debug -q` and for no flag; `HandleResult` marks a quiet plan `Unreported` and it renders "succeeded" |
| The Lock | a plan with zero counts and `Unreported` is kept applicable, for a tool that does not act without changes too |
| Nothing else moves | the existing apply-table and unreported tests, renamed to the new method |

## Risks / Trade-offs

- [Helmfile rewords the summary] → removals stop being counted and a
  plan reads no changes again; the parser is pinned to the v1.8.1
  wording by its tests, and `docs/plugins/helmfile.md` says removals are
  read from that summary.
- [A quiet plan never shows counts] → by decision (proposal); the docs
  warn against changing Helmfile's log level.

## Migration Plan

None. The plugin interface changes inside the repository only.
