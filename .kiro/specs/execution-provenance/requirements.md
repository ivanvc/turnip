# Requirements: Show What Ran and With What Scope (Slice 33)

## Introduction

Two things are true of every Operation turnip runs, and neither one is
visible on the pull request.

**The scope a plan ran with is durable state.** Slice 20 made a plan record
its arguments (`lock.PlanRecord.Args`, recorded through the Lock's
`EventPlanApplicable` edge — see `lockstate.go:38`, since Slice 18
replaced the `StorePlan` call this once cited) and made every
Mutating_Operation replay them (`execute.go:185`) while refusing arguments
of its own (`execute.go:99`). So a diff written with
`-l name=<release>` narrows what a later apply will do, for as long as that
Lock is held. Nothing in the comment says so: the summary line renders
`<project> · diff · +0 ~1 -0` (`comment.go:428`), identical to an unscoped
run, and the footer offers a bare `/turnip apply` beneath "This pull request
holds locks on `<project>` until applied or released" (`comment.go:382`).

That sentence is not unsafe. Slice 20 removed the dangerous case by refusing
an apply its own arguments, so nobody can apply a scope that was never
diffed. It is *stale*: Slice 17 wrote it before Slice 20 existed, and Slice
20 changed what "until applied" means without revisiting the text. What
remains is that a reviewer cannot distinguish a narrowed plan from a
whole-project one — while the scope itself outlives the comment, in Redis.

**turnip composes a command the author never sees.** `helmfile.go:51`
prepends `--environment <env>` taken from the Project's `config`, appends
the Operation and the trigger's own arguments, and runs the tool at
`helmfile.go:57`. The output block in the comment then opens directly with
the tool's first line of stdout. Nothing records which binary ran, at which
version, in which directory, or with which arguments — so reconstructing
what happened means opening `turnip.yaml` at that commit and replaying
turnip's injection rules by hand. That is the reconstruction that goes
wrong during an incident, and it is the one turnip is best placed to have
written down at the time.

Both gaps have the same shape: turnip knows something the reader needs and
does not say it.

## Glossary

Terms additional to the global spec's glossary:

- **Scope_Marker**: the short, always-visible indication on a Project's
  summary line that its Operation ran with Trailing_Arguments.
- **Execution_Transcript**: the annotation lines turnip writes into an
  Operation's own output, recording what it executed.
- **Provenance_Line**: the Execution_Transcript line naming the Project
  directory and the tool, as distinct from the line naming the command.
- **Injected_Argument**: an argument turnip supplies that the author never
  typed — today only `--environment <env>`, from the Project's `config`.
- **Resolved_Command**: the argv turnip actually passes to the tool —
  Injected_Arguments, the Operation, and Trailing_Arguments, in that order.

## Requirements

### Requirement 1: A scoped Operation is visible without expanding anything

**User Story:** As a reviewer, I want to see that a plan examined only part
of a Project, so that I do not read its change counts as covering the whole
Project.

#### Acceptance Criteria

1. WHERE a Project's Operation ran with Trailing_Arguments, THE comment
   SHALL render a Scope_Marker on that Project's summary line — the text a
   reader sees without expanding anything
2. THE Scope_Marker SHALL be rendered per Project, not once per comment
3. THE Scope_Marker SHALL reproduce the arguments verbatim, without
   interpreting which of them narrow scope and which do not
4. WHERE the arguments are too long for the summary line, THE marker SHALL
   be truncated there, and the full text SHALL remain available inside the
   Project's section
5. WHERE a Project's Operation ran with no Trailing_Arguments, THE comment
   SHALL render no Scope_Marker for it

*Rationale for 1.2: `ParseTriggers` stops collecting Project names at the
first `-`-prefixed token (`parser.go:101`), so a trigger carrying arguments
and no names falls through to bare-default selection and applies those same
arguments to every selected Project. One marker per comment would attribute
a scope to Projects it may not even be meaningful for.*

