## MODIFIED Requirements

### Requirement: Start timeout and sweep
Every Server replica SHALL sweep the Operation Records every 30 seconds and time out each Operation that has not been marked started by its start deadline (5 minutes after dispatch). Timing out SHALL complete the Project's check as a failure whose title is a diagnostic read from Kubernetes and whose summary is `The Runner never reported back within the start timeout.`, apply the plan or mutating timeout to the Lock (see the `locking` capability), record the outcome (see the `merge-check` capability), delete the record, and hand the diagnostic to the results comment as the Project's output. The diagnostic SHALL be the first that applies:

| Kubernetes state | Diagnostic |
|---|---|
| no Job name recorded, the Job is gone, or its status cannot be read | `No Job/Pod found for this Operation; it may have been deleted or never successfully scheduled` |
| a mutating Operation's tool container cannot pull its image | the recorded-digest diagnostic (see the `tool-images` capability) |
| a container is waiting with a reason (init containers first) | `Job <name>: container stuck (<reason>)` |
| a Pod exists | `Job <name>: Pod still <phase> after 5 minutes` |
| no Pod yet | `Job <name>: no Pod found yet after 5 minutes` |

When the Operation's Project lists keys of the repository's Secret (see the `runner-access` capability) and the diagnostic was read from a Pod still `Pending`, the Project's output in the results comment SHALL be the diagnostic followed by a line reading `This Project mounts keys <keys> of Secret <secret> in namespace <namespace>; if that Secret or one of those keys is missing, the Pod cannot start.`, with the keys joined by `, ` in the order listed. The check's title SHALL remain the diagnostic alone.

Once an Operation is marked started, nothing SHALL time it out before its record expires: the check stays in progress, the Lock stays as it was, and the dispatching replica gives up after 24 hours, reporting `waiting for result: context deadline exceeded` for the Project without completing its check.

Known issue: once started, nothing bounds an Operation; only the start is timed out (5 minutes, 30 second sweep). Fix proposed in change `runner-timeouts`.

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

#### Scenario: A missing repository Secret
- **WHEN** Project `lab` of repository id `123456789` lists `lab-kubeconfig`, no Secret `turnip-repository-123456789` exists in namespace `turnip-runners`, and the Pod is still `Pending` at the start deadline
- **THEN** the check fails titled with the diagnostic alone
- **AND** `lab`'s output in the results comment ends with `This Project mounts keys lab-kubeconfig of Secret turnip-repository-123456789 in namespace turnip-runners; if that Secret or one of those keys is missing, the Pod cannot start.`
