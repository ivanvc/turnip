# project-checks Specification

## Purpose
The per-Project GitHub check runs named `turnip/<operation>/<project>`, which show end users each Operation's progress and outcome in the pull request's checks list, with a title that names the cause whenever turnip knows it.

## Requirements

### Requirement: Check name and lifecycle
For each Operation it dispatches, turnip SHALL create a check run on the pull request's head commit named `turnip/<operation>/<project>` (for example `turnip/diff/web`), operation first. It SHALL be created `in_progress` before the Runner Job, with the summary ``Running `<operation>` for project `<project>`.``, and completed with conclusion `success` or `failure` when the Operation finishes. A check SHALL carry no details text, at creation or completion: the Operation's output is in the results comment (see the `pr-comment` capability), so a check's completion never depends on the output's size. Each new Operation SHALL create a new check run under the same name; an older run is never reused. Titles SHALL be plain text with parts joined by commas, and SHALL never repeat the status the icon already shows.

Known issue: a Runner that starts and then dies without reporting a result leaves its check in progress, since nothing bounds a started Operation. Fix proposed in change `runner-timeouts`.

#### Scenario: Plan runs and completes
- **WHEN** an automatic plan dispatches `diff` for Project `web`
- **THEN** a check `turnip/diff/web` appears in progress, titled `running`
- **AND** when the Runner reports success it is completed as `success`, titled with its count text, with no details text

#### Scenario: A plan with very large output
- **WHEN** a plan's output is 160,406 characters, more than GitHub accepts as a check's text
- **THEN** its check is completed as usual, since no output is sent with it, and the output appears in the results comment

### Requirement: Titles while running
A plan operation's check SHALL be titled `running`, followed by `, <args>` when it was given arguments. A mutating Operation's check SHALL be titled `running the recorded plan, <counts>` with the counts the Lock recorded for the plan it replays, followed by `, <args>` when that plan recorded arguments. Arguments SHALL be joined by spaces and truncated to 40 characters, the last replaced by `…`.

#### Scenario: Scoped plan running
- **WHEN** `/turnip diff web -- -l name=api` is running
- **THEN** the check `turnip/diff/web` is titled `running, -l name=api`

#### Scenario: Apply replaying a plan
- **WHEN** `/turnip apply web` runs a recorded plan of 1 addition, 4 changes and 2 destructions
- **THEN** the check `turnip/apply/web` is titled `running the recorded plan, +1 ~4 -2`

### Requirement: Change-count text
A count text SHALL read `+A ~C -D` (additions, changes, destructions), `no changes` when all are zero, or `succeeded` when a successful mutating Operation's counts were unreported (see the `plugins` capability), whatever their values. The same text SHALL be used by the results comment (see the `pr-comment` capability). A completed check's summary SHALL read `add: A, change: C, destroy: D` when something changed, `No changes.` when nothing did, `Succeeded.` when counts were unreported, and on failure `The operation failed. ` followed by the counts sentence, or `The operation failed.` alone when there are none.

#### Scenario: Sync with unreported counts
- **WHEN** a `sync` succeeds and its counts are unreported
- **THEN** its check is titled `succeeded` with summary `Succeeded.`, never `no changes`

#### Scenario: A failure with no counts
- **WHEN** a plan fails before its tool reports any counts
- **THEN** its check's summary is `The operation failed.`, pointing nowhere else

### Requirement: Titles on completion
A successful Operation's check SHALL be titled with its count text, followed by `, <args>` when it ran with arguments. A failed Operation's check SHALL be titled by the step that failed, as the Runner reports it: `<tool> exited <code>` when the tool ran and exited non-zero, `clone failed`, `workspace could not be prepared`, `<tool> could not be started`, or `failed` when the Runner named no step. turnip SHALL NOT interpret a tool's own failure beyond its exit code. An Operation whose Runner never started SHALL be completed as `failure`, titled with the diagnostic turnip found (worded by the `operation-lifecycle` capability), with the summary `The Runner never reported back within the start timeout.`

#### Scenario: Tool exits non-zero
- **WHEN** `helmfile diff` exits 1
- **THEN** the check is completed as `failure` titled `helmfile exited 1`

#### Scenario: Successful scoped plan
- **WHEN** a plan run with `-l name=api` reports no changes
- **THEN** the check is titled `no changes, -l name=api`

#### Scenario: Runner never started
- **WHEN** an Operation's Pod stays stuck pulling its image past the start timeout
- **THEN** the check is completed as `failure` titled with a diagnostic such as `Job <name>: container stuck (ImagePullBackOff)`

### Requirement: Refusal checks
A refusal SHALL put a check in the list only where it tells the reader something, and SHALL carry the full refusal reason as its summary. When a plan is refused before its check exists: a plan waiting on another pull request's Lock SHALL get a `queued` check titled `locked by PR #N, re-plan once it's released`, or `locked by another pull request, re-plan once it's released` when the holder cannot be read; a plan refused because `turnip.yaml` sets an override the Server does not permit (see the `runner-access` capability) SHALL get a check completed as `failure` titled `runner.serviceAccount is not permitted` or `clone.submodules is not permitted`; a plan refused because turnip could not take the Lock SHALL get a check completed as `failure` titled `lock could not be acquired`. A refused mutating Operation, and any refusal not listed here (for example arguments on a mutating command, or no usable recorded plan), SHALL create no check. A check that already exists when the Operation is refused SHALL be completed as `failure`, never moved back to queued, titled `operation could not be recorded` when the Operation Record could not be saved, `Runner Job could not be built` when the Job spec could not be assembled, `Runner Job could not be created` when Kubernetes refused the Job, or `operation could not be started` otherwise. A re-plan after a queued check SHALL create a new check under the same name; turnip SHALL NOT complete the queued one.

#### Scenario: Plan waits on another pull request
- **WHEN** a plan for `web` is refused because pull request #5 holds its Lock
- **THEN** a check `turnip/diff/web` is created `queued`, titled `locked by PR #5, re-plan once it's released`

#### Scenario: Override not permitted
- **WHEN** a plan is refused because the Project sets `runner.serviceAccount` and the Server does not permit it
- **THEN** a check is created completed as `failure`, titled `runner.serviceAccount is not permitted`, its summary naming the setting to add to `TURNIP_ALLOWED_OVERRIDES`

#### Scenario: Kubernetes refuses the Job
- **WHEN** the check for an Operation is in progress and Kubernetes rejects its Runner Job
- **THEN** the check is completed as `failure` titled `Runner Job could not be created`

#### Scenario: Refused apply
- **WHEN** `/turnip apply web` is refused because no plan is recorded
- **THEN** no check run is created for it

### Requirement: Check failures are reported, not fatal
Failing to create or update a Project's check SHALL NOT stop or fail the Operation. It SHALL be logged, and the Project's output in the results comment SHALL end with a note saying what the failure left behind. When the check could not be created, the note SHALL read `Note: this project's GitHub check run could not be created (<error>). The operation itself is unaffected; only its entry in the PR's checks tab is missing.` When an existing check could not be completed, it SHALL read `Note: this project's GitHub check run could not be completed (<error>). The operation itself is unaffected, but its check stays in progress in the PR's checks tab until the operation runs again.`

#### Scenario: GitHub rejects the check update
- **WHEN** completing a Project's check fails with a GitHub API error
- **THEN** the Operation's result is still posted, and its section in the results comment ends with the note saying the check could not be completed and stays in progress, naming the error

#### Scenario: GitHub rejects the check creation
- **WHEN** creating a Project's check fails with a GitHub API error
- **THEN** the Operation still runs, and its section in the results comment ends with the note saying the check could not be created and is missing, naming the error
