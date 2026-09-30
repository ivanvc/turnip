# Requirements: Scheduling: Concurrency and Dependencies (Slice 30)

## Introduction

turnip runs every Operation a trigger targets at once. `executeTargets`
starts one goroutine per Target, each creates its Runner Job
immediately, and nothing limits how many run together or in what order.

That has two costs at scale, and both are real for a repository of ~300
Projects spread over 8 clusters:

- **Capacity.** One comment can create hundreds of Runner Jobs. Not every
  cluster can schedule that many Pods, and a Pod left Pending past the
  start timeout is reported as a failure while its Lock stays held and
  the Pod may still run later (the start deadline is stamped when the
  Operation Record is created, not when the Pod is scheduled).
- **Order.** Some Projects must be applied before others. In the
  motivating repository, each cluster's `foundation-<cluster>` must be in
  place before that cluster's `staging-<cluster>` and
  `production-<cluster>`, and nothing else is ordered: a failure in one
  cluster's foundation has no bearing on another cluster.

This slice adds a **global concurrency cap with a fair queue**, and
**per-Project dependencies** that order and block mutating Operations.

**Why dependencies rather than execution-order groups.** Groups (an
ordered list, each group finishing before the next starts) were
considered first, and are what other GitOps IaC tools offer. They order
too coarsely for the motivating repository: a failure in any foundation
would stop staging and production in every cluster, and every cluster's
production would wait for the slowest foundation anywhere. Dependencies
block exactly the dependants of what failed, and start each Project as
soon as its own dependencies are in place.

**Not tied to Terraform.** The motivating case in the roadmap was
Terraform workspaces, but nothing here touches a Plugin: the cap and the
dependencies live in the orchestrator.

This slice amends the global spec's Requirement 17.1, which says the
Server "SHALL execute Operations for all Projects in parallel"; the
amendment is to rewrite it in place.

## Glossary

- **Slot**: permission for one Operation to have a Runner Job. An
  Operation holds a Slot from just before its Operation Record is created
  until the Record is finalized.
- **Global_Cap**: the most Slots the whole deployment may hold at once,
  across every repository, pull request and Server replica.
- **Repository_Limit**: the most Slots one repository's Operations may
  hold at once, set by the repository.
- **Dependency**: a Project another Project names in `dependsOn`.
- **Run**: the Operations one trigger (a push, or one command in a
  comment) targets.

## Requirements

### Requirement 1: A global cap, with a queue in turnip

**User Story:** As an operator, I want to limit how many Runner Jobs
turnip runs at once, so that infrastructure that cannot scale to hundreds
of Pods is not flooded, and nothing is reported as failed for waiting.

#### Acceptance Criteria

1. THE Server SHALL read a Global_Cap from its configuration
   (`TURNIP_MAX_CONCURRENT_OPERATIONS`); unset SHALL mean unlimited, which
   is today's behavior
2. THE Global_Cap SHALL count every Operation holding a Slot across the
   whole deployment — every repository, pull request and Server replica —
   using state in Redis
3. WHEN no Slot is free, THE Operation SHALL wait in turnip, before its
   Operation Record or Runner Job is created
4. AN Operation SHALL NOT be reported as timed out for time spent waiting
   for a Slot; its start deadline SHALL begin when it takes one
5. A Slot SHALL be released when its Operation is finalized, by its
   result or by the sweep, so a Server that stops mid-run does not keep
   Slots it held

*Rationale for 1.2: a cap per trigger or per replica would let two pull
requests, or two replicas, each use the full cap, so the cluster would
see a multiple of it. Decided 2026-09-24.*

*Rationale for 1.3 and 1.4: letting Kubernetes queue the extra Pods
instead would run the start-timeout clock while they wait, and fail them.*

*Rationale for 1.5: the Operation Records already mark exactly the
Operations in flight, and are already finalized on every path, so a Slot
tied to them cannot outlive its Operation.*

### Requirement 2: A repository may limit itself further

#### Acceptance Criteria

1. `turnip.yaml` SHALL accept a top-level `concurrency: N`, N ≥ 1, limiting
   how many of that repository's Operations hold Slots at once
2. THE Repository_Limit SHALL only ever be lower than or equal to the
   Global_Cap in effect; a higher value SHALL have no effect beyond the
   Global_Cap
3. `concurrency: 1` SHALL run the repository's Operations one at a time
4. THE Repository_Limit SHALL NOT be gated by `TURNIP_ALLOWED_OVERRIDES`

*Rationale for 2.4: every gated setting grants something, and
`turnip.yaml` comes from the pull request. A Repository_Limit can only
make the repository's own runs slower, never faster than the operator
allows, so the pull request gains nothing by setting it.*

### Requirement 3: Waiting triggers take turns

#### Acceptance Criteria

1. WHEN a Slot frees and more than one Run has Operations waiting, THE
   next Slot SHALL go to the Runs in turn, not to the Run that started
   waiting first
2. A Run that is the only one waiting SHALL take every free Slot

*Rationale: with a shared cap, first-come-first-served lets one large
apply hold every Slot for as long as it runs, and a one-line pull request
elsewhere waits behind hundreds of Operations. Taking turns keeps turnip
responsive for everyone while a large Run still uses all the capacity no
one else needs.*

### Requirement 4: Waiting is visible

#### Acceptance Criteria

1. WHILE an Operation waits for a Slot, its Project_Check SHALL exist with
   status `queued` and a Title saying it is waiting for a free Runner slot
2. WHEN it takes a Slot, THE same check SHALL move to in progress and
   continue as today
3. THE Titles SHALL come from `titles.go`, under Slice 35's rules

