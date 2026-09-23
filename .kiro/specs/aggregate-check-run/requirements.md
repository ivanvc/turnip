# Requirements: One Check Branch Protection Can Require (Slice 37)

## Introduction

turnip creates one check run per (Project, Operation), named
`turnip/<project>/<operation>` (`execute.go:366`). That is right for
detail and unusable for branch protection, because the set of Projects
differs per pull request. Mark `turnip/web/diff` required and any pull
request that does not touch `web` never reports it — and blocks forever
with nothing wrong. There is no name today that an operator can safely
require.

**What the check should assert.** turnip never applies on merge, and a pull
request's Locks are released when it merges. So a pull request that merges
before its plans are applied leaves them unapplied, with nothing left to
apply them. The check worth requiring is therefore not "every Project was
planned" but **"every plan this pull request made has been carried out"** —
applied, or found to have nothing to apply.

The shape is established among GitOps IaC tools: a per-pull-request record
of each project's latest outcome, and a check that counts how many are
applied or had nothing to apply. This slice adopts that shape and departs
from it where turnip's situation differs (Requirements 4.7, 6.3).

**Why the record and not the Lock.** A Lock answers "may another pull
request act on this Project"; it cannot answer "is this pull request done".
A Lock is released for reasons that mean opposite things — a successful
apply, a plan that found nothing, a *first* plan that failed
(`internal/lock/state.go:80`), an unlock — and a Lock another pull request
holds was never this pull request's at all. Every attempt to read the
verdict off Lock state needed a special case for one of those, so this
slice keeps the two concerns apart.

**Naming.** The aggregate is a single check run named `turnip`, reporting a
verdict on the pull request rather than the outcome of a command. turnip's
Operations are named in each tool's own vocabulary (`diff`, `plan`,
`preview`; `apply`, `sync`, `up`), so any role name — the conventional
`plan`/`apply`, or a coined synonym — either borrows one tool's word or sits
above a per-Project run it does not exclusively describe. A verdict needs
no such word.

**The per-Project name changes in the same slice**, to
`turnip/<operation>/<project>`, so that every `diff` or every `sync` lists
together. It is a breaking change for anyone who required a current name;
there is one installation, so it is nearly free now, and this slice is the
one that gives that operator a better thing to require.

This slice implements the global spec's Requirement 9 (GitHub Status
Checks). It keeps 9.5 — separate check runs per Project — and adds a check
that summarises them.

## Glossary

Terms additional to the global spec's glossary:

- **Aggregate_Check**: the single check run named `turnip`, reporting
  turnip's verdict on a pull request's head commit.
- **Project_Check**: a per-(Operation, Project) check run, under its new
  name `turnip/<operation>/<project>`.
- **Plan_Operation**: the Operation a Plugin designates as its plan
  (`GetPlanOperation`). **Mutating_Operation** is every other Operation, as
  in Slice 20 — for Helmfile both `apply` and `sync`.
- **Pull_Request_Record**: for one pull request at one head commit, every
  Project an Operation has run for, with that Project's latest Outcome.
- **Outcome**: what the Pull_Request_Record holds per Project — one of the
  values in Requirement 4.
- **Done**: an Outcome that needs nothing further before merge — applied,
  or nothing to apply.

## Requirements

### Requirement 1: One stable name an operator can require

**User Story:** As an operator, I want one check name I can mark required
in branch protection, so that turnip can keep a pull request from merging
with its plans unapplied, without my predicting which Projects it touches.

#### Acceptance Criteria

1. THE Server SHALL report the Aggregate_Check under the name `turnip`, on
   the pull request's head commit
2. THE Aggregate_Check's name SHALL NOT vary with the Projects, the tools
   or the Operations involved

*Rationale for 1.2: the name is the check's identity — required status
checks match on it — so anything variable in it recreates the problem this
slice fixes. What changes between runs belongs in the title and summary,
which nothing matches on.*

### Requirement 2: Per-Project checks are named Operation-first

#### Acceptance Criteria

1. THE Server SHALL name each Project_Check `turnip/<operation>/<project>`,
   where `<operation>` is the tool's own Operation name
2. THE Server SHALL continue to create one Project_Check per (Operation,
   Project), as Requirement 9.5 of the global spec requires
3. THE rename SHALL apply at every site that creates or updates a
   Project_Check, so that a check created under one name is never updated
   under the other

*Rationale for 2.1: the native Operation name stays in the Project_Check —
that is the level at which a reader wants to know whether it was `diff` or
`sync` — and only the Aggregate_Check, which spans tools, gives it up.*

*Rationale for 2.3: an update carries the name alongside the check run's
ID. Four sites set it today (`execute.go:212`, `execute.go:298`,
`result.go:87`, `sweep.go:108`); one left behind would rename a run
mid-flight.*

### Requirement 3: A Pull_Request_Record, separate from the Lock

#### Acceptance Criteria

1. THE Server SHALL keep a Pull_Request_Record per pull request and head
   commit
2. WHEN an Operation for a Project finishes or is refused, THE Server SHALL
   set that Project's Outcome in the record, leaving every other Project's
   Outcome unchanged
