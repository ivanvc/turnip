# Tasks

**The checkpoint that matters is task 5.1.** Each piece can be right on
its own while the fault is still there: the fault is a step that only
the replica that started it can take. Only two orchestrators sharing one
Redis, where the one that received the trigger is dropped and the other
receives the Runners' results, show that the later command is
dispatched and the comment is posted once. It runs in-process against
the Redis the orchestrator's tests already use, so it needs no cluster.

Wave 0 adds the trigger's storage and the Operation Record's new fields
and claim, which nothing uses yet. Wave 1 adds carrying a trigger
forward, behind a seam for dispatch, still unused. Wave 2 is the
switch: dispatch stops waiting and finalization starts completing, and
the test harness's fake Runner changes with it, so the package's tests
pass again only at the end of that wave. Wave 3 adds abandonment.

## 1. The trigger's storage (`internal/orchestrator`)

- [x] 1.1 A trigger store over the one hash `trigger:<id>` of the
  design's Decision 1: create (the `meta` JSON, the results of Projects
  refused during selection); begin a command (the `command` index, one
  `pending:<command>:<project>` field per Project, the 25-hour expiry
  set again); complete a Project with the script of Decision 2; read a
  trigger back (`meta`, the results in the order added); delete.
  Verify: unit tests against Redis (a trigger round-trips with its
  Targets intact; beginning a command resets the expiry; a completion
  returns how many are pending; a second completion of the same Project
  returns -1 and stores nothing; one Project in two commands is two
  pending fields; a completion for a trigger that is gone returns -1),
  and a property test, `// Feature:
  fix-results-comment-on-replica-loss, Property 1: Exactly one
  completion finishes a command`, over any number of Projects completed
  concurrently in any order: exactly one call returns 0 and every
  result is stored once
  - _Deltas: operation-lifecycle (Trigger progress)_

## 2. The Operation Record (`internal/orchestrator`)

- [x] 2.1 `OperationRecord` gains the trigger's id and the command's
  index; records expire after 25 hours; the claim script gains the
  `abandon` mode of Decision 6 (a record marked started, created at
  least 24 hours ago, not finalized). Verify: tests for each claim mode
  against each record state (not started and past its start deadline;
  started and 23 hours old; started and 24 hours old; finalized; gone),
  that `timeout` still refuses a started record and `abandon` refuses
  one not started, the new expiry, and the existing record tests
  - _Deltas: operation-lifecycle (Operation Records, Exactly-once
    finalization)_

## 3. Carrying a trigger (`internal/orchestrator`)

- [x] 3.1 In a file of its own: begin a command (set its pending
  fields, dispatch its Projects concurrently through a dispatch seam,
  complete each Project the dispatch refused); complete a Project and,
  when the store returns 0, begin the next command or post the comment
  with `postResults` and delete the trigger; a missing trigger or an
  empty trigger id is logged and nothing more (Decision 7). Verify,
  with a fake dispatch: a command whose Projects are all refused begins
  the next at once; the last completion of the last command posts one
  comment carrying every result and the notices, plan results before
  apply results, and deletes the key; a completion that is not the last
  posts nothing; a completion for a missing trigger returns without
  error
  - _Deltas: operation-lifecycle (Trigger progress), pr-comment (One
    results comment per trigger, split by kind)_
- [x] 3.2 The switch. `executeOne` ends when the Job is created and
  writes the trigger's id and the command's index into the Operation
  Record; its subscription and wait, `executeTargets`' wait and
  `notify.go` are removed. `HandleIssueComment` and `handlePlanTrigger`
  create the trigger and begin its first command in a goroutine; a
  trigger with nothing to dispatch posts at once and creates no record.
  `HandleResult` and the sweep's start timeout complete the Project in
  its trigger, in the call that finalized it, where they published
  before. The operation-duration metric and the `operation finished`
  log line move to finalization. The test harness's fake Runner reports
  through `HandleResult` instead of publishing.
  `docs/operating/deployment.md` ("Scaling up") says a trigger
  continues on whichever replica finishes its last step, and that
  Operations running across the upgrade to this version lose their
  comment once; `docs/operating/troubleshooting.md` (high availability)
  gains what a missing results comment means now (a replica killed in
  the middle of a step; the `trigger:` key and its expiry);
  `docs/using/pull-requests.md` says a comment's later commands run
  whichever replica is up. Verify: `go build ./...`; the package's
  existing tests, moved to the new harness, pass; no test or code
  mentions `operation-done`; a result for an Operation that names no
  trigger is finalized without error; `make docs-check`
  - _Deltas: operation-lifecycle (Trigger progress, Multi-replica
    behavior, Exactly-once finalization), pr-comment_

