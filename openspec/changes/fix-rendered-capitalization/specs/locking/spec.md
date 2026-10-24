## MODIFIED Requirements

### Requirement: What releases a Lock
A Lock SHALL be released by: a successful mutating command; a plan that succeeds with nothing to apply from `planning` or `plan_stale`; a plan that fails from `planning`; `/turnip unlock` from a writer on the holding pull request (targeting, permission and replies are the `commands` and `authorization` capabilities'); and the holding pull request closing or merging (which events trigger it is the `pull-request-lifecycle` capability's). Unlock and close SHALL release from any state. Releasing a Lock SHALL NOT change the `turnip` check (see the `merge-check` capability).

On close, the Server SHALL release, for each Project in the configuration at the pull request's head commit, the Lock that pull request holds, and SHALL post one comment listing them: `Unlocked the following projects since this PR closed:` followed by `- <project>` per line. No comment SHALL be posted when nothing was released. A Lock another pull request holds SHALL be left alone.

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
| released after a failed plan | `Lock released: the plan failed, so nothing was recorded. This project is free for another pull request to plan against.` |
| released after a plan with nothing to apply | `Lock released: the plan found nothing to apply. This project is free for another pull request to plan against.` |
| released after a successful mutating command | `Lock released: the operation completed. This project is free for another pull request to plan against.` |
| into `plan_stale` after a failed mutating command | `The stored plan is no longer valid: this operation failed part-way, so infrastructure may have changed since it was planned. Re-plan before retrying. The lock is still held, so no other pull request can act on this project meanwhile.` |
| into `plan_stale` after a mutating command that did not start in time | `The stored plan is no longer valid: no result arrived, and the operation may still be running. Re-plan before retrying. The lock is still held.` |
| a failed plan leaving `plan_stale` | `The plan failed, so the previously stored plan is still unusable. The lock is still held; a successful plan is required before applying.` |

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
