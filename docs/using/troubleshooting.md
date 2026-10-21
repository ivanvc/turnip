# Troubleshooting

> **For end users**: people writing `turnip.yaml` and commenting on pull
> requests. What you'll actually see (usually a PR comment or check run)
> for each common failure, and what to do about it. An entry you can meet
> but only whoever runs turnip can fix says so, and links to
> [Operator troubleshooting](../operating/troubleshooting.md), which also
> covers GitHub API errors, Runner execution errors, the Server's log
> entries for refusals, and multi-instance behavior.

## Configuration errors

**Symptom**: a PR comment saying configuration is missing, or a parse
error with a line number, instead of a plan.

- **No configuration found**: the comment names both accepted locations.
  Add `.turnip/config.yaml`, or `turnip.yaml` in the repository root.
  Only the `.yaml` extension is read, and `.github/turnip.yaml` (accepted
  by turnip before schema `v1alpha2`) is not read any more.
- **Invalid YAML syntax**: the comment includes the parse error and line
  number. Fix that line and push again (a new commit re-triggers the
  plan).
- **Unrecognized field**: the comment names the key and its line. turnip
  checks every field it defines by name, so this is usually a misspelling
  or a setting written at the wrong level (the one place arbitrary keys
  are allowed is inside `with`).
- **Unsupported `schemaVersion`**: reported on its own, without also
  listing everything the older schema allowed. Migrate the file rather
  than changing the version alone. From `v1alpha2` to `v1alpha3`, every
  `uses:` must name its version, written as the image tag the vendor
  publishes (e.g. `uses: helmfile@v1.7.4`).
- **`uses:` names no version**: turnip has no default version. Write the
  one to run, e.g. `uses: helmfile@v1.7.4`.
- **Unknown tool in `uses:`**: the error lists the tools this Server has
  Plugins for. A tool without a Plugin (today, `terraform` and `pulumi`)
  is not accepted. Any other image must be written in full, registry host
  included.
