# commands Specification

## Purpose
Defines the trigger comments people post on a pull request to ask turnip to plan, apply or unlock Projects: the line grammar, which Projects a command selects, how extra arguments are handled, and the replies a command receives. End users rely on it to drive turnip from pull request comments.

## Requirements

### Requirement: Trigger comments
turnip SHALL consider only newly created comments on pull requests; edited or deleted comments and comments on plain issues SHALL be ignored. Each line of a comment SHALL be examined separately after trimming surrounding whitespace, and a line SHALL be a trigger line only when its first whitespace-separated token is `/` followed by a trigger word: `turnip`, or the name of a tool this Server has a Plugin for (today `helmfile`). Trigger words SHALL be matched case-sensitively. A line whose first token is any other `/word`, or a bare `/`, SHALL be ignored silently: no operation and no reply. A comment with no trigger line and no malformed trigger line SHALL produce no reply and no authorization lookup.

#### Scenario: One comment, several triggers
- **WHEN** a comment contains the lines `/turnip diff web` and `/helmfile apply api`
- **THEN** both lines are parsed as commands

#### Scenario: Another bot's command
- **WHEN** a comment's only slash line is `/jira PROJ-1` or `/terraform plan`
- **THEN** turnip posts nothing and runs nothing

#### Scenario: Comment edited
- **WHEN** a comment containing `/turnip diff` is edited
- **THEN** turnip does nothing for the edit

#### Scenario: Plain issue
- **WHEN** `/turnip diff` is commented on an issue that is not a pull request
- **THEN** turnip does nothing

### Requirement: Malformed trigger lines
A trigger line consisting only of its trigger word, with no operation, SHALL be malformed. turnip SHALL report every malformed line of a comment in one reply reading `The following line(s) looked like a trigger command but couldn't be parsed:` followed by one bullet per line of the form ``- line <n>: `<line>` ``, where `<n>` is the 1-based line number in the comment. Well-formed lines in the same comment SHALL still be processed.

#### Scenario: Malformed line beside a valid one
- **WHEN** a comment's line 1 is `/turnip` and line 2 is `/turnip diff web`
- **THEN** turnip replies that line 1 (`/turnip`) couldn't be parsed
- **AND** still plans `web`

### Requirement: Command grammar
A trigger line SHALL be read as `/<trigger word> <operation> [selector...] [arguments...]`. The second token is the operation, taken verbatim. Following tokens are selectors until the first token beginning with `-`. If that token is exactly `--` it SHALL be consumed as a delimiter and everything after it becomes the extra arguments; otherwise that token and everything after it become the extra arguments. Any later `--` SHALL be passed through as an ordinary argument. `/turnip` SHALL consider every Project; `/<tool>` SHALL consider only Projects whose `uses:` resolves to that tool.

#### Scenario: Arguments without a delimiter
- **WHEN** a collaborator comments `/helmfile diff web -l name=ingress`
- **THEN** the selector is `web` and the extra arguments are `-l name=ingress`

#### Scenario: Explicit delimiter
- **WHEN** a collaborator comments `/turnip diff web -- --context=3`
- **THEN** the selector is `web` and the extra arguments are `--context=3`

#### Scenario: Tool scoping
- **WHEN** a collaborator comments `/helmfile diff web` and `web` is a Project that does not use Helmfile
- **THEN** turnip replies `Project "web" not found in turnip.yaml.` and runs nothing for that command

### Requirement: Operation recognition
For each selected Project, the operation SHALL be one its tool offers (see the `plugins` capability). A Project whose tool does not offer the operation SHALL be rejected without running, reported in the results comment as `operation "<operation>" is not recognized for tool "<tool>"`, with no check run created for it. A command whose operation is the selected Project's plan operation is a plan command; any other recognized operation is a mutating command, decided per Project because operation names are tool-native.

#### Scenario: Unknown operation
- **WHEN** a collaborator comments `/turnip plan web` and `web` uses Helmfile
- **THEN** the results comment reports `operation "plan" is not recognized for tool "helmfile"` for `web`

