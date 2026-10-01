# plugin-helmfile Specification

## Purpose
Defines how turnip runs Helmfile for a Project whose `uses:` resolves to the `helmfile` tool: its operations, its `with:` keys, its alias image, and how change counts are read from Helmfile's output. End users rely on it to know what `/helmfile` commands do; operators rely on it to know what a Helmfile image must provide.

## Requirements

### Requirement: Helmfile operations
The Helmfile tool SHALL offer exactly three operations: `diff`, which is its plan operation, and `apply` and `sync`, which are mutating operations. It SHALL NOT offer `destroy` or any other operation. Its trigger word SHALL be `/helmfile` (see the `commands` capability).

#### Scenario: The plan operation is diff
- **WHEN** turnip plans a Helmfile Project automatically or for a bare `/helmfile diff`
- **THEN** the Runner runs `helmfile diff`

#### Scenario: apply and sync are mutating
- **WHEN** a writer comments `/helmfile apply web` or `/helmfile sync web`
- **THEN** turnip treats the operation as mutating, subject to the plan, permission and requirement rules for mutating commands (see the `authorization` and `locking` capabilities)

#### Scenario: destroy is not an operation
- **WHEN** a collaborator comments `/helmfile destroy web`
- **THEN** turnip runs nothing for `web`
- **AND** the results comment reports `operation "destroy" is not recognized for tool "helmfile"` for that Project

### Requirement: Helmfile command line
The Runner SHALL invoke Helmfile in the Project's directory as `helmfile [--environment <environment>] <operation> [arguments...]`, where `<environment>` is the Project's `with.environment` and the arguments are the plan's extra arguments (replayed by mutating operations from the recorded plan; see the `locking` capability). `--environment` SHALL be passed only when `with.environment` is set to a non-empty value. Any other key under `with:` SHALL be accepted by validation and ignored by the Helmfile tool.

#### Scenario: Environment set
- **WHEN** a Project sets `with: {environment: staging}` and is planned with no extra arguments
- **THEN** the Runner runs `helmfile --environment staging diff`

#### Scenario: Environment omitted
- **WHEN** a Project has no `with.environment`
- **THEN** no `--environment` flag is passed and Helmfile uses its own default environment

#### Scenario: Extra arguments follow the operation
- **WHEN** a collaborator comments `/helmfile diff web -l name=ingress` and `web` sets `with.environment: staging`
- **THEN** the Runner runs `helmfile --environment staging diff -l name=ingress`

#### Scenario: Unknown with key
- **WHEN** a Project sets `with: {environment: staging, color: blue}`
- **THEN** the configuration is valid and `color` has no effect on the command line

### Requirement: Helmfile alias and image
The `helmfile` alias SHALL stand for the vendor image `ghcr.io/helmfile/helmfile` and SHALL accept only tag-specs matching `v*.*.*` or `sha256:*`, used verbatim as the image tag or digest (see the `tool-images` capability for the `uses:` grammar and its errors). Helmfile SHALL be provisioned run-in-image: the Runner runs inside the selected Helmfile image rather than copying a binary out of it, so `helm`, its plugins and any helpers Helmfile shells out to come from that image.

#### Scenario: Full version allowed
- **WHEN** a Project sets `uses: helmfile@v1.7.4`
- **THEN** its Runner runs in `ghcr.io/helmfile/helmfile:v1.7.4`

#### Scenario: Floating or unprefixed tag refused
- **WHEN** a Project sets `uses: helmfile@latest`, `uses: helmfile@v1.7` or `uses: helmfile@1.7.4`
- **THEN** configuration validation fails, naming the tag-specs the alias allows

#### Scenario: Helpers come from the image
- **WHEN** a Project's Helmfile image lacks `helm` on its `PATH`
- **THEN** the operation fails with Helmfile's own error (for example `exec: "helm": executable file not found in $PATH`), and turnip supplies no fallback

### Requirement: Helmfile acts without changes
The Helmfile tool SHALL report that it can act without changes, because `helmfile sync` upgrades every release regardless of the diff. Consequently a Helmfile Project's Lock SHALL be kept when its `diff` reports no changes (see the `locking` capability), and a Helmfile Project SHALL need its apply or sync to satisfy the `turnip` check even after a clean diff (see the `merge-check` capability).

