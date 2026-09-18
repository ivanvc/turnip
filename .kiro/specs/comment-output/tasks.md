# Implementation Plan: Pull Request Comment Output (Slice 17)

## Overview

Ordered so the risky part is finished and tested before anything is wired
to it.

`BuildConsolidatedComment` is a pure function over `[]ProjectResult`, and
every judgement in this slice — the verdict line's arithmetic, what a
collapsed summary says, how clamping picks what to drop — lives inside it.
So the renderer is built and tested **first**, in full, while the Server
still populates none of the new fields. A failure at that stage is
unambiguously about rendering.

The fields themselves land before the renderer, as data with no behaviour,
so the renderer has something to read. The orchestrator fills them last:
by then the only question left is whether the right value reaches the
right field, which is the easiest kind of failure to diagnose.

Clamping is deliberately its own stage rather than part of the renderer's.
It is the one piece with a genuine algorithmic change (Decision 6's two
tiers), it is independent of everything above it, and bundling it would
make a failure ambiguous between "the comment is wrong" and "the clamp is
wrong".

## Tasks

- [x] 1. `ProjectResult` carries what the renderer cannot know
  - [x] 1.1 Add the fields and a local counts type
    - `Changes`, `PlanOperation`, `ApplyOperation`, `Locked`, `BlockedBy`
    - The counts type is declared **in `internal/github`**, not imported
      from `internal/plugin`: a leaf package that talks to GitHub should
      not acquire the plugin system for one struct of three ints
      (Decision 1)
    - Data only — nothing reads these yet, so the build and every existing
      test are unaffected
    - _Requirements: 2.2, 3.1, 4.1, 5.1_

- [x] 2. Checkpoint - nothing behaves differently yet
  - `go build ./...` and `go test -race ./...` pass. The fields exist and
    are unread, so any failure here is a compilation mistake, not a
    behavioural one.

- [x] 3. The verdict line
  - [x] 3.1 Classify each result
    - with changes / no changes / failed, per Decision 4's table. A failed
      Project contributes nothing to the total however its counts read,
      because they describe an Operation that did not complete
    - _Requirements: 1.1, 1.2_
  - [x] 3.2 Render it, including the single-Project wording
    - One Project reads as a clause naming that Project, not as a count of
      one. The pilot repository has exactly one Project, so this is the
      common case rather than an edge case
    - The line states the total and names only the exceptions — no-changes
      and failures. It does **not** restate how many had changes: the
      total implies it and the rows below show it
    - _Requirements: 1.1, 1.4_

- [x] 4. Per-Project sections
  - [x] 4.1 The collapsed summary line
    - Name, status and change counts on the `<summary>`, which GitHub
      renders while the section is shut. This is the line that replaces
      the table, so it is the one that has to be right
    - "No changes" where all three counts are zero — never three zeros,
      and never invented counts for a Project that reported none
    - _Requirements: 2.1, 2.2, 2.3, 2.4, 2.5_
  - [x] 4.2 Next steps inside the section
    - Apply, re-plan and unlock for that Project alone, printed with the
      resolved operation names rather than guessed ones
    - **Offered by state, not by a fixed list**: apply only where there is
      something to apply, unlock only where a Lock is genuinely held. This
      is what lets Slice 18 change which Projects hold Locks without this
      slice changing at all
    - Inside the `<details>`, so the collapsed view stays one line per
      Project
    - _Requirements: 3.1, 3.2, 3.3, 3.4, 3.5_

- [x] 5. The comment's footer
  - [x] 5.1 Lock state and whole-run commands
    - Which Projects this pull request now holds, the command that applies
      every Project at once, and the command that releases the Locks
      without applying
    - _Requirements: 4.1, 4.2, 4.3_

- [x] 6. Remove the summary table
  - Deleted once the sections above replace what it carried, not before —
    so no intermediate state renders neither
  - Global Requirements 10.2 and 17.3 were amended for this; the roadmap's
    Slice 17 entry records the amendment
  - _Requirements: 1.3_

- [x] 7. Checkpoint - the renderer is complete
  - `go test -race ./internal/github/...` passes against the new shape.
    The Server still fills none of the new fields, so everything here was
    exercised through `ProjectResult` values the tests construct directly.

- [x] 8. Clamping inverts (Decision 6)
  - [x] 8.1 Tier 1 - drop whole leading sections
    - Drop from the front while the body is over budget and more than one
      section remains. Valid by construction: Slice 4 already guarantees
      each piece opens and closes its own fence and `<details>`
    - The note saying how many sections were dropped is prepended *after*
      the drop, so the verdict line still describes the whole run rather
      than the surviving fragment
    - _Requirements: 6.1, 6.2, 6.3, 6.4_
  - [x] 8.2 Tier 2 - cut within the last section
    - Reachable only when one section remains and its *scaffold alone*
      exceeds the budget. `splitDetailSection` otherwise prevents it, but
      it floors its arithmetic at one byte, so "prevents" is not "cannot"
    - Bytes come off the front of that section's output; its `<details>`,
      summary and fence are reopened **verbatim from the same builder that
      emitted them**. That is why byte-cutting is acceptable here and not
      in tier 1: exactly one section remains and turnip generated its
      scaffold, so the elements to reopen are known rather than guessed
    - _Requirements: 6.1, 6.2, 6.3_

