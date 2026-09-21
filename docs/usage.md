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

### Draft pull requests

turnip does **not** plan a draft automatically. Opening one, or pushing to
one, does nothing — no check run, no comment, no lock.

Marking it ready for review plans it, exactly as though it had just been
opened. You do not need an extra push.

You can still ask for a plan on a draft at any time by commenting
(`/turnip plan`, `/helmfile diff`, and so on). A comment-triggered plan on
a draft behaves like any other: it takes the project's lock, stores its
plan data, and can be applied or unlocked. Being a draft changes *when
turnip acts on its own* — never what you can ask it to do.

This is not configurable. There is no setting to turn automatic plans on
for drafts, so it isn't worth looking for one.

### Pull requests from forks

turnip runs nothing on a pull request whose branch lives in a different
repository — a fork. Opening one plans nothing, and commenting
(`/turnip plan`, `/helmfile diff`, and so on) on one does nothing either.
There is no check run, no comment and no lock.

turnip does not reply to say it declined. The refusal is recorded in the
Server's log rather than on the pull request — see "Fork pull requests
are refused" in `docs/troubleshooting.md`.

The reason is that everything in a fork is controlled by whoever opened
it, `turnip.yaml` included. Running an Operation on one would execute a
stranger's choice of tool and arguments in a pod holding the credentials
turnip deploys with, so the contents decide this, not the person asking.
A collaborator commenting on a fork gets the same refusal: being
authorized to *trigger* an operation says nothing about the *code* that
operation would run.

This is not configurable either, deliberately — there is no setting,
environment variable or `turnip.yaml` key that allows it, and an
allowlist of trusted contributors would not help, since the refusal is
about where the code comes from rather than who asked.

To run a fork's changes through turnip, bring them into a branch of this
repository — push the branch here yourself, or ask a maintainer to — and
open the pull request from there.

Closing a fork pull request still works normally: any lock it holds from
before is released, exactly as for any other pull request.

## Triggering by comment

Comment on the PR:

```
/turnip <operation> [project...] [args...]
```

or scope it to one tool by using the tool's own name instead of `turnip`:

```
/helmfile <operation> [project...] [args...]
```

The difference: `/turnip ...` considers projects using any tool;
`/helmfile ...` (or `/terraform`, `/pulumi`) considers only projects
whose `tool` field matches. `<operation>` must be one the tool actually
supports (Helmfile: `diff`, `apply`, `sync`).

## Which projects a command targets

Naming projects is optional. What you get when you omit them depends on
the operation:

| Trigger | Targets |
|---|---|
| `/turnip diff` | the projects whose `whenModified` patterns match this pull request's changed files — the same set the automatic plan picks |
| `/turnip apply` | the projects this pull request has already planned |
| `/turnip diff web` | `web`, whether or not the pull request touched it |
| `/turnip diff *` | every project |
| `/turnip diff gcp/*` | every project whose **name** matches the pattern |

A plan that matches nothing, and an apply with nothing planned, each get a
single reply saying so — not one refusal per configured project.

**Patterns match names, not directories.** They use the same glob syntax
as `whenModified`: `*` matches within one path segment and `**` crosses
segments, so `gcp/*` reaches a project named `gcp/project` but not
`gcp/team/project`, where `gcp/**` reaches both. A pattern matching
nothing is reported at the top of the results comment and the rest of the
command still runs — so a mistyped `gpc/*` tells you, rather than quietly
doing less than you asked.

**`*` on its own is a reserved word, not a pattern.** It means *every*
project, which matters because a `*` pattern stops at a `/` and would skip
projects named for their path. It cannot be combined with other
selectors: a trigger either names projects or asks for all of them.

A project name cannot contain `*` or begin with `-` — turnip rejects such
a name when it parses the configuration file, rather than leaving you to
discover that nothing can select it. A name containing whitespace parses
but can never be addressed either, since a trigger line is split on
spaces; avoid it.

**Wrap a trigger carrying two `*` characters in backticks when you type
it.** Markdown renders the text between two asterisks as italics and eats
them, so a comment reading `/turnip diff gcp/* aws/*` displays as
something other than what you wrote. Backticks make the comment show what
you typed; turnip receives the raw text either way, so this affects
whether *you* can check the command, not whether it works.

**Very large pull requests.** GitHub lists at most 3000 changed files, so
beyond that the matched set may be incomplete. turnip says so in the
results comment when it happens; `*` targets every project regardless.

Examples:

```
/turnip plan
/turnip plan web
/helmfile apply web api
/helmfile diff web -l name=ingress
/turnip plan -- --context=diff
```

Arguments are passed straight through to the tool's own CLI — turnip
doesn't interpret them. **The first argument beginning with `-` is where
the project names stop**, so a `--` separator is optional; write one if
you prefer, and a second `--` further along is passed through verbatim.

**Only the plan operation takes arguments.** `apply` and `sync` replay the
scope the plan recorded, so they accept none of their own and refuse a
trigger that supplies any, naming what they refused. That is what makes an
apply match the diff you reviewed — the two cannot disagree, because only
one of them chose a scope.

### Removing a release

There is no `destroy` for Helmfile. `helmfile destroy` has no dry-run and
uninstalls everything its selector matches regardless of `installed:`, so
no plan could show you what it would remove — and turnip only runs what a
plan described.

To remove a release, mark it `installed: false`. `diff` reports it as a
pending removal and `apply` carries it out, reviewed like any other
change.

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
apply succeeds, when a plan finds nothing to apply, when a plan fails
having recorded nothing, or when the PR merges or closes. Use `unlock` to abandon
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

### When a stored plan stops being usable

Holding a lock and having an appliable plan are different things. A lock
can be held while its plan is no longer usable, in which case an apply is
refused and asks for a fresh plan. That happens in three situations:

- **You pushed a commit.** The plan is superseded the moment the new plan
  is dispatched, not when it finishes — so there is no window in which an
  apply could run against code nobody reviewed.
- **An apply or sync failed part-way.** Infrastructure may have changed,
  so the plan describes a starting state that no longer exists. The lock
  stays held — another PR must not apply on top of an unknown state — but
  you must re-plan before retrying. Re-planning is also what shows you
  what the partial run actually did.
- **An operation timed out.** No result arrived and the Runner may still
  be running, so the same reasoning applies.

turnip says so in the comment each time, and says which of these it was.
A lock that is still held is always listed in the comment's footer with
`/turnip unlock` offered, including after a timeout.

### What releases a lock

| Outcome | Lock |
|---|---|
| plan succeeded, something to apply | held |
| plan succeeded, nothing to apply | released, **unless** the tool can act without changes |
| plan failed, nothing recorded yet | released |
| plan failed, but an earlier plan had succeeded | held, plan invalidated |
| apply or sync succeeded | released |
| apply or sync failed, or timed out | held, plan invalidated |
| PR merged or closed, or `/turnip unlock` | released |

Helmfile is a tool that *can* act without changes, because `helmfile
sync` upgrades every release regardless of the diff. So a Helmfile
project keeps its lock even when its diff comes back clean.

## Who can trigger what

- **Anyone who can open a PR** gets automatic plans for free — no
  authorization check happens for the PR-opened/synchronize path at all.
- **Triggering anything by comment** requires being a repository
  collaborator (any permission level, including read-only) — a
  non-collaborator's comment gets a reply explaining that, and nothing
  runs.
- **Anything beyond a plan** — apply, sync, unlock — additionally
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
