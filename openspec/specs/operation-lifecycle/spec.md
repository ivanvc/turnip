# operation-lifecycle Specification

## Purpose
Defines an Operation's life from dispatch to finalization: the record the Server keeps, the stream a Runner reports over, how delivery is retried, how failures are categorized, how a Runner that never starts is timed out, and how every Operation is finalized exactly once across Server replicas. Operators and pull request authors rely on it to know that every dispatched Operation ends in exactly one reported outcome.

## Requirements

### Requirement: Operation Records
For every Operation it dispatches, the Server SHALL create an Operation Record in Redis under the key `operation:<operation id>` (a random UUID) before creating the Runner Job, expiring 24 hours after creation. The record SHALL hold what finalization needs (the Project, pull request, head commit, operation, recorded arguments, check run, Job name, triggering user) and a start deadline 5 minutes after dispatch. The Server SHALL delete the record when the Operation is finalized, and SHALL delete it at once when the Runner Job could not be built or created. The Operations of one trigger SHALL be dispatched concurrently, one per Project.

#### Scenario: A dispatched Operation
- **WHEN** a plan for Project `web` is dispatched
- **THEN** a key `operation:<id>` exists in Redis until the Operation is finalized, and at most 24 hours

#### Scenario: Kubernetes refuses the Job
- **WHEN** creating the Runner Job fails
- **THEN** the Operation Record is deleted and the Operation is refused (see the `project-checks` capability)

### Requirement: Runner-to-Server stream contract
A Runner SHALL report over a single client-streaming gRPC call, `ExecuteOperation`, to the address in `TURNIP_SERVER_ADDR`, authenticated as defined by the `runner-security` capability. Each stream SHALL open with a start message describing the Operation (marked resumed when it is not the first stream for that Operation), followed by any number of log lines (timestamp, level `info` for stdout or `error` for stderr, message), and close with one result: success, the Operation's output, exit code, error message, add/change/destroy counts, plan data and a failure category.

The Server SHALL identify the Operation from the authenticated identity, never from the start message; a start message naming a different Operation SHALL be ignored with a logged warning. The first log line the Server receives on each stream SHALL mark its Operation started, and the Server SHALL do nothing for that stream's later lines. Log line content SHALL serve only as evidence of progress: the Server SHALL NOT store or display it, and the pull request sees only the result's output (see the `pr-comment` and `project-checks` capabilities). The Runner SHALL also write the tool's output, unstripped, to its own container's stdout and stderr.

The `runner` container SHALL connect before running the tool, send every line of the tool's record (see the `plugins` capability) in the record's order while the tool runs, and send the result when the tool finishes. While the stream stays open no line SHALL be left out, including lines the Operation's output omits. Sending SHALL NOT delay the tool, the recording of its output or the Runner's own log, however slowly the Server reads. The `clone` container SHALL connect only when the checkout fails, and send a result with no log lines (see the `checkout` capability).

#### Scenario: A plan that prints output
- **WHEN** a Runner's tool prints its first line
- **THEN** the line is streamed to the Server and the Operation is marked started

#### Scenario: A mismatched start message
- **WHEN** an authenticated Runner's start message names a different operation id
- **THEN** the Server logs a warning and applies the stream to the authenticated Operation

#### Scenario: A Server slower than the tool
- **WHEN** the Server reads log lines more slowly than the tool writes them
- **THEN** the tool is not slowed and the order of its record is unchanged
- **AND** every line of the record still reaches the Server, in the record's order, before the result

#### Scenario: A verbose plan
- **WHEN** a tool prints 20,000 lines over one stream
- **THEN** the Server marks the Operation started on the first of them and does nothing for the other 19,999

#### Scenario: Lines the output omits
- **WHEN** a failed Helmfile `diff`'s output omits its standard output (see the `plugin-helmfile` capability)
- **THEN** the stream still carried every line of its standard output

### Requirement: Retry budgets
The Runner SHALL retry opening its stream with exponential backoff (1 second base, capped at 30 seconds, full jitter) for up to 2 minutes; if it cannot connect within that budget, it SHALL exit with status 1 without running the tool. If the stream breaks while the tool runs, the Runner SHALL keep running the tool and buffer its output. The Runner SHALL retry delivering its result, reopening the stream as needed, with exponential backoff (1 second base, capped at 60 seconds, full jitter) for up to 15 minutes; if delivery fails within that budget, it SHALL exit with status 1. After delivering a result the Runner SHALL exit 0, whether the tool succeeded or failed. A result the Server refuses for its size SHALL NOT be retried: the Runner SHALL instead deliver a failed result with no output, no plan data and no category, whose error message reads `the result was too large to deliver (<size>; the Server accepts at most 4 MiB)`, under the same budget.

#### Scenario: The Server is briefly unreachable at start
- **WHEN** the Server becomes reachable 40 seconds after the Runner starts
- **THEN** the Runner connects and runs the tool

#### Scenario: The Server is unreachable for the whole connect budget
- **WHEN** the Runner cannot open a stream within 2 minutes
- **THEN** it exits 1 without running the tool, and the Operation is finalized by the start timeout

#### Scenario: A Server replica restarts mid-operation
- **WHEN** the stream breaks while the tool is running and a Server replica is reachable again within 15 minutes of the tool finishing
- **THEN** the Runner opens a resumed stream, replays its buffered log lines and delivers the result

#### Scenario: A failed tool
- **WHEN** the tool exits 1 and the result is delivered
- **THEN** the Runner exits 0

