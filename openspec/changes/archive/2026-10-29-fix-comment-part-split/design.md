# Design

## Context

See `proposal.md` for why, and the delta for the behavior. As the code
in `internal/github/comment.go` stands:

- **`splitDetailSection` cuts by bytes.** It works out `chunkCap`, the
  output that fits one part (a comment's budget less a pessimistic
  scaffold with `part 9999/9999`), takes the number of parts as the
  output's length divided by `chunkCap` rounded up, and slices the
  output every `chunkCap` bytes. Nothing looks for a line break or a
  character boundary.
- **The per-Project cap is already careful.** When the output would need
  more parts than the cap, `capOutput` removes whole lines from after
  the leading annotation lines, keeping the end, and falls back to a
  character boundary only when the last line alone is longer than the
  budget. It is given `maxParts * chunkCap` bytes, which is exact only
  while every part is filled to the byte.
- **The last resort is not.** `cutWithinSection`, when one section still
  does not fit a comment, keeps the end with
  `chunk[len(chunk)-room:]`, at an exact byte count.
- **`fencesFor` picks each part's fence from that part's own text**, so
  a part's fence is right whatever it holds.

## Goals / Non-Goals

**Goals**
- No part, and no cut, starts or ends inside a line, except a line
  longer than a part, and nothing ever inside a character
- The parts, joined, are exactly the output (when the cap did not cut)
- The cap's part count holds for parts that are no longer full

**Non-Goals**
- How much a long output keeps, and where the cap cuts (unchanged)
- The comment's other limits: packing sections into bodies, dropping
  sections

## Decisions

### 1. Parts are packed by whole lines

Splitting walks the output a line at a time and adds each line to the
current part while it fits `chunkCap`; a line that does not fit starts
the next part. A line longer than `chunkCap` on its own is split inside,
each piece ending at the last character boundary that fits, and the
part after its last piece goes on by whole lines. The number of parts
is the number this produces, not a division.

`chunkCap` keeps its pessimistic scaffold, so a part numbered for real
is never larger than budgeted.

- **Alternative considered:** keep slicing every `chunkCap` bytes and
  move each cut back to the previous line break.
- **Rejected because:** the next slice would then start from an offset
  that slicing did not choose, so it is packing by lines either way,
  with the bookkeeping spread over two loops.

### 2. The cap is checked against the parts actually produced

A part packed by lines can end with room to spare, up to a line's
length, so `maxParts * chunkCap` bytes no longer always fit in
`maxParts` parts. The split therefore packs first and counts: when the
count is over the cap, it shortens the output with `capOutput` to the
byte budget, packs again, and if still over, lowers the budget by the
bytes the parts left unused and tries again. Each try lowers the budget,
so it ends; in practice it ends on the first or second try, because the
slack is at most one line per part.

`capOutput` itself is unchanged: it already removes whole lines and
keeps the end.

- **Alternative considered:** pack from the end of the output backwards
  until the cap's parts are full, which needs no second pass.
- **Rejected because:** the leading annotation lines and the omission
  line must open the first part, so the first part would need packing
  forwards anyway, and two directions are harder to review than a
  loop over one.

### 3. The last resort keeps the end from a whole line

`cutWithinSection` keeps the end of the output from the start of the
first whole line within `room`, and from a character boundary only when
the last line alone is longer than `room`. That is `capOutput`'s rule
for where its kept text starts, so the two share one helper that finds
that point, rather than repeating it.

## Verification

| What | How |
|---|---|
| The delta's rule | property, `// Feature: fix-comment-part-split, Property 1: Parts end at whole lines`, over outputs of random lines (empty, short, longer than a part, multi-byte characters) and random budgets: every part ends with a newline unless it is the last or a piece of a line longer than a part; every part is valid UTF-8; the parts joined equal the output |
| The cap | property: for any output and cap, the section takes at most the cap's parts, and when it was cut, the first part opens with the leading annotation lines and the omission line |
| The delta's scenarios | unit tests for `port: 8888` at a part's end, `é` and `💡` at a part's end, a line longer than a part, and a section cut to fit a body |
| What stays | the existing split, cap and packing tests, and every body at most 65,536 characters |

## Risks / Trade-offs

- [A Project needs one more part than before, now and then] → only when
  the slack at the ends of its parts adds up past a part; the cap still
  bounds it.
- [A part with a very long line is mostly that line] → as before; the
  line is only split when it cannot fit whole, now between characters.

## Migration Plan

None. Comments are built fresh for each result.