### Requirement: Selectors
A selector token equal to `*` SHALL select every Project the trigger word considers. A selector containing `*` anywhere else SHALL be a pattern, matched against Project names (not directories) with the same doublestar glob semantics as `whenModified`, where `*` stays within one `/` segment and `**` crosses segments. Any other selector SHALL be an exact Project name. `*` SHALL NOT be combined with other selectors. Names and patterns SHALL select the union of their matches, each Project at most once, in configuration order. A name that matches no considered Project SHALL fail the whole command; a pattern that matches nothing SHALL be reported as a notice while the rest of the command runs.

#### Scenario: Pattern over names
- **WHEN** Projects are named `gcp/web`, `gcp/team/api` and `aws/web`, and a collaborator comments `/turnip diff gcp/*`
- **THEN** only `gcp/web` is planned
- **AND** `/turnip diff gcp/**` plans `gcp/web` and `gcp/team/api`

#### Scenario: Every Project
- **WHEN** a collaborator comments `/turnip diff *`
- **THEN** every Project is planned regardless of which files the pull request changed

#### Scenario: Star mixed with a name
- **WHEN** a collaborator comments `/turnip diff * web`
- **THEN** turnip replies `` `*` cannot be combined with other selectors (got web): a trigger either names projects or asks for all of them. `` and runs nothing for that command

#### Scenario: Unknown name
- **WHEN** a collaborator comments `/turnip diff web typo`
- **THEN** turnip replies `Project "typo" not found in turnip.yaml.` and runs nothing for that command, `web` included

#### Scenario: Pattern matching nothing
- **WHEN** a collaborator comments `/turnip diff web gpc/*` and no Project name matches `gpc/*`
- **THEN** `web` is planned
- **AND** the results comment carries the notice `` No project matched `gpc/*`. ``

### Requirement: Bare command defaults
A command with no selector SHALL choose Projects per candidate by what the operation means for that Project's tool. For a Project where the operation is the plan, it SHALL select the Projects whose `whenModified` patterns match the pull request's changed files, using the same matching as the automatic plan (see the `configuration` capability). For a Project where the operation is mutating, it SHALL select the Projects whose Lock is held by this pull request in state plan_ready (see the `locking` capability). A Project whose tool does not offer the operation SHALL be kept so it is rejected by name. When nothing is selected, turnip SHALL send a single reply instead of one refusal per Project: `` No project matched the files this pull request changes. Run `/<trigger word> <operation> *` to target every project.`` when any candidate took the plan meaning, otherwise `` No project has a plan from this pull request to apply. Plan first, or name a project explicitly with `/<trigger word> <operation> <project>`.``

#### Scenario: Bare plan
- **WHEN** a collaborator comments `/turnip diff` on a pull request that changed only files matching `web`'s `whenModified`
- **THEN** only `web` is planned

#### Scenario: Bare apply
- **WHEN** a writer comments `/turnip apply` and only `web` holds a plan_ready Lock for this pull request
- **THEN** only `web` is applied

#### Scenario: Nothing to apply
- **WHEN** a writer comments `/helmfile apply` and no Helmfile Project has a plan_ready Lock for this pull request
- **THEN** turnip replies `` No project has a plan from this pull request to apply. Plan first, or name a project explicitly with `/helmfile apply <project>`. `` and runs nothing

#### Scenario: Bare plan matching nothing
- **WHEN** a collaborator comments `/turnip diff` and no Project's `whenModified` matches the changed files
- **THEN** turnip replies `` No project matched the files this pull request changes. Run `/turnip diff *` to target every project. ``

### Requirement: Large pull request notice
When a bare plan command reads the pull request's changed files and GitHub returns 3000 or more, turnip SHALL add the notice `` This pull request changes more files than GitHub will list (3000), so the projects matched below may be incomplete. Run `/<trigger word> <operation> *` to target every project. `` The notice SHALL ride at the top of the results comment, or be posted as a reply of its own when the comment ran nothing.

