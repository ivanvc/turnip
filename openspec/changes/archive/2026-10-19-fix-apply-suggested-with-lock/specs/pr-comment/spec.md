## MODIFIED Requirements

### Requirement: Section trailer and next steps
After the fenced output, the final piece of a Project's section SHALL carry, in order: a blocked note when another pull request's Lock refused the plan (`Locked by [#N](url).`, or `Locked by pull request #N.` when no URL is known); the lock note explaining why this Project's Lock moved, when it moved (wording and transitions are the `locking` capability's); and a list of commands for that Project alone. The list SHALL offer `- Apply just this project: `/turnip <apply-op> <name>`` only for a successful plan while this pull request still holds the Project's Lock, whatever the plan's counts and whether they were reported, and never for the result of a mutating operation; `- Re-plan it: `/turnip <plan-op> <name>`` for any result its Runner reported; and `- Release its lock: `/turnip unlock <name>`` only while this pull request still holds the Project's Lock. The apply offered SHALL be the tool's own apply operation (`apply` for Helmfile). A refused Operation offers no command; a timed-out one offers only the unlock while its Lock is held.

#### Scenario: Plan with changes keeps its Lock
- **WHEN** `web`'s `diff` succeeds with changes and the pull request holds `web`'s Lock
- **THEN** the section ends with the three lines `Apply just this project: /turnip apply web`, `Re-plan it: /turnip diff web` and `Release its lock: /turnip unlock web`

#### Scenario: Plan blocked by another pull request
- **WHEN** `web`'s plan is refused because pull request #5 holds its Lock
- **THEN** the section's output is `locked by PR #5` and its trailer reads `Locked by [#5](https://github.com/<owner>/<repo>/pull/5).`

#### Scenario: A clean plan whose Lock is kept
- **WHEN** `web`'s Helmfile `diff` succeeds with no changes, and the pull request keeps `web`'s Lock because Helmfile acts without changes
- **THEN** the section ends with `Apply just this project: /turnip apply web`, `Re-plan it: /turnip diff web` and `Release its lock: /turnip unlock web`

#### Scenario: A plan whose counts were not reported
- **WHEN** `web`'s Helmfile `diff -q` succeeds, reads `succeeded`, and the pull request keeps `web`'s Lock
- **THEN** the section offers `Apply just this project: /turnip apply web`

#### Scenario: A clean plan whose Lock is released
- **WHEN** a plan for a tool that does not act without changes succeeds with no changes, and its Lock is released
- **THEN** the section offers only `Re-plan it`, with no apply and no unlock

#### Scenario: A successful apply
- **WHEN** `/turnip apply web` succeeds with changes
- **THEN** the section does not offer `Apply just this project`

#### Scenario: A failed plan
- **WHEN** `web`'s `diff` fails
- **THEN** the section does not offer `Apply just this project`
