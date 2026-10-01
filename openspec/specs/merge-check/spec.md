# merge-check Specification

## Purpose
The single `turnip` check reports, per pull request head commit, whether every plan the pull request made has been carried out: applied, or found to have nothing to apply. Operators require it in branch protection so a pull request cannot merge with a plan left unapplied; authors and reviewers read it as the pull request's overall state.

## Requirements

### Requirement: One check with a stable name
The Server SHALL report the verdict as one check run named `turnip`, on the pull request's head commit. The name SHALL NOT vary with the Projects, tools or Operations involved; what varies SHALL go in the title and summary. The per-Project checks (`turnip/<operation>/<project>`) are separate (see the `project-checks` capability). A repository with no turnip configuration file SHALL never get a `turnip` check.

#### Scenario: Requiring the check
- **WHEN** an operator marks `turnip` required and a pull request touches only `web`
- **THEN** the pull request is gated by `turnip`, regardless of which Projects it touches

#### Scenario: A repository without turnip
- **WHEN** a pull request is opened in a repository with no turnip configuration
- **THEN** no `turnip` check is ever reported for it

### Requirement: A record per pull request and head commit
The Server SHALL keep, in Redis or Valkey, one record per pull request and head commit holding each Project's latest Outcome on that commit. A Project SHALL enter the record when any plan or mutating command finishes, or a plan is refused, for it on that commit, whether triggered automatically or by comment and whether or not the pull request's changes match it. Writing one Project's Outcome SHALL replace that Project's previous Outcome and leave every other Project's alone. An Outcome SHALL be written to the record of the commit the Operation ran against, so a result arriving after a push lands on the earlier commit's record. A new head commit SHALL start with an empty record. A record SHALL expire 90 days after its last write and SHALL NOT be deleted when the pull request closes.

#### Scenario: Applying one of two planned Projects
- **WHEN** `web` and `api` were planned on a commit and `/turnip apply web` succeeds
- **THEN** the record shows `web` applied and `api` still awaiting apply

#### Scenario: A Project planned by name
- **WHEN** `/turnip diff api` plans a Project the pull request's changes do not match
- **THEN** `api` enters the record and counts toward the verdict

#### Scenario: A push
- **WHEN** a new commit is pushed
- **THEN** the `turnip` check on the new commit starts from an empty record

#### Scenario: A result arrives after a push
- **WHEN** a plan dispatched on commit A finishes after commit B was pushed
- **THEN** its Outcome is written to commit A's record, not commit B's

### Requirement: Outcomes
Each finished or refused Operation SHALL set its Project's Outcome as follows:

| What happened | Outcome | Summary text |
|---|---|---|
| plan succeeded with something to apply (see the `locking` capability) | awaiting apply | `planned, awaiting apply` |
| plan succeeded with nothing to apply | nothing to apply (done) | `planned, nothing to apply` |
| plan failed, or did not start in time | not planned | `not planned` |
| plan refused because another pull request holds the Lock | not planned | `not planned, locked by PR #<n>` (or `not planned` when the holder is unknown) |
| plan refused for a setting the Server does not permit (see the `runner-access` capability) | refused | `<setting> is not permitted; change turnip.yaml, or permit it in TURNIP_ALLOWED_OVERRIDES` |
| plan refused for turnip's own failure before the Runner Job existed | not planned | `not planned` |
| mutating command succeeded | applied (done) | `applied` |
| mutating command failed, or did not start in time | apply failed | `apply failed` |

A mutating command refused before it ran (no plan, a stale plan, arguments of its own, unmet mutation requirements, an unpermitted setting) SHALL NOT change the record. Releasing a Lock, by `/turnip unlock`, by close or otherwise, SHALL NOT change the record.

#### Scenario: A plan blocked by another pull request's Lock
- **WHEN** the plan for `web` is refused because pull request #5 holds its Lock
- **THEN** `web`'s Outcome is not planned, shown as `not planned, locked by PR #5`

#### Scenario: A refused apply
- **WHEN** `/turnip apply web` is refused because the stored plan is no longer valid
- **THEN** `web`'s Outcome is unchanged

#### Scenario: Unlock does not satisfy the check
- **WHEN** `web` is awaiting apply and a writer comments `/turnip unlock web`
- **THEN** `web` is still awaiting apply and the check does not pass

### Requirement: Whole-commit facts from the automatic plan
When the automatic plan finds the configuration invalid, or fails to fetch it for any reason other than the file being absent, the Server SHALL mark the commit's record as having an invalid configuration. When the automatic plan matches no Project, the Server SHALL mark the record as having nothing affected. Both SHALL be published at once.

Known issue: an invalid turnip.yaml found on the comment path does not mark the `turnip` check invalid; only the automatic path does. Fix proposed in change `fix-invalid-config-check-on-comment`.

#### Scenario: An invalid configuration on push
- **WHEN** a push carries a `turnip.yaml` that fails validation
- **THEN** the `turnip` check fails with the title `invalid turnip.yaml`

