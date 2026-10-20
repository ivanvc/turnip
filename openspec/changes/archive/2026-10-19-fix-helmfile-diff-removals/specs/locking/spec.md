## MODIFIED Requirements

### Requirement: The plan record
When a plan succeeds with something to apply, the Lock SHALL record, alongside the move to `plan_ready`: the arguments the plan ran with (as the Runner received them), the plan artifact the Runner returned (which may be empty, as for Helmfile), the plan's add, change and destroy counts, whether those counts were reported (see the `plugins` capability), and the sha256 digest of the image the plan's Pod ran. The digest SHALL be read from the plan's Pod when the result arrives; when it cannot be read it SHALL be recorded as empty and the plan still succeeds. No other event SHALL write the plan record. What a mutating command does with the recorded digest is the `tool-images` capability's.

#### Scenario: A scoped plan is recorded
- **WHEN** `/helmfile diff web -- -l name=api` succeeds with changes
- **THEN** the Lock records the arguments `-l name=api`, the counts and the image digest

#### Scenario: The digest cannot be read
- **WHEN** a plan succeeds but its Pod is gone before the digest is read
- **THEN** the Lock moves to `plan_ready` with an empty digest

#### Scenario: A plan whose counts were not reported
- **WHEN** `/helmfile diff web -- -q` succeeds, so its counts are not reported, and `/helmfile apply web` then replays it
- **THEN** the Lock records that the counts were not reported, and the apply's running check is titled `running the recorded plan, succeeded, -q`, never `no changes`
