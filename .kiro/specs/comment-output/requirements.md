# Requirements: Pull Request Comment Output (Slice 17)

## Introduction

turnip's consolidated comment reports that an Operation happened, and
almost nothing about what it did. A pilot run that changed four releases
produced this, in full:

```
| Project | Operation | Status |
|---|---|---|
| infra | diff | ✅ success |
```

The Runner had computed `Add:0 Change:4 Destroy:0` and the Server had
already stored it in the lock. None of it reached the reader.

**Part of this is unfinished work rather than new ambition.** Two numbered
criteria are already on the books and unimplemented:

- **Requirement 17.3** — the summary "SHALL include ... status and change
  counts". No counts are rendered anywhere in the comment.
- **Requirement 7.2** — a lock held by another pull request "SHALL ...
  post a comment indicating another PR holds the lock with a link to that
  PR". The implementation reports `locked by PR #<n>` but **no link**,
  falling back to a bare `locked by another PR` only when the status
  lookup fails. `LockStatus.PullRequestURL` is already in the struct being
  read, so the missing half is the link alone.

**This slice amends the global spec, and says so deliberately.** Global
Requirements 10.2 and 17.3 both mandate a *summary table*. This slice
removes it. The table was not a considered choice — it was the cheapest
shape to generate when those requirements were first written — and the
reason usually given for keeping it does not survive contact with how
GitHub actually renders a comment:

> A `<details>` element's `<summary>` text is visible while the section is
> **collapsed**.

So each Project's status and change counts can ride on its own collapsed
row. The scanning a table exists to provide comes free, without a grid,
and each row expands in place instead of sending the reader to a section
further down. Requirements 1 and 3 below replace the table; global 10.2
and 17.3 are amended to match.

**Prior art**, consulted and diverged from deliberately:

- **Atlantis** renders no table either: a numbered list of projects, an
  `### N. project / dir / workspace` heading each, a collapsed
  `<details>` past 12 lines, and a `### Plan Summary` footer. Per project
  it prints the exact commands to apply and re-plan, plus a link to
  delete that plan and its lock.
- **Its per-project commands are adopted**; its heading-per-project
  structure is not. Headings cost a screen for three projects and scale
  badly; a collapsed row costs one line.
- **Atlantis truncates the *beginning* of an oversized comment**, to
  preserve the end, "which usually contains more important information,
  such as warnings, errors, and the plan summary". turnip's
  `truncateBody` truncates the end, discarding exactly that. Adopted.

**Global requirements**: implements the unimplemented part of
Requirement 7.2; implements and **amends** Requirement 17.3; **amends**
Requirement 10.2. Extends Requirement 10 otherwise unchanged.

**Explicitly out of scope**, each deferred to named work:

- **Rewriting tool output so `+`/`-`/`~` markers highlight.** Recorded in
  `github-integration`'s Out of Scope and deferred to Slice 7 — helmfile
  needs none, Terraform will.
- **Suppressing auto-plan on draft pull requests.** A separate concern
  with its own slice.
