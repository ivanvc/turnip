# plugins Specification

## Purpose
Defines the set of IaC tools turnip runs and the behavior every tool shares: its operation vocabulary, which operation plans and which mutate, how a run's counts are reported, and the execution transcript in its output. End users rely on it to know what they can trigger and how to read results; each per-tool capability builds on it.

## Requirements

### Requirement: Tool registry
turnip SHALL run exactly the tools in its built-in registry, which today holds one tool, `helmfile` (see the `plugin-helmfile` capability). The Server and the Runner SHALL use the same registry. The registered tool names SHALL be the only tool words a trigger comment accepts after `/` besides `turnip` (see the `commands` capability), the only aliases `uses:` accepts, and the only tools a `TURNIP_ALLOWED_IMAGES` entry can name (see the `tool-images` capability). Error messages listing the tools SHALL list them in alphabetical order. The registry SHALL NOT be configurable by an operator.

#### Scenario: Unregistered tool word
- **WHEN** a comment line begins `/terraform plan` and no Terraform Plugin is registered
- **THEN** turnip SHALL ignore the line as not addressed to it

#### Scenario: Unregistered tool in uses
- **WHEN** a Project writes `uses: terraform@1.9.0`
- **THEN** validation SHALL fail listing `"helmfile"` as the only supported tool

#### Scenario: Runner handed an unregistered tool
- **WHEN** a Runner starts with a tool its registry does not hold
- **THEN** it SHALL exit with a failure before connecting to the Server or running anything

### Requirement: Operations in each tool's own vocabulary
Each tool SHALL expose its own native operation names rather than a shared vocabulary, and SHALL designate one of them as its plan operation and one as its apply operation. Only a tool's own operations SHALL be runnable for its Projects. For Helmfile these SHALL be `diff` (plan), `apply` (apply) and `sync`. The plan and apply names SHALL be the ones turnip prints in a comment's suggested next commands (see the `pr-comment` capability).

#### Scenario: Helmfile vocabulary
- **WHEN** a collaborator comments `/helmfile diff web`
- **THEN** turnip SHALL run Helmfile's plan operation for Project `web`

#### Scenario: Operation another tool would use
- **WHEN** a collaborator comments `/helmfile plan web`
- **THEN** turnip SHALL NOT run it, since `plan` is not a Helmfile operation

### Requirement: Plan and mutating operations
A tool's plan operation SHALL be its only non-mutating operation; every other operation it exposes SHALL be a mutating operation, with no per-operation list. An Operation for a tool the Server does not recognize SHALL be treated as mutating. What a plan and a mutating operation each do to a Lock, and when a mutating command is admitted, belong to the `locking` and `commands` capabilities.

#### Scenario: sync is mutating
- **WHEN** a collaborator comments `/helmfile sync web`
- **THEN** turnip SHALL treat it exactly as it treats `apply`: requiring a recorded plan and replaying it

### Requirement: Acting without changes
Each tool SHALL declare whether any of its mutating operations can affect infrastructure when its plan reported no changes. When a plan succeeds with zero additions, changes and destructions, turnip SHALL keep the plan applicable if the tool declares it acts without changes, and treat the plan as having nothing to apply otherwise (the resulting Lock transition belongs to the `locking` capability). Helmfile SHALL declare that it acts without changes, because `sync` upgrades every release regardless of the diff.

#### Scenario: Clean Helmfile diff
- **WHEN** a Helmfile `diff` succeeds with no changed releases
- **THEN** the plan SHALL be recorded as applicable, so `sync` stays available

#### Scenario: Tool that cannot act without changes
- **WHEN** a plan for a tool declaring it does not act without changes succeeds with all counts zero
- **THEN** turnip SHALL treat the plan as having nothing to apply

### Requirement: Unreported mutation counts
After a successful Operation, the tool SHALL judge from the run's whole transcript whether its add, change and destroy counts are meaningful. When they are not, the result SHALL render as `succeeded` with no counts wherever turnip shows change counts: the comment's summary text reads `succeeded`, a single-Project verdict reads `**Succeeded in `<project>`.**`, and the check summary reads `Succeeded.` (see the `pr-comment` and `project-checks` capabilities). Such a result SHALL never render as `no changes`, and SHALL add nothing to a comment's total. A plan whose counts are not reported SHALL be treated as having something to apply, never as a plan with no changes (see the requirement "Acting without changes"). A failed Operation's counts SHALL always be treated as reported, and a plan's SHALL be unless its tool says otherwise (for Helmfile, a quiet `diff`; see the `plugin-helmfile` capability).

