## ADDED Requirements

### Requirement: Trigger progress
Every trigger that dispatches an Operation (an automatic plan, or one trigger comment with all its commands) SHALL be recorded in Redis under the key `trigger:<trigger id>` (a random UUID) before its first Operation is dispatched. The record SHALL hold what the rest of the trigger needs: the pull request and head commit, its commands in order with the Projects each selected, the results gathered so far (including Projects refused before or during dispatch), the selection notices, and which of the current command's Operations are still outstanding. It SHALL expire 25 hours after its latest command was dispatched, and the Server SHALL delete it once the trigger's results comment is posted.

Each command's Projects SHALL be selected and authorized when the trigger arrives (see the `commands` and `authorization` capabilities) and admitted against their Locks when that command is dispatched (see the `locking` capability). Nothing SHALL be selected or authorized again when a later command is dispatched.

The replica that receives the trigger SHALL dispatch its first command. Finalizing an Operation SHALL add the Project's result to its trigger, and whichever replica finalizes the last outstanding Operation of a command SHALL dispatch the trigger's next command or, when none is left, post the trigger's results comment (see the `pr-comment` capability). Exactly one replica SHALL do so for each command, however many replicas finalize that command's Operations at once. A command none of whose Projects is dispatched SHALL count as finished as soon as its dispatch ends.

#### Scenario: A trigger in Redis
- **WHEN** a trigger dispatches a plan for Project `web`
- **THEN** a key `trigger:<id>` exists in Redis until the trigger's results comment is posted, and at most 25 hours after its latest command was dispatched

#### Scenario: A later command after the receiving replica restarts
- **WHEN** a comment holds `/turnip diff web` and then `/turnip apply web`, and the replica that received it restarts while the diff runs
- **THEN** the replica that finalizes the diff dispatches the apply
- **AND** the replica that finalizes the apply posts one results comment reporting both

#### Scenario: Two Operations of one command finish at once
- **WHEN** the last two outstanding Operations of a command are finalized on two replicas at the same moment
- **THEN** exactly one of the replicas dispatches the next command, or posts the results comment when there is none

#### Scenario: A command with nothing dispatched
- **WHEN** every Project of a comment's first command is refused at dispatch because another pull request holds its Lock, and a second command follows
- **THEN** the second command is dispatched at once

## MODIFIED Requirements

### Requirement: Operation Records
For every Operation it dispatches, the Server SHALL create an Operation Record in Redis under the key `operation:<operation id>` (a random UUID) before creating the Runner Job, expiring 25 hours after creation, an hour after the sweep abandons an Operation that never finishes (see Start timeout and sweep). The record SHALL hold what finalization needs (the Project, pull request, head commit, operation, recorded arguments, check run, Job name, triggering user, and its trigger) and a start deadline 5 minutes after dispatch. The Server SHALL delete the record when the Operation is finalized, and SHALL delete it at once when the Runner Job could not be built or created. The Operations of one command SHALL be dispatched concurrently, one per Project.

#### Scenario: A dispatched Operation
- **WHEN** a plan for Project `web` is dispatched
- **THEN** a key `operation:<id>` exists in Redis until the Operation is finalized, and at most 25 hours

#### Scenario: Kubernetes refuses the Job
- **WHEN** creating the Runner Job fails
- **THEN** the Operation Record is deleted and the Operation is refused (see the `project-checks` capability)

### Requirement: Start timeout and sweep
Every Server replica SHALL sweep the Operation Records every 30 seconds and time out each Operation that has not been marked started by its start deadline (5 minutes after dispatch). Timing out SHALL complete the Project's check as a failure whose title is a diagnostic read from Kubernetes and whose summary is `The Runner never reported back within the start timeout.`, apply the plan or mutating timeout to the Lock (see the `locking` capability), record the outcome (see the `merge-check` capability), delete the record, and add the diagnostic to its trigger as the Project's output (see Trigger progress). The diagnostic SHALL be the first that applies:

