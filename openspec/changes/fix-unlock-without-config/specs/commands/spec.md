## MODIFIED Requirements

### Requirement: Unlock command
The operation `unlock` SHALL be handled by turnip itself under any trigger word and SHALL never run a tool, create a Job, check run or Operation Record. It SHALL NOT read `turnip.yaml`: it SHALL find its Locks among this repository's Locks themselves, so it works whether the pull request's configuration is valid, invalid, missing or no longer declares the Project. The trigger word SHALL NOT narrow an unlock: `/<tool> unlock` SHALL act exactly as `/turnip unlock`, since a Lock does not record its Project's tool. Its selectors SHALL be exact Project names, compared as written; patterns and `*` SHALL NOT be interpreted. With no selector, it SHALL select every Lock this pull request holds; with selectors, the Lock of each named Project, whichever pull request holds it. Unlock requires write permission (see the `authorization` capability). turnip SHALL release each selected Lock held by this pull request (see the `locking` capability) and reply once per unlock command: `Unlocked:` followed by one `- <project>` line per released Lock, then `Not unlocked (held by a different PR):` followed by `- <project> (held by #<number>)` lines (the number omitted when the holder cannot be read). A named Project with no Lock SHALL go unmentioned, and when nothing is listed the reply SHALL be `No projects matched this unlock command.` When the Locks cannot be read, turnip SHALL release nothing for that command and reply `turnip could not read this pull request's locks, so nothing was unlocked. Comment again to retry.`

#### Scenario: Bare unlock
- **WHEN** a writer comments `/turnip unlock` and this pull request holds Locks on `web` and `api` only
- **THEN** both Locks are released
- **AND** turnip replies `Unlocked:` listing `web` and `api`, and no other Project

#### Scenario: Held elsewhere
- **WHEN** a writer comments `/turnip unlock web` and `web`'s Lock is held by pull request #12
- **THEN** the Lock is kept and the reply lists `web (held by #12)` under `Not unlocked (held by a different PR):`

#### Scenario: Nothing to unlock
- **WHEN** a writer comments `/turnip unlock web` and `web` has no Lock
- **THEN** turnip replies `No projects matched this unlock command.`

#### Scenario: Star is a name for unlock
- **WHEN** a writer comments `/turnip unlock *`
- **THEN** no Lock is released, since no Project can be named `*`, and turnip replies `No projects matched this unlock command.`

#### Scenario: Other pull requests' Locks are left out of a bare unlock
- **WHEN** a writer comments `/turnip unlock` on pull request #4, which holds `web`, while #12 holds `api`
- **THEN** `web` is released, and the reply does not mention `api`

#### Scenario: An invalid configuration
- **WHEN** a writer comments `/turnip unlock` on a pull request whose `turnip.yaml` names an image the Server does not allow
- **THEN** this pull request's Locks are released and turnip replies `Unlocked:` listing them, with no configuration reply

#### Scenario: A Project the configuration no longer declares
- **WHEN** this pull request holds a Lock on `web`, and its head commit has since renamed `web` to `frontend`, and a writer comments `/turnip unlock web`
- **THEN** `web`'s Lock is released

#### Scenario: A tool's trigger word
- **WHEN** a writer comments `/helmfile unlock` and this pull request holds Locks on `web` and `api`
- **THEN** both Locks are released, as `/turnip unlock` would

#### Scenario: The Locks cannot be read
- **WHEN** a writer comments `/turnip unlock` and reading this repository's Locks from Redis fails
- **THEN** no Lock is released and turnip replies `turnip could not read this pull request's locks, so nothing was unlocked. Comment again to retry.`
