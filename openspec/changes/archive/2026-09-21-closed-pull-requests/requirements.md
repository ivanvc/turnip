# Requirements: Refuse a Closed Pull Request, Plan a Reopened One (Slice 32)

## Introduction

Commenting `/helmfile diff` on a **closed** pull request runs it. turnip
plans, takes a Lock, creates a Job and executes — because it has no
concept of a pull request being open or closed. `github.PullRequest`
carries `Number`, `HeadSHA`, `BaseRef`, `HeadRef`, `Draft`, `Author` and
`HeadRepo`; GitHub's payload also offers `state`, `merged`, `merged_at`
and `closed_at`, and turnip reads none of them. `HandleIssueComment` has
no state guard at any point: it authorizes the commenter, fetches the
configuration at the head SHA, resolves Targets and executes.

This is a bug, not a missing feature. Two consequences follow, and both
are reachable today.

**A Lock with no remaining lifecycle event to discharge it.** Lock release
on close happens in `handlePRClosed`, and that event has already fired by
the time anyone comments on a closed pull request. There is no second
close event. So a plan taken afterwards acquires a Lock that only
`/turnip unlock` can clear, and it blocks every other pull request from
planning that Project meanwhile.

Slice 18 narrowed this without closing it. A plan that fails with nothing
recorded now releases, and a plan that finds nothing to apply releases for
a tool that is inert without changes. But a *successful* plan still holds,
and Helmfile is never inert — `sync` acts regardless of the diff — so the
ordinary case on the pilot's own tool still strands a Lock indefinitely.

**Applying changes that were rejected.** `/turnip apply` on its own is
refused, because closing released the Lock and an apply needs one carrying
a usable plan. But `diff` *then* `apply` re-acquires, re-plans and
applies. The Runner merges base into head, so on a pull request closed
**without merging** that deploys precisely the changes someone declined to
merge. On a merged pull request it is close to inert, since head is
already in base. The rejected case is the dangerous one, and nothing
distinguishes them today.

**The whole gap is the comment path.** `HandlePullRequest` autoplans on
`opened`, `synchronize` and `ready_for_review`, routes `closed` to Lock
release, and sends every other action to `default: return nil`. The
automatic path therefore cannot reach a closed pull request at all.

**Deliberately unlike drafts.** Slice 19 decided that a draft "changes
*when* turnip acts on its own, never what it can be asked to do", because
a draft is unfinished work whose author may legitimately want a plan. A
closed pull request is finished or abandoned, and the cleanup that follows
it has already run. The reasoning that makes a draft's comment trigger
valid is exactly what makes a closed one's invalid.

## Glossary

Terms additional to the global spec's glossary:

- **Open_Pull_Request**: a pull request GitHub reports as open. A merged
  pull request and one closed without merging are both *not* open.
- **Pull_Request_State**: whether a pull request is open, as turnip
  records it.

## Requirements

### Requirement 1: A pull request's state is known where a Trigger Command is handled

**User Story:** As a maintainer, I want turnip to know whether a pull
request is still open, so that it can decline to act on one that is
finished.

#### Acceptance Criteria

1. THE `PullRequest` type SHALL carry whether the pull request is open
2. THE Server SHALL populate it from `GetPullRequest`, which is the only
   source available on the `issue_comment` path — that payload carries a
   pull request number and nothing else
3. THE Server SHALL populate it from the `pull_request` webhook payload as
   well, so the field is not silently false on the path that has it
4. A merged pull request and one closed without merging SHALL both be
   treated as not open, with no distinction between them

*Rationale for 1.2: this mirrors `HeadRepo` in Slice 15 exactly. The
`issue_comment` payload populates only `Number`, which is why `Draft` is
documented as unreadable on that path — the same constraint applies here,
and `GetPullRequest` is the same answer.*

*Rationale for 1.4: the Lock argument applies to both. A merged pull
request's Lock is as unreleasable as a rejected one's, and distinguishing
them would invite the reading that a merged pull request is safe to act
on, which is true only of the second consequence and not the first.*

### Requirement 2: No Operation runs on a pull request that is not open

**User Story:** As a developer, I do not want a stray comment on a closed
pull request to deploy anything or to strand a Project.

#### Acceptance Criteria

