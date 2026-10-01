# Requirements: What a Mutating Command Changed (Slice 47)

## Introduction

**A bug, found 2026-09-29.** Every Helmfile mutating command reports
"no changes", whatever it did. An apply that upgraded three releases was
reported on the pull request as:

```text
No changes in web.
✅ web: apply, no changes
```

while its own transcript, one click below, listed the three releases it
upgraded. The plan before it had correctly reported `+0 ~3 -0`.

The cause is in the Plugin, not the rendering: `HelmfilePlugin.Execute`
computes a change summary only when the operation is `diff`, and returns
a zero summary for `apply` and `sync`. The comment, the Project_Check
Title and the summary line all render that zero faithfully.

The wrong count changes nothing about what ran or what turnip allows
next: a mutating Operation's summary decides no Lock event (only a plan's
does) and no Outcome. It is still the line a reviewer reads to learn what
happened to the infrastructure, and it says nothing happened.

**Where the truth is.** Helmfile ends every apply and sync that changed
something with summary tables naming each release it acted on: updated,
reinstalled and deleted releases, and failed ones. Current versions title
them `Updated Releases` inside a line of `=`; versions up to v1.5.x
wrote `UPDATED RELEASES:`. A helmfile with sub-helmfiles prints
one table per sub-helmfile that changed something, so one run can carry
several. Counting diff blocks instead
would be wrong for apply: a release whose diff is "empty after
suppression" is still upgraded, and appears in the table.

The plan is not affected: the same run's plan counted three changes, the
release with the suppressed diff among them, and the apply upgraded
exactly those three.

## Glossary

- **Mutating_Operation**: any Operation a Plugin exposes that is not its
  plan Operation (Slice 20).
- **Release_Table**: one of Helmfile's summary tables of affected
  releases: a banner line naming it, a header row, then one row per
  release until a blank line.

## Requirements

### Requirement 1: A mutating command reports what it changed

#### Acceptance Criteria

1. WHEN a Helmfile Mutating_Operation succeeds, ITS change summary SHALL
   count the releases Helmfile reports in its Release_Tables
2. EVERY Release_Table in the output SHALL be counted, one per
   sub-helmfile, summed
3. A release in an updated or reinstalled table SHALL count as changed,
   and one in a deleted table as destroyed; the failed tables are not
   counted
4. WHERE the output carries no Release_Table, THE summary SHALL be zero,
   and render as "no changes", as today
5. THE count SHALL be read from the tool's output as Helmfile prints it,
   with terminal color codes, since Helmfile colors the table banner
6. THE counts SHALL be read from the `=`-framed banner Helmfile prints
   from v1.6.0 on; the older upper-case banner ending in a colon SHALL NOT
   be parsed
7. WHERE a run's output carries the older banner, ITS result SHALL render
   as succeeded without counts (Requirement 5.3), never as "no changes"
8. THE documentation SHALL state that counts after an apply or sync need
   Helmfile v1.6.0 or later

*Rationale for 1.6 and 1.7 (decided 2026-09-30): supporting every
format Helmfile has used grows the parser for versions nobody should be
starting on. The framed banner covers v1.6.0 onward (a fixed-width frame
until v1.7.2, centered on the table from v1.7.3). An older run is told
apart by its output, not its version: `uses:` may name `latest`, a
digest or a custom image, none of which says which Helmfile is inside.
An older run that changed nothing prints no banner and correctly reads
"no changes"; one that changed something prints the older banner and
reads "succeeded". Neither misleads.*

*Rationale for 1.3: Helmfile's table does not separate a first install
from an upgrade, and the plan's count (Slice 2) does not either: both
report a changed release as a change. Matching the plan keeps the two
lines comparable: a plan of `~3` followed by an apply of `~3` is the
reader's confirmation that the apply did what was reviewed.*

### Requirement 2: `sync` reports what it ran

#### Acceptance Criteria

1. THE rule of Requirement 1 SHALL apply to `sync` unchanged: it reports
   the releases in its Release_Tables

*Rationale: `sync` upgrades every release whether or not it changed,
which is its whole difference from `apply` (the Plugin's
`ActsWithoutChanges`), and Helmfile's table lists them all. Reporting
them is honest: those releases were upgraded. A sync reporting fewer
releases than it ran would be the same bug in reverse.*

### Requirement 3: A failed command keeps today's rendering

#### Acceptance Criteria

1. A Mutating_Operation that fails SHALL render as failed, as today,
   whatever its Release_Tables say

*Rationale: the summary line shows "failed" rather than counts for any
failed Operation (`summaryLine`), and a partial count beside a failure
would invite reading it as complete. The transcript still shows every
table Helmfile printed.*

### Requirement 4: Tested from Helmfile's real output

#### Acceptance Criteria

1. THE parser SHALL be tested against fixtures in Helmfile's real output
   format: a single table; several tables from sub-helmfiles; a deleted
   table; a reinstalled table; a failed table; no table; the framed
   banner in both widths, with and without color codes; the older banner,
   rendering "succeeded"; and a release whose diff was empty after
   suppression that still appears in the table
2. FIXTURES SHALL use neutral placeholder names for releases,
   namespaces, environments, charts, registries and account ids, never
   names from a real deployment
3. AN end-to-end test SHALL show a successful apply's count reaching the
   comment's summary line and the Project_Check Title

### Requirement 5: Each Plugin decides whether its mutating commands report changes

**User Story:** As the author of a Plugin for a tool that applies a
saved plan, I want to leave the apply's counts unreported, since the
plan already showed them, without turnip rendering that as "no changes".

#### Acceptance Criteria

1. EACH Plugin SHALL declare whether a successful Mutating_Operation's
   change summary is meaningful, given that run's transcript, so a
   Plugin can decide per run (Helmfile, for an older version's output)
   or for every run
2. THE Helmfile Plugin SHALL declare that they do (Requirements 1 and 2)
3. WHERE a Plugin declares that they do not, for a run or for every run,
   A successful
   Mutating_Operation SHALL render as succeeded, with no counts, in the
   summary line, the comment's headline and the Project_Check Title, and
   SHALL NOT render as "no changes"
4. THE declaration SHALL NOT affect a plan Operation, which always
   reports its counts
5. THE decision SHALL be read by the Server from the Plugin_Registry, so
   the Runner-to-Server wire format does not change

*Rationale for 5.1 (decided 2026-09-30): for a tool whose apply replays a
saved plan, the counts repeat what the plan showed, and other GitOps IaC
tools show the log rather than parse it. For a tool with no plan file,
like Helmfile, the apply works out its own changes, which can differ
from the plan's, so its summary says something the plan did not. Which
applies is a property of the tool, so it belongs to its Plugin.*

*Rationale for 5.3: "no counts" and "zero changes" are the same value
today, which is this bug. A Plugin that chose not to count would
otherwise report "no changes" for every apply, by design.*

## Out of Scope

- **Terraform and Pulumi**: their Plugins do not exist yet (Slice 7).
  They are expected to report no counts for their mutating commands,
  rendering "succeeded" (Requirement 5): each applies a saved plan whose
  counts the plan already showed. Recorded in Slice 7's roadmap entry.
- **Separating installs from upgrades** in the count; Requirement 1.3
  explains why not.
- **Redis durability and the bare-apply refusal's wording.** Found in
  the same report (a plan lost to a Redis restart, and a refusal that
  gave no reason); set aside by the repository owner for now.
