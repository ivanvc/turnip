# Tasks

**The checkpoint that matters is task 2.1.** Each piece can be right on
its own while a reader still sees a broken line: the pieces of one
section end up in different comments, and only the whole comment,
built by `BuildConsolidatedComment` from a large output full of long
lines and multi-byte characters, shows that every comment stays within
GitHub's limit, every fence is closed, and the parts read back as the
output with nothing cut inside a line or a character. It runs in the
package's tests, so it needs no GitHub.

Every task changes `internal/github/comment.go`, so the waves run one
after another.

## 1. Splitting (`internal/github`)

- [x] 1.1 One helper finds where kept text starts when keeping the end
  of an output (the start of the first whole line that fits, or a
  character boundary when the last line alone is too long), used by
  `capOutput` and by `cutWithinSection`, whose last-resort cut now keeps
  the end from a whole line (design Decision 3). Verify: `capOutput`'s
  existing tests unchanged; a test that a section cut to fit a body
  keeps its output from the start of a whole line after
  `…(earlier output omitted)`, and from a character boundary when its
  last line alone is too long; every body at most 65,536 characters
  - _Deltas: pr-comment (Size limits, splitting and dropping)_
- [x] 1.2 `splitDetailSection` packs parts by whole lines, splitting a
  line longer than a part between characters, and numbers the parts it
  produced (design Decision 1). Verify: a property test,
  `// Feature: fix-comment-part-split, Property 1: Parts end at whole
  lines`, over outputs of random lines (empty, short, longer than a
  part, with multi-byte characters) and random part sizes: every part
  ends with a newline unless it is the last or a piece of a line longer
  than a part, every part is valid UTF-8, and the parts joined equal
  the output; unit tests for the delta's scenarios (`port: 8888` at a
  part's end, `é` and `💡` at a part's end, a line longer than a part);
  the existing split tests
  - _Deltas: pr-comment (Size limits, splitting and dropping)_
- [x] 1.3 The per-Project cap is checked against the parts actually
  produced, shortening the output again until they fit (design Decision
  2). `docs/using/pull-requests.md` says a long output is split between
  lines, never inside one unless a line is too long for a comment.
  Verify: a property test, `Property 2: The cap holds for parts packed
  by lines`, over random outputs and caps: the section takes at most
  the cap's parts, and when it was cut, the first part opens with the
  leading annotation lines and the `…(<K> earlier lines omitted)` line;
  the existing cap tests (80 parts capped to 5, a raised cap of 8);
  `make docs-check`
  - _Deltas: pr-comment (Size limits, splitting and dropping)_

## 2. Checkpoint: a whole comment

- [x] 2.1 A test building a comment with `BuildConsolidatedComment` from
  several Projects, one with a 2 MB output of lines of varied length
  (some longer than a comment), multi-byte characters throughout and a
  line placed where a byte cut would have split it, under the default
  cap and under a raised one. Every body is at most 65,536 characters
  and closes every fence it opens; for each Project, its parts' outputs
  joined equal its output (or its capped output); no part starts or
  ends inside a line except pieces of an overlong line, and none inside
  a character

## 3. Final checkpoint

- [x] 3.1 `go build ./...`, `gofmt -l ./internal ./cmd` clean,
  `go vet ./...`, `make test`, `make lint` pass
- [x] 3.2 Every scenario in the delta maps to a test or a doc change;
  `openspec validate fix-comment-part-split --strict` passes;
  `openspec/roadmap.md` shows the change as implemented
- [x] 3.3 An intermittent failure is recorded here with its full output,
  not rerun away

## Repository owner

- Review and commit
- Deploy a Server built from this change (the Runner is unchanged)
- The next plan long enough to span comments reads with no line cut
  across two; then this change is archived

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1.1"] },
    { "id": 1, "tasks": ["1.2"] },
    { "id": 2, "tasks": ["1.3"] },
    { "id": 3, "tasks": ["2.1"] },
    { "id": 4, "tasks": ["3.1", "3.2", "3.3"] }
  ]
}
```

## Deviations recorded at implementation

- 2026-10-16: Task 1.2 could not leave the cap logic untouched. With
  parts packed by lines, the existing test
  `TestSplitDetailSection_CapsPartsAndKeepsTheEnd` got one part more than
  the cap, so `splitDetailSection` already packs, counts, and lowers the
  `capOutput` budget by the bytes the cap's parts left unused until the
  parts fit (design Decision 2). Task 1.3 still owes its property test
  (Property 2) and the `docs/using/pull-requests.md` change.
- 2026-10-16: `packLines` leaves the last piece of a split overlong line
  open, so the lines after it can share its part; the part still ends at
  a line end, which Property 1 allows.
- 2026-10-16 (review fix): the cap loop in `splitDetailSection` never
  ended when the leading annotation lines and the omission line alone took
  more than the cap's parts (an annotation line longer than a part, such
  as a command line with long extra arguments), or when the output was
  only annotation lines with no final newline. The loop now stops once
  `capOutput` keeps nothing more than those lines and accepts the parts
  they take, as before this change; `clampBody` still keeps every body
  within the limit. Property 2 now draws annotation lines longer than a
  part and outputs made only of annotation lines.
- 2026-10-16 (review fix): `packSections` counted neither the blank lines
  between sections nor the footer, so a short part followed by a full one
  could share a body that overflowed, and `clampBody` dropped the short
  part (with the omission line that must open the output). It now counts
  the header and its blank line (a continuation header with a pessimistic
  count on later bodies), the blank line between sections, and room for
  the footer in every body, since a body does not know whether it is the
  last.
- 2026-10-16 (review fix): `splitDetailSection` and `cutWithinSection`
  sized a part with three-backtick fences, but `fencesFor` widens them past
  the longest backtick run, so output full of backticks gave bodies over
  65,536 bytes (present before this change). Both now subtract
  `fenceGrowth`, the widening the longest backtick run anywhere in the
  output can cause, which bounds the fences of any part of it.
- 2026-10-16 (review, not changed): when the cap cuts inside a single
  overlong line, the omission line reads `…(0 earlier lines omitted)`
  although part of that line was removed. This predates the change, and
  the omission line's wording belongs to the cap, which this change leaves
  as it is (proposal, "Not in this change"); deciding the wording and its
  scenario is left to the repository owner.
- 2026-10-16 (3.3): a rapid failure file for Property 1, written while
  task 1.2 was in progress, was left in `internal/github/testdata/`. It
  passes when replayed against the finished code, and was removed. No
  other intermittent failure was seen: the two properties passed at
  1,000 cases each, and the checkpoint and the review's regression
  tests 20 times, under `-race`.
- 2026-10-16 (final checkpoint): the design listed packing sections into
  comment bodies as a non-goal; the fix pass changed it anyway
  (`packSections` now counts the header, separators and footer),
  because packing by lines made a short part sit before a full one and
  be dropped. Recorded here rather than by editing the design.