1. WHERE a Trigger Command names a pull request that is not open, THE
   Server SHALL refuse the Operation
2. THE refusal SHALL apply to every Operation — the plan as well as the
   mutating ones. Refusing only the mutating ones would leave the
   stranded-Lock consequence untouched, which is the half that cannot be
   recovered from without a human
3. THE refusal SHALL occur before any Lock is acquired, before any Job is
   created, and before the repository's configuration is read
4. THE refusal SHALL hold regardless of the commenter's permission level

*Rationale for 2.2: the two consequences want different guards — one is
about mutating infrastructure, the other about acquiring a Lock nothing
will release. Guarding the whole comment path is the only thing that
covers both, and it is also the simpler rule to state.*

### Requirement 3: The refusal is reported on the pull request

**User Story:** As someone who commented on the wrong tab, I want to be
told why nothing happened, so that I do not conclude turnip is broken.

#### Acceptance Criteria

1. THE Server SHALL reply on the pull request saying that it will not act
   because the pull request is closed
2. THE reply SHALL be one per Trigger Command, not one per configured
   Project
3. THE Server SHALL record the refusal in its log

*Rationale: this deliberately differs from Slice 15, which refuses fork
pull requests in silence. There, the requester may be an attacker and
feedback is something to withhold. Here the requester is almost certainly
a colleague who commented on the wrong tab; there is no attacker to starve
of information, and silence would read as turnip being broken. The
decision is made explicitly rather than inherited.*

### Requirement 4: Closing a pull request still releases its Locks

#### Acceptance Criteria

1. THE `pull_request`/`closed` path SHALL continue to release the Locks
   this pull request holds
2. THE guard SHALL NOT be placed where it can reach that path

*Rationale: the same hazard Slice 15's Requirement 2.3 exists to prevent,
and the same shape — a guard written one level too high stops the cleanup
as well as the Operation, stranding exactly the Locks it was meant to
protect. The automatic path cannot reach a closed pull request anyway, so
a guard there would be both harmful and pointless.*

### Requirement 5: Reopening a pull request plans it again

**User Story:** As a developer who reopened a pull request, I want turnip
to tell me what it would now change, without my having to push a commit
to wake it up.

#### Acceptance Criteria

1. WHEN a pull request is reopened, THE Server SHALL plan it exactly as
   though it had just been opened
2. THE plan SHALL target the Projects whose `whenModified` patterns match
   this pull request's changed files — the same set any other automatic
   plan targets, not every configured Project
3. WHERE the reopened pull request is a draft, THE Server SHALL NOT plan
   it automatically, exactly as it would not for one opened as a draft

*Rationale: `reopened` falls to `default: return nil` today, so reopening
does nothing at all — no plan, no comment, no check run — until someone
pushes. That is the mirror of this slice's other half rather than a
separate subject: both are turnip reading a pull request's state and
acting on it. Requirement 2 makes a closed pull request refuse, and this
makes an open one answer.*

*Rationale for 5.2: closing released the pull request's Locks, so a
reopened one holds nothing. Re-planning is what makes it applicable again,
and planning the matched set rather than everything keeps a reopen from
costing more than the open did.*

*Rationale for 5.3: the draft guard already sits in the arm this action
joins, so a pull request reopened as a draft is covered by existing code
rather than by a second rule. Slice 19's principle is unchanged — being a
draft decides when turnip acts on its own, and reopening is turnip acting
on its own.*

### Requirement 6: Documentation

#### Acceptance Criteria

1. THE documentation SHALL state that turnip does not act on a closed or
   merged pull request
2. THE documentation SHALL state that reopening one plans it again, for
   the Projects its changes match

## Out of Scope

- **Guarding the automatic path against a closed pull request.** It
  cannot reach one — `closed` routes to Lock release and every other
  action but the three planning ones returns nil — so a guard there would
  add a branch no event can take. Requirement 5 adds an action to that
  switch; it does not add a state check to it.
- **Distinguishing merged from closed-unmerged**, per Requirement 1.4.
- **Locks already stranded by this bug.** Existing ones are cleared with
  `/turnip unlock`; this slice stops new ones, and a migration to find old
  ones would need a Lock-listing surface that Slice 28 owns.
