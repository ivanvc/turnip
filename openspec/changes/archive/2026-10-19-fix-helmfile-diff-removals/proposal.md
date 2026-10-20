# Proposal

## Why

A Helmfile plan that removes a release reports no changes. Seen
2026-10-14: a release set to `installed: false` was still deployed, the
`diff` said `no changes`, and the `apply` that followed uninstalled it,
reported as `-1`.

The cause is in how Helmfile reports a removal (read from Helmfile
v1.8.1's source, `pkg/app/run.go`). For a release it would update,
`helmfile diff` runs `helm diff`, whose `Comparing release=` header and
diff go to standard output. For a release it would remove, it runs no
`helm diff` at all: it finds the release with `helm list` and reports it
only in a summary its logger writes to standard error once the diffs are
done:

```
Affected releases are:
  web (example/web) DELETED
```

turnip counts a `diff` from standard output alone (a release counts as
changed when its header is followed by a diff), so a removal counts as
nothing. The `plugin-helmfile` spec says so in as many words, "a `diff`
SHALL report only changed counts, never added or destroyed ones", and
its "Pending removal" scenario assumes a release being removed shows a
diff, which it does not.

The cost is more than a wrong number: a plan that will uninstall a
release tells the reviewer nothing will change.

## What Changes

- A `diff` also reads Helmfile's `Affected releases are:` summaries from
  standard error, and counts each release listed as `DELETED` as
  destroyed, summed over every summary printed (Helmfile prints one per
  helmfile, as it does the release tables after an apply)
- Changed releases are still counted from the diffs on standard output.
  The summary lists `UPDATED` releases only when Helmfile's own
  `--detailed-exitcode` is passed, which turnip does not pass; those
  lines are not counted, so nothing is counted twice
- The "Pending removal" scenario is replaced by one that matches what
  Helmfile prints
- A quiet `diff` (see the decision below) reports no counts, and
  renders `succeeded`
- `docs/plugins/helmfile.md` ("Change counts") says a `diff` reports
  removals as `-`, and warns against changing Helmfile's log level

## Decision: a quiet plan reports no counts (2026-10-16)

Helmfile logs the `Affected releases are:` summary at info level, so a
plan run with `-q`, `--quiet` or `--log-level` above info prints none,
and turnip cannot see its removals. Such a `diff` is treated as not
reporting its counts, and renders `succeeded`, exactly as a quiet
`apply` or `sync` already does, rather than claiming `no changes` for a
plan that may remove a release. The detection is the one the quiet
`apply` uses: the log level on the command line turnip annotates.

`docs/plugins/helmfile.md` says plainly not to change Helmfile's log
level for a Project (flags, or `HELMFILE_LOG_LEVEL` and `HELMFILE_QUIET`
in the image): turnip reads what Helmfile prints, so with less printed
its counts, and the next steps they decide, may not be accurate.

Rejected: keep showing counts for a quiet plan and only document it,
which leaves a quiet plan that removes a release reading `no changes`,
the bug this change fixes.

## Not in this change

- Which next steps a plan with no changes offers: `fix-apply-suggested-with-lock`
- Detecting a removal when Helmfile's own logging is silenced through
  its environment (`HELMFILE_LOG_LEVEL`), which turnip cannot see, as
  for the apply tables today

## Capabilities

### New Capabilities

### Modified Capabilities
- `plugin-helmfile`: a `diff` counts the releases Helmfile will remove as destroyed, and a quiet `diff`, like a quiet `apply`, reports no counts
- `locking`: the plan record keeps whether its counts were reported, so the apply that replays a quiet plan is not titled `no changes` (found at implementation)
- `plugins`: a plan's counts may be not reported, where a tool cannot read them (a quiet Helmfile `diff`); until now only a mutating operation's could be

## Impact

`internal/plugin` (`helmfile_parse.go` and the `diff` branch of
`HelmfilePlugin.Execute`), `docs/plugins/helmfile.md`. The comment, the
check and the Lock need nothing: they already render and act on a
destroy count.
