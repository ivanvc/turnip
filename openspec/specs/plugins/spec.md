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
After a successful mutating Operation, the tool SHALL judge from the run's whole transcript whether its add, change and destroy counts are meaningful. When they are not, the result SHALL render as `succeeded` with no counts wherever turnip shows change counts: the comment's summary text reads `succeeded`, a single-Project verdict reads `**Succeeded in `<project>`.**`, and the check summary reads `Succeeded.` (see the `pr-comment` and `project-checks` capabilities). Such a result SHALL never render as `no changes`, and SHALL add nothing to a comment's total. A plan's counts and a failed Operation's counts SHALL always be treated as reported.

#### Scenario: Counts unreadable
- **WHEN** a Helmfile `apply` run with `--quiet`, which silences the tables its counts are read from, succeeds
- **THEN** the result SHALL read `succeeded`, never `no changes`

#### Scenario: Plan always counted
- **WHEN** a `diff` succeeds with zero changed releases
- **THEN** the result SHALL read `no changes`

### Requirement: Execution transcript and interleaved output
Every tool run SHALL be recorded as one sequence of lines, in the order they arrived, with the tool's standard output and standard error interleaved rather than one after the other (exact within each stream; two lines written to different streams at the same instant can land either way round). turnip SHALL open the record with two annotation lines and close it with one, each written `@@ turnip: <text> @@`:

| Line | Text |
|---|---|
| first | `<directory>, <tool> <tag-spec>`, the directory repository-relative (`.` for the root) and the tag-spec as `uses:` wrote it |
| second | the command's argument vector, `<tool> <args...>`, including arguments turnip supplied |
| last | `exit <code> in <duration>`, written after both streams have ended |

The same lines SHALL make up the Operation's output in the pull request comment and its live log stream. The annotations SHALL record arguments only, never the environment. A command that could not be started SHALL produce no transcript.

#### Scenario: Diff with a configured environment
- **WHEN** a Helmfile Project with directory `infra/web`, `uses: helmfile@v1.7.4` and `with: {environment: staging}` runs `diff -l name=api`
- **THEN** its output SHALL open with `@@ turnip: infra/web, helmfile v1.7.4 @@` and `@@ turnip: helmfile --environment staging diff -l name=api @@`
- **AND** it SHALL end with a line such as `@@ turnip: exit 0 in 4.2s @@`

#### Scenario: Warning written mid-run
- **WHEN** a tool writes a warning to standard error between two lines of standard output
- **THEN** the warning SHALL appear between those two lines in the output, not at its end

#### Scenario: Tool binary missing
- **WHEN** the tool's binary cannot be found in the image
- **THEN** the Operation SHALL fail with no transcript lines in its output
