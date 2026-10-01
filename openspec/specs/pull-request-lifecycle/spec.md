# pull-request-lifecycle Specification

## Purpose
Decides what turnip does, on its own, as a pull request is opened, pushed to, marked ready, reopened and closed, and which pull requests it refuses to act on at all (drafts, forks, closed ones). End users rely on it for automatic plans and Lock cleanup; operators rely on its refusals to keep untrusted code from running.

## Requirements

### Requirement: Which pull request events act
On a `pull_request` webhook, the Server SHALL run the automatic plan for the actions `opened`, `synchronize`, `reopened` and `ready_for_review`, and SHALL run close handling for `closed`. Every other action (for example `edited`, `labeled`, `assigned`, `converted_to_draft`) SHALL do nothing. A merged pull request arrives as `closed` and SHALL be handled exactly as one closed without merging.

#### Scenario: A pull request is opened
- **WHEN** a non-draft pull request from the same repository is opened
- **THEN** the automatic plan runs for it

#### Scenario: A pull request is converted to a draft
- **WHEN** a pull request is converted to a draft
- **THEN** turnip does nothing, and any Locks it holds stay held

#### Scenario: A pull request is merged
- **WHEN** a pull request is merged
- **THEN** close handling runs, as for a pull request closed without merging

### Requirement: The automatic plan
For an acting event that passes the draft and fork guards, the Server SHALL read the repository's turnip configuration at the pull request's head commit (see the `configuration` capability) and:

- when no configuration file exists, do nothing: no comment, no check, no Lock;
- when the configuration is invalid or cannot be fetched, post the configuration error comment (see the `configuration` capability) and fail the `turnip` check (see the `merge-check` capability), running nothing;
- when no Project's `whenModified` matches the pull request's changed files, run nothing and mark the `turnip` check skipped (see the `merge-check` capability);
- otherwise dispatch the plan Operation of every matched Project, concurrently, each taking its Lock (see the `locking` capability), with no authorization check (see the `authorization` capability), and post one results comment once every plan has finished (see the `pr-comment` capability).

The webhook SHALL be answered before the plans finish.

#### Scenario: A repository that has not adopted turnip
- **WHEN** a pull request is opened in a repository with neither `.turnip/config.yaml` nor `turnip.yaml`
- **THEN** turnip posts nothing and reports no check

#### Scenario: A push that matches two Projects
- **WHEN** a push changes files matched by `web` and `api`
- **THEN** a plan is dispatched for each, and one comment reports both once both finish

#### Scenario: A push that matches nothing
- **WHEN** a push changes only files no Project's `whenModified` matches
- **THEN** nothing runs and the `turnip` check is reported as skipped

### Requirement: A push re-plans and supersedes matched plans
A `synchronize` event SHALL run the automatic plan against the new head commit. Each matched Project's plan dispatch SHALL supersede that Project's stored plan (see the `locking` capability), and the `turnip` check SHALL start afresh on the new commit (see the `merge-check` capability). Projects the push does not match SHALL NOT be planned.

Known issue: a push invalidates only the plans of Projects the automatic plan matched; Projects planned by name or `*` outside `whenModified`, and all Projects on a draft, keep `plan_ready` across a push. Fix proposed in change `fix-plan-survives-push`.

#### Scenario: A push touches a planned Project
- **WHEN** `web` holds a `plan_ready` Lock and a push changes a file `web` matches
- **THEN** `web`'s stored plan stops being appliable as soon as the new plan is dispatched

#### Scenario: A push leaves an unmatched Project's plan in place
- **WHEN** `api` was planned with `/turnip diff api` and a later push changes no file `api` matches
- **THEN** `api`'s Lock stays `plan_ready` and `/turnip apply api` replays the earlier commit's plan

### Requirement: Drafts are not planned automatically
For `opened`, `synchronize` and `reopened` on a draft pull request, the Server SHALL do nothing: no configuration fetch, no Lock, no Runner Job, no check, no comment. `ready_for_review` SHALL plan the pull request exactly as though it had just been opened. Comment triggers SHALL work on a draft as on any other pull request, and close handling SHALL run for a draft. This SHALL NOT be configurable.

Known issue: a push to a draft does not invalidate any plan the draft holds, since no automatic plan is dispatched. Fix proposed in change `fix-plan-survives-push`.

#### Scenario: A draft is opened
- **WHEN** a draft pull request is opened
- **THEN** turnip does nothing

#### Scenario: A draft is marked ready
- **WHEN** a draft pull request is marked ready for review
- **THEN** the automatic plan runs, with no extra push needed

#### Scenario: A plan requested on a draft
- **WHEN** a collaborator comments `/turnip diff` on a draft
- **THEN** the plan runs, taking Locks as on any pull request

