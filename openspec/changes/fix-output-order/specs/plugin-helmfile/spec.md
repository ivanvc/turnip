## ADDED Requirements

### Requirement: Helmfile output
A successful Helmfile Operation's output SHALL be the whole record of its run (see the `plugins` capability). A failed one's, whatever the operation, SHALL be the record's standard error lines only, between turnip's annotation lines: Helmfile writes its progress and every error there, and its diffs to standard output. When a failed Operation wrote to standard output, the line `@@ turnip: <N> lines of standard output omitted because the operation failed @@` (`1 line` for one) SHALL come right after the leading annotation lines.

#### Scenario: A successful diff
- **WHEN** `helmfile diff` exits 0
- **THEN** its output is every line it wrote, standard output and standard error interleaved as recorded

#### Scenario: A diff that fails for two releases
- **WHEN** `helmfile diff` writes 30,000 lines of diffs to standard output, reports errors for two releases on standard error, and exits 1
- **THEN** its output is the two leading annotation lines, then `@@ turnip: 30000 lines of standard output omitted because the operation failed @@`, then its standard error lines in the order written, then `@@ turnip: exit 1 in <duration> @@`
- **AND** no line of a diff appears among the error lines

#### Scenario: A failure that printed nothing to standard output
- **WHEN** `helmfile diff` exits 1 having written only to standard error
- **THEN** its output has no omission line

#### Scenario: A failed apply
- **WHEN** `helmfile apply` exits 1 after printing a failed-releases table on standard error
- **THEN** its output shows that table and Helmfile's errors, without the diffs it printed to standard output

#### Scenario: The diffs of a failed plan
- **WHEN** an operator runs `kubectl logs` on the Runner Pod of a failed `diff`
- **THEN** the log shows the diffs the results comment omits (see the `observability` capability)

## MODIFIED Requirements

### Requirement: Helmfile operations
The Helmfile tool SHALL offer exactly three operations: `diff`, which is its plan operation, and `apply` and `sync`, which are mutating operations. It SHALL NOT offer `destroy` or any other operation. Its trigger word SHALL be `/helmfile` (see the `commands` capability).

#### Scenario: The plan operation is diff
- **WHEN** turnip plans a Helmfile Project automatically or for a bare `/helmfile diff`
- **THEN** the Runner runs Helmfile's `diff` operation

#### Scenario: apply and sync are mutating
- **WHEN** a writer comments `/helmfile apply web` or `/helmfile sync web`
- **THEN** turnip treats the operation as mutating, subject to the plan, permission and requirement rules for mutating commands (see the `authorization` and `locking` capabilities)

#### Scenario: destroy is not an operation
- **WHEN** a collaborator comments `/helmfile destroy web`
- **THEN** turnip runs nothing for `web`
- **AND** the results comment reports `operation "destroy" is not recognized for tool "helmfile"` for that Project

### Requirement: Helmfile command line
The Runner SHALL invoke Helmfile in the Project's directory as `helmfile --no-color [--environment <environment>] <operation> [arguments...]`, where `<environment>` is the Project's `with.environment` and the arguments are the plan's extra arguments (replayed by mutating operations from the recorded plan; see the `locking` capability). `--no-color` SHALL always be passed, so Helmfile's colored banners put no color codes in the results comment or the logs. `--environment` SHALL be passed only when `with.environment` is set to a non-empty value. Any other key under `with:` SHALL be accepted by validation and ignored by the Helmfile tool.

#### Scenario: Environment set
- **WHEN** a Project sets `with: {environment: staging}` and is planned with no extra arguments
- **THEN** the Runner runs `helmfile --no-color --environment staging diff`

#### Scenario: Environment omitted
- **WHEN** a Project has no `with.environment`
- **THEN** the Runner runs `helmfile --no-color diff`, and Helmfile uses its own default environment

#### Scenario: Extra arguments follow the operation
- **WHEN** a collaborator comments `/helmfile diff web -l name=ingress` and `web` sets `with.environment: staging`
- **THEN** the Runner runs `helmfile --no-color --environment staging diff -l name=ingress`

#### Scenario: Unknown with key
- **WHEN** a Project sets `with: {environment: staging, color: blue}`
- **THEN** the configuration is valid and `color` has no effect on the command line

#### Scenario: Release tables without color codes
- **WHEN** a `helmfile apply` succeeds and prints its `Updated Releases` table
- **THEN** the table's banner reaches the results comment as plain text, with no color code such as `[1;34m` around it

### Requirement: Unreported mutation counts
For a successful `apply` or `sync`, turnip SHALL treat the counts as unreported (rendered as "succeeded" rather than as counts or no changes; see the `plugins` capability) when either (a) any line of the whole transcript, trimmed, is one of the pre-v1.6.0 banners `UPDATED RELEASES:`, `REINSTALLED RELEASES:`, `DELETED RELEASES:`, `FAILED RELEASES:` or `FAILED TO DELETE RELEASES:`, or (b) the Helmfile command line recorded in the transcript sets Helmfile's log level above info. The log level SHALL be read with Helmfile's precedence, considering arguments only up to a `--`: `--debug` wins over `-q`/`--quiet`, which wins over `--log-level`; a `--log-level` of `warn`, `error`, `dpanic`, `panic` or `fatal` (case-insensitive) silences the tables. A level set through `HELMFILE_QUIET` or `HELMFILE_LOG_LEVEL` in the environment SHALL NOT be detected.

#### Scenario: Quiet plan replayed by apply
- **WHEN** a plan ran `helmfile diff -q` and the apply replays `-q`
- **THEN** the successful apply renders as succeeded with no counts

#### Scenario: Debug overrides quiet
- **WHEN** the recorded command line is `helmfile --no-color apply --quiet --debug`
- **THEN** the counts are read from the release tables as usual

#### Scenario: Older Helmfile
- **WHEN** a successful apply's transcript contains a line `UPDATED RELEASES:`
- **THEN** the apply renders as succeeded with no counts, not as no changes

#### Scenario: Environment silences tables undetected
- **WHEN** the image sets `HELMFILE_LOG_LEVEL=warn` and an apply changes releases
- **THEN** no release table is printed and the apply reports no changes
