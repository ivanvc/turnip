# Requirements: The Lock's Lifecycle as a State Machine (Slice 18)

## Introduction

A Lock is acquired when a plan starts (`execute.go:115`) and released only
by a successful Mutating_Operation (`result.go:157`), a manual
`/turnip unlock` (`comment.go:247`), or the pull request closing
(`pullrequest.go:148`). Everything else leaves it exactly as it was. That
single rule produces two distinct faults, and the second is the sharper
one.

**A Lock can guard nothing.** A plan that fails leaves the Lock held with
nothing recorded, blocking every other pull request from planning that
Project until a human intervenes, and giving the author no hint that they
are now the obstacle.

**A Lock can stay appliable when it should not be.** `StorePlan` runs only
on success, so a stored plan survives a new commit, a failed apply, and a
timed-out apply. Because the Lock records no state beyond `HasPlan`, all
three look identical to `execute.go:140-161`, which admits any
Mutating_Operation whose Lock is held and carries a plan. Three
consequences follow, all reachable today:

- **After a push**: the stored plan describes an older commit. Helmfile
  stores no artifact — `Execute` returns `PlanData: nil`, so the "plan" is
  only the recorded arguments, and the Runner clones at the pull request's
  *current* head. An apply therefore runs unreviewed code with the old
  plan's scope.
- **After a failed apply**: infrastructure is partly changed, so the plan
  describes a transition from a state that no longer exists. Retrying is
  permitted and applies it anyway.
- **After a timed-out apply**: the Runner may still be executing, and
  nothing prevents a second apply of the same plan on top of it.

**Why a state rather than more flags.** Expressing this with booleans needs
three — a plan exists, it is stale, and this pull request once established
one — because the third is the only thing that distinguishes "nothing was
ever planned here, so releasing evicts nobody" from "this author had a
working plan and hit a typo." A boolean that is cleared loses exactly the
history the release decision depends on. Two of the three combinations are
also meaningless. A state remembers by being a state, and repeated
failures become a self-loop rather than a contradiction.

So a held Lock occupies one of three states, and every rule in this slice
is a transition between them.

## Glossary

Terms additional to the global spec's glossary:

- **Lock_State**: the lifecycle position of a *held* Lock. The absence of
  a Lock is not a state — it is the absence of the Redis key, which is
  what `SET NX` and the compare-and-mutate script key their atomicity off.
- **Planning**: a Lock is held and no successful plan has been recorded
  against it. Nothing has been established.
- **PlanReady**: a recorded plan is believed to describe the delta between
  current infrastructure and the desired state at the current head commit.
- **PlanStale**: a plan was recorded, and something has since invalidated
  it. The Lock is still held; the plan cannot be applied.
- **Plan_Dispatch**: the moment turnip starts a plan Operation for a
  Project, before the Runner reports anything.
- **Mutating_Operation**: any Operation a Plugin exposes that is not its
  plan Operation. Defined by exclusion, as in Slice 20.
- **Applicable_Plan**: a successful plan that reported changes, or that
  reported none for a tool whose Mutating_Operations can act without them.

## Requirements

### Requirement 1: A held Lock has an explicit state

**User Story:** As a maintainer, I want the Lock's lifecycle to be one
enumerated thing, so that the rules governing it are a table rather than a
set of conditions scattered across the code that reads it.

#### Acceptance Criteria

1. THE Lock SHALL record which of Planning, PlanReady or PlanStale it is in
2. THE state SHALL be the single source of truth for whether a
   Mutating_Operation may run, replacing the present pair of conditions
3. WHERE a stored Lock carries no recognisable state — one written before
   this slice — THE Server SHALL treat it as not PlanReady, requiring a
   new plan
4. THE absence of a Lock SHALL remain the absence of its Redis key, NOT a
   state recorded inside one
5. EVERY lifecycle change SHALL be applied through the same entry point,
   the timeout path included, rather than any site deciding for itself
6. Manual unlock and pull-request close SHALL be applied as events through
   that entry point, releasing from any state
