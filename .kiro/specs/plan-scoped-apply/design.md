# Design: Apply Exactly What Was Planned (Slice 20)

## Overview

Seven decisions. The first three are forced by discoveries made while
designing the slice, and they change what it is for.

**A Helmfile apply cannot run today.** Two independent gates both key off
the plan *bytes* a tool produced, and Helmfile produces none:

| Gate | Location | Effect for Helmfile |
|---|---|---|
| `len(result.PlanData) > 0` | `internal/orchestrator/result.go:113` | `StorePlanData` is never called — the Lock records neither plan data nor summary |
| `len(data.PlanData) == 0` | `internal/lock/redis.go:94` | `GetPlanData` returns `ErrNoPlanData` |
| `len(data.PlanData) > 0` | `internal/lock/redis.go:130` | `GetLockStatus` reports `HasPlan` false — a field stating something untrue, though nothing reads it (see below) |

**The third row was found during implementation, not during design**, and
is recorded here rather than quietly fixed. This design originally named
two gates; the third feeds `LockStatus.HasPlan`.

**Its consequence was then overstated, which is the more instructive
error.** An earlier version of this paragraph claimed that third gate is
what a pull request comment consults to decide whether to offer an apply,
so that fixing only two would leave every comment insisting there was no
plan. That is false. `LockStatus.HasPlan` has **no production reader at
all**: both callers of `GetLockStatus` use only `Locked` and `PRNumber`,
and `nextSteps` decides from the `ProjectResult`'s own fields. The gate is
still worth correcting — a field that reports something false is a trap
for the first caller that reads it — but nothing is misled today.

Two lessons, and the second is the one that keeps recurring in this slice:
all three gates are the same expression and two sit in the same file, so
searching for the *expression* would have found them before the design was
written; and a claimed consequence needs its consumer checked rather than
assumed.

So a `/helmfile diff` acquires a Lock and stores nothing in it, and the
apply that follows is refused with *"no plan data stored; a new plan is
required"* — every time, for every Helmfile Project.

**And a `sync` or `destroy` never releases its Lock.** The release at
`result.go:122` is `case rec.IsApply`, and `IsApply` is an equality test
against `GetApplyOperation()` alone. Helmfile exposes four operations
(`helmfile.go:23`) — `diff`, `apply`, `sync`, `destroy` — so two of them
run to completion, mutate real infrastructure, and leave the Project
locked with no route to release it but `/turnip unlock` or closing the
pull request.

Together these mean the operation model is wrong in both directions: the
one operation that *should* consume a plan cannot, and two that *should*
discharge a Lock do not.

**And one of those four cannot be made correct at all.** `destroy` has no
plan that could describe it, so Decision 6 removes it from Helmfile
rather than granting it a guarantee it cannot keep.

That is why the hazard this slice was written to prevent has never been
observed: the dangerous sequences require an apply that runs, and none
does. The slice therefore has to make mutating operations work correctly
before it can make them scoped. Doing only the latter would ship exactly
the bug the requirements describe.

The tests do not catch any of this because every store/apply test injects
synthetic bytes (`[]byte("plan-data")`, `[]byte("plan-bytes")`) that the
real plugin never returns, and
`TestExecuteOne_ApplyWithoutPlanDataIsRejected` pins the refusal as
correct.

## The two operation classes

Every decision below follows from sorting operations into two classes and
giving each a complete rule. There is no third class.

| Class | Helmfile | Lock | Trailing arguments | On success |
|---|---|---|---|---|
| **Plan** | `diff` | acquires one | **accepted** | records a `PlanRecord`, keeps the Lock |
| **Mutating** | `apply`, `sync` | requires one held by this pull request | **refused** | replays the recorded scope, **releases** the Lock |

The plan is the only operation that chooses a scope, because it is the
only one a human reviews the output of. Everything else inherits that
scope and gives the Lock back.

`destroy` is absent from that table on purpose: it is the operation that
cannot inherit a reviewed scope, and Decision 6 stops Helmfile exposing
it. `sync` stays because it converges to the declared state, which is
exactly what `helmfile diff` describes.

This is narrower than today in two respects and wider in one: `sync` stops
accepting arguments and `destroy` goes away, while `sync` starts releasing
the Lock and replaying the plan's scope.

## The shape being built

