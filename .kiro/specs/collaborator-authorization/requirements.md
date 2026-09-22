# Requirements: Authorize on Permission Level, Not Call Success (Slice 23)

## Introduction

`Authorizer.IsCollaborator` returns true whenever the GitHub API call
merely *succeeds*:

```go
func (a *Authorizer) IsCollaborator(ctx context.Context, owner, repo, username string) (bool, error) {
	if _, err := a.permission(ctx, owner, repo, username); err != nil {
		return false, err
	}
	return true, nil
}
```

It discards the permission string it just fetched, so the gate admits
**anyone GitHub will answer about at all**. The call underneath is
`GetCollaboratorPermission` → `Repositories.GetPermissionLevel` →
`GET /repos/{owner}/{repo}/collaborators/{username}/permission`, which
reports an access level in a 200 body. Whatever that level is — `none`,
`read`, or anything else — it arrives as a success, and a success is all
this code tests.

That statement needs no claim about which values GitHub returns, which
matters because two such claims were checked while writing this and
**neither could be confirmed** against the documentation: that the
endpoint reports `none` for a non-collaborator, and that it reports `read`
for any user on a public repository. Both are plausible and both appear in
the roadmap entry; neither is load-bearing here, and the design must not
come to rest on them.

**The premise is written down, and it is wrong.**
`github-integration/design.md:387` justifies the single cached call with
"GitHub's API returns 404 for a non-collaborator on that endpoint". That
204/404 behaviour belongs to the *other* endpoint,
`/collaborators/{username}` — which this codebase already implements
correctly as `Client.IsCollaborator` and never calls from the Authorizer.
The tests pass because they encode the same assumption: the
non-collaborator fixture returns an *error*, and nothing asserts that a
`none` permission yields false.

**The deeper error is the question, not the threshold.** The permission
endpoint answers *"what access does this user have?"* On a public
repository every user legitimately has read access, so any answer it gives
conflates "can read this repository" with "is trusted to make turnip run
something". `Client.IsCollaborator` answers *"is this person a
collaborator"*, which is the question the gate means to ask.

**What the gate admits, once corrected.** A plan requires Collaborator
status at *any* level — read included. A Mutating_Operation and `unlock`
require write, maintain or admin, and `triage` is insufficient. Combined
with the glossary's note above, a plan on an organization repository with
a default member permission is triggerable by every member of the
organization. That is what "collaborator" means to GitHub and this slice
does not narrow it; narrowing it is a requirement on the trigger, which is
Slice 34's subject.

**What it reaches.** The collaborator check is the only gate on the
comment path for a plan: the write-permission check applies to apply, sync
and unlock, and `HasWritePermission` is correct — `permissionRank` maps
`none` to 1 and the comparison is `>= write`. So mutating Operations stay
protected. A plan does not, and a plan carries Project selection across
every Project in `turnip.yaml` and trailing arguments that reach the
tool's argv, which is where this compounds with Slice 22.

**Scope honestly.** Latent on a private repository, where anyone who can
comment already has read access and would pass a correct check anyway. It
matters on the first public installation, and it is an unambiguous
violation of global Requirement 16.1/16.2 either way.

## Glossary

Terms additional to the global spec's glossary:

- **Collaborator**: an account GitHub reports as a collaborator on the
  repository — the question `/collaborators/{username}` answers with 204
  or 404. Wider than the word suggests: GitHub's documentation states that
  for an organization-owned repository this includes outside
  collaborators, organization members who are direct collaborators,
  members with access **through team membership**, members with access
  **through default organization permissions**, and organization owners.
  Where an organization grants its members a default permission, every
  member is therefore a Collaborator on every repository.
- **Permission_Level**: the string
  `/collaborators/{username}/permission` returns: `none`, `read`,
  `triage`, `write`, `maintain` or `admin`.

## Requirements

### Requirement 1: The collaborator gate asks the question it means

**User Story:** As a maintainer, I want turnip to run nothing for someone
who is not a collaborator, so that the ability to comment is not the
ability to make turnip act.

#### Acceptance Criteria

1. THE Authorizer SHALL determine Collaborator status from the endpoint
   that answers it, NOT by observing that a Permission_Level lookup
   succeeded
2. THE Authorizer SHALL NOT decide Collaborator status by comparing a
   Permission_Level against a threshold
3. WHERE GitHub reports the account is not a Collaborator, THE Server
   SHALL refuse the Trigger Command exactly as it refuses one from a
   non-collaborator today

*Rationale for 1.2: the tempting fix is `permissionRank[perm] >=
permissionRank["read"]`, because `permissionRank` already exists and
already maps `none` to 1. It would close the `none` case and still be
wrong in kind: the endpoint reports what access an account has, and on a
public repository read access is universal, so a threshold cannot separate
"can read this repository" from "may make turnip run something". The fix
is the endpoint, not the threshold.*

*Note for the design: the roadmap's two supporting claims — that the
permission endpoint reports `read` for any user on a public repository,
and that it reports `none` for a non-collaborator — were both checked
against GitHub's documentation and **neither was confirmed**. Requirement
1.2 deliberately does not depend on either: the argument rests on what
question the endpoint answers, not on what values it returns.*