7. WHERE an event arrives for a Lock that no longer exists, THE Server
   SHALL treat the transition as a no-op rather than an error, and SHALL
   still report the Operation's own outcome

*Rationale for 1.6: these are the two sites that would otherwise keep
their own `ReleaseLock` call. They have no decision to make today — both
release from any state — which is exactly why they are easy to leave
behind, and why leaving them behind means the table is not the whole
story. A later state in which closing should not simply release has
nowhere to be expressed if two callers never consult the table.*

*Rationale for 1.7: closing a pull request while a Mutating_Operation is
in flight deletes the Lock, and the Operation's result then arrives with
nothing to transition. The result still has to reach the pull request —
the author needs to know what the apply did — so a missing Lock is an
expected outcome of a race, not a failure.*

*Rationale for 1.3: a tolerant decode rather than a migration, following
the precedent already set at `execute.go:149-151`, where the plan
requirement "turns a pre-upgrade Lock into a request to re-plan". Failing
toward re-planning costs one plan; failing the other way applies something
nobody reviewed.*

*Rationale for 1.4: key presence is the concurrency primitive. Folding
"free" into the stored value would mean the key always exists, and the
atomic acquire would have to become a read-modify-write of a value that
two Servers could race on.*

### Requirement 2: Dispatching a plan supersedes any stored plan

**User Story:** As a reviewer, I want a pushed commit to invalidate the
plan I was about to approve, so that nobody can apply a plan that no
longer describes the pull request.

#### Acceptance Criteria

1. WHEN a plan Operation is dispatched for a Project whose Lock this pull
   request holds in PlanReady, THE Lock SHALL move to PlanStale
2. THE transition SHALL occur at Plan_Dispatch, NOT when the plan's result
   arrives
3. THE transition SHALL be atomic with acquisition, so that no Operation
   can observe a Lock that has been acquired for a new plan while still
   reporting PlanReady
4. WHERE the Lock is held by a different pull request, THE Operation SHALL
   be rejected as it is today, and no state SHALL change

*Rationale for 2.2: a plan takes minutes. Transitioning on the result
leaves a window, beginning at the push and ending when the Runner reports,
in which the Lock still says PlanReady and an apply is admitted — which is
the exact case this requirement exists to prevent.*

*Rationale for 2.1: the invalidated set is the Projects being re-planned,
which `whenModified` has already decided. No comparison of head commits is
needed to reach it.*

### Requirement 3: A plan's result determines the state, and whether the Lock survives

#### Acceptance Criteria

1. WHEN a plan succeeds with an Applicable_Plan, THE Lock SHALL move to
   PlanReady and the plan SHALL be recorded
2. WHEN a plan succeeds with nothing to apply, THE Lock SHALL be released
3. WHEN a plan fails from Planning, THE Lock SHALL be released
4. WHEN a plan fails from PlanStale, THE Lock SHALL remain held in
   PlanStale
5. WHEN a plan times out, THE Lock SHALL remain held in the state it was
   already in
6. Exactly one Lock mutation SHALL occur per result

*Rationale for 3.3 versus 3.4: this is the whole eviction question.
Releasing from Planning takes nothing from anyone — nothing was ever
established. Releasing from PlanStale would evict an author who had a
working plan and pushed a typo, and would destroy the Lock along with it.*

*Rationale for 3.6: `ReleaseLock` deletes the key, and the plan record
lives inside it. Storing a plan and then releasing would write a record
that the next statement destroys, leaving a window in which a concurrent
Mutating_Operation on the same pull request could read a plan turnip had
already judged worthless.*

*Note on reachability: a plan result is only ever handled from Planning or
PlanStale, because every result is preceded by a Plan_Dispatch and
Requirement 2 moves PlanReady out of the way. PlanReady therefore never
sees a plan result, which is a property worth testing rather than
assuming.*

### Requirement 4: A Mutating_Operation runs only from PlanReady

#### Acceptance Criteria

1. THE Server SHALL admit a Mutating_Operation only where the Lock is held
   by this pull request in PlanReady