3. A Project SHALL enter the record when any Operation is run or refused
   for it on that commit — whether triggered automatically or by comment,
   and whether or not the pull request's changes affect it
4. WHEN the pull request's head commit changes, THE Server SHALL begin a new
   record, carrying no Outcome over from the previous commit
5. Releasing a Lock — by `/turnip unlock` or otherwise — SHALL NOT change
   the record
6. THE record SHALL be readable by every Server instance, and concurrent
   updates for different Projects of the same pull request SHALL NOT lose
   one another

*Rationale for 3.2: the merge is what lets a comment that applies one
Project leave the rest standing. It is also what keeps a narrower re-plan
from overwriting a failure it never looked at.*

*Rationale for 3.3: a Project planned by name that the change does not
touch still took a Lock and still has a plan waiting, and counting it
needs no rule to tell the cases apart.*

*Rationale for 3.4: check runs attach to a commit, so the Aggregate_Check
on a new head starts empty regardless; the record starting afresh keeps the
two in step. A Lock's recorded plan goes stale on a new commit for the same
reason (Slice 18).*

*Rationale for 3.5: unlocking is how someone abandons a plan, not how they
carry it out. Letting it pass the check would let any collaborator satisfy
a required check with a comment.*

*Rationale for 3.6: Server instances share nothing in process (the global
design's High Availability Design), and the Operations of one trigger
finish on whichever instance receives their Runner's result.*

### Requirement 4: What each Outcome means

#### Acceptance Criteria

1. A Plan_Operation that succeeded and left something to apply SHALL record
   **awaiting apply**
2. A Plan_Operation that succeeded with nothing to apply SHALL record
   **nothing to apply**, which is Done
3. THE distinction in 4.1–4.2 SHALL be the one the Lock lifecycle already
   draws — `EventPlanApplicable` versus `EventPlanNothingToApply`
   (`lockstate.go:31`) — including a Plugin's `ActsWithoutChanges`
4. A Plan_Operation that failed, timed out, or was refused — including
   because another pull request holds the Project's Lock — SHALL record
   **not planned**
5. A Mutating_Operation that succeeded SHALL record **applied**, which is
   Done
6. A Mutating_Operation that failed or timed out SHALL record **apply
   failed**
7. A Mutating_Operation refused before it ran — no plan recorded, a stale
   plan, arguments of its own — SHALL NOT change the Project's Outcome

*Rationale for 4.3: whether a plan with no changes still needs an apply
depends on the tool — Helmfile's `sync` acts regardless of the diff, so a
Helmfile Project keeps its Lock where a Terraform one releases. That answer
already exists in one place, and a second copy of it is how the two would
drift.*

*Rationale for 4.4: a Project whose plan never succeeded, for whatever
reason, has nothing that could be applied. Recording it keeps it in the
record, where it holds the check back; not recording it would let it drop
out of the count, which is how a Project blocked by another pull request's
Lock would otherwise merge unplanned.*

*Rationale for 4.6: a timed-out Mutating_Operation may still be running,
and a failed one may have changed infrastructure part-way; the Lock treats
both as the same, and so does this.*

*Rationale for 4.7, which departs from established practice — elsewhere a
refused apply is commonly recorded as an apply error, turning the check
red. A refused
Mutating_Operation did not run and changed nothing, and the author's
remedy is to re-plan — which is itself recorded. Marking it failed would
put a red check on a pull request whose infrastructure is untouched.*

### Requirement 5: The verdict

#### Acceptance Criteria

1. WHEN any Project's Outcome is **apply failed**, THE Aggregate_Check SHALL
   be completed with conclusion `failure`
2. WHEN every Project in the record is Done, THE Aggregate_Check SHALL be
   completed with conclusion `success`
3. OTHERWISE THE Aggregate_Check SHALL be not completed
4. THE Aggregate_Check's title SHALL count — how many Projects are Done,
   out of how many are in the record
5. THE Aggregate_Check's summary SHALL list each Project in the record with
   its Outcome and the name of its Project_Check
6. `failure` SHALL be reported for no reason other than 5.1 and
   Requirement 7

*Rationale for 5.3 and 5.6: a red check is how reviewers triage — a pull
request showing a failure reads as "the author is still fixing this", and
gets skipped. A plan awaiting its apply, a Project waiting on another pull
request's Lock, and a plan that has not yet succeeded are not that. The
first is exactly the state a pull request is in while under review. A check
that is not completed blocks a required check without claiming anything
false.*

*Rationale for 5.4: counting composes when Projects disagree ("2/3
applied") where a single word would not. Nothing to apply counts as Done.
The exact string is Slice 35's to format; this slice
fixes only that it counts.*

### Requirement 6: When the Aggregate_Check is published

#### Acceptance Criteria

1. THE Server SHALL NOT publish the Aggregate_Check for a commit until
   either a Mutating_Operation has run on that commit or the verdict is
   completed (`success`, `failure` or `skipped`)
2. ONCE published for a commit, THE Aggregate_Check SHALL be updated
   whenever the record changes
3. WHEN the automatic plan finds no Project affected, THE Server SHALL
   publish the Aggregate_Check as completed with conclusion `skipped`, with
   a title saying no Project was affected

*Rationale for 6.1: before any apply, the pull request is under review, and
a required check that has not reported shows as "Expected — waiting for
status to be reported": blocking, and not red. It is the quietest honest
signal available. The exception for a completed verdict covers a
configuration failure, which no apply will follow, and a pull request whose
every plan found nothing to apply, which no apply will ever arrive to
publish.*

*Rationale for 6.3: without it, requiring `turnip` would block every pull
request that touches no Project, forever. GitHub counts `skipped` as
satisfying a required check, and here it is true: there was nothing to
plan. `success` would pass too; `skipped` says what happened. It does not conflict with Slice 36's rule against
`skipped`: that concerns "turnip could not plan this", where passing would
let an unplanned change merge.*

### Requirement 7: An invalid configuration fails the check

#### Acceptance Criteria

1. WHEN the automatic plan finds a `turnip.yaml` that is present but
   invalid, THE Server SHALL publish the Aggregate_Check as completed with
   conclusion `failure`
2. WHEN no `turnip.yaml` exists, THE Server SHALL NOT publish the
   Aggregate_Check
3. WHEN a Project the pull request's changes affect names a tool this
   Server has no Plugin for, THE Server SHALL treat it as a configuration
   issue: the Aggregate_Check SHALL be completed with conclusion `failure`,
   naming the Project and the tool
4. A Project naming such a tool that the pull request's changes do not
   affect SHALL NOT affect the Aggregate_Check

*Rationale for 7.1: unlike everything Requirement 5.3 keeps from turning
red, this is the author's to fix, and it fixes by changing the pull
request. Today it produces a comment and no check, so a pull request
requiring `turnip` would block with the reason only in the comment.*

*Rationale for 7.3: today the automatic plan drops such a Project silently
(`target.go:54`). Left that way it would never enter the record, and
`turnip` could pass with the change unplanned. Recording it as merely not
planned would block the merge with no stated reason. The configuration
asks this Server for something it cannot do, which is the same kind of
problem as an invalid `turnip.yaml`, and is reported the same way.*

*Rationale for 7.4: a Project the change does not touch has nothing to
plan, so a tool turnip cannot run there is no reason to fail an unrelated
pull request. It fails the first pull request that does touch it.*

*Rationale for 7.2: a repository with no configuration has not opted in,
and the automatic path is deliberately silent there (`pullrequest.go`).
Requiring `turnip` on such a repository is a documentation matter
(Requirement 9).*

### Requirement 8: The verdict is never stranded

#### Acceptance Criteria

1. THE Aggregate_Check SHALL be published and updated from the
   Pull_Request_Record, by whichever Server instance records an Outcome —
   not held by the instance that received the trigger
2. WHEN the Aggregate_Check cannot be created or updated, THE Operations it
   covers SHALL still run, and the pull request comment SHALL say that the
   check is missing and why

*Rationale for 8.1: a Project_Check survives its creating instance because
the sweep finalises it from the Operation Record. An Aggregate_Check held
only by the goroutine waiting in `executeTargets` would stay in progress
forever if that instance stopped — on a required check, blocking the merge
with no remedy but a new push.*

*Rationale for 8.2: the soft-failure rule `appendCheckRunNote` already
applies to a Project_Check.*

### Requirement 9: Documentation

#### Acceptance Criteria

1. THE documentation SHALL state that `turnip` is the check to require in
   branch protection, and that Project_Checks should not be required
2. THE documentation SHALL state what `turnip` reports: absent until the
   first apply; not completed while any plan awaits its apply;
   `success` once every plan is applied or had nothing to apply; `failure`
   when an apply failed, `turnip.yaml` is invalid, or an affected Project
   names a tool this Server cannot run; `skipped` when no
   Project is affected
3. THE documentation SHALL state that unlocking does not satisfy the check
4. THE documentation SHALL state that requiring `turnip` on a repository
   with no `turnip.yaml` blocks every pull request
5. THE release notes SHALL state that Project_Checks were renamed from
   `turnip/<project>/<operation>` to `turnip/<operation>/<project>`, and
   that any branch protection naming the old form must be changed

## Out of Scope

- **A plan-only gate.** turnip publishes one verdict, and a plan-only one would let a pull request merge
  with its plans unapplied and nothing left to apply them.
- **How a refusal is presented** in the checks list, including whether a
  refused Operation gets a Project_Check at all. Slice 36; this slice only
  records the refusal's Outcome.
- **Title wording.** Slice 35 sweeps every title onto one formatter, and
  follows this slice so that the sweep covers the Aggregate_Check.
- **Pull requests turnip refuses outright** — forks (Slice 15), closed pull
  requests (Slice 32), drafts (Slice 19). None gets an Aggregate_Check. A
  draft cannot merge and plans on `ready_for_review`; a fork blocked by a
  required `turnip` blocks without saying why, which is Slice 36's
  territory.
- **Per-repository configuration** of the check's name or meaning.