- [x] 9. Checkpoint - clamping is correct in isolation
  - `go test -race ./internal/github/...` passes, including a case that
    forces tier 2 with an oversized scaffold. Nothing outside this package
    has changed, so a failure is about the clamp alone.

- [x] 10. The orchestrator fills the fields
  - [x] 10.1 Change counts and resolved operation names
    - The counts the Runner already reported, converted to the local type;
      the Plugin's plan and apply operation names, which the orchestrator
      already looks up to decide whether an Operation is a plan
    - _Requirements: 2.5, 3.1_
  - [x] 10.2 Lock state
    - Set from whether this pull request holds the Lock **once the result
      has been handled**, not from whether a plan succeeded — a failed
      plan holds a Lock today and will not after Slice 18, and the comment
      should tell the truth in both worlds without being edited
    - _Requirements: 4.1_
  - [x] 10.3 The contention message gains its link
    - Smaller than it looks: `GetLockStatus` is already called on the
      failed-acquire path and already reports the PR number. Only
      `PullRequestURL` — already in the struct being read — is unused. No
      new lookup, no change to `AcquireLock`'s contract
    - The existing generic fallback stays exactly as it is
    - _Requirements: 5.1, 5.2, 5.3_

- [x] 11. Checkpoint - end to end
  - `go build ./...` and `go test -race ./...` pass.

- [x] 12. Tests
  - [x] 12.1 `internal/github`: the rendered comment
    - The collapsed summary line, because it is what a reviewer actually
      sees: name, status and counts present, and no zeros for a Project
      that reported none
    - A failed Project is never invited to apply — the assertion whose
      absence would let turnip suggest applying an incomplete plan
    - Unlock appears only where a Lock is held, which is what keeps this
      slice correct across Slice 18
    - The verdict line's arithmetic as a table test over the
      with-changes / no-changes / failed combinations, including all-zero
      and single-Project
    - _Requirements: 1.1, 1.4, 2.2, 2.3, 2.4, 3.2, 3.4_
  - [x] 12.2 `internal/github`: clamping
    - Every surviving section intact — no partial `<details>` — which is
      the property tier 1 buys by dropping whole pieces
    - Tier 2 forced with a scaffold larger than the budget: still valid
      markdown, `<details>` and fence reopened, and the **end** of the
      output kept rather than the beginning
    - _Requirements: 6.1, 6.2, 6.3, 6.4_
  - [x] 12.3 `internal/orchestrator`: population
    - Counts and operation names reach the result; a contention rejection
      carries the holder's URL
    - _Requirements: 2.5, 3.1, 5.1, 5.2_

- [x] 13. Documentation
  - [x] 13.1 `docs/usage.md`
    - What a plan comment now contains, including the per-Project commands
    - That a plan holds a Lock until applied or released, and the command
      that releases it
    - _Requirements: 7.1, 7.2_

- [x] 14. Final checkpoint - full verification
  - `go build ./...`, `go test -race ./...`, `go mod tidy` clean,
    `gofmt -l .` clean, and the real golangci-lint v2 via
    `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest run ./...`

## Notes

- **No new dependencies.** The counts type is three ints declared locally,
  the commands are strings, and the clamp is arithmetic over slices.
- **The load-bearing fact is that `<summary>` renders while collapsed.**
  Everything that makes the table unnecessary rests on it. It is worth
  confirming against a real comment early rather than discovering at task
  12 that the counts are invisible until expanded.
- **This slice must not encode Slice 18's rule.** Unlock is offered where
  a Lock is held, full stop. Slice 18 changes which Projects hold Locks
  after a failed plan; if this slice hard-codes "failed means no unlock"
  it will be wrong today and right later, which is the worst of both.
- **Task 6 deletes the table last**, so there is no intermediate commit
  where the comment carries neither the table nor its replacement.
- **Coverage target**: 80% for touched packages, consistent with prior
  slices.

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1.1"] },
    { "id": 1, "tasks": ["3.1", "8.1"] },
    { "id": 2, "tasks": ["3.2", "4.1", "8.2"] },
    { "id": 3, "tasks": ["4.2", "5.1"] },
    { "id": 4, "tasks": ["6"] },
    { "id": 5, "tasks": ["10.1", "10.2", "10.3"] },
    { "id": 6, "tasks": ["12.1", "12.2", "12.3"] },
    { "id": 7, "tasks": ["13.1"] }
  ]
}
```
