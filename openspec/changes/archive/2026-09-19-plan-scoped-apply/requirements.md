# Requirements: Apply Exactly What Was Planned (Slice 20)

## Introduction

turnip's Lock exists to guarantee that an apply does exactly what its plan
showed. For Terraform that guarantee is carried by a plan file: the plan
is an artifact, the artifact is stored, and applying it cannot touch
anything the plan did not.

**Helmfile has no such artifact, and turnip stores nothing in its place.**
`HelmfilePlugin.Execute` returns `PlanData: nil`, and two separate gates
key off those absent bytes: `HandleResult` only stores a plan when
`len(result.PlanData) > 0`, and `GetPlanData` reports "no plan data" when
the stored bytes are empty. So a Helmfile Lock records *nothing* — not the
plan data, not the change summary, and not the arguments that decided the
scope of the diff.

**The consequence is that a Helmfile apply cannot run at all.** It is
refused with "no plan data stored; a new plan is required" every time, for
every Helmfile Project. This was discovered while designing the slice; the
tests miss it because every apply test injects synthetic plan bytes the
real plugin never produces.

That changes what this slice is for. Arguments do reach the tool today
(`args = append(args, opts.ExtraArgs...)`), and `executeOne` forwards
`t.ExtraArgs` with no branch on plan versus apply — so the moment an apply
becomes reachable, it runs unscoped:

| Sequence | Result once apply is reachable, if nothing else changes |
|---|---|
| `diff -- -l name=x` then `apply` | applies **everything** — the widest possible blast radius |
| `diff -- -l name=x` then `apply -- -l name=y` | applies a **different** scope than was reviewed |
| `diff` then `apply -- -l name=x` | applies less than planned — harmless, still not what was approved |

The second is the most dangerous: it looks deliberate, reads plausibly in
the pull request thread, and nothing marks the applied scope as one that
was never diffed.

**So this slice has to do two things at once**: make a Helmfile apply
reachable, and make it replay the plan's scope when it gets there.
Delivering only the first would ship exactly the hazard above — the Lock's
central guarantee failing toward doing more than was reviewed rather than
less.

**The arguments are the artifact.** Where Terraform stores a plan file,
Helmfile stores the arguments that produced the plan — because for
Helmfile those arguments *are* the description of what was examined.

**A second, smaller problem**: the arguments can only be written behind a
`--` delimiter. `ParseTriggers` splits the tokens after the operation at
the first `--`; without one, every remaining token becomes a Project name.
So `/turnip diff web -l name=example` fails with an unmatched-Project
error rather than running a scoped diff.

**Global requirements**: implements the unimplemented substance of
Requirement 7.5 — "WHEN an apply Operation is triggered, THE Server SHALL
verify the Lock is held by the current PR and retrieve the plan data from
the Lock" — which turnip satisfies literally (it retrieves the bytes) but
not in effect (the bytes are empty and the scope is not retrieved at all).
Extends Requirement 6.2's extra-argument handling from the `-destroy` flag
to arguments generally, and makes Requirement 6.5 — "THE user SHALL apply
the destroy plan by running '/turnip apply' … which uses the plan from the
lock" — true for Helmfile, where today it holds only for tools that have a
plan file.

**Explicitly out of scope**:

- **Teaching turnip which flags affect scope.** The rule below is that
  apply takes no arguments at all, precisely so that no per-tool
  knowledge of flag semantics is needed. A list of "scope-affecting
  flags" would need updating every time a tool gained one.
- **Terraform and Pulumi plan artifacts.** Slice 7 introduces those
  Plugins; whether they store a plan file, arguments, or both is theirs
  to decide. This slice must not assume a plan artifact exists.
- **Validating that stored arguments are still meaningful** at apply
  time — that a selector still matches something, say. The Lock records
  what was planned; the tool reports what it finds.

## Glossary

Terms additional to the global spec's glossary:

- **Plan_Scope**: whatever determines which subset of a Project an
  Operation acts on. For Helmfile this is the arguments; for Terraform it
  will be the plan file.
- **Trailing_Arguments**: tokens on a trigger line that are passed to the
  tool rather than interpreted by turnip.
- **Mutating_Operation**: any Operation a Plugin exposes that is not its
  plan Operation — for Helmfile, `apply` and `sync` once Requirement 4
  removes `destroy`. Defined by exclusion rather than by listing names, so
  an Operation a future Plugin adds is covered without amending this
  requirement.

