# Requirements: A Blocked Operation Blocks the Merge, Visibly (Slice 36)

## Introduction

When turnip refuses a plan, the pull request is correctly kept from
merging — Slice 37 records the Project as not planned, so the `turnip`
check cannot pass — but nothing in the checks list says why. Every refusal
in `executeOne` returns before the Project_Check is created, and `turnip`
itself does not appear until the first apply. A plan blocked by another
pull request's Lock is explained only in the comment.

Two refusals are also reported inconsistently with the rule Slice 37
settled — red means the author has something to fix by changing the pull
request:

- **A refused override** (`runner.serviceAccount` or `clone.submodules`
  not permitted by `TURNIP_ALLOWED_OVERRIDES`) is the author's to fix, yet
  `turnip` records it as not planned — in progress, never red. Slice 37
  already treats the closest case, an affected Project using an
  unsupported tool, as a configuration failure.
- **Two of turnip's own errors** leave the Project_Check in progress
  forever: a failure saving the Operation Record (`execute.go:278`) or
  building the Runner Job (`execute.go:313`) returns without completing
  the check that was just created.

**Other GitOps IaC tools report a lock held elsewhere as a failure**, with
a generic description ("Plan failed.") and the reason only in the
comment. This slice does not follow that: a Lock held by another pull
request is not something this pull request's author can fix, and a red
check is how reviewers decide a pull request is not ready to look at.

**The governing rule is unchanged**: turnip never reports a verdict that
lets a pull request merge while its infrastructure is unplanned. Nothing
here reports `success`, `neutral` or `skipped` for a refusal.

This slice builds on Slice 35's title formatter (`titles.go`) and Slice
37's Pull_Request_Record and verdict.

## Glossary

Terms additional to Slices 35 and 37:

- **Lock_Wait**: a plan refused because another pull request holds the
  Project's Lock.
- **Configuration_Refusal**: a plan refused because `turnip.yaml` asks for
  an override the Server does not permit.
- **Infrastructure_Error**: a plan that failed on turnip's own side —
  Redis, the Operation Record, building the Runner Job — with nothing wrong
  in the pull request.

## Requirements

### Requirement 1: A Lock_Wait is visible and not red

**User Story:** As a pull request author, I want to see in the checks list
that my plan is waiting on another pull request's Lock, and what to do,
without my pull request looking broken.

#### Acceptance Criteria

1. WHEN a plan is refused as a Lock_Wait, THE Server SHALL create the
   Project_Check with status `queued` and no conclusion
2. ITS Title SHALL name the blocking pull request and the remedy:
   `locked by PR #5, re-plan once it's released`
3. WHERE the holder cannot be determined, THE Title SHALL be
   `locked by another pull request, re-plan once it's released`