2. WHERE the Lock is in Planning or PlanStale, THE Operation SHALL be
   refused with a message saying a new plan is required
3. THE refusal SHALL distinguish "no plan has been recorded" from "the
   recorded plan is no longer valid", because the second tells the author
   something happened
4. THE selection of Projects for a bare Mutating_Operation SHALL follow the
   same state, so that a Project with an invalid plan is not silently
   selected and then refused

*Rationale for 4.4: `target.go:256` selects on `status.HasPlan` today. If
state governs admission but selection still reads a flag meaning "a plan
was recorded at some point", a bare `/turnip apply` would pick up Projects
it then rejects one by one.*

### Requirement 5: A Mutating_Operation that does not succeed invalidates the plan and keeps the Lock

#### Acceptance Criteria

1. WHEN a Mutating_Operation fails, THE Lock SHALL move to PlanStale and
   SHALL remain held
2. WHEN a Mutating_Operation times out, THE Lock SHALL move to PlanStale
   and SHALL remain held
3. WHEN a Mutating_Operation succeeds, THE Lock SHALL be released
4. A Mutating_Operation's result SHALL be applied whatever state the Lock
   is in when it arrives

*Rationale for 5.1: a failed apply leaves infrastructure partly changed,
so the plan describes a transition from a state that no longer exists.
Today the Lock keeps both its claim and its plan, so the same apply can be
retried against mutated infrastructure — reaching Terraform's own "saved
plan is stale" error, or a Helmfile re-diff nobody reviewed. The Lock must
stay held, because another pull request must not apply on top of an
unknown state; it must stop being appliable, because what it holds is no
longer true.*

*Rationale for 5.2: a timed-out Mutating_Operation needs this more than a
failed one, because the Runner may still be executing. Leaving it
appliable would permit a second apply on top of an Operation that never
reported.*

*Rationale for 5.4: a plan dispatched while a Mutating_Operation is in
flight moves the Lock to PlanStale beneath it. The result must still be
honoured — a successful apply releases, a failed one invalidates — rather
than being dropped because the state moved. This is a rare race, and
leaving it undefined is how it becomes a bug nobody can reproduce.*

### Requirement 6: A Plugin declares whether a Mutating_Operation can act without changes

#### Acceptance Criteria

1. THE `Plugin` interface SHALL expose a declaration of whether any
   Mutating_Operation it exposes can have an effect when the plan reported
   no changes
2. THE declaration SHALL be a property of the tool, NOT derived from a
   Project's configuration or from the tool's own configuration files
3. THE declaration SHALL account for every Mutating_Operation a Plugin
   exposes, NOT only its designated apply Operation
4. THE Helmfile Plugin SHALL declare that a Mutating_Operation CAN act
   without changes, because `GetOperations` exposes `sync`, which upgrades
   every release regardless of the diff

*Rationale for 6.3 and 6.4: `helmfile apply` alone would answer the other
way — it diffs first and syncs only changed releases. Deciding from the
apply Operation alone would silently remove `sync` from every Project
whose diff came back clean, and a Lock is released once, before turnip
knows which Mutating_Operation the author will choose.*

### Requirement 7: Every release and every invalidation is announced, with its reason

**User Story:** As the author of a pull request, I want to be told when a
Project's Lock is dropped or its plan stops being usable, and why, so that
I never have to infer either from the absence of something.

#### Acceptance Criteria

1. WHEN the Server releases a Lock, THE pull request SHALL state that the
   Lock was released and why
2. WHEN a stored plan is invalidated without the Lock being released, THE
   pull request SHALL state that too, and why
3. THE reason SHALL correspond to the transition taken, not to the state
   arrived at
4. WHERE a release or invalidation is attempted and fails, THE Lock SHALL
   be reported as it actually is, and nothing SHALL be announced
5. THE comment SHALL offer `unlock`, and list the Project in the "holds
   locks on …" footer, only where the Lock is actually still held
