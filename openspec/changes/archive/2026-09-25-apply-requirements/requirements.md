# Requirements: What a Pull Request Must Satisfy Before a Mutating Operation (Slice 34)

## Introduction

turnip admits a Mutating_Operation on two facts: the commenter holds write
permission (`comment.go`'s authorization), and the Project's Lock is in
`StatePlanReady` (Slice 18). It asks nothing about the pull request's
standing. Nobody need have approved the change, and it need not be
mergeable — a single collaborator can plan and apply their own pull
request with no second pair of eyes.

For most repositories that is the opposite of why a review process exists.
Other GitOps IaC tools have the control and it is worth porting.

**What other GitOps IaC tools do** (researched 2026-09-21): three
requirements, settable per command. `approved`: approved by at least one
person other than the author. `mergeable`: the pull request is able to be
merged. `undiverged`: no changes on the base branch since the most recent
plan. All three are **opt-in**; none is on by default. A repository's own
configuration cannot set them unless the server-side configuration permits
it.

**One trap is already known and must be designed around.** GitHub's
`mergeable_state` folds in required status checks, and the tools that use
it ship a switch to bypass that. turnip's `turnip` check (Slice 37) is the
one an operator marks required under branch protection, and by design it
cannot pass until every plan this pull request made is applied. Asking
whether the pull request is `clean` therefore asks a question whose answer
includes turnip's own: the pull request cannot be `clean` until an apply
runs, and the apply will not run until it is `clean`.

## Glossary

Terms additional to the global spec's glossary:

- **Mutating_Operation**: any Operation a Plugin exposes that is not its
  plan Operation. Defined by exclusion, as in Slice 20.
- **Mutation_Requirement**: a condition a pull request must satisfy before
  turnip will run a Mutating_Operation on it.
- **Requirement_Set**: the Mutation_Requirements an operator has enabled.
  Empty by default.

## Requirements

### Requirement 1: An operator can require conditions before a Mutating_Operation

**User Story:** As an operator, I want to require that a change was
approved before turnip will apply it, so that turnip cannot be used to
bypass the review process the repository already has.

#### Acceptance Criteria

1. THE Server SHALL read its Requirement_Set from its own configuration,
   as a list of named requirements
2. THE default Requirement_Set SHALL be empty, which is turnip's present
   behavior
3. WHERE the configuration names a requirement turnip does not recognize,
   THE Server SHALL fail to start
4. THE recognized names SHALL be reported in that failure

*Rationale for 1.3: the same reasoning `parseAllowedOverrides` already
records for override paths — accepting an unrecognized value "would gate
nothing while looking like it gated something". An operator who misspells
a requirement has asked for a control and received none, and a startup
failure is the only report they will certainly read.*

### Requirement 2: The Requirement_Set is operator-side only

**User Story:** As an operator, I do not want a pull request to be able to
relax the conditions it must itself satisfy.

#### Acceptance Criteria

1. THE Requirement_Set SHALL NOT be settable from `turnip.yaml`
2. THE Requirement_Set SHALL NOT be added to the override paths a
   repository may be permitted to set
3. `knownOverridePaths` SHALL remain unchanged by this slice

*Rationale: this deliberately breaks turnip's established pattern, where
an operator default is paired with a repository-side override gated by
`TURNIP_ALLOWED_OVERRIDES`. `runner.serviceAccount` and
`clone.submodules` are legitimately gateable — an operator may have reason
to let a repository choose, and the worst case is scoped to what that
choice can reach. A requirement is different in kind: its entire purpose
is to constrain the pull request, and the pull request supplies the
configuration file.*

*Merely offering it as a gateable path is the trap. An operator who adds
it to the permitted list has silently disabled the control for anyone who
can edit `turnip.yaml`, and nothing in the mechanism would hint that this
key is unlike its neighbors.*

### Requirement 3: `approved` means someone other than the author signed off

#### Acceptance Criteria

1. WHERE `approved` is in the Requirement_Set, THE Server SHALL require at
   least one approving review on the pull request
2. THE approval SHALL be from an account other than the pull request's
   author
3. THE approval SHALL be from an account with write permission on the
   repository, as a commenter needs to trigger an Operation
4. A review that was approving and has since been dismissed or superseded
   by a later `CHANGES_REQUESTED` review from the same account SHALL NOT
   count (a `COMMENTED` review does not supersede it, as in GitHub's own
   review decision)

*Rationale for 3.2: an approval a change's own author supplied is not a
second pair of eyes, and other GitOps IaC tools draw the same line. turnip already
records the author on `PullRequest` (Slice 15), so the comparison needs no
new field.*

*Rationale for 3.3: GitHub accepts an approving review from anyone who
can read the repository, which on a public repository is anyone; it marks
such a review non-binding, but the review list still reports it as
`APPROVED`. Without this criterion a stranger, or the author's second
account, satisfies the gate. Write permission is the line turnip already
draws for triggering an Operation, and it is what "someone who could have
merged this signed off" means in practice. Deferring to GitHub's own
review decision was considered and rejected: it is empty unless branch
protection requires reviews, which would make this gate depend on a
setting turnip neither reads nor controls.*

5. AN approval SHALL count whichever commit it was given on, for as long
   as GitHub reports it as approving

*Rationale for 3.5: whether an approval survives a new push is the
repository's own policy, set by branch protection's "Dismiss stale pull
request approvals when new commits are pushed". With it on, GitHub marks
the old approval dismissed and 3.4 excludes it; with it off, the approval
stands, and GitHub would let the pull request merge on it too. Following
GitHub means turnip applies exactly when the repository's review rules
would let the change merge: it cannot be used to get around them, and it
does not impose a stricter rule the repository chose not to enable.
Counting only approvals on the head commit was considered and rejected
for that reason; a repository that wants it has the setting.*

*Rationale for 3.4: GitHub keeps a review history rather than a single
verdict per reviewer, so the latest state per account is the question, not
whether an approval ever existed.*

### Requirement 4: `mergeable` means no merge conflict, and nothing about status checks

#### Acceptance Criteria

1. WHERE `mergeable` is in the Requirement_Set, THE Server SHALL require
   that GitHub reports the pull request as mergeable
2. THE Server SHALL use the conflict-only signal, NOT a state that
   incorporates required status checks
3. WHERE GitHub has not yet computed mergeability, THE Server SHALL read
   it again for a bounded time, well within GitHub's webhook delivery
   timeout
4. WHERE it is still not computed after that, THE Server SHALL refuse the
   Operation and say that the answer is not yet available, rather than
   treating unknown as either satisfied or failed

*Rationale for 4.2: this is what avoids the deadlock described in the
introduction, and it avoids it by construction rather than by a bypass
switch. With the conflict-only signal, `mergeable` means "no merge
conflict" and `approved` means "someone signed off"; the two compose, and
neither asks GitHub a question whose answer depends on turnip's own check
runs.*

*Rationale for 4.3 and 4.4: GitHub computes mergeability asynchronously,
so the field is null on a freshly opened or freshly pushed pull request,
and reading the pull request is what starts the computation, which
usually finishes within a second or two. Refusing at once would turn
away the likeliest case, an apply right after a push, for an answer about
to arrive; a short, bounded wait covers it with no action from the
author. The bound keeps the wait inside the webhook delivery the gate
runs in. Treating null as satisfied would make the requirement silently
skippable by acting quickly; treating it as failed would report a
conflict that may not exist. Neither is true, and the honest answer is to
say so and let the author retry.*

### Requirement 5: A refused Operation says which requirement it failed

#### Acceptance Criteria

1. THE Server SHALL reply on the pull request naming the requirement that
   was not satisfied
2. WHERE more than one requirement is unsatisfied, THE reply SHALL name
   them all, so that a second attempt is not needed to discover the second
   reason
3. THE reply SHALL be one per Trigger Command, not one per Project
4. THE Server SHALL record the refusal in its log

5. THE refusal SHALL create no Project_Check and SHALL NOT change the
   Pull_Request_Record, as for every refused Mutating_Operation (Slice 36,
   Requirement 7.1)

*Rationale for 5.5: the `turnip` check already blocks the merge while a
plan awaits its apply, which is exactly the state a refused apply leaves.
A red check would say the change is broken when it is only unapproved.*

*Rationale for 5.1–5.4: as with Slice 32's closed-pull-request refusal, the requester
is a colleague rather than an attacker, so silence would read as turnip
being broken. Naming every unmet requirement at once follows from the same
courtesy — discovering them one round trip at a time is its own annoyance.*

### Requirement 6: The gate applies to every Mutating_Operation, and only to those

#### Acceptance Criteria

1. THE Requirement_Set SHALL be evaluated for every Mutating_Operation a
   Plugin exposes, not only the designated apply
2. THE Requirement_Set SHALL NOT be evaluated for a plan Operation
3. THE Requirement_Set SHALL NOT be evaluated for `unlock`
4. THE evaluation SHALL occur once per Trigger Command, not once per
   Project

*Rationale for 6.1: `sync` changes infrastructure as surely as `apply`
does, and Slice 18 already learned that naming the apply alone is the
regression worth guarding against.*

*Rationale for 6.2: a plan is how someone finds out what a change would
do, and requiring approval before it would make review impossible —
nobody can approve what they cannot see. Other GitOps IaC tools keep
plan requirements apart from apply requirements for this reason; this
slice implements only the latter.*

*Rationale for 6.3: releasing a Lock is how someone recovers from a pull
request that cannot satisfy the requirements, so gating it would strand
exactly the cases the gate creates. It also needs no special case:
`HandleIssueComment` handles `unlock` before Target resolution, and it is
never a Plugin Operation.*

*Rationale for 6.4: both requirements need a per-pull-request API call.
Evaluating per Project would repeat it once for every Project a fleet-shaped
repository selects, to answer a question that cannot differ between them.*

### Requirement 7: Documentation

#### Acceptance Criteria

1. THE documentation SHALL state which requirements exist, that they are
   off by default, and how an operator enables them
2. THE documentation SHALL state that a repository cannot set or relax
   them, and why
3. THE documentation SHALL state that `mergeable` concerns merge conflicts
   and not status checks, so that an operator does not expect it to
   enforce branch protection
4. THE documentation SHALL state that `approved` follows GitHub's review
   state: an approval given before a later push still counts unless
   branch protection dismisses stale approvals, and that enabling that
   setting is how a repository requires approval of the commit being
   applied
5. THE documentation SHALL state that an approval counts only from an
   account with write permission, other than the author
6. `SECURITY.md` SHALL state that without `approved`, one collaborator can
   plan and apply their own pull request, and name the setting that
   prevents it

## Out of Scope

- **`undiverged`.** It needs the base commit recorded at plan time, which
  the Lock does not carry. It is also the cheap, deadlock-free
  approximation of the plan-freshness check the roadmap's Backlog records
  as accepted-and-declined, so picking it up should revisit that entry
  rather than arrive independently.
- **Requirements on the plan.** Other GitOps IaC tools offer them;
  Requirement 6.2 explains why this slice does not implement them.
- **Per-repository requirements.** One Requirement_Set applies to every
  repository this Server serves. Expressing it per repository needs the
  Backlog's per-repository server configuration, which is deliberately not
  built for a single setting.
- **Enforcing branch protection.** turnip reports what GitHub tells it; it
  does not evaluate protection rules, and Requirement 4.2 keeps it out of
  that business on purpose.
- **Auto-merge after a successful apply.** A different feature, and
  nothing here depends on it.