The path arguments travel today, unchanged by this slice except at its two
ends:

```
TriggerCommand.ExtraArgs        internal/github/types.go
  → Target.ExtraArgs            internal/orchestrator/target.go:104
  → OperationRecord.ExtraArgs   internal/orchestrator/execute.go:154
  → jobs.OperationParams        internal/orchestrator/execute.go:200
  → TURNIP_EXTRA_ARGS (JSON)    internal/jobs/build.go:132
  → runner cfg.ExtraArgs        internal/runner/config.go:130
  → plugin.ExecuteOptions       internal/runner/run.go:220
  → helmfile argv               internal/plugin/helmfile.go:43
```

This slice changes what is *put into* the first end for a mutating
operation, and adds a branch that records it at the far end. Nothing in
between moves.

## Decision 1: the Lock records a plan, not a byte slice

`LockData` gains two fields:

```go
type LockData struct {
	PRNumber       int                  `json:"pr_number"`
	PullRequestURL string               `json:"pull_request_url"`
	LockedAt       time.Time            `json:"locked_at"`
	LockedBy       string               `json:"locked_by"`
	HasPlan        bool                 `json:"has_plan"`
	PlanData       []byte               `json:"plan_data,omitempty"`
	PlanArgs       []string             `json:"plan_args,omitempty"`
	PlanSummary    plugin.ChangeSummary `json:"plan_summary,omitzero"`
}
```

`PlanArgs` is the scope. `HasPlan` is what makes the record legible
without it — see Decision 3.

**`HasPlan` is not redundant with `PlanArgs`.** Requirement 1.3 asks the
Lock to distinguish *a plan that ran with no arguments* from *no plan at
all*, and with `omitempty` both a nil and an empty `PlanArgs` disappear
from the JSON entirely. An explicit boolean is the only field here that
survives that round trip.

**Alternative considered**: dropping `omitempty` and relying on `null`
versus `[]` to carry the distinction. *Rejected* — it makes a
security-relevant decision depend on a JSON encoder's nil-slice
behaviour, which is exactly the kind of detail that changes silently and
is untestable by reading the struct.

## Decision 2: the interface stores a plan record, not a widening parameter list

`LockManager`'s two plan methods become:

```go
// PlanRecord is everything a plan leaves behind for a mutating Operation
// to replay.
type PlanRecord struct {
	Data    []byte
	Args    []string
	Summary plugin.ChangeSummary
}

StorePlan(ctx context.Context, projectKey string, prNumber int, plan PlanRecord) error
GetPlan(ctx context.Context, projectKey string, prNumber int) (PlanRecord, error)
```

replacing `StorePlanData(ctx, projectKey, prNumber, planData, summary)`
and `GetPlanData(ctx, projectKey, prNumber) ([]byte, ChangeSummary, error)`.

**Alternative considered**: adding an `args []string` parameter and a
fourth return value to the existing methods. *Rejected* — `StorePlanData`
already takes five parameters and `GetPlanData` already returns three
values; a fourth return makes every call site a four-name assignment for
a field most of them ignore. More to the point, Slice 7 adds Terraform and
Pulumi, whose plan artifacts will want to record more still (a plan file
path, a tool version). A record absorbs that; a parameter list pays the
same churn again each time.

**`ErrNoPlanData` is renamed `ErrNoPlan`.** Its meaning changes from "the
stored bytes are empty" to "no plan was recorded", and those are now
different conditions. Leaving the old name would make the identifier say
the opposite of what the code tests — the same species of defect as a
stale requirement citation, and just as hard to notice.

## Decision 3: a plan is recorded because it succeeded, not because it produced bytes

The two gates in the Overview both change to ask the question they
actually mean.

**Storing** (`result.go:113`) drops its byte test:

| | Condition |
|---|---|
| today | `ok && rec.Operation == p.GetPlanOperation() && len(result.PlanData) > 0` |
| after | `ok && rec.Operation == p.GetPlanOperation()` |

A successful plan records a `PlanRecord` whatever the tool handed back —
`Data` may be empty, and for Helmfile always is. The bytes become optional
payload; the *fact of a plan*, and the scope it ran with, are what the
Lock is for.

**Retrieving** (`redis.go:94`) tests `HasPlan` instead of
`len(data.PlanData)`. This is the change that makes a Helmfile mutating
operation reachable at all.

