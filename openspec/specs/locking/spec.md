# locking Specification

## Purpose
A per-Project Lock, kept in Redis or Valkey, stops two pull requests from acting on the same Project at once and carries a plan's record to the mutating command that replays it. End users rely on it to apply exactly what was reviewed; operators rely on it to keep infrastructure from being changed underneath a pending plan.

## Requirements

### Requirement: One Lock per Project, held by one pull request, with no expiry
The Server SHALL keep at most one Lock per Project, stored in the Redis or Valkey instance it is configured with under the key `lock:<owner>/<repo>/<project name>`. A Lock SHALL record the holding pull request's number and URL, when it was taken, who took it (the commenter's login, or `auto` for an automatic plan), its state, and the plan record (see "The plan record"). A Lock SHALL have no time to live: it SHALL be released only by the events in "What releases a Lock", never by a timer. Every Server replica SHALL read and write the same Locks, so a Lock taken through one replica is honored by all of them.

#### Scenario: A first plan takes the Lock
- **WHEN** a plan is dispatched for Project `web` and no Lock exists for it
- **THEN** the Server creates the Lock held by that pull request, in state `planning`

#### Scenario: An abandoned pull request keeps its Lock
- **WHEN** a pull request holding a Lock receives no further events for weeks
- **THEN** the Lock is still held, and only a release event or `/turnip unlock` frees it

#### Scenario: A Lock taken on one replica is honored by another
- **WHEN** one Server replica takes the Lock for `web` and another replica handles a different pull request's plan for `web`
- **THEN** the second replica refuses that plan as waiting on the Lock

### Requirement: Acquiring a Lock for a plan is atomic
Every plan, automatic or comment-triggered, SHALL acquire its Project's Lock before anything runs. Creating a Lock SHALL succeed only when none exists, so that when several replicas race for the same Project exactly one pull request ends up holding it. A plan from the pull request that already holds the Lock SHALL proceed and apply the plan-dispatched transition in the same step (see "Lock states and transitions"). Every state change SHALL be a compare-and-set on both the holding pull request and the current state; a change that loses a race SHALL be retried against the new state, up to five attempts.

When another pull request holds the Lock, the plan SHALL be refused as a lock wait: nothing runs, the Project's row in the results comment reads `locked by PR #<n>` and links that pull request, the Project's check is queued (see the `project-checks` capability), and the `turnip` check records the Project as not planned (see the `merge-check` capability). When the holder cannot be read, the row SHALL read `locked by another PR` without naming a number. The Server SHALL NOT re-plan on its own when the Lock is later released.

When the Lock cannot be acquired because Redis fails or the state keeps moving through every attempt, the plan SHALL be refused as turnip's own failure, with the reason `acquiring lock: <error>` in the comment and the Project's check failed with the title `lock could not be acquired`.

#### Scenario: Two pull requests race for one Project
- **WHEN** pull requests #4 and #5 dispatch a plan for `web` at the same moment through different replicas, and no Lock exists
- **THEN** exactly one of them holds the Lock and runs its plan
- **AND** the other is refused as a lock wait naming the holder

#### Scenario: The holder re-plans
- **WHEN** pull request #4 holds the Lock for `web` and dispatches another plan for it
- **THEN** the plan runs under the same Lock

#### Scenario: Another pull request holds the Lock
- **WHEN** pull request #5 plans `web` while #4 holds its Lock
- **THEN** no Runner Job is created for `web`
- **AND** the comment's row for `web` reads `locked by PR #4`, linking pull request #4

#### Scenario: Redis is unreachable
- **WHEN** a plan is dispatched and the Lock cannot be read or written
- **THEN** the plan is refused with `acquiring lock: <error>`
- **AND** the Project's check fails with the title `lock could not be acquired`

