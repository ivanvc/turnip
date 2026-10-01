# Requirements: What the Checks List Says (Slice 35)

## Introduction

A check run's title is the one line GitHub shows beside its name, in the
checks list and the merge box, without a click. turnip mostly spends it
repeating the icon next to it. A completed plan's Project_Check reads
`turnip/diff/web — success` while the change counts sit in the summary one
click away; a running one reads `in progress` beside a spinner; a failed
one reads `failure` beside a ❌.

Slice 37 added the Aggregate_Check with placeholder titles
(`1/2 projects applied`), left to this slice so that one sweep covers every
title rather than leaving a fifth site worded differently.

**The rule this slice applies everywhere**: a title never repeats the
status icon. When turnip knows the cause of an outcome, the title names it
in plain words; when it does not, the title states the plain fact and does
not guess. turnip's own failures (it could not clone, could not create the
Runner Job) are known causes. A tool exiting non-zero is not — the reason
is somewhere in the tool's output, and a confident wrong summary in a list
is what people act on without clicking — so the title says only that it
exited, and with what code.

**The Runner does not currently report which step failed.** It sends an
exit code and a free-text message, with `-1` standing for "turnip failed
around the tool". Naming failures by category therefore needs the Runner
to send the category over gRPC. Deriving it by parsing the message was
rejected: it breaks the first time a message is reworded.

This slice refines the global spec's Requirement 9 (GitHub Status Checks).
Check run *names* are untouched: they are identities that branch
protection matches on (Slice 37).

## Glossary

Terms additional to the global spec's and Slice 37's glossaries:

- **Title**: a check run's `output.title` — the one line GitHub shows
  beside the check's name.
- **Failure_Category**: which step of an Operation failed, as the Runner
  reports it (Requirement 5).
- **Scope_Marker**: the arguments an Operation ran with, as the comment's
  per-Project summary line already renders them (`-l name=api`).
- **Change_Counts**: `+<add> ~<change> -<destroy>`, as the comment already
  renders them.

## Requirements

### Requirement 1: One formatter, every site

#### Acceptance Criteria

1. THE Server SHALL build every Title — Project_Check and Aggregate_Check,
   at creation, completion, timeout and failure — through one shared
   formatter
2. THE formatter SHALL reuse the comment's existing renderings of
   Change_Counts, "no changes" and the Scope_Marker, so the same outcome
   reads the same in the comment and in the checks list
3. A Title SHALL be plain text: it SHALL NOT be HTML-escaped the way the
   comment's `<summary>` line is, and SHALL NOT contain markdown
4. A Title SHALL NOT use `·` as a separator; parts are joined with commas,
   as the comment's verdict line joins its notes
5. A Title SHALL NOT consist of a status word the check's icon already
   shows (`success`, `failure`, `in progress`)

*Rationale for 1.1: the sites that set a Title today are
`execute.go:233`, `execute.go:317`, `result.go:93`, `sweep.go:111` and
`verdictFor` in `verdict.go`. Wording that lives at each site is how five
sites end up with five voices.*

*Rationale for 1.3: the comment's summary line escapes its values because
GitHub renders `<summary>` as HTML. A Title is not HTML; escaping it would
put `&lt;` in front of the reader.*

### Requirement 2: A completed Project_Check says what changed

#### Acceptance Criteria

1. WHEN an Operation succeeds with changes, THE Title SHALL be its
   Change_Counts, e.g. `+1 ~4 -2`
2. WHEN an Operation succeeds with none, THE Title SHALL be `no changes`
3. WHEN the Operation ran with arguments, THE Title SHALL append its
   Scope_Marker, e.g. `+0 ~1 -0, -l name=api`

### Requirement 3: A running Project_Check says what is running

#### Acceptance Criteria

1. WHEN a Plan_Operation starts, THE Title SHALL be `running`, followed by
   its Scope_Marker when it has arguments (`running, -l name=api`)
2. WHEN a Mutating_Operation starts, THE Title SHALL be
   `running the recorded plan`, followed by the recorded plan's
   Change_Counts and, when it was scoped, its Scope_Marker
   (`running the recorded plan, +1 ~4 -2, -l name=api`)

*Rationale for 3.2: what an apply is about to change is exactly what
someone watching it wants, and it is known at creation — the Lock recorded
the plan, its counts and its scope (Slice 20). The Operation and Project
are not repeated: they are in the check's name.*

*Rationale for 3.1: a plan has no output yet, so `running` says little
beyond the spinner. It is kept over an empty title so that a running check
still reads as a sentence, and it becomes useful when the plan is scoped.*

### Requirement 4: A failed Project_Check names the failure category

#### Acceptance Criteria

1. WHEN an Operation fails, THE Title SHALL be determined by its
   Failure_Category:

   | Failure_Category | Title |
   |---|---|
   | the tool exited non-zero | `<tool> exited <code>`, e.g. `helmfile exited 1` |
   | the clone failed | `clone failed` |
   | the workspace could not be created | `workspace could not be prepared` |
   | the tool could not be started | `<tool> could not be started` |
   | not reported | `failed` |

