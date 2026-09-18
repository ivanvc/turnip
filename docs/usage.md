# Using turnip

This assumes turnip is already deployed (`docs/deployment.md`) and a
repository has a `turnip.yaml` (`docs/configuration.md`). It covers what
happens day-to-day on a pull request.

## Automatic plans

Opening a PR, or pushing a new commit to one, triggers a **plan**
automatically for every project whose `whenModified` glob matches at
least one changed file — no comment needed. For Helmfile, "plan" means
`helmfile diff`.

Each triggered project gets its own check run
(`turnip/<project>/<operation>`, e.g. `turnip/web/diff`) and a
consolidated PR comment summarizing every project touched by that PR
(a verdict line up top, then one collapsible section per project) — see
"What you'll see on the PR" below.

## Triggering by comment

Comment on the PR:

```
/turnip <operation> [project...] [-- extra args]
```

or scope it to one tool by using the tool's own name instead of `turnip`:

```
/helmfile <operation> [project...] [-- extra args]
```

The difference: `/turnip ...` considers every project in `turnip.yaml`;
`/helmfile ...` (or `/terraform`, `/pulumi`) only considers projects
whose `tool` field matches. `<operation>` must be one the tool actually
supports (Helmfile: `diff`, `apply`, `sync`, `destroy`); naming a project
list is optional — omit it to target every matching project at once.

Examples:

```
/turnip plan
/turnip plan web
/helmfile apply web api
/turnip apply web -- --auto-approve
/turnip plan -- --context=diff
```

Anything after `--` is passed straight through to the tool's own CLI
(`extra args` in the table above) — turnip doesn't interpret it.

You can put more than one trigger line in a single comment; each runs in
order (one finishes before the next starts), and a malformed line (e.g.
`/turnip` with nothing after it) is reported back without blocking the
other, well-formed lines in the same comment.

Only `/turnip`, `/terraform`, `/pulumi` and `/helmfile` are turnip's.
A line starting with anything else — another bot's command like `/jira`,
a `/cc`, or a file path pasted at the start of a line — is ignored
completely: no operation, and no reply saying it was ignored. Turnip
stays silent on comments that aren't addressed to it.

### Releasing a lock manually

```
/turnip unlock [project...]
```

Normally you'll never need this — a lock releases on its own once an
apply succeeds, or when the PR merges or closes. Use `unlock` to abandon
a stale plan (e.g. the PR is being reworked and the old plan no longer
applies) without merging or closing the PR first. Needs write permission
(see below), and never runs a tool or creates a Job — it's a pure Redis
operation.

## Plan → apply, and why apply needs a plan first

Plan and apply are linked: an `apply` re-uses exactly what the most
recent successful `plan` on that PR computed — it never re-plans first.
This is what makes the check run/comment you saw before clicking
"apply" an accurate preview of what's about to happen, not a stale guess.
Practically:

- Applying with no prior plan on this PR (or a plan that's since been
  superseded/unlocked) fails with a comment asking for a fresh
  `/turnip plan` first.
- A held lock blocks *other* PRs from planning the same project — you'll
  see a comment naming which PR holds it, and either wait for that PR to
  merge/close (auto-releases) or have someone unlock it.

## Who can trigger what

- **Anyone who can open a PR** gets automatic plans for free — no
  authorization check happens for the PR-opened/synchronize path at all.
- **Triggering anything by comment** requires being a repository
  collaborator (any permission level, including read-only) — a
  non-collaborator's comment gets a reply explaining that, and nothing
  runs.
- **Anything beyond a plan** — apply, sync, destroy, unlock — additionally
  requires write access to the repository. A collaborator without write
  access gets a reply naming the specific permission gap.

## What you'll see on the PR

- **Check runs**, one per (project, operation), named
  `turnip/<project>/<operation>` — `in_progress` while the Runner Job is
  executing, then `success`/`failure` with the tool's own output attached.
- **A PR comment** consolidating every project touched by that trigger.
  It opens with a **verdict line** — the total change across every
  project, and how many reported no changes or failed — so you can tell
  whether the PR needs attention without expanding anything.

  Below it, one collapsible section per project. Each section's heading
  stays visible while collapsed and carries that project's status and
  change counts (`✅ web · diff · +1 ~4 -2`), so a run across many
  projects is scannable at a glance. Expanding one shows the full command
  output, followed by the commands that act on **that project alone**:

  ```
  - Apply just this project: /turnip apply web
  - Re-plan it: /turnip diff web
  - Release its lock: /turnip unlock web
  ```

  Only the commands that actually apply are shown — a project that failed
  is never offered an apply, and unlock appears only where this PR is
  holding a lock. The comment closes by naming the projects this PR has
  locked, with `/turnip apply` and `/turnip unlock` for acting on all of
  them at once.

  A plan holds each project's lock until it is applied or released, which
  is what stops another PR planning the same project underneath you.
  `/turnip unlock` gives it up without applying.

  A single project's output that's too long for one GitHub comment
  splits across multiple comments rather than getting truncated. If even
  that isn't enough, the **earliest** sections are dropped first and the
  comment says how many — the end is kept, because that's where the
  errors and the summary are.

If something fails partway — a lock conflict, a GitHub API hiccup, a tool
error, a Job that never started — see `docs/troubleshooting.md` for what
each specific symptom means and what to do about it.
