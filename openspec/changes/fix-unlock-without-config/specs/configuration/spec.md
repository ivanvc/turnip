## MODIFIED Requirements

### Requirement: Replies for a missing, invalid or unreadable configuration
When the configuration cannot be used, turnip SHALL run no Operation for the event. On the automatic path (a pull request opened, pushed to, reopened or marked ready) a missing configuration SHALL be silent, while an invalid one or a failed fetch SHALL post a pull request comment and mark the commit's configuration invalid for the `turnip` check (see the `merge-check` capability). On the comment path all three SHALL post a reply comment, once per comment. The comment path SHALL read the configuration only for commands other than `unlock`, which never reads it (see the `commands` capability): a comment whose commands are all `unlock` SHALL NOT fetch the configuration and SHALL get none of these replies, and in a comment that also holds other commands, the reply SHALL answer those other commands while each `unlock` still runs. The replies SHALL read:

| Situation | Comment |
|---|---|
| missing | a `[!WARNING]` alert: "No turnip configuration was found in this repository (checked `.turnip/config.yaml` or `turnip.yaml`)." |
| invalid | a `[!WARNING]` alert: "The turnip configuration is invalid, so no operations ran.", followed outside the alert by a code fence listing every reported problem |
| fetch failed | a `[!CAUTION]` alert: "Fetching the turnip configuration failed, so no operations ran. This is usually an authentication, permission, or rate-limit problem with turnip's GitHub App rather than something wrong with this repository.", followed by a collapsed "Error" section holding the error |

Known issue: an invalid `turnip.yaml` found on the comment path posts the reply but does not mark the `turnip` check invalid; only the automatic path does. Fix proposed in change `fix-invalid-config-check-on-comment`.

#### Scenario: Repository that never onboarded stays quiet
- **WHEN** a pull request is opened in a repository with no configuration at either path
- **THEN** turnip SHALL post no comment and run nothing

#### Scenario: Explicit command in a repository without configuration
- **WHEN** a collaborator comments a trigger command other than `unlock` on a pull request whose head commit has no configuration
- **THEN** turnip SHALL reply with the missing-configuration warning naming both accepted paths

#### Scenario: Invalid file on the automatic path
- **WHEN** a push leaves `turnip.yaml` invalid
- **THEN** turnip SHALL post the invalid-configuration warning with every problem in a code fence
- **AND** the `turnip` check for that commit SHALL report an invalid configuration

#### Scenario: Invalid file on the comment path
- **WHEN** a trigger comment other than `unlock` is posted on a pull request whose `turnip.yaml` is invalid
- **THEN** turnip SHALL reply with the invalid-configuration warning
- **AND** the `turnip` check SHALL be left as it was

#### Scenario: GitHub refuses the fetch
- **WHEN** reading the configuration fails for a reason other than the file not existing (authentication, permission, rate limit)
- **THEN** turnip SHALL post the caution comment with the error in a collapsed section and run nothing

#### Scenario: Unlock alone with an invalid file
- **WHEN** a writer comments only `/turnip unlock` on a pull request whose `turnip.yaml` is invalid
- **THEN** turnip SHALL NOT fetch the configuration, SHALL post no invalid-configuration warning, and SHALL answer with the unlock's own reply

#### Scenario: Unlock beside another command with an invalid file
- **WHEN** a writer comments `/turnip unlock` and `/turnip diff` on a pull request whose `turnip.yaml` is invalid
- **THEN** the unlock runs and gets its own reply, the invalid-configuration warning is posted once for the diff, and no Operation runs
