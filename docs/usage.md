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

### Closed and merged pull requests

turnip runs nothing on a pull request that is no longer open. Commenting
`/turnip plan`, `/helmfile diff` or anything else on a closed or merged
one gets a reply saying so, and nothing else happens — no check run, no
lock, no Job.

There is no distinction between merged and closed-without-merging. Both
are closed, and the reason applies to each: the cleanup that runs when a
pull request closes has already happened, so anything turnip started
afterwards would hold a lock with no remaining event to release it. You
would have to clear it by hand with `/turnip unlock`.

On a pull request closed *without* merging there is a second reason.
turnip merges the base branch into the head commit before running, so
`diff` followed by `apply` there would deploy precisely the changes
someone decided not to merge.

**Reopening a pull request plans it again**, for the projects its changes
match — the same set an ordinary push would plan. You do not need to push
a commit to wake turnip up. A pull request reopened as a draft is not
planned automatically, exactly as one opened as a draft is not.

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

turnip recognises three roles, and does not distinguish further:

| Role | Who |
|---|---|
| **Outsider** | not a collaborator on the repository |
| **Collaborator** | a collaborator below write — read or triage |
| **Writer** | write, maintain or admin |

Write, maintain and admin are treated identically; nothing turnip does
needs to tell them apart.

| Action | Outsider | Collaborator | Writer |
|---|---|---|---|
| `/turnip plan`, `/helmfile diff` | no | **yes** | yes |
| `/turnip apply`, `/helmfile sync` | no | no | **yes** |
| `/turnip unlock` | no | no | **yes** |

An outsider's comment gets a reply saying so, and nothing runs. A
collaborator asking for an apply gets a reply naming the permission they
are missing.

**"Collaborator" is wider than it sounds.** For an organization-owned
repository, GitHub counts outside collaborators, members who are direct
collaborators, members with access through a team, members with access
through the organization's *default* permission, and organization owners.
If your organization grants its members a default permission on
repositories, every member can trigger a plan.

### Automatic plans have no actor

The table above governs comments. A plan that turnip starts by itself —
on open, push, or reopen — has no one to authorize: it is a consequence
of the commit existing. Its real gate is GitHub's, not turnip's, because
pushing the branch required access in the first place, and a pull request
from a fork is refused outright (see "Pull requests from forks").

So a read-level collaborator can ask for a plan but cannot cause one by
pushing. That asymmetry is deliberate: someone reviewing a change needs to
be able to ask what it would do, and a plan changes nothing.

### This is not configurable, on purpose

There is no setting to raise or lower these levels. An apply that could be
permitted below write would be a way to get it wrong quietly, and a
setting that only ever has one safe value is not a setting. If you need a
plan restricted more tightly than "any collaborator", the lever is
GitHub's — the repository's collaborator list and your organization's
default permission — rather than a turnip key that a repository could
later be permitted to set for itself.

### What else has to be true

Permission is necessary, not sufficient. Independently of who asks, turnip
refuses to act on a pull request from a fork, on one that is closed or
merged, and refuses a mutating operation whose project has no valid plan
recorded. Those are covered in their own sections above.

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
  projects is scannable at a glance. When the operation ran with extra
  arguments, the heading carries them too
  (`✅ web · diff · +0 ~1 -0 · -l name=api`), so you can tell a scoped
  run from one that looked at the whole project without expanding it. Expanding one shows the full command
  output, followed by the commands that act on **that project alone**:

  ```
  - Apply just this project: /turnip apply web
  - Re-plan it: /turnip diff web
  - Release its lock: /turnip unlock web
  ```

### What turnip ran

Each project's output opens and closes with turnip's own lines, marked
`# turnip · ` so they are never confused with the tool's:

```
# turnip · env/staging · helmfile v0.169.0
# turnip · helmfile --environment staging diff -l name=api
Comparing release=api, chart=charts/api
...
# turnip · exit 0 · 4.2s
```

They record what actually ran, including arguments turnip supplies that
you never typed — `--environment` above comes from the project's own
configuration. Reconstructing that later from `turnip.yaml` at that commit
is exactly the thing that goes wrong during an incident, so turnip writes
it down at the time. The version is the tool version the run used, which
is usually the answer when a diff changes and nobody touched the code.

The command line records **arguments only** — never the environment.
Credentials reach the tool through environment variables and mounted
files, not through its command line, which is what makes recording the
command safe. Anything that looks like turnip's GitHub token is removed
from everything sent back, including the tool's own output.

**Arguments are recorded and replayed.** The scope a plan ran with is
stored with its lock, and a later `apply` or `sync` replays exactly that
— which is why those operations refuse arguments of their own. Where a
locked project's plan recorded arguments, the comment's footer says that
applying replays that scope rather than covering the whole project.

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
