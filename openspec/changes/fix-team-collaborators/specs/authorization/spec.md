## MODIFIED Requirements

### Requirement: Roles
turnip SHALL recognize three roles for a comment's author on the repository, decided by asking GitHub. An account SHALL be a collaborator when GitHub's collaborator answer lists it, or when GitHub's permission answer for it reports access the repository's visibility does not explain: on a private repository any level other than none, on a public or internal repository a level above read (triage, write, maintain or admin). Any other account SHALL be an outsider. A collaborator whose permission level is write, maintain or admin SHALL be a writer, and these three SHALL be treated identically. The permission answer SHALL be asked only when the collaborator answer does not list the account. A read level on a public repository SHALL NOT make an account a collaborator, since GitHub reports it for every account, and neither SHALL a read level on an internal repository, since every member of the enterprise holds it. Whether the repository is private SHALL be taken from the webhook: only a repository whose visibility is private is, and a payload that does not say so reads as public. The roles and the levels above SHALL NOT be configurable by any Server setting or `turnip.yaml` key.

#### Scenario: Organization member through default permission
- **WHEN** an organization grants its members a default read permission, GitHub therefore reports a member as a collaborator, and that member comments `/turnip diff web`
- **THEN** `web` is planned

#### Scenario: Public repository reader
- **WHEN** an account with only the read access every account has on a public repository comments `/turnip diff`
- **THEN** turnip treats it as an outsider

#### Scenario: Write access through a team
- **WHEN** an organization member has write access to a private repository through a team, GitHub's collaborator answer does not list them, its permission answer reports write, and they comment `/turnip diff web` and then `/helmfile apply web`
- **THEN** `web` is planned, and then applied: they are a writer

#### Scenario: Triage through a team on a public repository
- **WHEN** an account has triage access to a public repository through a team, the collaborator answer does not list it and the permission answer reports triage, and it comments `/turnip diff web`
- **THEN** `web` is planned

#### Scenario: Enterprise member reading an internal repository
- **WHEN** an enterprise member with no grant on an internal repository comments `/turnip diff web`, the collaborator answer does not list them and the permission answer reports read
- **THEN** turnip treats them as an outsider

#### Scenario: No access to a private repository
- **WHEN** an account with no access to a private repository comments `/turnip diff`, and the permission answer for it reports none
- **THEN** turnip treats it as an outsider

### Requirement: Fail closed
When GitHub cannot be asked whether an author is a collaborator, or what permission they hold (for example an installation lacking access to collaborator information, a rate limit, or GitHub being unreachable), turnip SHALL run nothing it would not run for an author without that role, and SHALL log the underlying error on the Server. Its reply SHALL say that turnip could not check the author's permission, never that the author lacks it. A permission level turnip does not recognize SHALL count as below write.

#### Scenario: Collaborator lookup fails
- **WHEN** the collaborator lookup for a comment's author returns an error
- **THEN** turnip replies `@<user>: turnip could not check your permission on this repository, so nothing was run. Comment again to retry.` and runs nothing

#### Scenario: Permission lookup fails for an author the collaborator answer leaves out
- **WHEN** the collaborator answer does not list a comment's author and the permission lookup returns an error
- **THEN** turnip replies with the same message and runs nothing

#### Scenario: Permission lookup fails for an apply
- **WHEN** a collaborator comments `/helmfile apply web` and the permission lookup returns an error
- **THEN** `web` is reported as `could not check write permission to run "apply"; comment again to retry` and nothing runs for it

#### Scenario: Permission lookup fails for an unlock
- **WHEN** a collaborator comments `/turnip unlock web` and the permission lookup returns an error
- **THEN** turnip replies `@<user>: turnip could not check your write permission, so nothing was unlocked. Comment again to retry.` and `web`'s Lock is kept