## 4. Abandonment (`internal/orchestrator`)

- [x] 4.1 The sweep tries the `abandon` claim for every record it
  scans; an abandoned Operation's record is deleted, its check and its
  Lock are left as they are, and its Project is completed in its
  trigger with `No result was received from the Runner within 24
  hours.` `docs/using/troubleshooting.md` explains that line and that
  the check stays in progress. Verify: a started record 24 hours old is
  abandoned on the next sweep, and its trigger's comment carries the
  line; one 23 hours old is not; a result delivered after is ignored
  and the check untouched; two sweeps at once abandon it once;
  `make docs-check`
  - _Deltas: operation-lifecycle (Start timeout and sweep, Exactly-once
    finalization)_

## 5. Checkpoint: a trigger across two replicas

- [x] 5.1 Two orchestrators on one Redis, with the real trigger store,
  record store and sweep, and a recording GitHub client. (a) The first
  receives a comment holding `diff web` and `apply web` and dispatches
  the diff; it is dropped; the second receives the diff's result,
  dispatches the apply, receives its result, and one plan comment and
  one apply comment are posted, each once. (b) An orchestrator that
  dispatched nothing receives the only result of a trigger and posts
  its comment (every replica replaced). (c) The last two Projects of a
  command are finalized on the two orchestrators at the same moment,
  100 times over: the next command is begun once each time. (d) A
  first command refused whole at its Locks: the second is dispatched at
  once. In each, the `trigger:` key is gone at the end

## 6. Final checkpoint

- [x] 6.1 `go build ./...`, `gofmt -l ./internal ./cmd` clean,
  `go vet ./...`, `make test`, `make lint` pass
- [x] 6.2 Every scenario in the two deltas maps to a test or a doc
  change; `openspec validate fix-results-comment-on-replica-loss
  --strict` passes; `openspec/roadmap.md` shows the change as
  implemented, and its Backlog entry on the orchestrator's intermittent
  test failure is updated: the fake's publish, its suspected cause, no
  longer exists
