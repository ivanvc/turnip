# Requirements: What a Bare Command Targets (Slice 21)

## Introduction

turnip has two paths that choose which Projects an Operation runs for,
and they disagree.

The automatic plan filters: `MatchProjects(cfg.Projects, modifiedFiles)`
selects only Projects whose `whenModified` globs match the pull request's
changed files. A comment trigger does not: `resolveTargets` starts from
*every* configured Project and narrows only when the trigger names some.

So `/turnip plan` means **every Project in the repository**, regardless of
what the pull request touched.

**This is invisible with one Project and painful with eight.** It is not
merely slow. A plan acquires a Lock per Project and holds it until applied
or released, so one person typing four words locks the entire repository
against everyone else. Under today's lock rules a failed or no-change plan
among those keeps its Lock too, so the locks outlive the mistake.

**Prior art**: Atlantis' bare `atlantis plan` re-runs the autoplan set —
*"runs plan on the projects that were modified as determined by the
`when_modified` config"*. Its bare `atlantis apply` applies *"all
unapplied plans from this pull request"*. Both defaults are narrow, and
its `-p`/`-d` flags are how you reach past them.

turnip has the opposite default and no way to ask for the narrower thing,
which is the gap this slice closes.

**A bare apply is noisy for the same reason.** It targets every configured
Project, and `executeOne` rejects each one holding no Lock — so applying
one planned Project among eight produces one apply and seven refusals.

**Global requirements**: the two halves of this slice stand differently.

*The bare apply half amends Requirement 5.3*, which says a detected apply
trigger "SHALL trigger apply Operations for all Projects configured in
turnip.yaml" — written when every configured Project was the only
selection turnip had. Requirement 5.4, which covers naming a Project
explicitly, is extended rather than amended: `*` is a new Selector
standing beside a name, not a change to what a name means.

*The bare plan half fills a gap.* Requirement 4 governs the automatic plan
only, Requirement 5 governs apply, and Requirement 6.1 mentions a
comment-triggered plan solely to carry `-destroy`. No numbered criterion
says which Projects a comment-triggered plan targets, so Requirement 1
below overturns nothing.

**Explicitly out of scope**:

- **Matching against `Project.Directory`** rather than name. Requirement 2
  selects with patterns over *names*, which covers a repository that names
  its Projects for their paths (`gcp/project`) without a second mechanism.
  A repository that names them otherwise still cannot address them by
  location; that remains a gap, recorded in the roadmap's Backlog.
- **Changing the automatic plan.** It already filters correctly; this
  slice brings the comment path into line with it, not the reverse.
- **Lock lifecycle.** Slice 18 decides which outcomes release a Lock.
  This slice reduces how many Locks a careless command takes in the first
  place; the two compound but neither depends on the other.

## Glossary

Terms additional to the global spec's glossary:

- **Modified_Set**: the Projects whose `whenModified` globs match the pull
  request's changed files — what the automatic plan already selects.
- **Selector**: a token naming which Projects a trigger targets. Today
  only a Project name; this slice adds `*`.

## Requirements

### Requirement 1: A bare plan targets what the pull request modified

**User Story:** As a developer on a repository with many Projects, I want
`/turnip plan` to re-plan what my pull request actually touched, so that a
four-word comment does not lock every Project in the repository against my
colleagues.

#### Acceptance Criteria

1. WHERE a plan trigger names no Selector, THE Server SHALL target the
   Modified_Set — the same Projects the automatic plan would select
2. THE Server SHALL use the same matching the automatic plan uses, not a
   parallel implementation of it
3. WHERE the Modified_Set is empty, THE Server SHALL say so rather than
   silently doing nothing, since the reader typed a command and is owed
   an answer
4. WHERE the pull request's file listing reaches the maximum GitHub
   returns for one pull request, THE Server SHALL state in its reply that
   the listing may be truncated and that `*` targets every Project
   regardless — an incomplete Modified_Set would otherwise present as a
   plan that simply had less to do

### Requirement 2: `*` targets everything, and patterns select by name

**User Story:** As a developer, I want a way to plan everything
deliberately, and a way to reach a group of Projects that share a name
prefix, so that narrowing the default removes no capability and naming
eight Projects one at a time is not the only way to address a group.

#### Acceptance Criteria

1. WHERE a trigger names the Selector `*`, THE Server SHALL target every
   configured Project, subject to the tool filter already applied by a
   tool-scoped trigger
2. `*` SHALL be usable wherever a Project name is accepted
3. `*` SHALL NOT be combinable with Project names in the same trigger —
   a trigger either names Projects or asks for all of them
4. WHERE a Selector other than `*` contains `*`, THE Server SHALL treat it
   as a glob pattern matched against Project names, using the same glob
   engine and semantics `whenModified` uses
5. THE bare Selector `*` SHALL remain a reserved word meaning every
   Project rather than being evaluated as a pattern, because a `*` pattern
   does not match a name containing `/` and would silently exclude every
   Project named for its path
6. Patterns SHALL be combinable with Project names and with other
   patterns, since each selects a set and the union is well defined —
   unlike `*`, which already means all of them
7. WHERE a pattern matches no Project, THE Server SHALL report that once,
   rather than failing the whole command as it does for a named Project
   that does not exist
8. THE report SHALL quote the pattern that matched nothing, so that a
   mistyped pattern is visible as a mistake without being treated as one

### Requirement 3: Reserved Selectors cannot be Project names

**User Story:** As an operator, I want turnip to reject a Project name it
could never address, so that the failure arrives when I write the
configuration rather than when someone tries to plan it.

#### Acceptance Criteria

1. THE configuration parser SHALL reject a Project whose name contains
   `*`, which a trigger line would read as a pattern rather than a name
2. THE configuration parser SHALL reject a Project name beginning with
   `-`, which a trigger line would read as the start of tool arguments
3. THE rejection SHALL name the Project and say why the name cannot be
   addressed, rather than reporting a generic validation failure

### Requirement 4: A bare apply targets what this pull request planned

**User Story:** As a developer, I want `/turnip apply` to apply the plans
my pull request is holding, so that I do not receive a refusal for every
Project I never planned.

#### Acceptance Criteria

1. WHERE an apply trigger names no Selector, THE Server SHALL target only
   Projects for which this pull request holds a Lock with plan data
2. WHERE no such Project exists, THE Server SHALL report that once, not
   once per configured Project
3. Naming Projects explicitly SHALL continue to reach any Project, with
   the existing refusal when one holds no plan

### Requirement 5: Documentation

#### Acceptance Criteria

1. `docs/usage.md` SHALL state what a bare plan and a bare apply target,
   and show `*` for the everything case
2. THE documentation SHALL correct its current claim that omitting a
   Project list targets "every matching project" — today it targets every
   project, matching or not
3. THE documentation SHALL describe name patterns, including that `*`
   matches within one path segment and `**` crosses segments, matching
   what `whenModified` already means by the same syntax
4. THE documentation SHALL state that a Project name must be typeable as
   a single trigger token: a name containing whitespace parses as two
   selectors and can never be addressed, even though the configuration
   parser accepts it
5. THE documentation SHALL note that a trigger line containing two `*`
   characters is rendered as italics by Markdown, and should be written
   in backticks so the author sees what they typed