### Requirement: Lock states and transitions
A held Lock SHALL be in one of three states: `planning` (held, no successful plan recorded), `plan_ready` (a recorded plan describes the change at the current head and may be applied) or `plan_stale` (a plan was recorded and has since been invalidated; the Lock is held but cannot be applied). An absent Lock has no state. A stored state the Server does not recognize, including a Lock written with no state at all, SHALL be treated as `plan_stale`.

Each finished or dispatched Operation SHALL move the Lock according to this table, where "released" deletes the Lock and "unchanged" leaves it as it was:

| Event | from `planning` | from `plan_ready` | from `plan_stale` |
|---|---|---|---|
| plan dispatched | `planning` | `plan_stale` | `plan_stale` |
| plan succeeded with something to apply | `plan_ready` | unchanged | `plan_ready` |
| plan succeeded with nothing to apply | released | unchanged | released |
| plan failed | released | unchanged | `plan_stale` |
| plan did not start in time | `planning` | unchanged | `plan_stale` |
| mutating command succeeded | released | released | released |
| mutating command failed | `plan_stale` | `plan_stale` | `plan_stale` |
| mutating command did not start in time | `plan_stale` | `plan_stale` | `plan_stale` |
| `/turnip unlock` | released | released | released |
| pull request closed | released | released | released |

A plan has something to apply when it reported any addition, change or destruction, or when its tool can act without changes (see the `plugins` capability). An Operation whose tool is not registered SHALL be treated as a mutating command. A start timeout is the operation-lifecycle sweep's (see the `operation-lifecycle` capability).

#### Scenario: A successful plan with changes
- **WHEN** a plan for `web` in state `planning` succeeds and reports one change
- **THEN** the Lock moves to `plan_ready` and records the plan

#### Scenario: A first plan fails
- **WHEN** the first plan for `web` fails
- **THEN** the Lock is released, since nothing had been recorded

#### Scenario: A re-plan fails after an earlier plan succeeded
- **WHEN** a plan for `web` fails while its Lock is `plan_stale`
- **THEN** the Lock stays held in `plan_stale`

#### Scenario: A clean plan for a tool that acts without changes
- **WHEN** a Helmfile plan in state `planning` succeeds with no changes
- **THEN** the Lock moves to `plan_ready`, since `sync` can still act

#### Scenario: An apply fails part-way
- **WHEN** a mutating command for `web` fails while its Lock is `plan_ready`
- **THEN** the Lock stays held and moves to `plan_stale`

#### Scenario: An unrecognized stored state
- **WHEN** a Lock's stored state is empty or a value the Server does not know
- **THEN** it is treated as `plan_stale`: a mutating command is refused and a failed plan does not release it

### Requirement: A new plan supersedes the stored plan when it is dispatched
Dispatching a plan for a Project whose Lock is `plan_ready` SHALL move it to `plan_stale` at once, before the Runner reports, so that no mutating command can be admitted against the previous plan while the new one runs. The supersession itself SHALL NOT be announced; the new plan's own result says what it found, and a failing re-plan announces that the stored plan is still unusable.

Known issue: a push invalidates only the plans of Projects the automatic plan matched; Projects planned by name or `*` outside `whenModified`, and all Projects on a draft, keep `plan_ready` across a push. Fix proposed in change `fix-plan-survives-push`.

#### Scenario: A push re-plans a matched Project
- **WHEN** `web` has a `plan_ready` Lock and a push changes a file its `whenModified` matches
- **THEN** the automatic plan's dispatch moves the Lock to `plan_stale`
- **AND** an apply arriving before the new plan finishes is refused as no longer valid

#### Scenario: A push leaves a Project planned by name ready
- **WHEN** `api` was planned by name and holds a `plan_ready` Lock, and a push changes no file its `whenModified` matches
- **THEN** the Lock stays `plan_ready` and an apply replays the plan recorded on the earlier commit