*Rationale for 4.2: queued to in progress is a check run's documented
forward lifecycle, unlike the transitions Slices 36 and 37 avoided.*

### Requirement 5: Per-Project dependencies

**User Story:** As a repository maintainer, I want to declare that a
Project depends on others, so that its changes are applied only once
theirs are in place, and a failure blocks only what depends on it.

#### Acceptance Criteria

1. A Project SHALL accept `dependsOn`, a list of Project names or name
   patterns, using the same pattern syntax trigger comments accept
   (Slice 21)
2. A pattern SHALL NOT match the Project that declares it
3. THE dependency graph SHALL be validated when `turnip.yaml` is parsed,
   as part of its existing validation, and each of these SHALL be a
   validation error:
   - a `dependsOn` entry that matches no Project
   - a Project naming itself explicitly
   - a cycle, of any length, naming the Projects in it
4. THE whole graph SHALL be validated on every trigger, whichever Projects
   the pull request touches
5. AN invalid graph SHALL make the configuration invalid: no Operation
   SHALL run for any Project, no Lock SHALL be taken and no Runner Job
   created, and it SHALL be reported as any invalid `turnip.yaml` is — a
   comment on the pull request, and the Aggregate_Check failed as
   `invalid turnip.yaml` (Slice 37, Requirement 7.1)
6. Graph errors SHALL be reported with every other configuration
   violation, in one pass

*Rationale for 5.3's first case: a renamed Project must not quietly drop
out of the dependencies that name it.*

*Rationale for 5.3 and 5.5, failing before anything runs: an invalid graph
has no correct order. Discovering a cycle mid-Run would leave some
Projects applied and the rest stuck waiting on each other, which is worse
than applying nothing. Parse time is the one point that sees the whole
graph before any Lock or Job exists, and a failure there already stops
everything.*

*Rationale for 5.4: a cycle among Projects this pull request does not
touch is still a broken file. Validating only the touched part would let
it merge and fail on whichever later pull request first reaches it.*

### Requirement 6: Dependencies order and block mutating Operations only

#### Acceptance Criteria

1. WITHIN a Run, A Mutating_Operation SHALL NOT start until every
   Dependency in the same Run has completed its Mutating_Operation
   successfully
2. WHEN a Dependency's Mutating_Operation in the same Run fails, or does
   not run, THE dependant's SHALL NOT run, transitively, and SHALL say
   which Dependency blocked it
3. Operations with no dependency relationship SHALL NOT wait for each
   other, beyond the caps
4. Plan_Operations SHALL NOT wait for or be blocked by Dependencies; they
   are subject only to the caps

*Rationale for 6.4: planning a Dependency first does not change the
cluster, so a dependant's plan sees the same state either way, and a
failed Dependency plan does not make the dependant's diff wrong. Ordering
plans would serialize every automatic plan at this repository's scale and
hide diffs a reviewer needs, for no correctness gain.*

### Requirement 7: Dependencies outside the Run

#### Acceptance Criteria

1. A Dependency the pull request does not change — absent from the
   Pull_Request_Record for this commit — SHALL count as satisfied
2. A Dependency the pull request changes but the Run does not include
   SHALL count as satisfied only when its Outcome in the
   Pull_Request_Record is applied or nothing to apply
3. OTHERWISE the dependant's Mutating_Operation SHALL NOT run, and SHALL
   say which Dependency to apply first ("waiting on foundation-a, apply it
   first")
4. turnip SHALL NOT add a Dependency to a Run the command did not name

*Rationale for 7.1: a foundation the pull request does not touch is as
in place as it will be; requiring it to be planned or applied anyway
would make every one-line change to a dependant touch its foundations.*

*Rationale for 7.4: an apply that changes infrastructure the command did
not name is surprising, and hard to see in review.*

### Requirement 8: A Server that stops mid-run

#### Acceptance Criteria

1. Operations still waiting — for a Slot or for a Dependency — when their
   Server stops SHALL be lost, not resumed by another replica
2. Each SHALL remain visible as its queued Project_Check
3. Repeating the command SHALL run only what is left: Projects already
   applied are up to date in the Pull_Request_Record and need nothing
4. THE documentation SHALL say so plainly

*Rationale: persisting Runs so another replica can resume them needs a
durable queue with ownership and hand-over — a slice of its own. Capped,
dependency-ordered Runs last longer than today's, which widens the window,
but repeating the command is safe and nothing is silent. Decided
2026-09-24; the durable alternative is recorded in the roadmap Backlog.*

### Requirement 9: What does not change

#### Acceptance Criteria

1. WITHOUT a Global_Cap, a Repository_Limit or any `dependsOn`, turnip
   SHALL schedule exactly as today
2. Commands in one comment SHALL still run one after another, each
   command's Run completing before the next starts

### Requirement 10: Documentation

#### Acceptance Criteria

1. `docs/configuration.md` SHALL document `TURNIP_MAX_CONCURRENT_OPERATIONS`,
   `concurrency` and `dependsOn`, with the motivating per-cluster example
2. `docs/usage.md` SHALL explain the waiting states, that dependencies
   order applies but not plans, and what to do after a Server restart
3. THE global spec's Requirement 17.1 SHALL be rewritten in place to
   describe capped, dependency-ordered execution

## Out of Scope

- **Resuming Runs across Server restarts** (Requirement 8's alternative).
- **Completing stranded queued checks by a sweep.** It needs the same
  mechanism as the Backlog entry on a Runner that dies after starting, and
  belongs with it.
- **Ordering plans**, and dependencies between repositories.
- **Execution-order groups.** Dependencies express every order groups
  can; see the introduction.
