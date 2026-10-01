# Requirements: One Place to Add a Tool (Slice 45)

## Introduction

The Plugin interface was designed so that everything tool-specific lives
in the tool's Plugin. The code has drifted from that. Knowledge of which
tools exist, and how each is provisioned, now lives in five places outside
`internal/plugin`, and they duplicate each other:

| Where | What it knows |
|---|---|
| `internal/config` (`config.go:116`, `validate.go:229`) | the tool names, as a fixed list `turnip.yaml` is validated against |
| `internal/github/parser.go:19` | the same names again, as the trigger-comment vocabulary (`/helmfile diff`) |
| `internal/jobs/versions.go` | each tool's image template, provisioning strategy, binary path, and a list of versions whose first entry is used when `uses:` names none |
| `internal/orchestrator/registry.go` | the Server's map from tool name to Plugin |
| `internal/runner/run.go:31` | the Runner's own `switch` from tool name to Plugin |

Adding a tool means editing all five. Worse, the two registries can
disagree: a tool the Server has a Plugin for but the Runner's `switch`
lacks builds a Runner Job that cannot find its Plugin. And the fixed list
in `internal/config` accepts tools no Plugin exists for, which is why a
Project using `terraform` passes validation and is only refused later, as
an unsupported tool (Slice 37).

This slice restores the design: **one registry of Plugins, the source of
every tool-specific fact**, with everything outside `internal/plugin`
asking it rather than keeping its own copy.

It is a refactor, with three deliberate breaking changes, taken here
because turnip is pre-1.0 and simpler is better: a tool with no Plugin
becomes a validation error (Requirement 3.3); `uses:` must name its
version, since turnip no longer supplies a default (Requirement 4); and
the version is used exactly as written, with no `v` added or removed
(Requirement 4). Together they change what a valid `uses:` line means, so
the schema version advances to `v1alpha3` (Requirement 5). It precedes Slice 44, which changes what a
Plugin declares about its image; doing the restructuring first means that
change happens in one place.

## Glossary

- **Plugin_Registry**: the one set of registered Plugins, keyed by name.
- **Tool_Name**: a registered Plugin's name (today `helmfile`).
- **Provisioning_Strategy**: how a Runner Job obtains its tool (today
  `copyOut`, copying the binary out of the vendor image into turnip's
  Runner image, and `runInImage`, running turnip's runner binary inside
  the vendor image).

## Requirements

### Requirement 1: One registry, for the Server and the Runner

#### Acceptance Criteria

1. THE Plugin_Registry SHALL be defined once, in `internal/plugin`
2. THE Server and the Runner SHALL both obtain Plugins from it; the
   Runner's own selection by name SHALL be removed
3. ADDING a tool SHALL require only writing its Plugin and registering it
   in that one place

*Rationale for 1.2: two registries can disagree, and when they do the
failure appears only in a Runner Job, after a Lock is held and a check is
running. One registry cannot disagree with itself.*

### Requirement 2: Each Plugin declares its provisioning

#### Acceptance Criteria

1. EACH Plugin SHALL declare its Provisioning_Strategy, its image, and
   where its binary lives in that image when the strategy copies it out.
   A Plugin SHALL NOT declare a default version
2. THE Provisioning_Strategies SHALL be defined and implemented outside
   any Plugin, once for every Plugin; a Plugin only names the one it uses
3. THE per-tool table in `internal/jobs/versions.go` SHALL be removed;
   `internal/jobs` SHALL receive the resolved image, strategy and binary
   path as inputs rather than looking a tool up
4. A Plugin SHALL NOT depend on Kubernetes types

*Rationale for 2.2 and 2.4: a strategy builds Kubernetes objects (init
containers, volumes, the main container). Implementing each once, outside
the Plugins, keeps those types out of every Plugin and means a new tool
reuses a strategy rather than reimplementing one. The names the Plugins
use need a home of their own that `internal/jobs` also imports, so that a
Plugin never imports `internal/jobs`.*

*Rationale for 2.3: a lookup table in `internal/jobs` is the central edit
this slice exists to remove. Passing resolved values in also makes the Job
builder testable without a Plugin.*

### Requirement 3: The tool vocabulary comes from the registry

#### Acceptance Criteria

1. `turnip.yaml` validation SHALL accept exactly the registered Tool_Names
   in `uses:`, receiving them as input so that `internal/config` depends on
   no Plugin
2. THE trigger-comment parser SHALL accept exactly `turnip` plus the
   registered Tool_Names, receiving them as input
3. A Project naming a tool with no registered Plugin SHALL be a validation
   error when `turnip.yaml` is parsed, listing the registered Tool_Names
4. THE validation error for a missing `uses:` SHALL list the registered
   Tool_Names, not a fixed list
5. THE orchestrator's handling of a `turnip.yaml` Project whose tool has
   no Plugin SHALL be removed, since 3.3 makes it unreachable: the
   `unsupported` Outcome and its verdict branch, Title and summary line
   (Slice 37, Requirements 7.3 and 7.4), the notice the automatic plan
   posts for such a Project, and `executeOne`'s "tool is not supported"
   refusal
6. Lookups of a Plugin for an Operation that already exists — a result
   arriving, a timeout, a Lock event, a result's comment section — SHALL
   keep handling a missing Plugin as they do today

*Rationale for 3.1 and 3.2: the configuration package is deliberately a
pure leaf, and the parser has no business knowing which tools exist.
Receiving the names keeps both independent while making the registry the
only definition.*