#### Scenario: Clean diff keeps the Lock
- **WHEN** a Helmfile Project's `diff` succeeds with no releases changed
- **THEN** the Project's Lock is kept with its plan recorded, so `/helmfile sync` remains available

### Requirement: Change counts for diff
For `diff`, turnip SHALL count as changed every release whose `Comparing release=<name>,` header on standard output is followed by at least one non-blank line before the next such header or the end of output. Only standard output SHALL be read, so Helmfile's progress and error lines on standard error never make a release count as changed, and turnip's own `@@ turnip: ... @@` transcript lines SHALL NOT count as a release body. A `diff` SHALL report only changed counts, never added or destroyed ones.

#### Scenario: Two of three releases differ
- **WHEN** `helmfile diff` prints headers for releases `a`, `b` and `c`, with diff lines after `a` and `c` only
- **THEN** the plan reports 2 changed, 0 added and 0 destroyed

#### Scenario: Last release unchanged
- **WHEN** the last `Comparing release=` header is followed only by turnip's closing transcript line
- **THEN** that release is not counted as changed

#### Scenario: Pending removal
- **WHEN** a release marked `installed: false` shows a diff
- **THEN** it counts as changed, not destroyed

### Requirement: Change counts for apply and sync
For `apply` and `sync`, turnip SHALL read Helmfile's closing release tables from standard error only, recognizing the `=`-framed banners Helmfile prints from v1.6.0 on, with ANSI color codes ignored. Rows under `Updated Releases` and `Reinstalled Releases` SHALL count as changed and rows under `Deleted Releases` as destroyed; the first non-blank line after a banner is its header row and is not counted; a table's rows end at a blank line or the next banner. Tables with any other title (such as failed releases) SHALL NOT be counted. Counts SHALL be summed over every table printed, including one set per sub-helmfile. A run whose tables report nothing SHALL report zero counts, rendered as no changes.

#### Scenario: Updated and deleted releases
- **WHEN** a successful `helmfile apply` prints an `Updated Releases` table with 2 rows and a `Deleted Releases` table with 1 row on standard error
- **THEN** the apply reports 2 changed and 1 destroyed

#### Scenario: Sub-helmfiles summed
- **WHEN** two sub-helmfiles each print an `Updated Releases` table with 1 row
- **THEN** the apply reports 2 changed

#### Scenario: Failed releases not counted
- **WHEN** the output carries a failed-releases table
- **THEN** its rows are counted neither as changed nor as part of any neighboring table

#### Scenario: Table text in a diff body
- **WHEN** an `Updated Releases` banner appears only on standard output, inside a manifest
- **THEN** it is not counted

### Requirement: Unreported mutation counts
For a successful `apply` or `sync`, turnip SHALL treat the counts as unreported (rendered as "succeeded" rather than as counts or no changes; see the `plugins` capability) when either (a) any line of the whole transcript, trimmed, is one of the pre-v1.6.0 banners `UPDATED RELEASES:`, `REINSTALLED RELEASES:`, `DELETED RELEASES:`, `FAILED RELEASES:` or `FAILED TO DELETE RELEASES:`, or (b) the Helmfile command line recorded in the transcript sets Helmfile's log level above info. The log level SHALL be read with Helmfile's precedence, considering arguments only up to a `--`: `--debug` wins over `-q`/`--quiet`, which wins over `--log-level`; a `--log-level` of `warn`, `error`, `dpanic`, `panic` or `fatal` (case-insensitive) silences the tables. A level set through `HELMFILE_QUIET` or `HELMFILE_LOG_LEVEL` in the environment SHALL NOT be detected.

#### Scenario: Quiet plan replayed by apply
- **WHEN** a plan ran `helmfile diff -q` and the apply replays `-q`
- **THEN** the successful apply renders as succeeded with no counts

#### Scenario: Debug overrides quiet
- **WHEN** the recorded command line is `helmfile apply --quiet --debug`
- **THEN** the counts are read from the release tables as usual

#### Scenario: Older Helmfile
- **WHEN** a successful apply's transcript contains a line `UPDATED RELEASES:`
- **THEN** the apply renders as succeeded with no counts, not as no changes

#### Scenario: Environment silences tables undetected
- **WHEN** the image sets `HELMFILE_LOG_LEVEL=warn` and an apply changes releases
- **THEN** no release table is printed and the apply reports no changes