4. THE Pull_Request_Record SHALL record the Project as not planned, as
   Slice 37 does today, together with the blocking pull request's number,
   and THE Aggregate_Check's summary SHALL show it
   (`web`: not planned, locked by PR #5)

*Rationale for 1.1: a check that is not completed blocks a required check
without claiming anything false. `failure` was considered — it is what
other tools do — and rejected: it says the change is broken, sending the
author to debug a diff that is fine, and it would disagree with the
`turnip` check, which Slice 37 keeps from turning red for a wait.*

*Rationale for 1.2: turnip does not re-plan when another pull request's
Lock frees, so the Title has to say what the reader must do. "Once it's
released" rather than "once it merges": a Lock is released when the
holder applies successfully, merges, closes, or is unlocked (Slice 18).*

### Requirement 2: A later plan replaces the queued check

#### Acceptance Criteria

1. WHEN a later plan runs for the same Project on the same commit, THE
   Server SHALL create a new Project_Check as it does for any plan
2. THE Server SHALL NOT track the queued check run to update it

*Rationale: GitHub evaluates the most recently updated run of a name,
which Slice 37 already relies on for the `turnip` check. Tracking the
queued run would add state per Project and commit to remove an entry
visible only in the commit's full check history, not in the pull
request's checks list. A push starts a new commit, leaving the queued run
on the commit it describes.*

### Requirement 3: A Configuration_Refusal fails, like an unsupported tool

#### Acceptance Criteria

1. WHEN a plan is refused as a Configuration_Refusal, THE Server SHALL
   create the Project_Check completed with conclusion `failure`
2. ITS Title SHALL name the setting: `runner.serviceAccount is not
   permitted`, `clone.submodules is not permitted`
3. THE Pull_Request_Record SHALL record a new Outcome, **refused**, which
   fails the Aggregate_Check as an unsupported tool does
4. THE Aggregate_Check's Title SHALL name the first refused Project and
   setting and count the rest, as Slice 35's unsupported-tool Title does:
   `not permitted: web sets runner.serviceAccount, and 1 more`
5. THE full refusal — which setting, and that the Server's
   `TURNIP_ALLOWED_OVERRIDES` must include it — SHALL remain in the
   comment and in the check's summary

*Rationale: the author can fix it by changing the pull request, and
Slice 37 settled that such a problem is red in both places. Red in one and
in progress in the other would be the disagreement Requirement 1 avoids.*

### Requirement 4: Infrastructure_Errors fail the run, not the pull request

#### Acceptance Criteria

1. WHEN a plan fails with an Infrastructure_Error, THE Project_Check SHALL
   be completed with conclusion `failure`, creating it first where it does
   not yet exist
2. ITS Title SHALL name turnip's own cause:

   | Where | Title |
   |---|---|
   | acquiring the Lock (`execute.go:167`) | `lock could not be acquired` |
   | saving the Operation Record (`execute.go:278`) | `operation could not be recorded` |
   | building the Runner Job (`execute.go:313`) | `Runner Job could not be built` |

3. THE Pull_Request_Record SHALL record the Project as not planned, so the
   Aggregate_Check stays in progress
4. NO path SHALL leave a Project_Check it created in progress

*Rationale for 4.1 and 4.3: this follows the precedent Slice 35 set for
`Runner Job could not be created`. The Project_Check reports what
happened to this run, and the run failed; the Aggregate_Check reports who
must act for the pull request to merge, and for an Infrastructure_Error
that is no one in particular — a re-plan will probably succeed. That is
different from a Lock_Wait, where the run did not fail at all.*

*Rationale for 4.4: `execute.go:278` and `:313` are the two paths that
break it today; the requirement is the invariant, so a path added later
is held to it too.*

### Requirement 5: Titles from the shared formatter

#### Acceptance Criteria

1. EVERY Title this slice introduces SHALL come from `titles.go`
2. Titles SHALL follow Slice 35's rules: plain text, no `·`, and never a
   bare status word

### Requirement 6: Only for pull requests turnip acts on

#### Acceptance Criteria

1. A Project_Check for a refusal SHALL be created only for a refusal of an
   individual Target
2. Refusals of the whole trigger — a fork (Slice 15), a closed pull request
   (Slice 32), a commenter without access (Slice 23) — SHALL continue to
   create no check run

*Rationale: check runs attach to a commit, not a pull request. On a
merge-commit strategy the head of a closed pull request can be an ancestor
of the base branch, and a check run created there would write into the
base branch's history for a run that never happened. Whole-trigger
refusals return before any Target is resolved, so this holds by structure;
the requirement is to keep it that way.*

### Requirement 7: What does not change

#### Acceptance Criteria

1. A refused Mutating_Operation — no plan, a stale plan, arguments of its
   own — SHALL create no Project_Check and SHALL NOT change the
   Pull_Request_Record, as Slice 37 established
2. An affected Project whose tool has no Plugin SHALL remain handled by
   Slice 37's unsupported-tool path: it has no Operation, so no
   Project_Check name exists for it

### Requirement 8: Documentation

#### Acceptance Criteria

1. `docs/usage.md` SHALL show the queued Project_Check and explain that
   turnip does not re-plan on its own when the Lock is released
2. `docs/troubleshooting.md` SHALL list each refusal Title and what to do

## Out of Scope

- **Re-planning automatically when a Lock is released.** The queued Title
  tells the reader to re-plan; doing it for them is a separate feature,
  with its own questions about who triggered the Operation.
- **Notifying the waiting pull request** when the Lock is released.
- **Seeing and dropping Locks** across pull requests — Slice 28.
