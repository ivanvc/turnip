## ADDED Requirements

### Requirement: Idle timeout
The Runner SHALL stop the tool once neither of the tool's streams has written a line for the idle timeout: `TURNIP_RUNNER_IDLE_TIMEOUT` on the Server, 30 minutes by default (see the `server` capability), passed to the Runner (see the `runner-job` capability). Only the tool's own lines SHALL reset the clock; turnip's annotation lines (see the `plugins` capability) SHALL NOT. The Runner SHALL stop the tool as "Stopping the tool" requires and then report a failed result with category timed out (see Failure categories) and the error message `The tool wrote nothing for <idle timeout>, so turnip stopped it.` That result SHALL take the ordinary path: the Project's check fails, the result's event moves the Lock (a stopped mutating Operation leaves its plan stale, as any failed mutating Operation does; see the `locking` capability), and the trigger's results comment shows the output and the message.

#### Scenario: A tool silent past the idle timeout
- **WHEN** a `diff` prints a line and then writes nothing for 30 minutes
- **THEN** the Runner interrupts the tool and reports a failure with category timed out
- **AND** the Project's output ends with `The tool wrote nothing for 30m0s, so turnip stopped it.`

#### Scenario: A slow tool that keeps writing
- **WHEN** a tool writes a progress line every 10 seconds for 2 hours
- **THEN** the idle timeout never stops it

#### Scenario: A stopped apply
- **WHEN** an `apply` is stopped by the idle timeout
- **THEN** its check fails, the Lock stays held, and the stored plan is no longer valid, as for any failed mutating Operation

