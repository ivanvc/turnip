## MODIFIED Requirements

### Requirement: Check name and lifecycle
For each Operation it dispatches, turnip SHALL create a check run on the pull request's head commit named `turnip/<operation>/<project>` (for example `turnip/diff/web`), operation first. It SHALL be created `in_progress` before the Runner Job, with the summary ``Running `<operation>` for Project `<project>`.``, and completed with conclusion `success` or `failure` when the Operation finishes. A check SHALL carry no details text, at creation or completion: the Operation's output is in the results comment (see the `pr-comment` capability), so a check's completion never depends on the output's size. Each new Operation SHALL create a new check run under the same name; an older run is never reused. Titles SHALL be plain text with parts joined by commas, and SHALL never repeat the status the icon already shows.

Known issue: a Runner that starts and then dies without reporting a result leaves its check in progress, since nothing bounds a started Operation. Fix proposed in change `runner-timeouts`.

#### Scenario: Plan runs and completes
- **WHEN** an automatic plan dispatches `diff` for Project `web`
- **THEN** a check `turnip/diff/web` appears in progress, titled `running`
- **AND** when the Runner reports success it is completed as `success`, titled with its count text, with no details text

#### Scenario: A plan with very large output
- **WHEN** a plan's output is 160,406 characters, more than GitHub accepts as a check's text
- **THEN** its check is completed as usual, since no output is sent with it, and the output appears in the results comment

### Requirement: Change-count text
A count text SHALL read `+A ~C -D` (additions, changes, destructions), `no changes` when all are zero, or `succeeded` when a successful mutating Operation's counts were unreported (see the `plugins` capability), whatever their values. The same text SHALL be used by the results comment (see the `pr-comment` capability). A completed check's summary SHALL read `add: A, change: C, destroy: D` when something changed, `No changes.` when nothing did, `Succeeded.` when counts were unreported, and on failure `The operation failed. ` followed by the counts sentence, or `The operation failed.` alone when there are none.

#### Scenario: Sync with unreported counts
- **WHEN** a `sync` succeeds and its counts are unreported
- **THEN** its check is titled `succeeded` with summary `Succeeded.`, never `no changes`

#### Scenario: A failure with no counts
- **WHEN** a plan fails before its tool reports any counts
- **THEN** its check's summary is `The operation failed.`, pointing nowhere else

### Requirement: Check failures are reported, not fatal
Failing to create or update a Project's check SHALL NOT stop or fail the Operation. It SHALL be logged, and the Project's output in the results comment SHALL end with a note saying what the failure left behind. When the check could not be created, the note SHALL read `Note: this Project's GitHub check run could not be created (<error>). The operation itself is unaffected; only its entry in the PR's checks tab is missing.` When an existing check could not be completed, it SHALL read `Note: this Project's GitHub check run could not be completed (<error>). The operation itself is unaffected, but its check stays in progress in the PR's checks tab until the operation runs again.`

#### Scenario: GitHub rejects the check update
- **WHEN** completing a Project's check fails with a GitHub API error
- **THEN** the Operation's result is still posted, and its section in the results comment ends with the note saying the check could not be completed and stays in progress, naming the error

#### Scenario: GitHub rejects the check creation
- **WHEN** creating a Project's check fails with a GitHub API error
- **THEN** the Operation still runs, and its section in the results comment ends with the note saying the check could not be created and is missing, naming the error
