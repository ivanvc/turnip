## MODIFIED Requirements

### Requirement: Guard order
On the automatic path the Server SHALL apply, in order: the event's action, the draft guard, the fork refusal, then the configuration fetch. On the comment path it SHALL apply, in order: a comment that was just created and contains a trigger (see the `commands` capability), the commenter's collaborator check (see the `authorization` capability), the fork refusal, the closed-pull-request refusal, the configuration fetch for every command other than `unlock` (which never reads the configuration; see the `commands` capability), then each command's own checks. A guard that refuses SHALL stop every later step.

#### Scenario: An outsider comments on a fork pull request
- **WHEN** someone who is not a collaborator comments `/turnip diff` on a fork's pull request
- **THEN** turnip replies that they lack permission, since the collaborator check comes before the fork refusal

#### Scenario: A draft from a fork is opened
- **WHEN** a draft pull request from a fork is opened
- **THEN** nothing is logged as a fork refusal, since the draft guard returns first

#### Scenario: Unlock on a closed pull request
- **WHEN** a writer comments `/turnip unlock` on a merged pull request
- **THEN** turnip replies with the closed-pull-request message and releases nothing, since closing already released the pull request's Locks

#### Scenario: Unlock on a fork pull request
- **WHEN** a writer comments `/turnip unlock` on a pull request from a fork
- **THEN** no reply is posted and nothing is released, since the fork refusal applies to `unlock` too