| Kubernetes state | Diagnostic |
|---|---|
| no Job name recorded, the Job is gone, or its status cannot be read | `No Job/Pod found for this Operation; it may have been deleted or never successfully scheduled` |
| a mutating Operation's tool container cannot pull its image | the recorded-digest diagnostic (see the `tool-images` capability) |
| a container is waiting with a reason (init containers first) | `Job <name>: container stuck (<reason>)` |
| a Pod exists | `Job <name>: Pod still <phase> after 5 minutes` |
| no Pod yet | `Job <name>: no Pod found yet after 5 minutes` |

When the Operation's Project lists keys of the repository's Secret (see the `runner-access` capability) and the diagnostic was read from a Pod still `Pending`, the Project's output in the results comment SHALL be the diagnostic followed by a line reading `This Project mounts keys <keys> of Secret <secret> in namespace <namespace>; if that Secret or one of those keys is missing, the Pod cannot start.`, with the keys joined by `, ` in the order listed. The check's title SHALL remain the diagnostic alone.

Once an Operation is marked started, the sweep SHALL NOT time it out. Instead, 24 hours after dispatch the sweep SHALL abandon it: the check stays in progress, the Lock stays as it was, the record is deleted, and its trigger receives `No result was received from the Runner within 24 hours.` as the Project's output.

Known issue: once started, nothing bounds an Operation short of its abandonment after 24 hours, which leaves its check in progress and holds back its trigger's later commands and results comment until then. Fix proposed in change `runner-timeouts`.

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
- **THEN** the Operation is never timed out, and its check stays in progress
- **AND** 24 hours after dispatch its trigger's results comment reports `No result was received from the Runner within 24 hours.` for the Project

#### Scenario: A missing repository Secret
- **WHEN** Project `lab` of repository id `123456789` lists `lab-kubeconfig`, no Secret `turnip-repository-123456789` exists in namespace `turnip-runners`, and the Pod is still `Pending` at the start deadline
- **THEN** the check fails titled with the diagnostic alone
- **AND** `lab`'s output in the results comment ends with `This Project mounts keys lab-kubeconfig of Secret turnip-repository-123456789 in namespace turnip-runners; if that Secret or one of those keys is missing, the Pod cannot start.`

### Requirement: Exactly-once finalization
Each Operation SHALL be finalized exactly once, by whichever comes first of its result, its start timeout and its abandonment (see Start timeout and sweep), through an atomic claim on its record. A result SHALL be accepted whether or not the Operation was marked started. A timeout SHALL be applied only to an Operation not yet started and past its start deadline, and an abandonment only to one marked started and 24 hours past dispatch. A result, timeout or abandonment for an Operation already finalized, or whose record no longer exists, SHALL be ignored, and the Runner's stream SHALL still close successfully.

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

### Requirement: Multi-replica behavior
Any Server replica SHALL accept a Runner's stream and finalize its Operation, and any replica SHALL carry a trigger forward (see Trigger progress). No replica SHALL wait on another for an Operation's result, so a trigger's later commands and its results comment SHALL NOT depend on the replica that received the trigger staying up.

#### Scenario: A result reaching another replica
- **WHEN** replica A dispatches the only Operation of a trigger and the Runner's stream lands on replica B
- **THEN** replica B completes the check, moves the Lock and posts the results comment

#### Scenario: The dispatching replica restarts
- **WHEN** the replica that dispatched a trigger restarts before its Operations finish
- **THEN** the Operations' checks are finalized, and the replica that finalizes the last of them posts the trigger's results comment

#### Scenario: An apply that replaces every replica
- **WHEN** `/turnip apply` upgrades turnip's own Server, replacing every replica while the apply runs
- **THEN** the Runner delivers its result to a new replica within its retry budget (see Retry budgets), which completes the check, moves the Lock and posts the results comment
