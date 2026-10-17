## MODIFIED Requirements

### Requirement: Change counts for diff
For `diff`, turnip SHALL count as changed every release whose `Comparing release=<name>,` header on standard output is followed by at least one non-blank line before the next such header or the end of output. Only standard output SHALL be read for changes, so Helmfile's progress and error lines on standard error never make a release count as changed, and turnip's own `@@ turnip: ... @@` transcript lines SHALL NOT count as a release body. turnip SHALL count as destroyed every release listed as `<name> (<chart>) DELETED` under an `Affected releases are:` line on standard error, summed over every such summary Helmfile prints (one per helmfile), since Helmfile reports a release it will remove only there and runs no diff for it. Lines of that summary marked `UPDATED` SHALL NOT be counted, so a release is never counted twice. A `diff` SHALL never report added releases.

#### Scenario: Two of three releases differ
- **WHEN** `helmfile diff` prints headers for releases `a`, `b` and `c`, with diff lines after `a` and `c` only
- **THEN** the plan reports 2 changed, 0 added and 0 destroyed

#### Scenario: Last release unchanged
- **WHEN** the last `Comparing release=` header is followed only by turnip's closing transcript line
- **THEN** that release is not counted as changed

#### Scenario: Pending removal
- **WHEN** a release set to `installed: false` is still deployed, and `helmfile diff` prints, on standard error, `Affected releases are:` followed by `  web (example/web) DELETED`
- **THEN** the plan reports 1 destroyed, and the release is not counted as changed

#### Scenario: Removals and changes together
- **WHEN** `helmfile diff` prints diffs for releases `a` and `b` on standard output, and a summary listing `c` as `DELETED` on standard error
- **THEN** the plan reports 2 changed and 1 destroyed

#### Scenario: Removals in two helmfiles
- **WHEN** two helmfiles each print an `Affected releases are:` summary listing one release as `DELETED`
- **THEN** the plan reports 2 destroyed

#### Scenario: Updated releases in the summary
- **WHEN** the summary lists a release as `UPDATED`, because the plan ran with Helmfile's `--detailed-exitcode`
- **THEN** that line is not counted, and the release counts as changed only through its diff

#### Scenario: The summary's text in a diff body
- **WHEN** a manifest on standard output contains the lines `Affected releases are:` and `  web (example/web) DELETED`
- **THEN** nothing is counted as destroyed

### Requirement: Unreported mutation counts
For a successful `apply` or `sync`, turnip SHALL treat the counts as unreported (rendered as "succeeded" rather than as counts or no changes; see the `plugins` capability) when either (a) any line of the whole transcript, trimmed, is one of the pre-v1.6.0 banners `UPDATED RELEASES:`, `REINSTALLED RELEASES:`, `DELETED RELEASES:`, `FAILED RELEASES:` or `FAILED TO DELETE RELEASES:`, or (b) the Helmfile command line recorded in the transcript sets Helmfile's log level above info. For a successful `diff`, turnip SHALL treat the counts as unreported when (b) holds, since Helmfile then prints no `Affected releases are:` summary and a release it will remove cannot be seen. The log level SHALL be read with Helmfile's precedence, considering arguments only up to a `--`: `--debug` wins over `-q`/`--quiet`, which wins over `--log-level`; a `--log-level` of `warn`, `error`, `dpanic`, `panic` or `fatal` (case-insensitive) silences the tables and the summary. A level set through `HELMFILE_QUIET` or `HELMFILE_LOG_LEVEL` in the environment SHALL NOT be detected.

#### Scenario: Quiet plan replayed by apply
- **WHEN** a plan ran `helmfile diff -q` and the apply replays `-q`
- **THEN** the successful apply renders as succeeded with no counts

#### Scenario: A quiet plan
- **WHEN** a plan runs `helmfile --no-color diff -q` and succeeds
- **THEN** it renders as succeeded with no counts, never as no changes, even when it printed diffs

#### Scenario: Debug overrides quiet
- **WHEN** the recorded command line is `helmfile --no-color apply --quiet --debug`
- **THEN** the counts are read from the release tables as usual

#### Scenario: Older Helmfile
- **WHEN** a successful apply's transcript contains a line `UPDATED RELEASES:`
- **THEN** the apply renders as succeeded with no counts, not as no changes

#### Scenario: Environment silences tables undetected
- **WHEN** the image sets `HELMFILE_LOG_LEVEL=warn` and an apply changes releases
- **THEN** no release table is printed and the apply reports no changes