### Requirement: The plan record
When a plan succeeds with something to apply, the Lock SHALL record, alongside the move to `plan_ready`: the arguments the plan ran with (as the Runner received them), the plan artifact the Runner returned (which may be empty, as for Helmfile), the plan's add, change and destroy counts, whether those counts were reported (see the `plugins` capability), and the sha256 digest of the image the plan's Pod ran. The digest SHALL be read from the plan's Pod when the result arrives; when it cannot be read it SHALL be recorded as empty and the plan still succeeds. No other event SHALL write the plan record. What a mutating command does with the recorded digest is the `tool-images` capability's.

#### Scenario: A scoped plan is recorded
- **WHEN** `/helmfile diff web -- -l name=api` succeeds with changes
- **THEN** the Lock records the arguments `-l name=api`, the counts and the image digest

#### Scenario: The digest cannot be read
- **WHEN** a plan succeeds but its Pod is gone before the digest is read
- **THEN** the Lock moves to `plan_ready` with an empty digest

#### Scenario: A plan whose counts were not reported
- **WHEN** `/helmfile diff web -- -q` succeeds, so its counts are not reported, and `/helmfile apply web` then replays it
- **THEN** the Lock records that the counts were not reported, and the apply's running check is titled `running the recorded plan, succeeded, -q`, never `no changes`