2. WHEN the Runner Job could not be created, THE Title SHALL be
   `Runner Job could not be created`
3. WHEN an Operation times out, THE Title SHALL be the diagnostic turnip
   already builds (`timeoutDiagnostic`, e.g. `Job X: container stuck
   (ImagePullBackOff)`)
4. THE full error message and output SHALL remain in the check run's
   summary and text, unchanged
5. THE exit code SHALL appear in a Title only for "the tool exited
   non-zero"; `-1` SHALL NOT appear in any Title

*Rationale for 4.1's tool name: `helmfile exited 1` is shorter and more
specific than `the tool exited 1`, and the Server knows the Project's tool
when it builds the Title.*

*Rationale for 4.1's last row: a Runner that reports a failure without a
category — including one a future step adds — still gets an honest Title.
This is the protocol field's zero value, not compatibility with older
Runners.*

*Rationale for 4.2 and 4.3: these are failures turnip itself observed, so
their causes are known and are named, following the rule in the
introduction. `failure` today looks the same as the tool rejecting the
author's change, and sends them to debug a diff that is fine.*

### Requirement 5: The Runner reports the Failure_Category

#### Acceptance Criteria

1. THE Runner's result message SHALL carry a Failure_Category, as a new
   field in `proto/turnip/v1/operation.proto`
2. THE Runner SHALL set it at each point where a failure reaches the
   Server:

   | Failure_Category | Set where (`internal/runner/run.go`) |
   |---|---|
   | the clone failed | `runCloneWith`'s two failures |
   | the workspace could not be created | `execute`, when `resolveWorkspace` fails |
   | the tool could not be started | `execute`, when the Plugin's `Execute` returns an error |
   | the tool exited non-zero | `execute`, when the tool's exit code is not 0 |

3. THE Server SHALL carry the Failure_Category and exit code from the
   result through to the Title
4. THE Failure_Category SHALL NOT be derived by inspecting the error
   message's text

*Rationale for 5.2: only these four failures reach the Server. A Runner
that cannot select a Plugin, reach the Server, or report its result never
delivers a result at all, and is caught by the start-deadline sweep, whose
Title is 4.3's.*

### Requirement 6: The Aggregate_Check counts Projects up to date

#### Acceptance Criteria

1. THE Aggregate_Check's Title SHALL count Projects **up to date** — applied,
   or planned with nothing to apply — out of all Projects in the
   Pull_Request_Record: `1/2 projects up to date`
2. WHEN any apply failed, THE Title SHALL append the count of failed
   Projects: `1/2 projects up to date, 1 failed`
3. WHEN affected Projects use a tool this Server cannot run, THE Title
   SHALL name the first and count the rest:
   `unsupported tool: infra uses terraform, and 2 more`
4. THE Titles for nothing affected (`no projects affected`) and an invalid
   configuration (`invalid turnip.yaml`) SHALL remain as Slice 37 wrote
   them
5. THE Aggregate_Check's summary SHALL continue to list every Project on
   its own line, including every unsupported one

*Rationale for 6.1: "applied" was untrue of a Project whose plan found
nothing to apply, which the count also includes. "Up to date" is true of
both: the infrastructure matches this pull request, which is what the
check guarantees before merge. "Done" was considered and is acceptable;
"up to date" was preferred for saying what is done.*

*Rationale for 6.3: the API documents no length limit for a Title, but
GitHub shows it on one line and cuts it off. One named example is
something to act on; the full list is one click away, in the summary.*

### Requirement 7: The comment and the checks list share vocabulary, not shape

#### Acceptance Criteria

1. THE comment's verdict line SHALL be unchanged by this slice
2. A word used in both the comment and a Title SHALL mean the same thing
   in both — "failed" is an Operation that did not succeed, "no changes" is
   a plan that found none
3. "Up to date" SHALL appear only in the Aggregate_Check

*Rationale: the comment's verdict line reports what one trigger's runs
found (`**7 changes across 3 projects** — 1 with no changes, 1 failed.`);
the Aggregate_Check reports how far the pull request is from mergeable,
across every trigger. They answer different questions, so neither is
forced into the other's shape. "Up to date" is meaningless for one
trigger: a plan never makes anything up to date.*

### Requirement 8: Documentation

#### Acceptance Criteria

1. `docs/usage.md` SHALL show example Titles for the Project_Check and
   Aggregate_Check states
2. `docs/troubleshooting.md` SHALL explain each failure Title and where the
   full error is

## Out of Scope

- **Extracting a failure's reason from a tool's output.** Per-Plugin
  parsing is the only way to name why a tool failed, and it is a separate,
  per-tool concept.
- **Refusal Titles.** Slice 36 adds check runs for refused Operations; it
  uses this slice's formatter.
- **Check run text over 65,535 bytes.** GitHub rejects an `output.text`
  larger than that, and the completion site puts the tool's entire output
  there, so a large enough diff would leave a Project_Check stuck in
  progress. Found while settling this slice; it is a bug, and gets its own
  spec.
- **Check run names**, which stay as Slice 37 set them.