- [x] 6.3 An intermittent failure is recorded here with its full
  output, not rerun away; the concurrency tests (1.1's property, 5.1)
  are run 20 times under `-race` (`go test -race -count=20`) before
  this is ticked

## 7. Follow-up: a completion whose answer is lost

- [x] 7.1 Each completion carries a token, the same on every try; the
  completion script records the token and its answer in
  `completed:<command>:<project>` and gives a try with the same token
  that answer again, and any other token -1 (2026-10-14: a completion
  Redis ran whose answer was lost, by a broken connection or by the
  Redis client retrying on its own, read as already completed and left
  its trigger without a comment until it expired); design Decision 2
  shows the script, and Decision 8 no longer lists the case as not
  recovered. Verify: a try with the same token is given the same answer
  and stores nothing, another token is answered -1; a completion whose
  answer is dropped after Redis ran it still dispatches the next command
  once, or posts the comment once (this test fails with a fresh token
  per try); the script on a real Valkey; Property 1 and the two-replica
  tests

## Repository owner

- Review and commit
- Deploy when no Operation is running: one in flight across the upgrade
  is finalized but its comment is not posted
- Confirm on real runs: an apply that upgrades turnip itself posts its
  results comment; a comment holding a diff and then an apply, with the
  Server restarted during the diff, runs the apply and posts both; then
  this change is archived

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1.1", "2.1"] },
    { "id": 1, "tasks": ["3.1"] },
    { "id": 2, "tasks": ["3.2"] },
    { "id": 3, "tasks": ["4.1"] },
    { "id": 4, "tasks": ["5.1"] },
    { "id": 5, "tasks": ["6.1", "6.2", "6.3"] },
    { "id": 6, "tasks": ["7.1"] }
  ]
}
```

## Deviations recorded at implementation

- 2026-10-13 (3.2): `OperationRecord` gains a third field,
  `CheckRunError`. The note that a Project's check could not be created
  was appended by the dispatcher's wait, which no longer exists; the
  record now carries the error so the finalizer, on any replica, appends
  the same note to the result.
- 2026-10-13 (3.2): finalization completes the Project in its trigger
  (and so begins the next command or posts) on a context the caller's
  cancellation does not reach. It still runs inside the Runner's RPC or
  the sweep's tick, as Decision 4 requires; the Operation is already
  claimed by then, so a Runner that hangs up right after sending its
  result must not leave the trigger without it.
- 2026-10-13 (3.2): the trigger record is created in the webhook call
  and only beginning its first command runs in the goroutine, so a Redis
  failure at creation is returned to the webhook. A trigger with nothing
  to dispatch posts its comment in the webhook call too, not in a
  goroutine.
- 2026-10-13 (3.2): the operation-duration metric moved to finalization
  for dispatched Operations only. A refused Operation is still observed
  by the dispatcher, as before, since it never reaches a finalizer. The
  dispatched counter's `success` and `failure` outcomes moved with the
  `operation finished` log line.
- 2026-10-13 (3.2): the `waiting for result` refusal went away with the
  wait, and its test became one asserting that a dispatcher refuses
  nothing once the Job exists. Tests that Redis failing mid-dispatch
  completes the check call `executeOne` directly: through a trigger the
  refusal cannot be recorded either, because Redis is down, and that
  trigger is stranded until it expires (the Risks' killed-in-a-step case,
  reached without a kill).
- 2026-10-13 (3.2): `Orchestrator.redis` is no longer read outside
  tests. It is left in place; removing it is a cleanup for 6.1.
- 2026-10-13 (fix pass, sweep): a sweep tick finalizes an Operation it
  has claimed on a context the stop does not cancel (the Job's status,
  the Lock event, the check, the outcome and the record's deletion, not
  only the trigger step). The scan and the claims still stop with `Run`.
  Before, a stop in the middle of a tick skipped the check and the Lock
  and still carried the trigger forward.
- 2026-10-13 (fix pass, trigger steps): each step of a trigger against
  Redis (creating it, beginning a command, completing a Project, reading
  it back, deleting it) is tried up to 4 times, waiting 250 ms, 500 ms
  and 1 s between tries, before the error is logged or returned. The
  Risks cover a replica killed inside a step, not a Redis error inside
  one, and nothing else would complete the Project. Not recovered: a
  completion that Redis ran and whose reply was lost reads as already
  completed on the next try, so if it was its command's last the
  trigger is still stranded until it expires. Telling that case apart
  needs a change to Decision 2's script.
- 2026-10-13 (fix pass, trigger creation): when the trigger record
  cannot be written, the results comment is still posted before the
  error is returned to the webhook: the Projects refused during
  selection with their reasons, every other Project as refused with
  `recording the trigger: <error>. Nothing was dispatched; comment again
  to retry.`, and the selection notices. Those Projects get no check
  and no entry in the pull request's record, unlike a refusal made
  during dispatch.
- 2026-10-13 (fix pass, docs): "A Project reports no result from the
  Runner" in `docs/using/troubleshooting.md` no longer says that
  requiring `turnip` avoids the merge block. An abandoned Operation
  records no outcome, so `turnip` does not pass either; the section now
  says to plan and apply the Project again, or push a commit.
- 2026-10-13 (fix pass, comment): `deleteRecord`'s comment names
  `operationTTL` where it said 24 hours.
- 2026-10-13 (final checkpoint, real Redis): the tests that need a real
  Redis (`ha_test.go`, skipped unless `TURNIP_TEST_REDIS_ADDR` is set)
  were skipped throughout the waves, and turned out to be broken since
  before this change: their fake GitHub client had no `IsCollaborator`
  (a nil dereference since collaborator authorization), and their fake
  Job reported no image id, so every apply was refused for want of a
  recorded digest (since `tool-images`). Both fakes are fixed. All four
  then pass against a throwaway Valkey 8, three times over, as does the
  whole package: the completion script and a plan on one instance
  followed by an apply on another are exercised on a real server, not
  only the in-memory one. CI does not set the address, so these tests
  still do not run there.
- 2026-10-13 (final checkpoint): `Orchestrator.redis` stays. No code
  outside the tests reads it, but nine test files do.
- 2026-10-13 (6.3): no intermittent failure was seen. The concurrency
  tests (Property 1, the four two-replica tests, the fault and sweep
  tests) passed 20 times under `-race`.
- 2026-10-14 (7.1): the case the fix pass left unrecovered, a completion
  Redis ran whose answer was lost, is recovered by a token per
  completion. Also found: the Redis client retries a failed command up
  to 3 times on its own, with turnip's settings, so the case did not
  need the fix pass's retry to happen; a short network break was
  enough.