**This slice does not decide which outcomes record a plan** beyond
"succeeded". Whether a failed or no-change plan keeps its Lock is
Slice 18's question, and the two compose cleanly: Slice 18 changes
*whether there is a Lock*, this slice changes *what a Lock contains* and
*what gives it back*.

**Requirement 1.2 costs nothing.** It asks that the stored arguments be
the ones the Operation ran with rather than the ones its trigger line
requested. `HandleResult` already holds the `OperationRecord` the Job was
built from, so the value to store is `rec.ExtraArgs` — the same slice that
was marshalled into `TURNIP_EXTRA_ARGS`. Reading it from the record rather
than re-deriving it from the trigger is both correct and less work.

## Decision 4: only the plan operation accepts arguments

A trigger for any mutating operation that carries trailing arguments is
refused outright. The refusal names the arguments and says the plan's own
scope is what would be used.

**The rule is stated on the plan, not on apply**, which is what makes it
complete. "Apply takes no arguments" invites the reading that `sync` is
unconstrained — it mutates real infrastructure just as apply does, and a
scope nobody reviewed is exactly as dangerous there. Sorting by *which
operation the human read the output of* leaves no operation unaccounted
for, including any a future Plugin adds.

**Where**: `executeOne`, immediately after `isPlan`/`isApply` are computed
(`execute.go:79-80`) and before the Lock is touched, alongside the
ServiceAccount and submodule refusals already placed there "so that a
refused override must not leave a Lock held for an Operation that never
runs". The condition is `!isPlan && len(t.ExtraArgs) > 0` — note it keys
off *not being a plan*, so it covers `sync` and anything else a Plugin
exposes, rather than enumerating operation names.

**Why not the parser**: `ParseTriggers` cannot know. It deliberately does
not validate operations — `github-integration/design.md` records that
`operation` is "not validated against a Plugin's operations" because the
package has no Plugin registry. Which token is the plan is tool-native
(`diff` for Helmfile, `plan` for Terraform, `preview` for Pulumi) and only
`GetPlanOperation()` can answer it.

**Why refuse rather than ignore**: Requirement 2.3's reasoning, worth
keeping in the code's comment as well as the spec — a silently discarded
argument is indistinguishable from an honoured one until the
infrastructure changes, and by then the author's belief about what they
applied is wrong with nothing on the page to correct it.

**It does not weaken destroy-planning for the tools that can do it.**
Global Requirement 6.1 has the user plan a destroy with
`/turnip plan -- -destroy`; that is a *plan* carrying arguments, which
stays legal. Requirement 6.5 then has the apply "use the plan from the
lock" — under this design the stored `PlanArgs` contain `-destroy`, so the
apply replays it. That requirement becomes true for the first time here
rather than being contradicted. It is precisely because Terraform can
express a destroy *as a plan* that Decision 6 does not touch it.

## Decision 5: every mutating operation replays the scope and releases the Lock

The release at `result.go:122` stops being apply-specific. Within
`if result.Success`, the switch becomes:

| Case | Action |
|---|---|
| the Operation is the Plugin's plan operation | store the `PlanRecord`, keep the Lock |
| anything else | release the Lock |

No new field is needed on `OperationRecord`: `HandleResult` already looks
the Plugin up and compares `rec.Operation` against `GetPlanOperation()` at
`result.go:113`, so the same test that selects the store branch selects
the release branch by falling through it.

**And one existing field stops being needed, which this design first got
wrong.** An earlier version said `IsApply` stays for the replay path in
`executeOne`. It does not: Decision 5 also broadens that fetch to every
mutating Operation, so nothing reads the field at all once both halves
land. `IsApply` and its local were removed, along with
`OperationRecord.PlanData` — the Job takes the plan bytes from a local,
never from the record. Both were found by a dead-code sweep immediately
after this slice, not by the design, which is the lesson: a field kept
"for" a path deserves re-checking when that path changes in the same
slice.

**A record always carries a registered tool**, because `executeOne`
rejects an unsupported one before any record exists, so the fall-through
cannot release a Lock for a Plugin turnip does not have.