#### Scenario: The configuration cannot be fetched
- **WHEN** the automatic plan's configuration fetch fails with a GitHub API error
- **THEN** the `turnip` check also fails with the title `invalid turnip.yaml`

#### Scenario: An invalid configuration found by a comment
- **WHEN** `/turnip diff` is commented on a commit whose `turnip.yaml` is invalid, and the automatic plan did not already mark it
- **THEN** the configuration error is replied but the `turnip` check is not marked invalid

### Requirement: The verdict
The verdict SHALL be computed from the whole record, in this order of precedence, the first matching row winning:

| Condition | Status, conclusion | Title |
|---|---|---|
| the configuration is invalid | completed, `failure` | `invalid turnip.yaml` |
| nothing affected and no Project in the record | completed, `skipped` | `no projects affected` |
| any Project refused | completed, `failure` | `not permitted: <first refused project> sets <setting>`, plus `, and <n> more` when others were refused |
| any Project apply failed | completed, `failure` | `<done>/<total> projects up to date, <failed> failed` |
| every Project done (applied, or nothing to apply) | completed, `success` | `<done>/<total> projects up to date` |
| otherwise | in progress | `<done>/<total> projects up to date` |

The summary SHALL list every Project in the record, sorted by name, one line each, as ``- `<project>`: <summary text> (`turnip/<operation>/<project>`)`` using the Outcome's summary text. For an invalid configuration the summary SHALL read `The turnip configuration on this commit is invalid, so nothing could be planned. The pull request comment has the details.`; for nothing affected, `No project's files changed in this pull request, so there is nothing to plan or apply.`. A failure SHALL mean only something the author can fix by changing the pull request; a plan awaiting apply, a lock wait or a failed plan SHALL NOT make the check fail.

#### Scenario: All applied
- **WHEN** `web` and `api` were both applied on the head commit
- **THEN** the check succeeds with the title `2/2 projects up to date`

#### Scenario: One apply failed
- **WHEN** of three Projects two were applied and one's apply failed
- **THEN** the check fails with the title `2/3 projects up to date, 1 failed`

#### Scenario: A plan that found nothing for a tool that acts without changes
- **WHEN** a Helmfile Project's plan finds no changes
- **THEN** it is awaiting apply, not done, since `sync` can still act

#### Scenario: A refused setting outranks a failed apply
- **WHEN** one Project was refused for `runner.serviceAccount` and another's apply failed
- **THEN** the title is `not permitted: <project> sets runner.serviceAccount`

### Requirement: When the check is published
The Server SHALL publish the check on a commit only when one of these holds for that commit: it was already published, a mutating command's Outcome (applied or apply failed) has been recorded, or the verdict is completed. Until then no `turnip` check SHALL exist on the commit, so a required check shows as GitHub's "Expected — Waiting for status to be reported", blocking without failing. Once published, every later write to the record SHALL republish it. A completed check run SHALL never be reopened: when the verdict leaves a completed state, the Server SHALL create a new `turnip` check run on the commit and keep that one current. When updating the recorded check run fails, the Server SHALL create a new one.

#### Scenario: Plans ran, nothing applied yet
- **WHEN** a push plans `web` and `api`, both with changes, and nothing has been applied
- **THEN** no `turnip` check exists on the commit and GitHub shows it as Expected

#### Scenario: The first apply
- **WHEN** `web` is applied while `api` still awaits apply
- **THEN** the check appears in progress with the title `1/2 projects up to date`

#### Scenario: Every plan found nothing
- **WHEN** every planned Project's plan finds nothing to apply, for tools that cannot act without changes
- **THEN** the check is published at once as succeeded

#### Scenario: A re-plan after success
- **WHEN** the check succeeded and a comment then re-plans `web` with changes on the same commit
- **THEN** a new in-progress `turnip` check run is created on the commit rather than the completed one being reopened

### Requirement: Publishing from any replica
Any Server replica SHALL be able to record an Outcome and publish the verdict for any pull request, computing it from the shared record rather than from what that replica has seen, without coordinating with other replicas. After publishing, a replica SHALL re-read the record and publish again if it changed meanwhile, up to 100 rounds, so the last verdict GitHub receives reflects the latest record. When an Outcome cannot be recorded or the check cannot be published, the Project's section of the results comment SHALL carry the note "Note: the `turnip` check could not be updated for this result (<error>). The operation itself is unaffected; the check catches up on the next result for this commit."; a failure publishing an invalid-configuration or nothing-affected mark SHALL only be logged.

#### Scenario: Two Projects finish on different replicas
- **WHEN** `web`'s result is handled by one replica and `api`'s by another at the same time
- **THEN** neither Outcome is lost and the check converges to the verdict for both

#### Scenario: Redis fails while recording
- **WHEN** a result arrives and its Outcome cannot be written
- **THEN** the Project's section of the comment carries the "could not be updated" note