### Requirement: Stopping the tool
Whenever the Runner stops the tool, it SHALL first send the tool an interrupt (`SIGINT`), wait up to 30 seconds for it to exit, and only then kill it, so a tool that cleans up on interrupt (releasing a state lock, for example) can. The Runner SHALL stop the tool this way for the idle timeout, and when the Runner itself receives `SIGTERM` (as the kubelet sends at the Job's deadline, on an eviction, or when the Pod is deleted). After stopping the tool for a `SIGTERM`, the Runner SHALL report a failed result with category stopped (see Failure categories) and the error message `The Runner was told to stop (its deadline, an eviction or a deletion), so turnip stopped the tool.` The Pod's grace period (see the `runner-job` capability) SHALL leave time to do so; a Runner that cannot report is finalized by the sweep (see Start timeout and sweep).

#### Scenario: A tool that exits on interrupt
- **WHEN** the Runner stops a tool that exits within 2 seconds of an interrupt
- **THEN** the tool is not killed, and its exit is recorded in the transcript

#### Scenario: A tool that ignores the interrupt
- **WHEN** the Runner stops a tool that keeps running after the interrupt
- **THEN** the tool is killed 30 seconds after the interrupt

#### Scenario: The Job's deadline is reached
- **WHEN** a Runner still running its tool reaches the Job's deadline
- **THEN** the Runner interrupts the tool and reports a failure with category stopped before its Pod is killed
- **AND** the Project's output ends with `The Runner was told to stop (its deadline, an eviction or a deletion), so turnip stopped the tool.`

## MODIFIED Requirements

### Requirement: Failure categories
A Runner's result SHALL be successful exactly when the tool exited 0. A failed result SHALL carry one failure category, which the Project's check title reflects (see the `project-checks` capability):

| Category | When | Exit code |
|---|---|---|
| tool exited | the tool ran and exited non-zero | the tool's |
| clone failed | the checkout failed (see the `checkout` capability) | -1 |
| workspace failed | the Runner had no workspace and could not create a temporary one | -1 |
| tool not started | the tool could not be run, for example its binary is missing or the operation is not one it supports | -1 |
| timed out | the tool wrote nothing for the idle timeout and the Runner stopped it (see Idle timeout) | the tool's, or -1 when it was killed |
| stopped | the Runner was told to stop and stopped the tool (see Stopping the tool) | the tool's, or -1 when it was killed |
| unspecified | a result carried no category | as sent |

When a failed result carries an error message, the output shown for the Project SHALL be the tool's output followed by a blank line and the error message.

#### Scenario: A tool error
- **WHEN** Helmfile exits 1
- **THEN** the result is a failure with category tool exited and exit code 1

#### Scenario: The tool binary is missing
- **WHEN** the tool's binary cannot be found on the Runner's `PATH`
- **THEN** the result is a failure with category tool not started and exit code -1

#### Scenario: A tool the Runner does not know
- **WHEN** `TURNIP_TOOL` names no registered tool
- **THEN** the Runner exits 1 without connecting, and the Operation is finalized by the start timeout

#### Scenario: A tool stopped for silence
- **WHEN** the Runner stops a tool for the idle timeout and the tool exits 130 on the interrupt
- **THEN** the result is a failure with category timed out and exit code 130

### Requirement: Start timeout and sweep
Every Server replica SHALL sweep the Operation Records every 30 seconds and time out each Operation that has not been marked started by its start deadline (5 minutes after dispatch). Timing out SHALL complete the Project's check as a failure whose title is a diagnostic read from Kubernetes and whose summary is `The Runner never reported back within the start timeout.`, apply the plan or mutating timeout to the Lock (see the `locking` capability), record the outcome (see the `merge-check` capability), delete the record, and add the diagnostic to its trigger as the Project's output (see Trigger progress). The diagnostic SHALL be the first that applies:

| Kubernetes state | Diagnostic |
|---|---|
| no Job name recorded, the Job is gone, or its status cannot be read | `No Job/Pod found for this operation; it may have been deleted or never successfully scheduled` |
| a mutating Operation's tool container cannot pull its image | the recorded-digest diagnostic (see the `tool-images` capability) |
| a container is waiting with a reason (init containers first) | `Job <name>: container stuck (<reason>)` |
| a Pod exists | `Job <name>: Pod still <phase> after 5 minutes` |
| no Pod yet | `Job <name>: no Pod found yet after 5 minutes` |

When the Operation's Project lists keys of the repository's Secret (see the `runner-access` capability) and the diagnostic was read from a Pod still `Pending`, the Project's output in the results comment SHALL be the diagnostic followed by a line reading `This project mounts keys <keys> of Secret <secret> in namespace <namespace>; if that Secret or one of those keys is missing, the Pod cannot start.`, with the keys joined by `, ` in the order listed. The check's title SHALL remain the diagnostic alone.

Once an Operation is marked started, the sweep SHALL NOT time it out for its start deadline. Instead, the sweep SHALL finalize as stopped a started Operation whose Runner ended without reporting a result: its Job has a `Failed` or `Complete` condition and its Pod is neither `Pending` nor `Running`, or its Job or Pod is gone. A Job that has failed while its Pod is still terminating is left for a later sweep, so a Runner inside its grace period can still report. Finalizing it SHALL complete the Project's check as a failure whose title is a diagnostic read from Kubernetes and whose summary is `The Runner stopped before reporting a result.`, apply the same plan or mutating timeout to the Lock, record the outcome, delete the record, and add the diagnostic to its trigger as the Project's output. The diagnostic SHALL be the first that applies:

| Kubernetes state | Diagnostic |
|---|---|
| the Job is gone, or its status cannot be read | `No Job/Pod found for this operation; it may have been deleted or never successfully scheduled` |
| the Job's `Failed` condition, or its `FailureTarget` condition, has reason `DeadlineExceeded` | `Job <name>: stopped at its deadline of <deadline>` |
| the Pod is gone | `No Job/Pod found for this operation; it may have been deleted or never successfully scheduled` |
| a container terminated with a reason (init containers first) | `Job <name>: container <container> terminated (<reason>)` |
| the Pod was evicted | `Job <name>: Pod evicted` |
| otherwise | `Job <name>: finished without reporting a result` |

The Job's deadline (see the `runner-job` capability) bounds every started Operation, so this finalization replaces waiting for abandonment. Abandonment stays as the last resort for a record the sweep cannot otherwise resolve: 24 hours after dispatch the sweep SHALL abandon a started Operation still unfinalized: the check stays in progress, the Lock stays as it was, the record is deleted, and its trigger receives `No result was received from the Runner within 24 hours.` as the Project's output.

#### Scenario: An unschedulable Pod
- **WHEN** a Runner Pod stays `Pending` with no container waiting for 5 minutes
- **THEN** within 30 more seconds the check fails with title `Job <name>: Pod still Pending after 5 minutes`

#### Scenario: An image that cannot be pulled
- **WHEN** a plan's tool container is waiting with reason `ImagePullBackOff` at the start deadline
- **THEN** the check fails with title `Job <name>: container stuck (ImagePullBackOff)`

#### Scenario: A Runner that crashed before reporting
- **WHEN** the Runner Pod failed without ever reaching the Server
- **THEN** at the start deadline the check fails with title `Job <name>: Pod still Failed after 5 minutes`

#### Scenario: A started Operation that hangs
- **WHEN** a tool prints a line and then hangs forever
- **THEN** 30 minutes later the Runner stops it for the idle timeout, and its check fails with that message (see Idle timeout)

#### Scenario: A Runner killed at its deadline without reporting
- **WHEN** a started Runner is killed at the Job's deadline of 4 hours before it can report
- **THEN** within 30 seconds of its Job failing, the sweep fails the check titled `Job <name>: stopped at its deadline of 4h0m0s`, summary `The Runner stopped before reporting a result.`

#### Scenario: A Runner still inside its grace period
- **WHEN** a started Runner's Job has a `Failed` condition while its Pod is still `Running`
- **THEN** the sweep leaves the Operation alone, and the Runner's own stopped result finalizes it if it arrives

#### Scenario: An out-of-memory Runner
- **WHEN** a started Runner's `runner` container is killed with reason `OOMKilled`
- **THEN** the sweep fails the check titled `Job <name>: container runner terminated (OOMKilled)`

#### Scenario: A missing repository Secret
- **WHEN** Project `lab` of repository id `123456789` lists `lab-kubeconfig`, no Secret `turnip-repository-123456789` exists in namespace `turnip-runners`, and the Pod is still `Pending` at the start deadline
- **THEN** the check fails titled with the diagnostic alone
- **AND** `lab`'s output in the results comment ends with `This project mounts keys lab-kubeconfig of Secret turnip-repository-123456789 in namespace turnip-runners; if that Secret or one of those keys is missing, the Pod cannot start.`

### Requirement: Exactly-once finalization
Each Operation SHALL be finalized exactly once, by whichever comes first of its result, its start timeout, its finalization as stopped and its abandonment (see Start timeout and sweep), through an atomic claim on its record. A result SHALL be accepted whether or not the Operation was marked started. A timeout SHALL be applied only to an Operation not yet started and past its start deadline, a finalization as stopped only to one marked started whose Job has ended, and an abandonment only to one marked started and 24 hours past dispatch. A result, timeout, finalization as stopped or abandonment for an Operation already finalized, or whose record no longer exists, SHALL be ignored, and the Runner's stream SHALL still close successfully.

Finalizing a result SHALL complete the Project's check (see the `project-checks` capability), record the plan's image digest for a plan (see the `tool-images` and `locking` capabilities), apply the result's event to the Lock (see the `locking` capability), record the outcome (see the `merge-check` capability), delete the record, and add the Project's result to its trigger (see Trigger progress).

#### Scenario: A Runner retries a result the Server already finalized
- **WHEN** the Server finalized a result but the Runner did not receive the acknowledgment and sends the result again
- **THEN** the second result is ignored and the check and Lock are updated once

#### Scenario: A result after a timeout
- **WHEN** an Operation was timed out and its Runner later delivers a result
- **THEN** the result is ignored

#### Scenario: Two replicas sweep at once
- **WHEN** two replicas find the same Operation past its start deadline
- **THEN** exactly one of them times it out

#### Scenario: A result after abandonment
- **WHEN** an Operation was abandoned and its Runner later delivers a result
- **THEN** the result is ignored and the check stays in progress

#### Scenario: A result and the stop detection race
- **WHEN** a Runner told to stop sends its result as the sweep finds its Job failed
- **THEN** exactly one of them finalizes the Operation, and the other is ignored
