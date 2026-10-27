## MODIFIED Requirements

### Requirement: Titles on completion
A successful Operation's check SHALL be titled with its count text, followed by `, <args>` when it ran with arguments. A failed Operation's check SHALL be titled by the step that failed, as the Runner reports it: `<tool> exited <code>` when the tool ran and exited non-zero, `clone failed`, `workspace could not be prepared`, `<tool> could not be started`, `<tool> stopped: no output for <idle timeout>` when the Runner stopped a silent tool, `<tool> stopped: the Runner was told to stop` when the Runner stopped the tool on being told to stop, or `failed` when the Runner named no step. turnip SHALL NOT interpret a tool's own failure beyond its exit code. An Operation whose Runner never started SHALL be completed as `failure`, titled with the diagnostic turnip found (worded by the `operation-lifecycle` capability), with the summary `The Runner never reported back within the start timeout.` An Operation whose Runner started and then stopped without reporting SHALL be completed as `failure`, titled with the diagnostic turnip found (worded by the `operation-lifecycle` capability), with the summary `The Runner stopped before reporting a result.`

#### Scenario: Tool exits non-zero
- **WHEN** `helmfile diff` exits 1
- **THEN** the check is completed as `failure` titled `helmfile exited 1`

#### Scenario: Successful scoped plan
- **WHEN** a plan run with `-l name=api` reports no changes
- **THEN** the check is titled `no changes, -l name=api`

#### Scenario: Runner never started
- **WHEN** an Operation's Pod stays stuck pulling its image past the start timeout
- **THEN** the check is completed as `failure` titled with a diagnostic such as `Job <name>: container stuck (ImagePullBackOff)`

#### Scenario: A tool stopped for silence
- **WHEN** a Helmfile `diff` is stopped after 30 minutes with no output
- **THEN** its check is completed as `failure`, titled `helmfile stopped: no output for 30m0s`

#### Scenario: A Runner that stopped without reporting
- **WHEN** a started Runner is killed at its deadline before reporting
- **THEN** its check is completed as `failure`, titled with the deadline diagnostic, with the summary `The Runner stopped before reporting a result.`