- **A tag the alias does not allow**: the error names the tags the alias
  allows, which the tool's page also lists (for Helmfile, see
  [its troubleshooting](../plugins/helmfile.md#troubleshooting)). Write
  the full version the vendor publishes.
- **`uses:` names its tag with `:`**: `uses:` separates the tag with `@`;
  the error shows the corrected line to paste back.
- **This Server does not allow an image**: the image and tag match no
  entry of the Server's `TURNIP_ALLOWED_IMAGES`. Ask whoever runs turnip
  to add one (see
  [Writing your own image](turnip-yaml.md#writing-your-own-image) and
  [Runner access](../operating/runner-access.md)), or use the built-in
  alias.
- **Invalid project configuration**: the comment names the specific
  project and the validation error (e.g. a project missing its required
  `directory`). Fix that project's entry. Every one of these fails the
  `turnip` check as `invalid turnip.yaml`.

`/turnip unlock` still works while the configuration is missing or
invalid, since it never reads the file. If a broken change left a plan's
lock held, comment `/turnip unlock` to release it, then fix the file.

## Lock contention

**Symptom**: a PR comment saying a project is locked by another PR,
instead of a plan or apply running.

- **Locked by another PR**: the comment names the PR holding the lock,
  and the project's check is queued with the same PR in its title. Either
  wait for that PR to merge/close (which releases the lock automatically)
  or have someone with write access comment `/turnip unlock <project>` on
  this PR to force it. That is only appropriate if you're sure the other
  PR's plan is stale or abandoned. Then re-plan: turnip does not re-plan
  on its own when the lock is released.
- **Apply rejected: no plan data / wrong PR holds the lock**: this means
  either no plan has run yet for this PR, or the lock is held by a
  *different* PR. Comment `/turnip diff <project>` (or push a commit that
  touches the project) to get a fresh plan on this PR before applying.
- **Locks don't expire on their own**: unlike some similar tools, turnip
  never times out a lock. It only releases on PR merge, PR close, or a
  manual `/turnip unlock`. If a PR is abandoned without being closed, its
  locks stay held until someone unlocks them.

## Reading a failed check's title

**Symptom**: a per-project check (`turnip/diff/web`) failed. Its title says
which step failed; a check carries only a title and a summary, so the full error and output
are in the results comment.

- **`helmfile exited 1`** (or another tool and code): the tool itself ran
  and failed. The reason is in its output; turnip does not interpret it.
  Usually a problem in the change being planned. The tool's page lists
  errors specific to it (for Helmfile, see
  [its troubleshooting](../plugins/helmfile.md#troubleshooting)).
- **`clone failed`**: the Runner could not check out the pull request's
  commit. The results comment carries git's message, often a merge conflict with the
  base branch, or credentials the repository's submodules need.
- **`workspace could not be prepared`**: the Runner could not create its
  working directory. A node or volume problem, not the change; this is
  whoever runs turnip's to fix. See
  [Runner execution errors](../operating/troubleshooting.md#runner-execution-errors).
- **`helmfile could not be started`**: the tool's binary was missing or
  the operation isn't one the tool supports. See
  [Plugin execution errors](#plugin-execution-errors).
- **`Runner Job could not be created`**: Kubernetes refused the Job: a
  quota, an admission policy, or the Server's RBAC. The operation never
  ran, and this is whoever runs turnip's to fix. See
  [Runner execution errors](../operating/troubleshooting.md#runner-execution-errors).
- **A description of a stuck Job or Pod**: the operation timed out, and
  the title is what turnip found when it gave up. See
  [Runner execution errors](../operating/troubleshooting.md#runner-execution-errors).
- **`failed`** alone: the Runner reported a failure without saying which
  step. The results comment has its message.
- **Queued, `locked by PR #5, re-plan once it's released`** (or `locked by
  another pull request, ...` when turnip couldn't tell which): not a
  failure. Another pull request holds the project's lock, so the plan
  never ran. Wait until that pull request applies, merges or closes, or
  until someone unlocks it, then comment `/turnip diff` or push a commit
  here. turnip does not re-plan by itself when the lock is released, and
  the queued check stays queued until you do.
- **`runner.serviceAccount is not permitted`** (or `clone.submodules is
  not permitted`): this project's `turnip.yaml` sets an override the
  Server does not allow, so the plan was refused. Either remove the
  setting from `turnip.yaml`, or ask whoever runs turnip to add it to the
  Server's `TURNIP_ALLOWED_OVERRIDES`. The results comment says which.
- **`lock could not be acquired`**, **`operation could not be
  recorded`** or **`Runner Job could not be built`**: the Server failed
  before any Runner started, and nothing is wrong with the change.
  Re-plan; if it recurs, ask whoever runs turnip (see
  [Failed checks that are the Server's to fix](../operating/troubleshooting.md#failed-checks-that-are-the-servers-to-fix)).

## A note about the check run in the results comment

**Symptom**: a Project's section in the results comment ends with a note
that its GitHub check run could not be created or completed. The
operation itself is unaffected and its output is in the comment; the
note says what the failure left in the PR's checks tab.

- **`Note: this Project's GitHub check run could not be created (<error>).
  The operation itself is unaffected; only its entry in the PR's checks tab
  is missing.`**: the check was never created, so the tab has no entry for
  this operation. Read the result in the comment.
- **`Note: this Project's GitHub check run could not be completed (<error>).
  The operation itself is unaffected, but its check stays in progress in
  the PR's checks tab until the operation runs again.`**: the check exists
  but was never finished. A plan's check is replaced by running the plan
  again (for example `/turnip diff`), which creates a new check under the
  same name. A successful apply's check cannot be replaced that way: the
  apply released its lock, so running it again is refused until a new
  plan has something to apply. If a branch protection rule requires the
  per-Project checks, that stuck check blocks the merge; requiring the
  `turnip` check instead avoids this (see
  [The `turnip` check](#the-turnip-check)). The error names the cause,
  often a GitHub rate limit or outage; if it recurs, ask whoever runs
  turnip.

## A Project reports no result from the Runner

**Symptom**: a Project's section in the results comment says `No result
was received from the Runner within 24 hours.`, and its check is still in
progress in the PR's checks tab.

The Runner started the operation but never reported back, so turnip gave
up waiting after 24 hours and let the rest of the comment go out. Nothing
is known about what the operation changed: it may still have been running
or may have finished without its result arriving. For that reason turnip
leaves the check in progress and the Project's lock held, instead of
marking either as finished. A result that arrives later is ignored.

Look at the Project's real state before acting (for example with a
fresh `/turnip diff`), then run `/turnip unlock <project>` if the lock should not
stay. The stuck check blocks the merge if a branch protection rule
requires it, and the `turnip` check does not pass either, because the
Project was never reported as applied (see
[The `turnip` check](#the-turnip-check)); unlocking changes neither.
What clears both is running the Project again: plan it and apply it, so
a new check of the same name replaces the stuck one and `turnip` moves
with the apply, or push a commit, which starts every check afresh. If it
recurs, ask whoever runs turnip.

## The `turnip` check

**Symptom**: the required `turnip` check blocks a merge, or shows
something unexpected. What each state means is in
[Pull requests](pull-requests.md#requiring-turnip-before-merge)
("Requiring turnip before merge"); these are the ones that surprise.

- **"Expected — Waiting for status to be reported" after a plan**: by
  design. `turnip` appears with the first apply; until then the pull
  request is under review, and the check blocks without being red. Apply
  (or sync) each project to move it. A configuration problem the author
  has to fix is the exception: it shows at once, red (an invalid
  `turnip.yaml`, under [Configuration errors](#configuration-errors), or
  the `not permitted` entry below).
- **Stuck in progress after everything was applied**: look at the check's
  summary for the project that isn't done. Usually it is one planned by
  name that the change doesn't touch (it counts too), or one whose plan
  was refused because another pull request holds its lock, listed as
  `not planned, locked by PR #5`. Re-plan that one once the lock is
  released; turnip won't do it for you. Apply the rest, or push a commit
  so the next plan starts from a fresh record.
- **`failure` titled `not permitted: web sets runner.serviceAccount, and 1
  more`**: an affected project asks for an override the Server doesn't
  permit. The summary lists every refused project and its setting. Remove
  the setting from `turnip.yaml`, or have whoever runs turnip add it to
  `TURNIP_ALLOWED_OVERRIDES`, then re-plan.
- **Blocks every pull request in a repository**: that repository has no
  `turnip.yaml`, so turnip never reports `turnip` there. Don't require it
  on repositories that don't use turnip.
- **A note in the PR comment saying the `turnip` check could not be
  updated**: a Redis or GitHub error while publishing. The operation
  itself is unaffected, and the next result for the same commit publishes
  the check again.

## Plugin execution errors

**Symptom**: the results comment shows a failure with the tool's own
output attached.

- **Tool not installed**: the comment names the missing tool/exit code.
  A tag the alias or the Server's `TURNIP_ALLOWED_IMAGES` does not allow
  never gets this far; it's rejected when the file is parsed, as a
  validation comment naming `uses`. What reaches here is an allowed tag
  that was never published, so there was nothing to pull: check the tag
  after the `@` in `uses` against the image's published tags. For an
  apply, it can also be the digest its plan recorded, since deleted from
  the registry; the error says so, and a re-plan fixes it.
- **A helper binary or plugin is missing** (e.g. `exec: "helm":
  executable file not found in $PATH`, or the tool reporting it can't find
  a plugin): Helmfile runs inside the vendor's image, so these come from
  that image rather than from turnip. The fix is an image that bundles
  what you need, allowed by the Server's
  `TURNIP_ALLOWED_IMAGES` (see
  [Writing your own image](turnip-yaml.md#writing-your-own-image)); for a
  custom image, check it provides what the tool's page lists (for
  Helmfile,
  [What a custom Helmfile image must provide](../plugins/helmfile.md#what-a-custom-helmfile-image-must-provide)).
- **Tool command failed** (e.g. `helmfile exited 1`): the tool's
  output is in the PR comment. What a failed operation's output shows is
  the tool's choice; Helmfile's is its messages and errors without the
  diffs (see
  [A failed operation shows Helmfile's messages, not the diffs](../plugins/helmfile.md#a-failed-operation-shows-helmfiles-messages-not-the-diffs)).
  This is the tool telling you
  something real about your infrastructure code, not a turnip failure.
  Fix what the tool reports and push again.
- **Output parsing failed, but the comment shows zero changes**: the
  operation itself succeeded; turnip just couldn't extract a change count
  from the tool's output format. Check the comment's full output section
  for what actually happened rather than trusting the summary count in
  this case. When a tool's counts can't be trusted is on its page (for
  Helmfile, see [Change counts](../plugins/helmfile.md#change-counts)).

## Output with "earlier lines omitted"

**Symptom**: a project's output in the results comment has a line saying
earlier lines were omitted. There are two such notes, and each counts
something different:

- `@@ turnip: <K> earlier lines omitted (<size>) @@` is written by the
  Runner before it sends the result, when the tool's output is too large
  to deliver. `<K>` counts the tool's lines it removed from the start,
  and `<size>` is how much that was.
- `…(<K> earlier lines omitted)` is written when the comment is built,
  when the output would take more comments than the operator allows per
  project (5 by default). `<K>` counts the lines of the output, as
  received, that were removed from its start. It appears after any
  `@@ turnip:` lines the output opens with, so both notes can show in
  one section.

In both cases the end of the output is kept, which is where a plan's
summary and a failure's last error are. If you need the lines that were
cut, ask the operator quickly: they are in the Runner Pod's log only
until its Job is deleted, 15 minutes after it finishes, and a very long
output may have lost its start there too. Running the plan again, with
helmfile's `--selector` to limit it to the releases you care about,
gives a shorter output that fits.

## A Project that lists `runner.secrets` never started

**Symptom**: the check fails with a title like `Job <name>: container
stuck (PodInitializing)`, and the Project's output ends with `This
Project mounts keys <keys> of Secret <secret> in namespace <namespace>;
if that Secret or one of those keys is missing, the Pod cannot start.`

The Secret belongs to the operator, not the repository, and turnip
mounts only the keys your Project lists. Check that each name in
`runner.secrets` matches a key of the Secret exactly, then ask the
operator to confirm the Secret and those keys exist. The operator's
steps are in
[Troubleshooting (operators)](../operating/troubleshooting.md#a-project-lists-keys-of-a-secret-and-the-runner-never-started).

## Authorization errors

**Symptom**: a reply comment saying you don't have permission, instead of
the operation running.

- **Non-collaborator trigger**: only repository collaborators can trigger
  operations via comment at all. Ask a maintainer to add you as a
  collaborator. Pushing a commit is no way around it: pushing a branch to
  this repository needs access in the first place, and a pull request
  from a fork runs nothing (see
  [Pull requests from forks](pull-requests.md#pull-requests-from-forks)).
- **Insufficient permission for apply/sync/unlock**: these need write
  access specifically (not just any collaborator access, e.g. triage or
  read). Ask a maintainer to grant write access, or ask them to run the
  command themselves.
- **"turnip could not check your permission"**: not a refusal. turnip
  could not ask GitHub (an error, a rate limit or an outage), so it ran
  nothing rather than guess. Comment again. An apply reports `could not
  check write permission to run "apply"; comment again to retry`, and an
  unlock replies that it could not check your write permission. If it
  keeps happening, whoever runs turnip can find the GitHub error in the
  Server's logs.

## Mutation requirements not met

**Symptom**: a mutating command (an apply, a sync, or any operation other
than a plan) does nothing, and turnip replies instead:

```text
`/helmfile apply` was not run. This pull request must first:
- be approved by someone with write access other than its author
- have no merge conflicts (GitHub reports one)
```

Whoever runs turnip set `TURNIP_MUTATION_REQUIREMENTS` (see
[Mutation requirements](../operating/server-settings.md#mutation-requirements)).
The reply lists every requirement that is not met, so fixing all of them
and commenting the same command again is enough. Nothing ran: no lock
changed, no Runner started and no check run was touched, and the plan you
already have is still there to apply. A plan in the same comment still
runs; only the mutating commands are held back.

- **be approved by someone with write access other than its author**: no
  one who counts has an approval standing on the pull request. Ask a
  reviewer with write access to approve it, then comment again. If there
  is an approval and this still appears, it doesn't count because it is
  your own, because the reviewer has only read or triage access, because
  the same reviewer later requested changes, or because it was dismissed,
  which branch protection does to every approval on each new push when
  "Dismiss stale pull request approvals when new commits are pushed" is
  on. Get a fresh approval on the current commit.
- **have no merge conflicts (GitHub reports one)**: the branch conflicts
  with its base. Resolve the conflict the usual way (merge or rebase the
  base branch and push); the push plans again, and you apply that plan.
  This is about conflicts only; failing status checks never cause it.
- **have no merge conflicts (GitHub has not finished checking; try again
  in a moment)**: GitHub computes mergeability in the background, usually
  within seconds of a push, and still had no answer after turnip waited a
  few seconds for it. Comment the same command again shortly.
- **either line followed by "(turnip could not check this; try again)"**:
  turnip couldn't ask GitHub (listing reviews, checking an approver's
  permission, or reading the pull request failed), and it refuses rather
  than assume the requirement holds. Comment again. If it keeps
  happening, whoever runs turnip can check the Server's logs, which carry
  the GitHub error (see
  [Mutation requirement refusals](../operating/troubleshooting.md#mutation-requirement-refusals)).

## Fork pull requests are refused

**Symptom**: a pull request opened from a fork gets no check run, no
comment and no lock, and comment triggers on it are ignored, with
nothing posted to the pull request explaining why. The silence is
intentional (see
[Pull requests from forks](pull-requests.md#pull-requests-from-forks)).
The refusal is logged by the Server instead, for whoever runs turnip (see
[Fork pull requests are refused](../operating/troubleshooting.md#fork-pull-requests-are-refused)
in the operator troubleshooting).

## Next

- [Operator troubleshooting](../operating/troubleshooting.md): GitHub API
  errors, Runner execution errors, and multi-instance behavior
- [`turnip.yaml` reference](turnip-yaml.md)
- [Commands](commands.md) and [Pull requests](pull-requests.md)