*Rationale for 1.3: Slice 20 put "teaching turnip which flags affect scope"
explicitly out of scope, because such a list needs updating every time a
tool gains a flag. Displaying arguments verbatim keeps that promise —
turnip reports what it passed, and the reader, who knows their own tool,
decides what it meant.*

### Requirement 2: The footer describes what an apply will actually do

**User Story:** As a reviewer deciding whether to approve, I want the
comment's own next steps to tell me what applying will cover.

#### Acceptance Criteria

1. WHERE a locked Project's recorded plan carries arguments, THE footer
   SHALL state that applying replays the recorded scope, rather than
   implying the Operation covers the whole Project
2. THE footer SHALL NOT offer a command that turnip would refuse — an apply
   written with arguments is rejected at `execute.go:99`, so the scope
   SHALL be conveyed as prose rather than as a copy-pasteable command
3. WHERE no locked Project recorded arguments, THE footer SHALL read as it
   does today

### Requirement 3: The output records what turnip executed

**User Story:** As an operator reading a pull request months later, I want
to know exactly what turnip ran, so that I do not have to reconstruct it
from configuration at that commit.

#### Acceptance Criteria

1. THE Runner SHALL write an Execution_Transcript into the Operation's
   output
2. THE transcript SHALL record the Resolved_Command for each command
   executed, including Injected_Arguments
3. THE Provenance_Line SHALL name the Project's directory relative to the
   repository, and the tool together with the version turnip requested for
   it
4. THE transcript SHALL record each command's exit status and elapsed time
5. THE transcript SHALL be written once per executed command, in execution
   order

*Rationale for 3.5: Helmfile runs exactly one subprocess today
(`helmfile.go:57`), but Terraform is `init` then `plan` (Slice 7). A record
shaped as "the command" becomes retroactively ambiguous the moment a second
one runs, and every transcript already written would need reinterpreting.*

*Rationale for 3.3: `resolveVersion` (`versions.go:125`) returns either the
version the Project requested or the tool's default, and a floating tag is
rejected before it gets there — so today the requested version is the
version that runs, and the Server already knows it.*

### Requirement 4: The transcript is emitted where the commands are run

**User Story:** As a maintainer, I want the record to be produced by the
code that actually executes, so that it cannot drift from what happened.

#### Acceptance Criteria

1. THE transcript SHALL be emitted by the shared command-execution seam
   (`execCommand`, `command.go:68`), NOT by each Plugin individually
2. THE transcript SHALL reach the comment through the Operation's existing
   output, requiring no new field on `OperationResult`
3. THE transcript SHALL be emitted at the point of execution, so that it
   precedes the output of the command it describes

*Rationale for 4.1: a Plugin that forgets is indistinguishable from a
command that never ran. Emitting from the seam every Plugin already calls
means a new Plugin is covered by existing code rather than by its author
remembering — the same reasoning that put Slice 20's argument refusal on
`!isPlan` rather than on a list of operation names.*

*Rationale for 4.3: emitting at execution time rather than composing the
record afterwards is what makes 3.5 hold for any number of commands, and
what lets a future live view (Slice 26) show each command before its
output with no further work.*

### Requirement 5: The transcript never carries a secret

**User Story:** As an operator, I want turnip's bookkeeping not to become a
disclosure channel.

#### Acceptance Criteria

1. THE transcript SHALL record a command's arguments, and SHALL NOT record
   the process environment
2. THE Resolved_Command SHALL pass through the Runner's existing redaction
   before being written
3. THE transcript SHALL NOT introduce absolute pod-internal paths into the
   comment, reusing the existing workspace-path stripping
   (`workspacepath.go:20`) rather than adding a second mechanism

*Rationale for 5.1: credentials reach the tool through the environment and
mounted files — cloud credentials, kubeconfig, the ServiceAccount token —
not through argv. Recording argv is therefore safe in a way that recording
the environment would not be, and the distinction is worth stating rather
than leaving to whoever extends this later.*

