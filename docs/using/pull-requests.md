# Pull requests

> **For end users**: people writing `turnip.yaml` and commenting on pull
> requests. This assumes turnip is already deployed and a repository has
> a [`turnip.yaml`](turnip-yaml.md). For triggering operations by
> comment, see [Commands](commands.md).

## Automatic plans

Opening a PR, or pushing a new commit to one, triggers a **plan**
automatically for every project whose `whenModified` glob matches at
least one changed file, no comment needed. "Plan" means the tool's own
plan command (for Helmfile, `helmfile diff`; see
[Plan commands and mutating commands](commands.md#plan-commands-and-mutating-commands)).
A project with no `whenModified` is never planned this way: it runs only
when a comment names it (see
[Which projects a command targets](commands.md#which-projects-a-command-targets)).

Each triggered project gets its own check run
(`turnip/<operation>/<project>`, e.g. `turnip/diff/web`) and a
consolidated PR comment summarizing every project touched by that PR
(a verdict line up top, then one collapsible section per project); see
[What you'll see on the PR](#what-youll-see-on-the-pr) below. A single
`turnip` check sums the pull request up; it is the one to require in
branch protection (see
[Requiring turnip before merge](#requiring-turnip-before-merge)).

### Draft pull requests

turnip does **not** plan a draft automatically. Opening one, or pushing to
one, does nothing: no check run, no comment, no lock.

Marking it ready for review plans it, exactly as though it had just been
opened. You do not need an extra push.

You can still ask for a plan on a draft at any time by commenting
(`/turnip diff`, `/helmfile diff`, and so on). A comment-triggered plan on
a draft behaves like any other: it takes the project's lock, stores its
plan data, and can be applied or unlocked. Being a draft changes *when
turnip acts on its own*, never what you can ask it to do.

This is not configurable. There is no setting to turn automatic plans on
for drafts, so it isn't worth looking for one.

### Pull requests from forks

turnip runs nothing on a pull request whose branch lives in a different
repository (a fork). Opening one plans nothing, and commenting
(`/turnip diff`, `/helmfile diff`, and so on) on one does nothing either.
There is no check run, no comment and no lock.

turnip does not reply to say it declined. The refusal is recorded in the
Server's log rather than on the pull request; see
[Fork pull requests are refused](troubleshooting.md#fork-pull-requests-are-refused).

The reason is that everything in a fork is controlled by whoever opened
it, `turnip.yaml` included. Running an Operation on one would execute a
stranger's choice of tool and arguments in a pod holding the credentials
turnip deploys with, so the contents decide this, not the person asking.
A collaborator commenting on a fork gets the same refusal: being
authorized to *trigger* an operation says nothing about the *code* that
operation would run.

This is not configurable either, deliberately: there is no setting,
environment variable or `turnip.yaml` key that allows it, and an
allowlist of trusted contributors would not help, since the refusal is
about where the code comes from rather than who asked.

To run a fork's changes through turnip, bring them into a branch of this
repository (push the branch here yourself, or ask a maintainer to), and
open the pull request from there.

Closing a fork pull request still works normally: any lock it holds from
before is released, exactly as for any other pull request.

### Closed and merged pull requests

turnip runs nothing on a pull request that is no longer open. Commenting
`/turnip diff`, `/helmfile diff` or anything else on a closed or merged
one gets a reply saying so, and nothing else happens: no check run, no
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
match, the same set an ordinary push would plan. You do not need to push
a commit to wake turnip up. A pull request reopened as a draft is not
planned automatically, exactly as one opened as a draft is not.

## Plan → apply, and why apply needs a plan first

Plan and apply are linked: an `apply` re-uses exactly what the most
recent successful `plan` on that PR computed; it never re-plans first.
This is what makes the check run/comment you saw before clicking
"apply" an accurate preview of what's about to happen, not a stale guess.
Practically:

- Applying with no prior plan on this PR (or a plan that's since been
  superseded/unlocked) fails with a comment asking for a fresh
  `/turnip diff` first.
- A held lock blocks *other* PRs from planning the same project. You'll
  see a comment naming which PR holds it, and a queued check saying the
  same. Either wait for that PR to merge/close (auto-releases) or have
  someone unlock it, then re-plan: turnip does not re-plan for you.

### When a stored plan stops being usable

Holding a lock and having an appliable plan are different things. A lock
can be held while its plan is no longer usable, in which case an apply is
refused and asks for a fresh plan. That happens in three situations:

- **You pushed a commit.** The plan is superseded the moment the new plan
  is dispatched, not when it finishes, so there is no window in which an
  apply could run against code nobody reviewed.
- **An apply or sync failed part-way.** Infrastructure may have changed,
  so the plan describes a starting state that no longer exists. The lock
  stays held (another PR must not apply on top of an unknown state), but
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

A tool *can act without changes* when one of its mutating commands can
change infrastructure even though the plan found nothing; a project of
such a tool keeps its lock even when its plan comes back clean. Each
tool's page says whether it is one. Helmfile is, because of `sync`; see
[`sync` acts even when the diff is empty](../plugins/helmfile.md#sync-acts-even-when-the-diff-is-empty).

## Requiring turnip before merge

Mark **`turnip`** as a required status check in branch protection. Don't
require the per-project checks (`turnip/diff/web` and so on): a pull
request that doesn't touch `web` never reports `turnip/diff/web`, so
requiring it blocks every such pull request forever.

`turnip` answers one question: **has every plan this pull request made
been carried out**: applied, or found to have nothing to apply? turnip
never applies on merge, and merging releases the locks, so a plan left
unapplied at merge would stay unapplied.

| What's going on | `turnip` shows | Title |
|---|---|---|
| No project's files changed | `skipped`: passes; there is nothing to plan | `no projects affected` |
| Plans ran, nothing applied yet | **nothing**: GitHub shows the required check as "Expected — Waiting for status to be reported", which blocks the merge without a red ❌ | (none) |
| Some projects up to date, others waiting | in progress | `1/3 projects up to date` |
| Every project applied, or its plan found nothing to apply | `success` | `3/3 projects up to date` |
| An apply (or `sync`) failed | `failure` | `2/3 projects up to date, 1 failed` |
| `turnip.yaml` is invalid | `failure` | `invalid turnip.yaml` |
| A project's `turnip.yaml` asks for an override the Server doesn't permit | `failure` | `not permitted: web sets runner.serviceAccount, and 1 more` |

"Up to date" means the infrastructure matches this pull request: the
project was applied, or its plan found nothing to apply.

Details worth knowing:

- **Red means the author has something to fix.** A project waiting on
  another pull request's lock, or a plan that failed and needs re-running,
  keeps `turnip` from passing without turning it red. For a lock, that
  means in progress (or "Expected", before anything on the pull request
  has been applied), with the summary listing the project as `` `web`:
  not planned, locked by PR #5 ``. A refused override is different:
  changing `turnip.yaml` fixes it, so it is red here and on the project's
  own check.
- **Whether a no-change plan needs an apply depends on the tool.** A plan
  that finds nothing to apply counts as done, except for a tool that can
  act without changes (see [What releases a lock](#what-releases-a-lock)),
  whose projects always need their apply. Helmfile is one; see
  [`sync` acts even when the diff is empty](../plugins/helmfile.md#sync-acts-even-when-the-diff-is-empty).
- **`/turnip unlock` does not satisfy it.** Unlocking abandons a plan; it
  doesn't carry it out. Push a new commit, or apply, to move `turnip`.
- **Every project planned on the commit counts**, including one you
  planned by name that the change doesn't touch: it took a lock and has a
  plan waiting.
- **Each commit starts fresh.** A push resets `turnip` to "Expected" on
  the new commit until its plans are applied again.
- **A repository with no `turnip.yaml` never reports `turnip`.** Requiring
  it there blocks every pull request.

## What you'll see on the PR

- **Check runs**, one per (project, operation), named
  `turnip/<operation>/<project>`: `in_progress` while the Runner Job is
  executing, then `success`/`failure` with the tool's own output attached.
  Operation first, so every `diff` (or every `sync`) lists together. The
  title beside each says what the icon doesn't:

  | State | Title |
  |---|---|
  | a plan running | `running`, or `running, -l name=api` when scoped |
  | an apply or sync running | `running the recorded plan, +1 ~4 -2` |
  | succeeded | `+1 ~4 -2`, `no changes`, or `succeeded` when an apply's counts can't be read, plus the scope when there was one |
  | the tool failed | `helmfile exited 1` |
  | turnip failed around the tool | `clone failed`, `workspace could not be prepared`, `helmfile could not be started` |
  | the Runner Job was refused | `Runner Job could not be created` |
  | timed out | what turnip found, e.g. `Job ...: container stuck (ImagePullBackOff)` |
  | a plan waiting on another pull request's lock | `queued`: `locked by PR #5, re-plan once it's released` |
  | `turnip.yaml` asks for an override the Server doesn't permit | `runner.serviceAccount is not permitted`, `clone.submodules is not permitted` |
  | turnip failed before the Runner started | `lock could not be acquired`, `operation could not be recorded`, `Runner Job could not be built` |

  A tool's failure is reported only as its exit code: the reason is in its
  output, which is in the results comment, and turnip does not guess at
  it.

  **A mutating command's counts are what the tool itself reports acting
  on**, read from its own output rather than recomputed from the plan;
  how each tool reports them is under "Gotchas" on its page (for
  Helmfile, [Change counts](../plugins/helmfile.md#change-counts)). It
  renders `succeeded`, with no counts, instead of a count of zero, in two
  cases: the Plugin says its mutating commands don't report meaningful
  counts at all (a tool whose apply replays a plan the plan already
  showed), or the tool printed nothing the Plugin can read counts from.
  Either way, `succeeded` never means `no changes`; a mutating command
  that changed nothing still shows `no changes`, exactly as a plan does.

  **A queued check is not a failure.** The plan did not run because
  another pull request holds the project's lock, which is nothing this
  pull request's author can fix, so the check waits rather than turning
  red. When the holder is unknown the title reads `locked by another pull
  request, re-plan once it's released`. **turnip does not re-plan on its
  own when the lock is released**. The lock frees when the other pull
  request applies, merges, closes or is unlocked, and after that someone
  has to comment `/turnip diff` (or push a commit) here. The re-plan gets
  a new check under the same name, which replaces the queued one in the
  checks list.

  The last row is turnip's own trouble (Redis, the Operation Record, the
  Job spec), with nothing wrong in the change. The run failed, so the
  check is red, but a re-plan usually succeeds; see
  [Troubleshooting](troubleshooting.md).
- **The `turnip` check**, one per commit, once there is something to say:
  whether every plan this pull request made has been carried out. See
  [Requiring turnip before merge](#requiring-turnip-before-merge).
- **A PR comment** consolidating every project touched by that trigger,
  posted once everything it started has finished. A comment with several
  commands runs them one after the other, each starting when the one
  before it has finished, and that holds whichever turnip Server replica
  is up: a restart or an upgrade of turnip in the middle (including one
  your own apply causes) does not drop the later commands or the
  comment.
  It opens with a **verdict line** (the total change across every
  project, and how many reported no changes or failed), so you can tell
  whether the PR needs attention without expanding anything.

  Below it, one collapsible section per project. Each section's heading
  stays visible while collapsed and carries that project's status and
  change counts (✅ `web`: diff, +1 ~4 -2), so a run across many
  projects is scannable at a glance. When the operation ran with extra
  arguments, the heading carries them too
  (✅ `web`: diff, +0 ~1 -0, `-l name=api`), so you can tell a scoped
  run from one that looked at the whole project without expanding it.
  Expanding one shows the full command output, followed by the commands
  that act on **that project alone**:

  ```
  - Apply just this project: /turnip apply web
  - Re-plan it: /turnip diff web
  - Release its lock: /turnip unlock web
  ```

### What turnip ran

Each project's output opens and closes with turnip's own lines, written
as `@@ turnip: ... @@` so they are never confused with the tool's. GitHub
highlights them the way it highlights a diff's hunk headers:

```diff
@@ turnip: infra/web, helmfile v1.7.4 @@
@@ turnip: helmfile --no-color --environment staging diff -l name=api @@
Comparing release=api, chart=charts/api
...
@@ turnip: exit 0 in 4.2s @@
```

In between is the tool's output as it was written: its standard output
and standard error together, in the order the tool wrote them, not one
after the other, so a warning appears where it happened, not at the end.
Each of the two is always in its own order. When a tool writes to both
at the very same moment, those lines can land either way round, close to
where they were written.

Bytes in the output that are not valid UTF-8 (a file in another
encoding, say) are shown as `�`, one for each run of them, so the
output always reaches the comment.

What a tool's output shows is decided per tool. Helmfile's is everything
it wrote when the operation succeeds; when it fails, it is Helmfile's
messages and errors alone, with a line saying how many lines of diffs
were left out (see
[A failed operation shows Helmfile's messages, not the diffs](../plugins/helmfile.md#a-failed-operation-shows-helmfiles-messages-not-the-diffs)).

They record what actually ran, including arguments turnip supplies that
you never typed: `--no-color` above is turnip's, and `--environment`
comes from the project's own configuration. Reconstructing that later from `turnip.yaml` at that commit
is exactly the thing that goes wrong during an incident, so turnip writes
it down at the time. The version is the tool version the run used, which
is usually the answer when a diff changes and nobody touched the code.

The command line records **arguments only**, never the environment.
Credentials reach the tool through environment variables and mounted
files, not through its command line, which is what makes recording the
command safe. Anything that looks like turnip's GitHub token is removed
from everything sent back, including the tool's own output.

**Arguments are recorded and replayed.** The scope a plan ran with is
stored with its lock, and a later `apply` or `sync` replays exactly that,
which is why those operations refuse arguments of their own. Where a
locked project's plan recorded arguments, the comment's footer says that
applying replays that scope rather than covering the whole project.

Only the commands that actually apply are shown: a project that failed
is never offered an apply, and unlock appears only where this PR is
holding a lock. The comment closes by naming the projects this PR has
locked, with `/turnip apply` and `/turnip unlock` for acting on all of
them at once.

A plan holds each project's lock until it is applied or released, which
is what stops another PR planning the same project underneath you.
`/turnip unlock` gives it up without applying.

A single project's output that's too long for one GitHub comment splits
across multiple comments rather than getting truncated. A project's
output takes at most 5 comments by default (the operator can change
that; see [Server settings](../operating/server-settings.md)). Beyond
that, turnip cuts the **start** of the output and keeps the end, and puts
`…(<K> earlier lines omitted)` where the cut was, after any `@@ turnip:`
lines the output opens with. The end is kept because that's where the
errors and the summary are. If even that isn't enough, the earliest
sections are dropped first and the comment says how many.

If something fails partway (a lock conflict, a GitHub API hiccup, a tool
error, a Job that never started), see
[Troubleshooting](troubleshooting.md) for what each specific symptom
means and what to do about it.

## Next

- [`turnip.yaml` reference](turnip-yaml.md)
- [Commands](commands.md): triggering operations by comment
- [Helmfile](../plugins/helmfile.md): the change-count and lock behavior
  specific to that tool