#### Scenario: A result too large to deliver
- **WHEN** the Server refuses a result because it exceeds 4 MiB
- **THEN** the Runner sends no second copy of it, and delivers instead a failed result whose error message says it was too large to deliver

### Requirement: Log buffering
On a stream that never broke, the Runner SHALL send every log line once and replay none. When a stream broke, the Runner SHALL replay, on the stream that replaces it and before its result, the most recent 256 KiB of the record's log lines (counting timestamp, level and message), preceded by a line at level `warn` reading `<N> lines dropped` when the record holds N lines older than those replayed.

#### Scenario: A long-running, verbose Operation
- **WHEN** a tool prints more than 256 KiB of output and the stream never breaks
- **THEN** every line reaches the Server once, and no `lines dropped` line is sent

#### Scenario: A verbose Operation whose stream broke
- **WHEN** a tool prints more than 256 KiB of output and the stream breaks while it runs
- **THEN** the replay on the resumed stream begins with `<N> lines dropped` and carries only the most recent lines

### Requirement: Failure categories
A Runner's result SHALL be successful exactly when the tool exited 0. A failed result SHALL carry one failure category, which the Project's check title reflects (see the `project-checks` capability):

| Category | When | Exit code |
|---|---|---|
| tool exited | the tool ran and exited non-zero | the tool's |
| clone failed | the checkout failed (see the `checkout` capability) | -1 |
| workspace failed | the Runner had no workspace and could not create a temporary one | -1 |
| tool not started | the tool could not be run, for example its binary is missing or the operation is not one it supports | -1 |
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

### Requirement: Workspace paths are stripped
Before anything leaves the Runner for the Server, every occurrence of the workspace directory followed by `/` SHALL be removed, and every remaining occurrence of the workspace directory SHALL be replaced with `.`. This SHALL apply to streamed log lines, the result's output and error message, and checkout failure messages.

#### Scenario: A path in tool output
- **WHEN** the tool prints `error in /turnip/src/infra/web/helmfile.yaml`
- **THEN** the Server receives `error in infra/web/helmfile.yaml`

#### Scenario: The workspace root itself
- **WHEN** an error message names `/turnip/src` alone
- **THEN** the Server receives `.` in its place

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

### Requirement: Exactly-once finalization
Each Operation SHALL be finalized exactly once, by whichever comes first of its result and its start timeout, through an atomic claim on its record. A result SHALL be accepted whether or not the Operation was marked started. A timeout SHALL be applied only to an Operation not yet started and past its deadline. A result or timeout for an Operation already finalized, or whose record no longer exists, SHALL be ignored, and the Runner's stream SHALL still close successfully.

Finalizing a result SHALL complete the Project's check (see the `project-checks` capability), record the plan's image digest for a plan (see the `tool-images` and `locking` capabilities), apply the result's event to the Lock (see the `locking` capability), record the outcome (see the `merge-check` capability), delete the record, and hand the Project's result to the results comment (see the `pr-comment` capability).

#### Scenario: A Runner retries a result the Server already finalized
- **WHEN** the Server finalized a result but the Runner did not receive the acknowledgment and sends the result again
- **THEN** the second result is ignored and the check and Lock are updated once

#### Scenario: A result after a timeout
- **WHEN** an Operation was timed out and its Runner later delivers a result
- **THEN** the result is ignored

#### Scenario: Two replicas sweep at once
- **WHEN** two replicas find the same Operation past its start deadline
- **THEN** exactly one of them times it out

### Requirement: Multi-replica behavior
Any Server replica SHALL accept a Runner's stream and finalize its Operation. The finalizing replica SHALL hand the Project's result to the dispatching replica by publishing it on the Redis channel `operation-done:<operation id>`, to which the dispatching replica subscribes before creating the Job; the dispatching replica SHALL wait for it up to 24 hours and then post the trigger's results comment (see the `pr-comment` capability).

Known issue: the results comment is posted by the dispatching replica's goroutine; if it restarts, checks finalize but no comment is posted. Fix proposed in change `fix-results-comment-on-replica-loss`.

#### Scenario: A result reaching another replica
- **WHEN** replica A dispatches an Operation and the Runner's stream lands on replica B
- **THEN** replica B completes the check and moves the Lock, and replica A posts the results comment

#### Scenario: The dispatching replica restarts
- **WHEN** the replica that dispatched a trigger restarts before its Operations finish
- **THEN** the Operations' checks are still finalized, but no results comment is posted for that trigger

### Requirement: Output sent with a result
The Runner SHALL send at most 1 MiB (1,048,576 bytes) of an Operation's output with its result. A longer output SHALL be cut at a line boundary, keeping its end: the leading run of turnip's annotation lines (`@@ turnip: ... @@`) and the closing one SHALL be kept, the start of the lines between them removed, and the line `@@ turnip: <K> earlier lines omitted (<size>) @@` put right after the leading annotation lines, where `<size>` is the bytes removed, in KiB below 1 MiB and in MiB from 1 MiB, with one decimal. Change counts SHALL be read from the whole output before it is cut (see the `plugins` capability), and plan data SHALL never be cut. What the live log stream carries is not cut.

#### Scenario: A first install's plan
- **WHEN** a plan's output is 5,069,133 bytes
- **THEN** its result carries at most 1 MiB of output: the leading annotation lines, then `@@ turnip: <K> earlier lines omitted (3.8 MiB) @@`, then the end of the tool's output and the closing annotation line
- **AND** the plan's change counts are those of the whole output

#### Scenario: Output within the limit
- **WHEN** a plan's output is 200 KiB
- **THEN** its result carries the whole output, with no omission line