#### Scenario: Counts unreadable
- **WHEN** a Helmfile `apply` run with `--quiet`, which silences the tables its counts are read from, succeeds
- **THEN** the result SHALL read `succeeded`, never `no changes`

#### Scenario: Plan always counted
- **WHEN** a `diff` without a quiet flag succeeds with zero changed releases
- **THEN** the result SHALL read `no changes`

#### Scenario: A plan whose counts are unreadable
- **WHEN** a Helmfile `diff` run with `-q` succeeds
- **THEN** the result SHALL read `succeeded`, and the plan is kept as one that may have something to apply

#### Scenario: A quiet plan replayed
- **WHEN** `/helmfile apply web` replays a recorded plan whose counts were not reported, run with `-q`
- **THEN** the Lock recorded that the counts were not reported, and the check `turnip/apply/web` is titled `running the recorded plan, succeeded, -q`, never `no changes`

### Requirement: Execution transcript and interleaved output
Every tool run SHALL be recorded as one sequence of lines, each marked with the stream it was written to, with the tool's standard output and standard error interleaved rather than one after the other. Each stream's lines SHALL be in the order they were written. Across the two streams the record SHALL be in write order when the tool writes to one stream and then, later, to the other, however much it wrote to the first; lines the tool writes to both streams at the same moment, or within the time turnip takes to read one of them, can land either way round, close to where they were written. turnip SHALL open the record with two annotation lines and close it with one, each written `@@ turnip: <text> @@`:

| Line | Text |
|---|---|
| first | `<directory>, <tool> <tag-spec>`, the directory repository-relative (`.` for the root) and the tag-spec as `uses:` wrote it |
| second | the command's argument vector, `<tool> <args...>`, including arguments turnip supplied |
| last | `exit <code> in <duration>`, written after both streams have ended |

The Operation's live log stream and the Runner's own log SHALL carry the record as it happened (see the `operation-lifecycle` and `observability` capabilities). The Operation's output in the pull request comment SHALL be what the tool makes of the record (see the requirement "Output chosen by the tool"). The annotations SHALL record arguments only, never the environment. A command that could not be started SHALL produce no transcript.

#### Scenario: Diff with a configured environment
- **WHEN** a Helmfile Project with directory `infra/web`, `uses: helmfile@v1.7.4` and `with: {environment: staging}` runs `diff -l name=api`
- **THEN** its output SHALL open with `@@ turnip: infra/web, helmfile v1.7.4 @@` and `@@ turnip: helmfile --no-color --environment staging diff -l name=api @@`
- **AND** it SHALL end with a line such as `@@ turnip: exit 0 in 4.2s @@`

#### Scenario: Warning written mid-run
- **WHEN** a tool writes a line to standard output, a moment later a warning to standard error, and a moment later another line to standard output
- **THEN** the warning SHALL appear between those two lines in the record, not at its end

#### Scenario: Errors written after a long output
- **WHEN** a tool writes 20,000 lines to standard output and then 5 lines to standard error
- **THEN** every one of the 20,000 lines SHALL precede the first standard error line in the record

#### Scenario: Errors written after a burst
- **WHEN** a tool writes several megabytes to standard output in one write and then an error to standard error
- **THEN** the error SHALL follow every line of that write in the record

#### Scenario: Both streams written at once
- **WHEN** a tool writes to standard output and to standard error at the same time, or one right after the other with no pause
- **THEN** each stream's lines SHALL keep their own order in the record
- **AND** a line of one stream MAY be recorded before or after a line of the other written at that moment

#### Scenario: Tool binary missing
- **WHEN** the tool's binary cannot be found in the image
- **THEN** the Operation SHALL fail with no transcript lines in its output

### Requirement: Output chosen by the tool
Each tool SHALL decide what an Operation's output is: the whole record of its run, or a selection of the record's lines by stream, kept in the record's order. Whatever the tool selects, the output SHALL open with the record's leading annotation lines and close with its closing one, so no tool can leave them out. The Server SHALL treat the output as text, knowing nothing of which stream a line was written to. Change counts SHALL be read from the record, never from a selection of it.

#### Scenario: The whole record
- **WHEN** a Helmfile `diff` succeeds
- **THEN** its output SHALL be every line of the record, standard output and standard error interleaved as recorded

#### Scenario: A selection by stream
- **WHEN** a Helmfile `diff` fails, so its output is its standard error alone (see the `plugin-helmfile` capability)
- **THEN** the output SHALL still open with the directory and command annotation lines and end with the `exit <code> in <duration>` line

#### Scenario: Counts of a selected output
- **WHEN** a Helmfile `diff` fails after printing diffs for 2 releases on standard output
- **THEN** its result SHALL report 2 changed, though its output shows no standard output