Symmetrically, the retrieval side broadens: `executeOne` currently fetches
the plan only `if isApply` (`execute.go:115`). It fetches for every
mutating operation instead, so `sync` replays the recorded scope rather
than running unscoped. It already requires the Lock — `execute.go:111`
puts every non-plan operation through `IsLockedByPR` — so this adds the
plan record to a gate that already exists rather than introducing one.

**What this does not change**: a `sync` with no prior `diff` is already
refused today, because `execute.go:111` routes every non-plan Operation
through `IsLockedByPR` and only a plan acquires a Lock. The gate exists;
this slice adds to it the requirement that the Lock carry a recorded
plan. In practice that is the same gate, since after this slice every
successful plan records one — the two differ only for a Lock taken before
the upgrade, which the Edge cases table covers.

## Decision 6: Helmfile does not expose `destroy`

`HelmfilePlugin.GetOperations()` returns `["diff", "apply", "sync"]`. The
`"destroy"` entry at `helmfile.go:23` is removed, and with it the
operation.

Decision 5 gives every mutating operation a reviewed plan to inherit.
`destroy` cannot have one, for two independent reasons:

- **It has no dry-run.** `helmfile destroy`'s complete flag set as of
  v1.8.0 is `--args`, `--cascade`, `--concurrency`, `--skip-charts`,
  `--deleteWait`, `--deleteTimeout`. There is no `--dry-run`. The only
  safety gate is the global `--interactive`, a confirmation prompt that
  an automated Runner cannot answer.
- **It is not a convergence operation.** `destroy` uninstalls every
  release the selector matches *regardless of `installed:`*, so it does
  not converge to the declared state. `helmfile diff` describes
  convergence to that state, so it cannot describe a destroy even in
  principle.

A `destroy` gated by a `diff` would therefore inherit a plan describing a
different operation — which is exactly the condition Atlantis gates
`import` and `state rm` for. Both are documented as discarding the plan
result and requiring a fresh plan before apply, leaving the pull request's
approval evidence no longer describing what happened.

**Nothing is lost, because teardown keeps a reviewed path.** Mark the
release `installed: false` — helmfile's reference documents the field as
*"set `false` to uninstall this release on sync"* — and the removal then
travels the ordinary `diff` → `apply` loop. `helmfile diff` does surface
it: since PR #1186 it prints the pending removal as
`<name> (<chart>) DELETED` beneath an `Affected releases are:` heading and
exits 2 under `--detailed-exitcode`. That behaviour is live in current
source (`pkg/app/app.go`, `pkg/app/run.go`) and covered by
`pkg/app/diff_test.go`.

Two caveats recorded rather than glossed: that output is a **summary line,
not a line-level diff** — helm-diff structurally cannot diff a deletion,
per the maintainer's own explanation — so a removal is reviewed more
coarsely than a change. And it is not documented in prose on helmfile's
docs site, only in source and the merged pull request. It remains strictly
more review than `destroy` offers, which is none.

**This is Helmfile-specific, not a platform rule.** Terraform and Pulumi
express a destroy *as a plan* (`terraform plan -destroy`,
`pulumi preview --destroy`), so the artifact genuinely describes the
destruction and the review loop holds. Slice 7 keeps destroy there through
the plan path; global Requirement 6 and Property 21 stand untouched.

**Alternative considered**: keep `destroy` and gate it behind a
server-side operation allowlist, mirroring Atlantis's `--allow-commands`
(whose default omits `import` and `state`). *Rejected* — an allowlist
answers "may this operator run it?", when the problem is that *no* review
of it is possible. A flag would let a deployment opt into an unreviewable
mutation, which is the guarantee this slice exists to establish. Should
turnip later want an operation allowlist for unrelated reasons, it is a
separate concern and not a substitute for this.