## Requirements

### Requirement 1: A plan records the scope it ran with

**User Story:** As a reviewer, I want the Lock to remember what a plan
actually examined, so that applying it cannot reach further than the
review did.

#### Acceptance Criteria

1. WHEN a plan Operation completes successfully, THE Server SHALL store
   its Trailing_Arguments in the Lock alongside the plan data
2. THE stored arguments SHALL be the ones the Operation actually ran
   with, not the ones its trigger line requested, so that any
   normalization turnip performs is what gets replayed
3. WHERE a plan ran with no arguments, THE Lock SHALL record that
   absence, which is distinct from having recorded nothing

### Requirement 2: A Mutating_Operation replays the plan's scope, takes none of its own, and releases the Lock

**User Story:** As a developer, I want every operation that changes
infrastructure to do exactly what the diff showed, so that I never have to
reason about whether my two commands agreed.

#### Acceptance Criteria

1. WHEN a Mutating_Operation runs, THE Server SHALL pass the Lock's stored
   arguments to the tool
2. THE Server SHALL NOT accept Trailing_Arguments on a Mutating_Operation —
   the plan is the only Operation that chooses a scope, because it is the
   only one whose output a human reviews
3. IF a Mutating_Operation's trigger carries Trailing_Arguments, THEN THE
   Server SHALL refuse the Operation and say why, rather than ignoring
   them — a silently discarded argument is indistinguishable from an
   honored one until the infrastructure changes
4. THE refusal SHALL name the arguments it refused and state that the
   plan's own scope is what will be used
5. WHEN a Mutating_Operation completes successfully, THE Server SHALL
   release the Lock — today only the apply Operation does, so a successful
   `sync` or `destroy` leaves the Project locked with no route to release
   it but a manual unlock or closing the pull request
6. WHERE no Lock with a recorded plan is held by this pull request, THE
   Server SHALL refuse the Mutating_Operation, so that no operation
   reaches infrastructure without a reviewed scope to inherit

### Requirement 3: Trailing arguments need no delimiter

**User Story:** As a developer, I want to write a scoped diff the way I
would type it in a shell, so that the common case does not need a
delimiter I have to remember.

#### Acceptance Criteria

1. THE parser SHALL treat the first token beginning with `-` as the start
   of Trailing_Arguments, with every token before it a Project name
2. THE explicit `--` delimiter SHALL continue to work, and SHALL take
   precedence where present, so existing trigger lines are unaffected
3. WHERE `--` appears after arguments have already begun, it SHALL be
   passed through as an ordinary argument rather than treated as a second
   delimiter

### Requirement 4: Helmfile does not expose an Operation it cannot plan

**User Story:** As a reviewer, I want turnip to withhold any operation
whose effects no plan can describe, so that nothing reaches infrastructure
with the pull request showing a review of something else.

#### Acceptance Criteria

1. THE Helmfile_Plugin SHALL NOT expose `destroy` as a supported
   Operation — `helmfile destroy` has no dry-run, and it uninstalls every
   release its selector matches regardless of `installed:`, so it does not
   converge to the declared state that `helmfile diff` describes
2. WHERE a trigger names `destroy` for a Helmfile Project, THE Server
   SHALL reject it as an unrecognized Operation, by the same path as any
   other unknown Operation name, and SHALL create no Lock, check run or
   Runner Job
3. THE rule SHALL be specific to Helmfile rather than to destroy in
   general: a Plugin whose plan Operation can express a destruction —
   Terraform's `plan -destroy`, Pulumi's `preview --destroy` — keeps that
   capability through the plan path, because there the artifact describes
   the destruction
4. THE removal SHALL NOT be expressed as an operator-configurable flag: a
   setting that permitted it would let a deployment opt into a mutation no
   reviewer can see, which is the guarantee this slice establishes

### Requirement 5: Documentation

#### Acceptance Criteria

1. `docs/usage.md` SHALL show a scoped plan written without `--`, and
   state that apply takes no arguments because it replays the plan's own
2. THE documentation SHALL explain that this is what makes an apply match
   its diff, rather than presenting it as a restriction
3. THE documentation SHALL stop listing `destroy` among Helmfile's
   operations, and SHALL describe the reviewed alternative — marking a
   release `installed: false`, which `diff` reports as a pending removal
   and `apply` then performs
