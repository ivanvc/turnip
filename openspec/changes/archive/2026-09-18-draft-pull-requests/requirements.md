# Requirements: Draft Pull Requests (Slice 19)

## Introduction

turnip plans automatically on `opened` and `synchronize`. GitHub sends
`opened` for a **draft** pull request too, and `synchronize` on every push
to one — so turnip runs IaC against a real cluster for work its author has
explicitly marked unfinished.

That costs more than noise. Every draft push creates a Runner Job, holds
cloud and cluster credentials, and acquires a Lock on each matched
Project — and a Lock taken by a plan is held until applied, unlocked, or
the pull request closes. A developer iterating in a draft therefore
blocks every other pull request from planning those Projects, without
being told.

**turnip has no concept of a draft at all.** `github.PullRequest` carries
`Number`, `HeadSHA`, `BaseRef` and `HeadRef`; the webhook parser never
reads a draft flag. So this is not a condition to add to an existing
check — the information does not reach the orchestrator, which is the
same shape of gap Slice 16 met with `Target` and `*config.Config`.

**Prior art**, consulted and diverged from deliberately:

- **Atlantis** gates this behind `--allow-draft-prs`, defaulting to
  `false` — so it does not plan on drafts out of the box either.
- **It treats `ready_for_review` as a freshly opened pull request**, which
  is the half that makes skipping drafts usable: without it, a draft
  marked ready would get no plan until its next push.
- **Its draft guard deliberately exempts `closed`**, so a closed draft
  still releases its Locks.
- **Manual comment triggers still work on a draft.** The gate is on the
  event path only.
- **turnip adds no flag.** Atlantis made it configurable; turnip does not.
  Planning work that is declared unfinished has no constituency, and an
  operator who wants it can comment. A setting nobody should switch on is
  a setting not worth having.

**Global requirements**: **amends Requirement 4.1**, which says "WHEN a PR
is opened, THE Server SHALL trigger plan Operations for all matching
Projects" without qualification. Requirement 4.2's `synchronize` clause is
amended for the same reason.

**Explicitly out of scope**:

- **Any per-repository or per-Project draft setting.** If this ever needs
  to vary, it varies for a whole deployment, not a file read from the
  pull request's own head commit.
- **Refusing fork pull requests** (Slice 15), which is a different
  question about a different kind of untrusted input.
- **Draft-aware behavior for anything but the automatic plan.** Comment
  triggers, apply, unlock and close all behave identically on a draft.

## Glossary

Terms additional to the global spec's glossary:

- **Draft_PR**: a pull request GitHub reports as a draft. GitHub sends the
  same `opened` and `synchronize` actions for one as for any other pull
  request; only a field in the payload distinguishes it.
- **Ready_Transition**: the `ready_for_review` action GitHub sends when an
  author takes a pull request out of draft.

## Requirements

### Requirement 1: A draft is not planned automatically

**User Story:** As a developer, I want turnip to leave my draft alone, so
that unfinished work does not run against a cluster or take Locks other
people are waiting on.

#### Acceptance Criteria

1. WHEN a pull request is opened as a Draft_PR, THE Server SHALL NOT
   trigger plan Operations, amending global Requirement 4.1
2. WHEN a Draft_PR is synchronized, THE Server SHALL NOT trigger plan
   Operations, amending global Requirement 4.2
3. THE Server SHALL take no Lock, create no Runner Job, and post no
   comment for a skipped Draft_PR — skipping means doing nothing, not
   doing something quietly
4. THE decision SHALL be made before any repository configuration is
   fetched, so a draft costs no API calls beyond receiving the webhook

### Requirement 2: Marking ready plans as though newly opened

**User Story:** As a developer, I want marking my pull request ready to
produce a plan, so that taking it out of draft does not require an extra
push or a comment to get the review started.

#### Acceptance Criteria

1. WHEN a pull request undergoes a Ready_Transition, THE Server SHALL
   trigger plan Operations exactly as it does for a newly opened pull
   request
2. THAT plan SHALL use the pull request's current head commit, not any
   state remembered from while it was a draft

### Requirement 3: Skipping drafts never strands a Lock

**User Story:** As a platform operator, I want a draft that gets closed to
release anything it holds, so that skipping drafts cannot leak Locks.

A Draft_PR that was never auto-planned can still hold Locks: a comment
trigger takes them (Requirement 4), and they persist until applied,
unlocked, or the pull request closes. Skipping the automatic plan must
therefore not reach the close path, or the one thing that reliably
releases them stops running for exactly the pull requests most likely to
be abandoned.

#### Acceptance Criteria

1. WHEN a Draft_PR is closed, THE Server SHALL release its Locks exactly
   as for any other pull request. A Draft_PR cannot be *merged* — GitHub
   requires it be marked ready first, at which point Requirement 2 applies
   and it is no longer a Draft_PR — so closing is the only terminal state
   a draft reaches while still a draft
2. THE draft check SHALL NOT be applied to the `closed` action

### Requirement 4: A draft still responds to being asked

**User Story:** As a developer, I want to be able to plan my draft
deliberately, so that skipping automatic plans does not prevent me from
checking my work.

#### Acceptance Criteria

1. WHERE a Draft_PR receives a comment trigger, THE Server SHALL execute
   it normally
2. THE draft check SHALL apply only to the automatic plan path, not to
   comment-triggered Operations
3. A comment-triggered plan on a Draft_PR SHALL acquire a Lock, store its
   plan data, and be appliable and unlockable exactly as on any other
   pull request — being a draft changes *when turnip acts on its own*,
   never what it is capable of
4. THE Server SHALL NOT read a pull request's draft state on any path
   other than the automatic plan, so no other behavior can come to
   depend on it

### Requirement 5: Documentation

#### Acceptance Criteria

1. `docs/usage.md` SHALL state that drafts are not planned automatically,
   that marking one ready produces a plan, and that a comment trigger
   works on a draft
2. THE documentation SHALL say this is not configurable, so a reader does
   not go looking for the setting