*Rationale for 5.2: the tool container should never hold the installation
token (`jobs/build.go` keeps it out), so redaction here is belt-and-braces
rather than a known leak. Passing through it anyway keeps the property true
by construction if that ever changes.*

### Requirement 6: turnip's annotations cannot be mistaken for, or corrupt, the tool's output

**User Story:** As a reader, I want to be able to tell turnip's voice from
the tool's, and I want neither to be able to forge the other.

#### Acceptance Criteria

1. THE annotation SHALL NOT begin with `+` or `-`, which belong to the
   payload — an annotation so prefixed would be read as a change
2. THE comment SHALL neutralise any sequence in rendered content that would
   terminate the enclosing code fence, for the Execution_Transcript and for
   the tool's own Output alike
3. THE annotation SHALL render consistently whether the Operation succeeded
   or failed

*Rationale for 6.2: `Output` is interpolated into a fence with no escaping
today (`comment.go:407`). Content that closes the fence early makes
everything after it render as markdown inside a comment authored by turnip,
which a reader trusts differently from one authored by a contributor. The
exposure exists already; adding a line built from trigger-supplied tokens
widens it, so this slice closes it for both.*

*Rationale for 6.3: `fenceFor` (`comment.go:508`) uses a `diff` fence only
on success, so a failed Operation's block has no syntax highlighting at
all — the annotation would look different in precisely the case a reader
scrutinises most.*

### Requirement 7: Documentation

#### Acceptance Criteria

1. THE documentation SHALL state that a plan's arguments are recorded and
   replayed by a later apply, and SHALL show where the pull request reports
   that
2. THE documentation SHALL describe the Execution_Transcript as the record
   of what ran, and SHALL state that it carries arguments but never the
   environment

### Note: what Slice 18 changed underneath this

Slice 18 landed after these requirements were written. Three things it
changed are load-bearing here:

- `StorePlan` is gone. A plan is recorded by the Lock's
  `EventPlanApplicable` edge, and the arguments it stores are the same
  ones — `rec.ExtraArgs`, the Operation's own.
- A mutating Operation is admitted only from `StatePlanReady`, so
  Requirement 2's footer has a second thing it could say: a Lock may be
  held with a plan that is no longer appliable. That is Slice 18's
  message to write, not this slice's, but the two sentences sit in the
  same place and must read as one voice.
- `ProjectResult` already carries `LockNote`, rendered in the trailer by
  `lockNote(r)` beside `nextSteps`. This slice's Scope_Marker and
  transcript join an area that now has an occupant; the design decides the
  order, and "whichever lands second rebases" has been settled by Slice 18
  landing first.

## Out of Scope

- **Classifying which arguments narrow scope.** Unchanged from Slice 20,
  and for its reason: a list of scope-affecting flags needs updating every
  time a tool gains one. This slice displays; it does not interpret.
- **Distinguishing stdout from stderr in the transcript.** `execCommand`
  already tags every line with its stream and the two are concatenated
  without a marker (`helmfile.go:62-68`). Using that tag is a real
  improvement and a separate change; this slice must not make it harder.
- **Serving output while it runs.** Slice 26 owns that. This slice is
  shaped so Slice 26 inherits the transcript rather than reimplementing it.
- **Recording the concrete version that actually ran under a floating
  tag.** The Backlog's "Resolving a floating tool version" owns that
  question, including where the resolved version would come from. Today
  every version is concrete, so Requirement 3.3 is satisfiable now, and the
  transcript is the surface that entry would later fill in.
- **Terraform and Pulumi command sequences.** Slice 7 introduces those
  Plugins. This slice must not assume a single command per Operation, but
  it does not need to know what theirs will be.
- **Rejecting a Project directory that escapes the workspace.** A separate
  Backlog entry, which shares Requirement 5.3's helper but is a validation
  change rather than a display one.
