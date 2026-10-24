## MODIFIED Requirements

### Requirement: Footer
The last comment body SHALL end with a footer only when at least one Project in the comment is still locked by this pull request. The footer SHALL read `This pull request holds locks on `a`, `b` until applied or released.`, followed, when any locked Project's Operation ran with arguments, by `Applying replays the scope each plan recorded, which may be narrower than the whole project.`, and then `` `/turnip apply` or `/turnip unlock` ``. The footer SHALL never print a command carrying arguments.

#### Scenario: Nothing locked
- **WHEN** every Project in the comment released or never held its Lock
- **THEN** the comment has no footer

#### Scenario: Scoped plan holding a Lock
- **WHEN** `web`'s plan ran with `-l name=api` and holds its Lock
- **THEN** the footer names `web`, carries the replay sentence, and ends with `` `/turnip apply` or `/turnip unlock` ``
