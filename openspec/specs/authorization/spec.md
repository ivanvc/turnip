# authorization Specification

## Purpose
Defines who may trigger turnip by pull request comment and what each person may run, and the operator-set conditions a pull request must meet before any mutating command runs on it. Repository maintainers rely on it to know that plans, applies and unlocks are gated, and operators rely on it to require review before infrastructure changes.

## Requirements

### Requirement: Roles
turnip SHALL recognize three roles for a comment's author on the repository, decided by asking GitHub: an outsider is an account GitHub does not report as a collaborator; a collaborator is a collaborator whose permission level is below write (read, triage, or none); a writer is a collaborator whose permission level is write, maintain or admin. Write, maintain and admin SHALL be treated identically. Collaborator status SHALL be taken from GitHub's collaborator answer itself and never inferred from a permission level, so an account GitHub reports read access for but does not list as a collaborator is an outsider. The roles and the levels below SHALL NOT be configurable by any Server setting or `turnip.yaml` key.

#### Scenario: Organization member through default permission
- **WHEN** an organization grants its members a default read permission, GitHub therefore reports a member as a collaborator, and that member comments `/turnip diff web`
- **THEN** `web` is planned

#### Scenario: Public repository reader
- **WHEN** an account with only the read access every account has on a public repository comments `/turnip diff`
- **THEN** turnip treats it as an outsider

### Requirement: Who may trigger what
Every comment that carries a trigger line or a malformed trigger line (see the `commands` capability) SHALL require its author to be a collaborator or writer. An outsider's comment SHALL be answered only with `@<user> does not have permission to trigger operations on this repository.` and nothing else SHALL happen: this check SHALL precede reading the pull request, the fork and closed refusals (see the `pull-request-lifecycle` capability) and reading `turnip.yaml`. A plan command SHALL be allowed to collaborators and writers. A mutating command SHALL be allowed only to writers: for each selected Project whose operation is mutating, a non-writer's request SHALL be rejected without touching its Lock, check run or Job, reported in the results comment as `write permission is required to run "<operation>"`. `unlock` SHALL be allowed only to writers; a non-writer SHALL get the reply `@<user> does not have write permission required to unlock.` and no Lock SHALL be released. The unlock permission check SHALL follow resolving its Project names, so an unknown name is answered as not found first.

#### Scenario: Outsider asks for a plan
- **WHEN** an outsider comments `/turnip diff web`
- **THEN** turnip replies `@<user> does not have permission to trigger operations on this repository.` and runs nothing

#### Scenario: Collaborator plans
- **WHEN** a collaborator with triage permission comments `/helmfile diff web`
- **THEN** `web` is planned

#### Scenario: Collaborator asks for an apply
- **WHEN** a collaborator with read permission comments `/helmfile apply web`
- **THEN** nothing runs for `web`
- **AND** the results comment reports `write permission is required to run "apply"` for `web`

#### Scenario: Collaborator asks for an unlock
- **WHEN** a collaborator with read permission comments `/turnip unlock web`
- **THEN** turnip replies `@<user> does not have write permission required to unlock.` and `web`'s Lock is kept

### Requirement: Fail closed
When GitHub cannot be asked whether an author is a collaborator, or what permission they hold (for example an installation lacking access to collaborator information), turnip SHALL refuse exactly as it would for an author without that role, and SHALL log the underlying error on the Server. A permission level turnip does not recognize SHALL count as below write.

#### Scenario: Collaborator lookup fails
- **WHEN** the collaborator lookup for a comment's author returns an error
- **THEN** turnip replies `@<user> does not have permission to trigger operations on this repository.` and runs nothing

#### Scenario: Permission lookup fails for an apply
- **WHEN** a collaborator comments `/helmfile apply web` and the permission lookup returns an error
- **THEN** `web` is reported as `write permission is required to run "apply"` and nothing runs for it

### Requirement: Automatic plans are not authorized
A plan turnip starts by itself from a pull request event (see the `pull-request-lifecycle` capability) SHALL run with no authorization check, since it has no comment author; pushing to the repository already required access, and pull requests from forks are refused.

#### Scenario: Push by any author
- **WHEN** a commit is pushed to an open, non-draft pull request from a branch of the same repository
- **THEN** the affected Projects are planned without any collaborator or permission lookup

### Requirement: Permission answers per comment
Within the handling of one comment, turnip SHALL ask GitHub for each account's collaborator status and permission level at most once each, reusing an answer for at most 5 minutes. A later comment SHALL ask GitHub again, so a change to an account's access takes effect on that account's next comment.

#### Scenario: Several mutating Projects
- **WHEN** a writer comments `/turnip apply web api`
- **THEN** the author's permission level is read from GitHub once for that comment