6. THE Server SHALL NOT infer Lock state from an Operation's success
7. WHERE an Operation times out and the Lock remains held, THE comment
   SHALL report it as held and offer the command that releases it

*Rationale for 7.3: several transitions arrive at the same state for
different reasons — PlanStale is reached by a push, by a failed re-plan,
by a failed apply and by a timed-out apply. "This plan is stale" is not a
useful message; "your apply failed part-way, so re-plan before trying
again" is.*

*Rationale for 7.7: this is a live defect, not a hypothetical.
`reportTimeout` builds its `ProjectResult` with `ProjectName`, `Tool`,
`Operation`, `Success` and `Output`, and never sets `Locked` — so it
defaults to false while `sweep.go:89` leaves the Lock held. Both
renderers key off that field, so a timed-out Project is absent from the
"holds locks on …" footer and is never offered `unlock`. The Lock is held
*and invisible*, recoverable only by someone who already knows to type
the command.*

*Rationale for 7.4: `HandleResult` sets `Locked` false only after
`ReleaseLock` returns without error (`result.go:157-162`). Announcing
before confirming would tell an author a Project is free while Redis still
holds it, so the next pull request's plan is rejected naming a pull
request whose comment claims it released.*

### Requirement 8: The global specification is amended, not contradicted

#### Acceptance Criteria

1. Global Requirement 7.3 SHALL be amended: a successful plan keeps its
   Lock only where it leaves something to apply
2. Global Requirement 7.4 SHALL be amended to include this slice's
   releases among the triggers that end a Lock's life
3. THE amendments SHALL be made in place, with the change recorded in the
   roadmap's Slice 18 entry, following the convention the global spec
   already uses

*Rationale: the halves stand differently. Releasing after a failed plan
fills a gap — failure appears nowhere in Requirement 7. Releasing after a
plan that found nothing contradicts 7.3, which says a plan that "completes
successfully" keeps its Lock.*

### Requirement 9: The requirement citations in the Lock code are corrected

#### Acceptance Criteria

1. THE citation "Requirement 6.6-6.8" at `result.go:45` SHALL be corrected
2. THE citation "Requirement 6.8/8.5" at `sweep.go:89` SHALL be corrected
3. No behaviour SHALL change from this correction

*Rationale: Requirement 6 is Plan with Destroy Flag and has five criteria,
none about Lock lifecycle. The wrong citation is what made this behaviour
look specified when nothing specified it — which is how it survived
unexamined.*

### Requirement 10: Documentation

#### Acceptance Criteria

1. THE documentation SHALL state the Lock's states and what moves between
   them
2. THE documentation SHALL state that pushing a commit invalidates a
   stored plan, and that a Mutating_Operation then requires a new plan
3. THE documentation SHALL state that a failed or timed-out
   Mutating_Operation requires a re-plan before it can be retried, and why

## Out of Scope

- **Recording the head commit on the Lock.** Requirement 2 reaches the
  same outcome through Plan_Dispatch without comparing commits. The
  remaining case a commit would close is two plans completing out of
  order, where the older result overwrites the newer plan. `OperationRecord`
  already carries `HeadSHA` (`execute.go:199`), so a later slice can
  refuse an out-of-order store; this one must not assume the field exists
  on the Lock.
- **Releasing a Lock under contention.** Letting another pull request
  evict a Lock, or expiring one on a timer, is a different mechanism with
  different failure modes. Every rule here decides from an Operation's own
  outcome. It is also the only mechanism that would relieve the
  many-similar-environments case for Helmfile without removing `sync`.
- **Declared values for Terraform and Pulumi.** Slice 7 introduces those
  Plugins; each answers Requirement 6 for its own tool.
- **Changing the atomicity primitive.** `SET NX` and the compare-and-mutate
  script keep deciding who holds a Lock. This slice adds a state inside
  the value they guard.
- **Concurrent Mutating_Operations on one Project.** Two applies dispatched
  from the same pull request both pass today's gate. Requirement 5.4
  defines what happens to their results, but preventing the overlap is a
  separate question.
