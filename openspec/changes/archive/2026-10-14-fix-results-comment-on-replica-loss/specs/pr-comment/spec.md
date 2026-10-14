## MODIFIED Requirements

### Requirement: One results comment per trigger, split by kind
Once every Operation started by one trigger (an automatic plan, or one trigger comment with all its commands) has finished, turnip SHALL post the results of all its Projects together, including Projects refused before or during dispatch, as fresh pull request comments. Results whose Operation is its tool's plan operation (for Helmfile, `diff`) SHALL form the plan comment; every other Operation's results, and any result whose tool is unknown, SHALL form a separate apply comment. When a trigger has both, the plan comment SHALL be posted first. A trigger that produced no Project result (only replies, or nothing at all) SHALL post no results comment. Replies to malformed lines, unlocks and selection errors are separate comments (see the `commands` capability).

The results comment SHALL be posted once per trigger by whichever Server replica finishes the trigger (see the `operation-lifecycle` capability), so it SHALL be posted even when the replica that received the trigger is gone.

#### Scenario: Plan and apply in one comment
- **WHEN** a single trigger comment contains `/turnip diff web` and `/turnip apply api`
- **THEN** turnip posts one plan comment carrying `web`'s result and, after it, one apply comment carrying `api`'s result

#### Scenario: Refused Projects still reported
- **WHEN** a trigger comment asks for `apply` on a Project and the author lacks write permission, alongside a `diff` that runs
- **THEN** the refused Project appears in the apply comment as a failed section whose output is the refusal reason

#### Scenario: Automatic plan
- **WHEN** the automatic plan for a pushed commit finishes for every matched Project
- **THEN** turnip posts one plan comment covering all of them

#### Scenario: The receiving replica restarts
- **WHEN** the replica that received `/turnip apply web` restarts while the apply runs
- **THEN** the apply comment for `web` is still posted, once