*Rationale for 3.3, the first of the three breaking changes: today such
a Project passes validation and is refused afterwards as an unsupported
tool (Slice 37, Requirement 7.3) — a failed `turnip` check naming the tool. After this, it
is an invalid `turnip.yaml` like any other, reported with every other
violation in one pass, and also a failed `turnip` check (Slice 37,
Requirement 7.1). It is caught earlier with the same visibility.*

*Rationale for 3.5 and 3.6: they differ in where they start. The
unsupported-tool path starts from `turnip.yaml`, which after 3.3 can no
longer name an unregistered tool, so it is a whole feature path (an
Outcome, a verdict branch, a Title, a notice, a refusal) that cannot
trigger; its job moves to validation, which reports it better, on the pull
request, in one pass. The lookups start from an Operation already
dispatched: one sent before a deploy that removed its Plugin still
delivers a result, times out, or holds its Lock afterwards, and each lookup
is a line or two that keeps a missing map entry from being a nil
dereference in the Server.*

### Requirement 4: The repository states the exact version

#### Acceptance Criteria

1. `uses:` SHALL name a version: `<tool>@<version>`
2. `uses:` with no `@` SHALL be a validation error that says to name a
   version, with an example (`helmfile@v1.7.4`)
3. turnip SHALL keep no default version, and no list of known versions,
   for any tool
4. THE version SHALL become the image's tag exactly as written: turnip
   SHALL NOT add or remove a `v`, for any tool. `internal/config` SHALL
   stop stripping it, and the Helmfile Plugin's image SHALL be
   `ghcr.io/helmfile/helmfile:<version>` rather than `…:v<version>`
5. THE version-shape check SHALL accept an optional leading `v`, so
   `helmfile@v1.7.4` validates; it SHALL still reject floating tags

*Rationale for 4.1–4.3: turnip must never be what a repository waits on
to adopt a tool release. That is the problem with tools that bake the IaC
binary into their own image: a new release of the tool is usable only once the
automation tool ships an image carrying it. Running each tool in its
vendor's own per-version image removed that bottleneck, and a default
version would bring it back one layer down — the version most Projects
run would be whatever turnip's latest release chose, moving only on
turnip's cadence.*

*It is also drift: a default means the version a Project runs is chosen by
turnip, not the repository, and changes whenever turnip's default does,
with no diff in the repository and nothing in review to notice. Every
other part of turnip treats the version as the repository's to state: a
floating tag is refused, and an apply replays exactly what was planned. A
missing version is the same drift by another route. turnip is pre-1.0 and
ships no migration; the validation error names the fix.*

*Rationale for 4.4: vendors differ. helmfile's tags carry a `v`
(`v1.7.4`); terraform's and pulumi's do not. Stripping it for every tool in
the configuration package and re-adding it in one tool's template was tool
knowledge in two wrong places. Used verbatim, the tag is whatever the
vendor publishes, and the repository writes what it would `docker pull`.*

*A known sharp edge until Slice 44: `helmfile@1.7.4` passes the shape
check but names a tag helmfile never published, so it fails when the
Runner's image is pulled, reported by the start-timeout diagnostic
("container stuck (ErrImagePull)"). Slice 44's per-image tag globs catch
it at parse time instead.*

*This removes the version list in `internal/jobs/versions.go` outright
rather than moving it into each Plugin. It was documented as examples,
"not an exhaustive allowlist", and had two uses: the default, and example
versions in the error for a malformed one (`versions.go:141`). The error
can show a fixed example instead, as Requirement 4.2's does.*

### Requirement 5: The schema version advances to `v1alpha3`

#### Acceptance Criteria

1. `turnip.yaml` SHALL require `schemaVersion: v1alpha3`
2. A file declaring `v1alpha2` SHALL be rejected on its own, naming both
   the version it found and the one this turnip supports, as any other
   schema mismatch is today
3. THE documentation's examples SHALL all declare `v1alpha3`

*Rationale: this is the policy `docs/configuration.md` already states for
the alpha schema. Advancing costs no turnip release and promises no
migration window, and a file on an older schema is rejected outright,
never silently upgraded. Reporting the version alone, rather than every
`uses:` line it breaks, tells the author the one fact that explains them.*

### Requirement 6: Behavior otherwise unchanged

#### Acceptance Criteria

1. `uses: helmfile@v1.7.4` SHALL run the same image, provisioned the same
   way, as `uses: helmfile@1.7.4` did before this slice
2. THE existing tests SHALL pass, changed only where they name the moved
   code, or where they assert a default version, the `v` handling, the
   schema version, or Requirement 3.3's refusal
3. A test SHALL assert that every registered Plugin declares a
   Provisioning_Strategy and an image, so a new Plugin cannot be
   registered without them

### Requirement 7: Documentation

#### Acceptance Criteria

1. A new `docs/development.md` SHALL describe how to add a tool: write
   the Plugin, declare its provisioning, register it; `README.md`'s
   documentation table SHALL link to it
2. `docs/configuration.md`'s tool support section SHALL say that the
   accepted tools are the ones this Server has Plugins for
3. `docs/configuration.md` SHALL drop every mention of a default version,
   and its `uses:` examples SHALL all name one

## Out of Scope

- **Changing what a Plugin declares about its image**, from a template to
  Access_List-style Entries. That is Slice 44, which follows.
- **The Terraform and Pulumi Plugins.** Slice 7. Until then, the
  registry has only Helmfile, and `terraform`/`pulumi` are validation
  errors (Requirement 3.3).
- **Loading Plugins at runtime** (Go plugins, external processes).