**This reverses a documented decision, deliberately.**
`plugin-helmfile/design.md:81-101` includes `"destroy"` in
`GetOperations()`. That decision settled *how* destroy is invoked — a peer
operation rather than a flag layered on `apply` — on interface-shape
grounds, rejecting `ExtraArgs` string-sniffing. It never asked whether
destroy could be offered at all, because the plan/apply review-loop
guarantee is what this slice introduces. Its stated authority was global
design Property 22, which asserts *"Destroy should execute `helmfile
destroy`"* while its own scope line reads **"Validates: Requirements 13.2,
13.3, 13.4, 13.5"** — none of which mention destroy. Global Requirement
13.2 lists only `diff`, `apply` and `sync`; 13.7 adds destroy "when
explicitly requested", a conditional the implementation dropped.

## Decision 7: the first `-` token starts the arguments

The grammar at `github-integration/design.md:319` becomes:

```
line     := "/" tool WS operation (WS project)* (WS argStart (WS arg)*)?
argStart := "--" | token beginning with "-"
```

One scan over the tokens after `operation` finds the first token beginning
with `-`. If that token is exactly `--` it is consumed as a delimiter;
otherwise it is itself the first argument. Everything before it is a
Project name, everything after it is an argument, verbatim.

All three of Requirement 3's criteria fall out of that single rule rather
than needing separate handling:

| Criterion | Why it holds |
|---|---|
| 3.1 first `-` token starts the arguments | the rule itself |
| 3.2 explicit `--` still works and takes precedence | `--` begins with `-`, so it is found by the same scan; being exactly `--` is what makes it consumed rather than passed through |
| 3.3 a later `--` is an ordinary argument | the scan stops at the first match, so subsequent tokens are never re-examined — already true today |

The parser keeps producing `ExtraArgs` for every operation. It has no
Plugin registry and cannot tell a plan from a mutating operation, so
Decision 4's refusal is what rejects them, one layer later, where the
answer is knowable.

**Alternative considered**: keeping `--` mandatory and treating this as
documentation rather than grammar. *Rejected* — the requirement exists
because `/turnip diff web -l name=x` currently fails with an
unmatched-Project error, which reads as turnip not knowing the Project
rather than as a syntax problem. A message cannot fix that; the grammar
can.

**Depends on Slice 21 for safety, not for function.** A Project whose name
begins with `-` becomes unaddressable under this rule. Slice 21 rejects
such names at configuration-parse time, which turns a confusing runtime
mismatch into a startup error. If this slice lands first the gap is
narrow — such a name is already unusable in practice — but the roadmap's
sequencing note recommends Slice 21 first for exactly this kind of
overlap.

## Edge cases

| Case | Behaviour |
|---|---|
| Helmfile `diff` then `apply` | apply runs — the case that is impossible today |
| scoped `diff` then bare `apply` | apply replays the stored scope |
| `diff` with no arguments, then `apply` | `HasPlan` true, `PlanArgs` empty; apply replays nothing |
| automatic plan, then `apply` | `planTargetsFor` never sets `ExtraArgs`, so the record stores none and apply replays none |
| `diff` then `sync` | sync replays the diff's scope and releases the Lock |
| `/helmfile destroy` | refused — `operation "destroy" is not recognized for tool "helmfile"` (`target.go:79`), the same path any unknown operation takes |
| removing a release | mark it `installed: false`; `diff` reports `<name> (<chart>) DELETED`; `apply` performs it — reviewed like any other change |
| `sync` with no prior `diff` | refused — no Lock is held, so there is no reviewed scope to inherit |
| any mutating operation carrying arguments | refused before any check run or Job; the message names the arguments |
| `apply` with no prior plan | unchanged — "no lock (or a different PR's lock) is held" |
| plan ran, Lock manually unlocked, then `apply` | unchanged — the Lock is gone, so `GetPlan` reports no Lock |
| `/turnip diff web -l name=x` | `web` is a Project, `-l name=x` the arguments |
| `/turnip diff -- -l name=x` | identical result; the `--` is consumed |
| `/turnip diff web -- -l x -- y` | first `--` delimits, second is an ordinary argument |
| `/turnip plan -- -destroy` then `apply` (Terraform, Slice 7) | the plan records `-destroy`; the apply replays it (global Requirement 6.5) |
| Terraform/Pulumi (Slice 7) | store real `Data` *and* `Args`; `HasPlan` true either way; destroy stays available through the plan path |
| a Lock written before this slice | no `has_plan` key → decodes false → mutating operations refused with "a new plan is required" |

That last row is the migration, and it is deliberately the conservative
direction: an in-flight pull request holding a pre-upgrade Lock is asked
to re-plan rather than having an unknown scope replayed on its behalf. No
migration step is needed, and the condition clears itself on the next
plan.

## Testing approach

The tests that matter are the ones that fail today. The first three are
the regression net for the Overview's discoveries:

- **A Helmfile plan records a plan.** A plan result with `PlanData: nil`
  must still call `StorePlan` with `HasPlan` true. Fails today at
  `result.go:113`.
- **An apply after a Helmfile plan is not refused.** The end-to-end
  assertion that both byte gates are gone. Fails today at `redis.go:94`.
  Stated as a behaviour rather than a mock expectation, because mock
  expectations are what hid the bug —
  `TestExecuteOne_ApplyWithoutPlanDataIsRejected` needs re-reading in this
  light, not deleting: the refusal is still correct when no plan ran.
- **A successful `sync` releases the Lock.** Fails today at
  `result.go:122`, and is the assertion that would catch anyone narrowing
  the release back to `IsApply`.
- **`destroy` is not a Helmfile operation.** Asserted twice: on
  `GetOperations()` directly, and end to end — a `/helmfile destroy`
  trigger is rejected as unrecognized and creates no Lock, check run or
  Job. The existing `helmfile_test.go` rows covering destroy
  (lines 34, 98, 172) and the `property_test.go` sampled-operation set
  (line 37) are updated rather than deleted, so nothing silently stops
  being exercised.
- **`sync` replays the recorded scope**, asserted on the built Job's
  `TURNIP_EXTRA_ARGS` — the only place the value is observable end to end.
- **Every mutating operation refuses arguments, with no side effects** —
  no check run, no Job, no Lock read — table-driven over `apply` and
  `sync` so a future operation is not silently exempt. Asserted by
  absence, as Slice 19 did, since asserting the message alone would pass
  with the guard in the wrong place.
- **A plan still accepts arguments**, which is the other half of the same
  table and guards against over-broad refusal.
- **The stored arguments are the Operation's, not the trigger's** —
  asserted by storing a record whose `ExtraArgs` differ from the trigger
  line's, and checking which reaches the Lock.
- **`HasPlan` survives a JSON round trip** in all three states: absent
  (legacy), true with empty args, true with args.
- **Grammar table tests** in `internal/github`, one row per line of
  Decision 7's criteria table plus the existing `--` cases, which must
  keep passing unchanged.

## Amendments to earlier slices

This slice changes contracts four earlier slices committed to, so each
needs an appended amendment task rather than an edited history:

| Slice | What changes |
|---|---|
| 2 `plugin-helmfile` | `GetOperations()` drops `"destroy"`; Requirements 3.2 and 3.8 amended; the "Reconciling `GetOperations()` with global Requirement 13" section records the reversal and why the question it answered was a different one — next task number is 17 |
| 3 `redis-lock-manager` | `LockData` gains `HasPlan`/`PlanArgs`; `StorePlanData`/`GetPlanData` become `StorePlan`/`GetPlan` taking a `PlanRecord`; `ErrNoPlanData` becomes `ErrNoPlan`; the "empty `PlanData` means no plan data" rule in its Data Model and Edge Cases sections is replaced by `HasPlan` |
| 4 `github-integration` | the grammar in `design.md:319` and Requirement 4.2's `[-- extra args]` wording gain the implicit delimiter — next task number is 25 |
| 6 `server-orchestration` | `executeOne` gains the argument refusal and the replay substitution; `HandleResult`'s storage condition loses its byte test and its release stops being apply-specific |

`docs/usage.md` also carries an example that this slice makes invalid —
`/turnip apply web -- --auto-approve` — alongside the `[-- extra args]`
grammar line and, at lines 54 and 117, `destroy` in the list of Helmfile
operations. `docs/troubleshooting.md:172` and `docs/configuration.md:353`
name destroy too. All are Requirement 5's business, recorded here so the
documentation task is not discovered late.

**The global spec needs two amendment markers**, which is a change from
this design's first draft:

- **Requirements 13.7 and 13.9** mandate Helmfile destroy support and are
  overturned by Decision 6. 13.2, which lists only `diff`, `apply` and
  `sync`, becomes correct as written.
- **Design Property 22** asserts destroy behaviour and must drop that
  clause; its "Validates: Requirements 13.2–13.5" line then matches what
  it actually claims.

Requirement 7.5 still needs no marker — it is being implemented rather
than overturned — and Requirement 6.2's extra-argument handling is
extended rather than contradicted. Requirement 6.5 — apply "uses the plan
from the lock" — becomes true for Helmfile for the first time.