- **Hiding no-change projects** (Atlantis' `--hide-unchanged-plan-comments`)
  and **operator-customisable templates** (its
  `--markdown-template-overrides-dir`). Preference surfaces, not missing
  information.
- **Changing when a comment is posted versus updated.** `server-orchestration`
  decided plan comments are minimized-then-reposted and apply comments
  never touched; that stands.

## Glossary

Terms additional to the global spec's glossary:

- **Change_Counts**: the add/change/destroy triple a Plugin extracts from
  an Operation's output, carried as `plugin.ChangeSummary` and persisted
  in the Lock as `PlanSummary`.
- **Verdict_Line**: the comment's opening sentence, stating the overall
  outcome across every Project before any detail.
- **Next_Steps**: the commands a comment prints telling a reader how to
  act — apply, re-plan, or unlock.

## Requirements

### Requirement 1: The comment opens with a verdict

**User Story:** As a reviewer, I want the first line to tell me whether
this pull request needs my attention, so that I do not have to open
anything to find out.

#### Acceptance Criteria

1. THE comment SHALL begin with a Verdict_Line stating the total change
   across every Project, how many reported no changes, and how many
   failed. It SHALL NOT also state how many had changes: the total
   implies it and the per-Project rows below show it
2. THE Verdict_Line SHALL be derived from the same per-Project results
   rendered below it, never counted separately
3. THE comment SHALL NOT render a summary table, amending global
   Requirements 10.2 and 17.3
4. WHERE only one Project was operated on, THE Verdict_Line SHALL read
   naturally for a single Project rather than reporting a count of one

### Requirement 2: Each Project is one collapsed, self-describing row

**User Story:** As a reviewer, I want each Project's outcome legible
without expanding it, so that I can scan a multi-Project run and open
only what matters.

#### Acceptance Criteria

1. EACH Project SHALL be rendered as a collapsible section whose summary
   line is visible while collapsed
2. THAT summary line SHALL carry the Project's name, its status, and its
   Change_Counts, satisfying global Requirement 17.3's intent
3. WHERE an Operation reports no changes, THE summary line SHALL say so
   rather than rendering three zeros
4. WHERE an Operation failed, THE summary line SHALL NOT present counts as
   though they described a completed change
5. THE Change_Counts SHALL come from the value the Runner already
   reported; nothing SHALL be re-parsed from the Operation's output

### Requirement 3: Each section carries its own next steps

**User Story:** As a developer, I want to apply or re-plan one Project
without looking up turnip's syntax, so that acting on a plan does not
require leaving the comment.

#### Acceptance Criteria

1. EACH Project's section SHALL print the Next_Steps for that Project
   specifically: the command to apply it alone, the command to re-plan it
   alone, and the command to release its Lock alone
2. THE Next_Steps offered SHALL match what is actually available for that
   Project's state, rather than printing a fixed list of three
3. THE printed commands SHALL be ones turnip already accepts, so a reader
   can copy them verbatim
4. WHERE an Operation failed, THE section SHALL NOT invite the reader to
   apply it
5. THE Next_Steps SHALL sit inside the collapsible section, so that the
   collapsed view stays one line per Project

### Requirement 4: The comment says what it locked and how to release it

**User Story:** As a developer, I want to know that a plan is holding
locks, so that I learn it from the comment rather than from a later
Operation being refused.

#### Acceptance Criteria

1. WHERE a plan succeeded and its result is held under a Lock, THE
   comment SHALL state which Projects this pull request now holds
2. THE comment SHALL print the command that releases those Locks without
   applying
3. THE comment SHALL print the command that applies every Project at once,
   alongside the per-Project commands of Requirement 3

### Requirement 5: A blocked Operation names the pull request blocking it

**User Story:** As a developer whose plan was refused, I want to know
which pull request holds the lock, so that I can go and look at it
instead of guessing.

#### Acceptance Criteria

1. WHEN an Operation is rejected because another pull request holds the
   Lock, THE comment SHALL name that pull request and link to it,
   satisfying global Requirement 7.2
2. THE link SHALL come from the Lock's stored pull request URL rather
   than being reconstructed from parts
3. IF the holder cannot be determined, THEN THE message SHALL say the
   Project is locked without inventing a reference

### Requirement 6: Oversized comments keep their most useful end

**User Story:** As a reviewer of a very large plan, I want the verdict and
any errors to survive truncation, so that the part I cannot afford to
lose is not the part that is dropped.

#### Acceptance Criteria

1. WHERE a comment body must be clamped, THE clamp SHALL preserve the end
   of the content and remove from the beginning
2. THE clamped body SHALL remain valid markdown, closing any code fence
   or `<details>` element it cuts through
3. THE clamped body SHALL state that content was removed, and from where
4. Splitting content across multiple bodies SHALL remain the primary
   mechanism; clamping SHALL remain the last resort it is today

### Requirement 7: Documentation

#### Acceptance Criteria

1. `docs/usage.md` SHALL show what a plan comment now contains, including
   the per-Project Next_Steps
2. THE documentation SHALL state that a plan holds a Lock until applied or
   released, and name the command that releases it
