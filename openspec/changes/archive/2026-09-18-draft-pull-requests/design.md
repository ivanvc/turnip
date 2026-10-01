# Design: Draft Pull Requests (Slice 19)

## Overview

Four decisions, and the last three are consequences of the first: where
the draft flag enters, where it is read, how a pull request marked ready
gets its plan, and why the skip is silent.

The whole change is small — a field, a guard, and one action added to a
switch. What makes it worth designing is that the obvious placements are
wrong in ways that only show up later: a guard one function deeper costs
an API call per draft push, and a guard one level higher silently stops
releasing Locks.

## Decision 1: the flag enters where every other PR field does

`github.PullRequest` gains `Draft bool`, populated in
`pullRequestWebhookEvent` alongside `HeadSHA`, `BaseRef` and `HeadRef`.

turnip has no concept of a draft today — the parser never reads one — so
this is the same shape of gap Slice 16 met when `Target` carried no
`config.Config`: not a condition to add to an existing check, but a fact
that has to be carried before anything can act on it.

**Alternative considered**: asking the GitHub API for the pull request's
draft state when the plan path runs. *Rejected* — the webhook payload
already carries it, so a request would buy nothing and cost a round trip
on the hot path, in a handler that fires on every push to every pull
request.

## Decision 2: the guard is the first statement of the plan-trigger arm

**The decision**: `HandlePullRequest` keeps its action switch exactly as
it is, and the draft test becomes the first thing the plan-trigger arm
does — returning nil before `handlePlanTrigger` is called. It is not a
check before the switch, not a new `case`, and not anything inside
`handlePlanTrigger`.

That one placement is what satisfies two requirements at once, and it is
the only placement that satisfies both:

- **Nothing else runs for a skipped draft** (Requirement 1.4). The arm
  returns before `handlePlanTrigger`, whose first act is fetching
  `turnip.yaml` — so a draft costs one webhook and no API call.
- **`closed` is untouched by construction** (Requirement 3.2). It is a
  different arm of the same switch, so no reasoning is required to know
  the draft test cannot reach it.

Two alternatives, and why each fails:

| Placement | Fails because |
|---|---|
| before the switch, guarding the whole handler | `closed` is skipped too, so a draft's Locks are never released |
| inside `handlePlanTrigger` | every push to every draft fetches `turnip.yaml` before discovering it has nothing to do |

The first of those is the natural-looking implementation — "if it's a
draft, ignore it" — and it is the one that breaks Lock release. It breaks
it silently, and for exactly the pull requests most likely to be
abandoned rather than closed cleanly. That failure mode is the reason
this placement is a decision rather than a detail.

## Decision 3: `ready_for_review` needs no special case

GitHub sends `ready_for_review` when an author takes a pull request out of
draft, and **the payload's draft field is already false by then**. So the
action simply joins `opened` and `synchronize` on the plan-trigger arm,
and Decision 2's guard lets it straight through — the test it fails is
the test it was always going to fail once the pull request stopped being
a draft.

That is the whole of Requirement 2. No remembered state, no catch-up
logic, no separate path — the plan it produces is an ordinary plan at the
pull request's current head, because that is what the event carries.

Atlantis reaches the same behavior by mapping `ready_for_review` onto its
`OpenedPullEvent`, treating it as a freshly opened pull request. turnip
needs even less machinery because its guard is a field test rather than an
event-type translation.

**Without this action handled, skipping drafts would be a trap**: a draft
marked ready would sit with no plan until someone pushed again or
commented, and the failure would look like turnip ignoring a normal pull
request.

## Decision 4: the skip is silent, and reads draft state nowhere else

A skipped draft produces nothing — no comment, no check run, no Lock, no
Job (Requirement 1.3). A comment saying "skipped because draft" on every
push to every draft would be worse than the behavior it explains, and
the pull request's own draft badge already says why.

**Requirement 4.4 needs no enforcement**, which is worth recording
because it looks like it should. The `issue_comment` path builds its
`PullRequest` with `Number` alone — no head, no base, and no draft state
to read. A comment trigger therefore *cannot* consult the flag even by
accident, so "the Server reads draft state only on the automatic plan
path" is a property of the parsing rather than a rule the code must
remember to obey.

## Edge cases

| Case | Behavior |
|---|---|
| Draft opened, then pushed to repeatedly | nothing, each time, at the cost of one webhook |
| Draft marked ready | planned as though newly opened, at the current head |
| Draft closed without ever being planned | `closed` runs as normal; there is nothing to release |
| Draft closed holding Locks from a comment trigger | `closed` runs as normal and releases them — the case Requirement 3 exists for |
| `/turnip plan` on a draft | runs, takes Locks, stores plan data, appliable — being a draft changes when turnip acts on its own, not what it can do |
| Draft converted back to draft after being ready | `converted_to_draft` is not a plan trigger, so nothing runs; any Lock already held stays held until released or the pull request closes |
| A non-draft pull request | unchanged in every respect |

## Testing approach

The guard is one boolean, so the tests that matter are about *which path*
runs rather than what it computes:

- **A draft `opened` and a draft `synchronize` reach no plan**, asserted
  by the absence of side effects — no Job created, no Lock acquired, no
  comment posted. Asserting "returns nil" would pass even if the guard
  were in the wrong place.
- **`closed` on a draft still releases Locks.** The assertion that fails
  if the guard is ever hoisted to the top of `HandlePullRequest`, which
  is the mistake Decision 2 exists to prevent.
- **`ready_for_review` plans.** With `Draft` false, as GitHub sends it.
- **A comment trigger on a draft runs**, which in practice tests that the
  comment path never consults the field.

`internal/github` covers the parsing separately: a payload with
`draft: true` yields `Draft: true`, and one without yields false — the
default that keeps every existing test and every non-draft pull request
behaving exactly as before.