### Requirement 2: An answer turnip cannot interpret denies

#### Acceptance Criteria

1. WHERE the authorization lookup fails, THE Server SHALL refuse the
   Trigger Command
2. WHERE GitHub returns a Permission_Level turnip does not recognise, THE
   write-permission check SHALL treat it as insufficient
3. No authorization decision SHALL be reached by a path that treats an
   absent or unreadable answer as permission granted

*Rationale for 2.1: this is already today's behaviour and is stated so the
fix cannot regress it. It is also the failure mode that matters most in
practice: GitHub's documentation notes the caller needs write, maintain or
admin privileges to read collaborator information, so an installation
whose permissions are too narrow makes this call error — and erring toward
refusal is the only safe direction.*

*Rationale for 2.2: `permissionRank` returns 0 for an unknown string, so a
new role GitHub introduces is already treated as insufficient. Stated so
that a later "unknown means read" convenience is recognised as a weakening
rather than a tidy-up.*

### Requirement 3: The write-permission gate keeps its meaning

#### Acceptance Criteria

1. `HasWritePermission` SHALL continue to require `write`, `maintain` or
   `admin`
2. THE change to Collaborator determination SHALL NOT alter which accounts
   pass the write check

*Rationale: this half is already correct, and the risk in this slice is
collateral damage rather than an incomplete fix. Saying so makes the
existing write tests a guard on the change rather than bystanders.*

### Requirement 4: The authorization cost stays bounded

#### Acceptance Criteria

1. THE Authorizer SHALL NOT issue an unbounded number of GitHub API calls
   per Trigger Command
2. Repeated questions about the same account on the same repository within
   the cache lifetime SHALL NOT re-issue a call

*Rationale: the design's one-call-per-comment property is the only
defensible part of the decision being corrected, and asking two endpoints
instead of one must not become asking two per Project. How the cache is
arranged — one entry answering both questions, or one per question — is
the design's to settle; this requirement fixes only that it stays bounded.*

### Requirement 5: The wrong premise is corrected where it was written

#### Acceptance Criteria

1. THE claim in `github-integration/design.md` that the permission
   endpoint returns 404 for a non-collaborator SHALL be corrected in place
2. THE correction SHALL record what the endpoint actually returns and why
   the original reasoning produced a gate that admitted everyone
3. THE correction SHALL be recorded in the roadmap's slice entry, per the
   convention the global spec already uses

*Rationale: a wrong premise left in place is what produced this defect,
and it is the part most likely to produce the next one — the next person
to optimise an API call will read the same sentence and reach the same
conclusion. Quietly editing it would lose the fact that turnip once
believed it.*

### Requirement 6: The case that passes today is pinned by a test

#### Acceptance Criteria

1. A test SHALL assert that an account GitHub reports as not a
   Collaborator is refused
2. THE test SHALL exercise the case that currently passes wrongly — a
   *successful* API response describing no access — rather than an error
   response
3. THE refusal SHALL be asserted at the Trigger Command level as well as
   at the Authorizer, so that the gate is shown to reject and not merely
   the helper

*Rationale for 6.2: the existing tests pass because their non-collaborator
fixture is an error, which the broken code already handles. A regression
test built the same way would pass against the unfixed code and prove
nothing. The fixture has to be a 200 response that says no access, which
is the shape GitHub actually sends.*

### Requirement 7: `Client.IsCollaborator` is not removed before it is used

#### Acceptance Criteria

1. `Client.IsCollaborator` SHALL remain, and SHALL gain the production
   caller this slice introduces

*Rationale: a dead-code sweep flags it as test-only, and correctly —
nothing in production calls it today. But it is the *right*
implementation, and this slice's entire fix is to start calling it.
Deleting it as dead would remove the correct code and leave the broken
code in place, which is the worst available outcome. Recorded as a
requirement because the finding and the fix live in different documents.*

### Requirement 8: Documentation

#### Acceptance Criteria

1. THE documentation SHALL state that triggering any Operation by comment
   requires being a collaborator on the repository, and that read access
   alone is not sufficient

## Out of Scope

- **Trailing-argument filtering.** Slice 22 owns it. This slice reduces
  its blast radius by restoring the gate that should precede it, and does
  not otherwise touch argument handling.
- **Requiring write permission for a plan.** Today a plan needs
  Collaborator status and a mutating Operation needs write; this slice
  corrects how the first is determined and does not move the line between
  them.

  **Considered and declined** (2026-09-21): making the levels
  configurable, so an operator could require write for a plan. Declined in
  favour of documenting the rule. A configurable *apply* threshold has no
  safe value below write, so the setting would only ever be a way to get
  it wrong; and a configurable *plan* threshold would take from a
  read-level reviewer the only means they have of asking what a change
  does, while the automatic path — which has no actor to authorize — would
  keep planning regardless. Where a plan must be restricted more tightly,
  the lever is GitHub's collaborator list and the organization's default
  permission, not a turnip key. Recorded in `docs/usage.md`.
- **Per-repository authorization policy.** One rule applies to every
  repository this Server serves, as today.
- **Caching strategy.** Requirement 4 bounds the cost; the arrangement is
  the design's.