### Requirement: Admitting a mutating command
A mutating command (any Operation other than the tool's plan) SHALL run only when the requesting pull request holds the Project's Lock in state `plan_ready` with a non-empty recorded image digest. It SHALL then run with the recorded arguments and plan artifact, and its check SHALL show the recorded counts while it runs (see the `project-checks` capability). Otherwise it SHALL be refused before any Runner Job or check is created, leaving the Lock and the `turnip` check unchanged, with one of these reasons in the Project's row of the results comment:

| Condition | Reason |
|---|---|
| no Lock, or another pull request's Lock | `no lock (or a different PR's lock) is held; a new plan is required` |
| Lock in `plan_stale` | `the recorded plan is no longer valid: a new commit, a failed plan, or an operation that did not complete has superseded it. Re-plan before retrying.` |
| Lock in `planning` | `no plan recorded; a new plan is required` |
| recorded digest empty | `the image this plan ran could not be recorded; re-plan` |
| the Lock cannot be read | `reading lock: <error>` or `retrieving plan: <error>` |

Refusals decided before the Lock is consulted (arguments on a mutating command, unmet mutation requirements, a refused override) leave no trace on the Lock; they belong to the `commands`, `authorization` and `runner-access` capabilities.

Known issue: two mutating commands for one Project are both admitted, since the Lock stays `plan_ready` until a result arrives. Fix proposed in change `fix-concurrent-mutations`.

#### Scenario: Apply after a successful plan
- **WHEN** `/turnip apply web` is commented and this pull request holds `web`'s Lock in `plan_ready` with a recorded digest
- **THEN** the apply runs with the arguments and artifact the plan recorded

#### Scenario: Apply after a push superseded the plan
- **WHEN** `/turnip apply web` is commented while `web`'s Lock is `plan_stale`
- **THEN** no Runner Job is created and the row reads the "no longer valid" reason

#### Scenario: Apply on a Project another pull request holds
- **WHEN** `/turnip apply web` is commented on #5 while #4 holds `web`'s Lock
- **THEN** the apply is refused with `no lock (or a different PR's lock) is held; a new plan is required`

#### Scenario: Two applies in quick succession
- **WHEN** two `/turnip apply web` comments arrive while the Lock is `plan_ready`
- **THEN** both are admitted and both run

### Requirement: What releases a Lock
A Lock SHALL be released by: a successful mutating command; a plan that succeeds with nothing to apply from `planning` or `plan_stale`; a plan that fails from `planning`; `/turnip unlock` from a writer on the holding pull request (targeting, permission and replies are the `commands` and `authorization` capabilities'); and the holding pull request closing or merging (which events trigger it is the `pull-request-lifecycle` capability's). Unlock and close SHALL release from any state. Releasing a Lock SHALL NOT change the `turnip` check (see the `merge-check` capability).

On close, the Server SHALL release, for each Project in the configuration at the pull request's head commit, the Lock that pull request holds, and SHALL post one comment listing them: `Unlocked the following Projects since this PR closed:` followed by `- <project>` per line. No comment SHALL be posted when nothing was released. A Lock another pull request holds SHALL be left alone.

Known issue: close walks the head commit's configuration, so renamed or removed Projects keep their Locks, and an unreadable configuration at close releases nothing (it is only logged). Fix proposed in change `fix-lock-release-on-close`.

#### Scenario: Apply succeeds
- **WHEN** a mutating command for `web` succeeds
- **THEN** the Lock is released and `web` is free for another pull request

#### Scenario: Merge releases every Lock the configuration names
- **WHEN** a pull request holding Locks on `web` and `api` is merged
- **THEN** both Locks are released and the pull request gets a comment listing `web` and `api`

#### Scenario: Close with an invalid configuration
- **WHEN** a pull request holding a Lock on `web` is closed and its head commit's `turnip.yaml` cannot be parsed
- **THEN** no Lock is released and no comment is posted

#### Scenario: A Project removed by the pull request
- **WHEN** a pull request holding a Lock on `web` removes `web` from `turnip.yaml` and is merged
- **THEN** `web`'s Lock stays held

### Requirement: Results report what happened to the Lock
After applying an Operation's result or start timeout to the Lock, the Server SHALL tell the results comment whether this pull request still holds the Project's Lock and, when the transition warrants it, a note saying why it moved (rendering is the `pr-comment` capability's). Both SHALL come from the transition Redis confirmed, never from the transition attempted. The notes SHALL be:

| Transition | Note |
|---|---|
| released after a failed plan | `Lock released: the plan failed, so nothing was recorded. This Project is free for another pull request to plan against.` |
| released after a plan with nothing to apply | `Lock released: the plan found nothing to apply. This Project is free for another pull request to plan against.` |
| released after a successful mutating command | `Lock released: the operation completed. This Project is free for another pull request to plan against.` |
| into `plan_stale` after a failed mutating command | `The stored plan is no longer valid: this operation failed part-way, so infrastructure may have changed since it was planned. Re-plan before retrying. The Lock is still held, so no other pull request can act on this Project meanwhile.` |
| into `plan_stale` after a mutating command that did not start in time | `The stored plan is no longer valid: no result arrived, and the operation may still be running. Re-plan before retrying. The Lock is still held.` |
| a failed plan leaving `plan_stale` | `The plan failed, so the previously stored plan is still unusable. The Lock is still held; a successful plan is required before applying.` |

Every other transition SHALL carry no note. A timed-out Operation SHALL still report its Lock as held when it is.

#### Scenario: A failed apply
- **WHEN** an apply for `web` fails from `plan_ready`
- **THEN** the comment shows `web` as still locked, with the "failed part-way" note

#### Scenario: A plan that found nothing
- **WHEN** a plan for a tool that cannot act without changes reports no changes from `planning`
- **THEN** the comment shows `web` as not locked, with the "found nothing to apply" note

#### Scenario: A plan that did not start in time
- **WHEN** a plan's Runner never reports before the start timeout
- **THEN** the comment still lists the Project as locked by this pull request

### Requirement: Results for a Lock that has moved on
A result or timeout for a Project whose Lock no longer exists SHALL change nothing and SHALL still be reported, with the Project shown as not locked. A combination the transition table leaves unchanged SHALL change nothing. A result for a Lock now held by a different pull request SHALL change nothing and be logged as an error.

#### Scenario: Closed while an apply runs
- **WHEN** a pull request is closed while its apply for `web` is running, and the apply then succeeds
- **THEN** the result is still posted, showing `web` as not locked

#### Scenario: A result for another pull request's Lock
- **WHEN** a result arrives for `web` after #4's Lock was unlocked and #5 has since taken it
- **THEN** #5's Lock is unchanged