#### Scenario: A draft holding a plan is pushed to
- **WHEN** a draft whose `web` Lock is `plan_ready` receives a push
- **THEN** the Lock stays `plan_ready`

### Requirement: Pull requests from another repository are refused
The Server SHALL refuse to act on a pull request whose head repository differs from the base repository by owner or name, or reports no head repository at all (for example, a deleted fork). The refusal SHALL hold on both paths: for an acting `pull_request` event, before the configuration is fetched; for a comment trigger, before the configuration is fetched and regardless of the commenter's permission. A refused pull request SHALL get no comment, no check, no Lock and no Runner Job; the refusal SHALL be logged as a warning, `refusing operation on a pull request from another repository`, with `owner`, `repo`, `pr_number`, `head_owner`, `head_repo` and `actor` (the pull request's author on the automatic path, the commenter on the comment path), and the webhook SHALL be answered as a handled refusal (see the `server` capability). Close handling SHALL still run for such a pull request. This SHALL NOT be configurable.

#### Scenario: A fork pull request is opened
- **WHEN** a pull request whose branch lives in another repository is opened
- **THEN** turnip runs nothing, posts nothing and logs the refusal

#### Scenario: A writer comments on a fork pull request
- **WHEN** a writer comments `/turnip diff` on a fork's pull request
- **THEN** nothing runs and no reply is posted

#### Scenario: The fork was deleted
- **WHEN** an acting event arrives for a pull request with no head repository
- **THEN** it is refused as from another repository

#### Scenario: A fork pull request holding a Lock is closed
- **WHEN** a fork's pull request that holds a Lock from before this refusal existed is closed
- **THEN** close handling releases it

### Requirement: Closed pull requests are refused on the comment path
A comment trigger on a pull request that is no longer open (closed or merged) SHALL run nothing (no Lock, no check, no Runner Job, no configuration fetch) and SHALL get one reply:

```
This pull request is closed, so turnip will not run anything on it.

Reopen it to plan again, or open a new pull request with these changes.
```

The refusal SHALL be logged at info level as `refusing operation on a closed pull request`, and SHALL be answered as a handled refusal (see the `server` capability). A failure to post the reply SHALL be logged, not retried.

#### Scenario: Diff on a merged pull request
- **WHEN** a collaborator comments `/turnip diff` on a merged pull request
- **THEN** turnip replies with the closed-pull-request message and runs nothing

#### Scenario: Diff on a closed fork pull request
- **WHEN** a collaborator comments `/turnip diff` on a closed pull request from a fork
- **THEN** no reply is posted, since the fork refusal comes first

### Requirement: Guard order
On the automatic path the Server SHALL apply, in order: the event's action, the draft guard, the fork refusal, then the configuration fetch. On the comment path it SHALL apply, in order: a comment that was just created and contains a trigger (see the `commands` capability), the commenter's collaborator check (see the `authorization` capability), the fork refusal, the closed-pull-request refusal, the configuration fetch, then each command's own checks. A guard that refuses SHALL stop every later step.

#### Scenario: An outsider comments on a fork pull request
- **WHEN** someone who is not a collaborator comments `/turnip diff` on a fork's pull request
- **THEN** turnip replies that they lack permission, since the collaborator check comes before the fork refusal

#### Scenario: A draft from a fork is opened
- **WHEN** a draft pull request from a fork is opened
- **THEN** nothing is logged as a fork refusal, since the draft guard returns first

### Requirement: Reopening plans again
`reopened` SHALL plan exactly the Projects an ordinary push would plan for the pull request's changed files, without a new commit. A pull request reopened as a draft SHALL NOT be planned.

#### Scenario: A pull request is reopened
- **WHEN** a closed pull request touching `web` is reopened
- **THEN** a plan for `web` is dispatched

### Requirement: Closing cleans up
On `closed`, for drafts and forks as for any pull request, the Server SHALL release the pull request's Locks and post the list of released Projects (see the `locking` capability for which Locks and the comment's wording), and SHALL delete the pull request's record of plan comments to minimize (see the `pr-comment` capability). The record SHALL be deleted even when the configuration cannot be read. Closing SHALL NOT change the `turnip` check.

Known issue: close finds the Locks to release from the head commit's configuration, so renamed or removed Projects keep their Locks and an unreadable configuration releases nothing (only logged). Fix proposed in change `fix-lock-release-on-close`.

#### Scenario: A pull request with Locks is closed
- **WHEN** a pull request holding Locks on `web` and `api` is closed without merging
- **THEN** both Locks are released and a comment lists them

#### Scenario: A pull request with no Locks is closed
- **WHEN** a pull request holding no Lock is closed
- **THEN** no comment is posted

#### Scenario: Close with an unreadable configuration
- **WHEN** a pull request is closed and its configuration cannot be fetched
- **THEN** no Lock is released, the failure is logged, and the plan-comment record is still deleted
