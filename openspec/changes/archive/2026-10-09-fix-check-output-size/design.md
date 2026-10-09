# Design

## Context

See `proposal.md` for why, and the `project-checks` delta for the
behavior. As the code stands:

- `github.CheckRunOptions` has a `Text` field, and `toCheckRunOutput`
  sends it as the check run's `output.text`.
- Two places set it, both when completing a check: the result path
  (`internal/orchestrator/result.go`, the whole output) and the
  start-timeout path (`sweep.go`, the diagnostic, which is short).
  GitHub refuses the whole update when `output.text` exceeds
  65,535 characters, so only the result path has ever hit it.
- `appendCheckRunNote(result, err)` adds the same note for every failed
  check call, whether the check was never created or was created and
  never completed. Its four callers are dispatch (creation), the refusal
  path (creation or completion), the result path (completion) and the
  sweep (completion).

## Goals / Non-Goals

**Goals**
- No check carries the output, so no output size can stop a check from
  completing
- The note in the comment says which of the two failures happened

**Non-Goals**
- Retrying a check update that fails for any other reason
- A link from the check to the output (live output, roadmap Slice 26)
- Results too large to reach the Server at all (the 4 MiB gRPC limit),
  which leave the check in progress for a different reason; that is the
  large-output work on the roadmap

## Decisions

### 1. Remove `Text` from `CheckRunOptions`

The field goes, along with its handling in `toCheckRunOutput`, rather
than both callers merely leaving it empty.

- **Alternative considered:** keep the field and stop setting it.
- **Rejected because:** the next caller to set it reintroduces the
  failure, and only on outputs large enough to be rare in tests. Without
  the field the spec's "a check carries no details text" holds by
  construction. The comment calling `CheckRunOptions` a field-for-field
  mirror of the global design's sketch is corrected: that design is
  archived history, and this field is deliberately gone.

### 2. The note names the action that failed

Each caller says which call failed, and the note is worded from that:

| Caller | Check call | Note |
|---|---|---|
| dispatch (`execute.go`) | create | "could not be created", missing |
| refusal (`refusalCheck`) | create a refusal check, or complete the existing one as `failure` | whichever call it made |
| result (`result.go`) | complete | "could not be completed", in progress |
| start timeout (`sweep.go`) | complete | "could not be completed", in progress |

`refusalCheck` already knows which call it made, so it returns its
error tagged with that action; the other callers pass theirs
explicitly. The note helper takes the action as a parameter, so a
caller cannot omit it.

- **Alternative considered:** one neutral wording ("could not be
  recorded") that fits both.
- **Rejected because:** it is today's wording, and it misled: a check
  that could not be completed is still there, in progress, and may block
  a merge. The reader needs to know which.

### 3. The failure summary drops its pointer

`The operation failed. See the output below.` becomes `The operation
failed.` in `result.go`'s summary builder. Nothing else in the summary
or title changes, so the change-count text shared with the comment is
untouched.

## Risks / Trade-offs

- [Readers who opened a check's details for the output find none] →
  the four doc pages that send them there (`docs/plugins/helmfile.md`,
  both troubleshooting pages, `docs/using/pull-requests.md`) point to the
  results comment instead, which carries the full output.
- [A check still stays in progress when completing it fails for another
  reason (rate limit, outage)] → the note now says so plainly; retrying
  is out of scope.
- [Tests assert the output in the check's text (`result_test.go`,
  `sweep_test.go`, `images_test.go`, `client_test.go`)] → they change to
  assert it is absent, and that the output still reaches the comment.

## Migration Plan

None. Checks created before the change keep their text; new ones carry
none.