#### Scenario: Truncated listing
- **WHEN** a collaborator comments `/turnip diff` on a pull request whose changed-file listing holds 3000 entries
- **THEN** the results comment opens with the notice that the matched projects may be incomplete

### Requirement: Extra arguments only on plans
Extra arguments SHALL be passed verbatim to the tool, uninterpreted, and only by a plan operation. A mutating operation given extra arguments SHALL be refused for each selected Project before any Lock, check run or Job is touched, with the reason `"<operation>" does not accept arguments (got <arguments>); it replays the scope the plan recorded. Pass arguments to "<plan operation>" instead.` reported in the results comment. A mutating operation SHALL instead run with the arguments its plan recorded (see the `locking` capability).

#### Scenario: Arguments on apply
- **WHEN** a writer comments `/helmfile apply web -l name=ingress`
- **THEN** nothing runs for `web`
- **AND** the results comment reports `"apply" does not accept arguments (got -l name=ingress); it replays the scope the plan recorded. Pass arguments to "diff" instead.`

#### Scenario: Scoped plan then apply
- **WHEN** `/helmfile diff web -l name=ingress` succeeded and a writer then comments `/helmfile apply web`
- **THEN** the apply runs with `-l name=ingress`

### Requirement: Command ordering and replies
Commands in one comment SHALL run in the order written, each command's Projects finishing before the next command's begin, while the Projects within one command SHALL run concurrently. Replies (the malformed-line reply, then each command's own replies such as selection errors, mutation-requirement refusals and unlock results, in command order) SHALL be posted as separate comments before the operations' results, and all Projects' results from the whole comment SHALL be posted together as one results comment (see the `pr-comment` capability) once every command has finished. When reading the changed-file listing or a Lock fails while selecting Projects, turnip SHALL post nothing for the comment and run nothing.

#### Scenario: Plan then apply in one comment
- **WHEN** a writer comments `/helmfile diff web` on line 1 and `/helmfile apply web` on line 2
- **THEN** the diff finishes before the apply starts
- **AND** one results comment reports both

### Requirement: Unlock command
The operation `unlock` SHALL be handled by turnip itself under any trigger word and SHALL never run a tool, create a Job, check run or Operation Record. Its selectors SHALL be exact Project names among the Projects the trigger word considers; with none, every considered Project is a candidate. Patterns and `*` SHALL NOT be interpreted. An unknown name SHALL be answered `Project "<name>" not found in turnip.yaml.` Unlock requires write permission (see the `authorization` capability). turnip SHALL release each candidate's Lock held by this pull request (see the `locking` capability) and reply once per unlock command: `Unlocked:` followed by one `- <project>` line per released Lock, then `Not unlocked (held by a different PR):` followed by `- <project> (held by #<number>)` lines (the number omitted when the holder cannot be read). Candidates with no Lock SHALL go unmentioned, and when nothing is listed the reply SHALL be `No Projects matched this unlock command.`

#### Scenario: Bare unlock
- **WHEN** a writer comments `/turnip unlock` and this pull request holds Locks on `web` and `api` only
- **THEN** both Locks are released
- **AND** turnip replies `Unlocked:` listing `web` and `api`, and no other Project

#### Scenario: Held elsewhere
- **WHEN** a writer comments `/turnip unlock web` and `web`'s Lock is held by pull request #12
- **THEN** the Lock is kept and the reply lists `web (held by #12)` under `Not unlocked (held by a different PR):`

#### Scenario: Nothing to unlock
- **WHEN** a writer comments `/turnip unlock web` and `web` has no Lock
- **THEN** turnip replies `No Projects matched this unlock command.`

#### Scenario: Star is a name for unlock
- **WHEN** a writer comments `/turnip unlock *`
- **THEN** turnip replies `Project "*" not found in turnip.yaml.`