#### Scenario: Access revoked between comments
- **WHEN** a writer's write access is removed and they then comment `/helmfile apply web`
- **THEN** the new comment is judged on the reduced access

### Requirement: Mutation requirements setting
The Server setting `TURNIP_MUTATION_REQUIREMENTS` SHALL be a comma-separated list of conditions every mutating command's pull request must meet, each one of `approved` or `mergeable`. Whitespace around each name SHALL be trimmed, empty entries skipped and repeated names counted once. An unrecognized name SHALL stop the Server from starting with the error `unknown mutation requirement "<name>"; recognized requirements are approved, mergeable`. Unset or blank SHALL require nothing and ask GitHub nothing. The setting SHALL be read only from the Server's environment: no `turnip.yaml` field and no `TURNIP_ALLOWED_OVERRIDES` path can set or relax it, and it applies to every repository the Server serves.

#### Scenario: Both required
- **WHEN** the Server starts with `TURNIP_MUTATION_REQUIREMENTS= approved, mergeable,approved`
- **THEN** both `approved` and `mergeable` are required

#### Scenario: Misspelled requirement
- **WHEN** the Server starts with `TURNIP_MUTATION_REQUIREMENTS=aproved`
- **THEN** the Server refuses to start, naming `aproved` and the recognized requirements

### Requirement: Mutation requirement gate
When the requirement set is non-empty, turnip SHALL evaluate it for a command only after the write-permission check and only when at least one of the command's selected Projects is to run a mutating operation, and at most once per comment. When any requirement is unmet, turnip SHALL withhold every mutating Project of that command (no Lock change, no check run, no Job and no row in the results comment) and SHALL reply once for the command: `` `<command>` was not run. This pull request must first: `` followed by one `- ` line per unmet requirement, in the order `approved`, `mergeable` regardless of the order configured. `<command>` SHALL be the trigger word, operation and selectors as written, without extra arguments. Plan operations, including plan Projects in the same `/turnip` command, and `unlock` SHALL never be gated.

The `approved` line SHALL read `be approved by someone with write access other than its author`, and is met when some account other than the pull request's author (compared case-insensitively) and holding write permission has a latest standing review of APPROVED, where an account's standing is its most recent APPROVED, CHANGES_REQUESTED or DISMISSED review and COMMENTED or PENDING reviews leave it unchanged. The commit a review was given on SHALL NOT be considered. The `mergeable` line SHALL read `have no merge conflicts`, and is met when GitHub reports the pull request mergeable; it reflects merge conflicts only, not status checks or branch protection. When GitHub has not yet computed mergeability, turnip SHALL read the pull request again up to 3 times, 1 second apart. Each line SHALL carry a suffix for its reason: ` (GitHub reports one)` for a conflict, ` (GitHub has not finished checking; try again in a moment)` when mergeability is still unknown, and ` (turnip could not check this; try again)` when listing reviews, checking every qualifying approver's permission or reading the pull request failed. A failure SHALL leave the requirement unmet.

#### Scenario: Unapproved apply
- **WHEN** `TURNIP_MUTATION_REQUIREMENTS=approved` and a writer comments `/helmfile apply web` on a pull request with no qualifying approval
- **THEN** turnip replies `` `/helmfile apply web` was not run. This pull request must first: `` followed by `- be approved by someone with write access other than its author`
- **AND** `web`'s Lock, check runs and Jobs are untouched

#### Scenario: Self-approval
- **WHEN** the only APPROVED review comes from the pull request's author
- **THEN** `approved` is unmet

#### Scenario: Approval then changes requested
- **WHEN** a writer approved and later requested changes on the same pull request
- **THEN** that writer's approval no longer counts

#### Scenario: Approval by a reader
- **WHEN** the only approving review comes from an account with read permission
- **THEN** `approved` is unmet

#### Scenario: Conflict
- **WHEN** `TURNIP_MUTATION_REQUIREMENTS=mergeable` and GitHub reports a merge conflict
- **THEN** the reply lists `- have no merge conflicts (GitHub reports one)`

#### Scenario: Mergeability still computing
- **WHEN** GitHub has not computed mergeability after turnip's three further reads
- **THEN** the reply lists `- have no merge conflicts (GitHub has not finished checking; try again in a moment)`

#### Scenario: Plan in the same comment
- **WHEN** requirements are unmet and a writer comments `/helmfile diff api` on line 1 and `/helmfile apply web` on line 2
- **THEN** `api` is planned and only the apply is withheld

#### Scenario: Non-writer never reaches the gate
- **WHEN** requirements are unmet and a collaborator with read permission comments `/helmfile apply web`
- **THEN** the results comment reports `write permission is required to run "apply"` and no requirement reply is posted
