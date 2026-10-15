# Proposal

## Why

turnip starts every Operation a trigger targets at once: one Runner Job
per Project, immediately, with nothing limiting how many run together or
in what order. That costs twice in a repository with hundreds of
Projects across several clusters:

- **Capacity.** One comment can create hundreds of Runner Jobs. A
  cluster that cannot schedule that many Pods leaves some Pending past
  the start timeout, which reports them as failed while their Locks stay
  held and the Pods may still run later.
- **Order.** Some Projects must be applied before others (a cluster's
  foundation before what runs on it), and a failure in one should block
  only what depends on it, not unrelated Projects.

This is roadmap Slice 30, carried over from its Kiro draft. The drafted
requirements, with the rationale for each, travel with this change as
`kiro-requirements.md` (Kiro's format, unchanged); the spec deltas are
written from them.

## What Changes

- An operator setting, `TURNIP_MAX_CONCURRENT_OPERATIONS`, caps how many
  Operations hold a Runner Job at once across the whole deployment
  (every repository, pull request and Server replica), counted in
  Redis. Unset means no cap, which is today's behavior
- An Operation with no free slot waits in turnip, before its Operation
  Record or Runner Job exists, and its start deadline begins when it
  takes a slot, so nothing is reported as timed out for waiting
- A repository may limit itself further with a top-level
  `concurrency: N` in `turnip.yaml`, never above the operator's cap, and
  not gated: it can only slow the repository's own runs
- Triggers waiting for slots take turns, so one large apply does not
  hold every slot while a one-line pull request waits behind it
- A waiting Operation's check exists as `queued`, with a title saying it
  waits for a free Runner slot, and moves to in progress when it starts
- A Project may declare `dependsOn`, a list of Project names or name
  patterns (the syntax trigger comments accept). The whole graph is
  validated when `turnip.yaml` is parsed: an entry matching nothing, a
  Project naming itself, or a cycle makes the configuration invalid
- Dependencies order and block mutating Operations only: one starts
  when every dependency in the same trigger has applied successfully,
  and a failed or blocked dependency blocks its dependants, each saying
  which dependency blocked it. Plans are subject to the caps alone
- A dependency the pull request does not change counts as satisfied; one
  it changes but the command did not include must already be applied,
  and turnip never adds a Project the command did not name

## Decisions carried over (2026-09-24)

- **Dependencies rather than ordered groups.** Groups order too
  coarsely: a failure anywhere in one group stops every later group,
  and each group waits for its slowest member.
- **One cap for the deployment, held in Redis.** A cap per trigger or
  per replica would let the cluster see a multiple of it.
- **turnip queues, not Kubernetes.** Pods left Pending run the start
  timeout while they wait.

## Not in this change

- Resuming a trigger's waiting Operations after its Server replica
  stops: as drafted, they are lost and repeating the command runs what
  is left. `fix-results-comment-on-replica-loss` (archived 2026-10-14)
  now keeps a trigger's progress in Redis, which a resume would build
  on; whether this change resumes is decided in its design
- Completing stranded queued checks by a sweep: `runner-timeouts`
- Ordering plans, and dependencies between repositories
- Execution-order groups

## Dependencies

- **Depends on `fix-concurrent-mutations`.** Both decide when a mutating
  command is admitted; the queue and the dependency rules build on how
  a running mutating command holds its Lock, which that change settles.
- **Depends on `fix-invalid-config-check-on-comment`.** An invalid
  dependency graph is reported as any invalid `turnip.yaml` is, which
  on the comment path fails the `turnip` check only once that change
  lands.
- **Depends on `runner-timeouts`, in part.** Completing a queued check
  stranded by a replica that stopped is left to it (see Not in this
  change); this change can land first and leave those checks queued.

## Capabilities

### New Capabilities
- `scheduling`: the concurrency cap, the repository limit, fair turns between waiting triggers, and dependency ordering of mutating Operations

### Modified Capabilities
- `configuration`: `concurrency` and `dependsOn`, and validation of the dependency graph
- `server`: the `TURNIP_MAX_CONCURRENT_OPERATIONS` setting
- `operation-lifecycle`: Operations are dispatched as slots allow rather than all at once, and the start deadline begins when a slot is taken
- `project-checks`: the queued check and its title, and the title of an Operation blocked by a dependency

## Impact

`internal/orchestrator` (dispatch, the queue, slot accounting in Redis),
`internal/config` (the two fields and graph validation),
`internal/github` (titles), `docs/using/turnip-yaml.md`,
`docs/operating/server-settings.md`, `docs/using/pull-requests.md`.
It builds on the trigger record `fix-results-comment-on-replica-loss`
added (archived 2026-10-14): a command's Operations are dispatched from
it, so the queue belongs there.
