## MODIFIED Requirements

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
