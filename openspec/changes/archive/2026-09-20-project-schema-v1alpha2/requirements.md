# Requirements: Project Schema v1alpha2 (Slice 13)

## Introduction

A Project in `turnip.yaml` currently spreads settings across levels in a
way that doesn't match what reads them. `tool` sits at the top of a
Project while the tool's version sits inside `config`, and `config` — a
map documented as "free-form, tool-specific, a plugin only reads the keys
it understands" — holds three recognized keys of which only one reaches a
Plugin:

| Key | Read by |
|---|---|
| `config.environment` | `internal/plugin` (the Helmfile Plugin) |
| `config.version` | `internal/jobs`, to pick the initContainer image |
| `config.serviceAccount` | `internal/orchestrator`, to set the Pod's identity |

So the map's documentation describes the minority case. The whole map is
also marshaled into `TURNIP_TOOL_CONFIG` and handed to the Plugin, which
means two settings the Plugin has no use for are shipped to it every run.

This is not merely untidy. It produced a real incident during the pilot:
a `serviceAccount` key written at the *file's* top level — where the
mental model says identity belongs — was read, discarded and never
mentioned, because the parser uses `yaml.Unmarshal` with no strict field
checking. The Runner then ran as the namespace's `default` ServiceAccount,
obtained no cloud credentials, and failed several layers away with a
message about an EC2 metadata endpoint. Nothing in turnip said the key was
unknown, so the schema's silence was the bug.

This slice replaces the Project shape with three fields that answer three
distinct questions — **what to run** (`uses`), **how to call it**
(`with`), and **where it runs** (`runner`) — makes an unrecognized key an
error rather than silence, and generalizes the single override boolean
into an operator-controlled list. All of it rides one `schemaVersion`
bump to `v1alpha2`.

The same release also moves where the file is found. `.turnip/config.yaml`
becomes the preferred location, giving a repository a directory to keep
turnip-related files in rather than a single file at its root — a Project
can already reference a sibling there, such as an AWS config, through
`runner.env` and the fixed workspace path Slice 12 established. Root
`turnip.yaml` remains accepted; `.github/turnip.yaml` does not, which is
breaking and so belongs in this release rather than a later additive one.

**Global requirements**: amends global Requirement 18, restated around
`uses` and `with`, and corrects Requirement 14.2a's cross-reference to it.
The amendment also repairs a clause that was already stale: 18.8 still
described rejecting a version "the Server recognizes", an allowlist that
`grpc-runner` task 22 deliberately replaced with a well-formedness check
without updating the global text.

It also amends global Requirement 1.1, which specifies only "the
repository root" — already narrower than what ships, since
`server-orchestration` added a second location at slice level without a
global amendment. Requirement 8 below replaces both.

Adds no new global requirement. The `runner` grouping consolidates
`serviceAccount` and `env`, neither of which any global requirement
describes today, matching Slice 12's precedent of shipping `env` with
"Global requirements covered: none directly".

**Explicitly out of scope**, each deferred to named future work:

- **Resolving a floating version** (`uses: terraform@latest`) — recorded
  in `roadmap.md`'s Backlog, together with the `imagePullPolicy: Always`
  and Lock-storage design that makes it viable. This slice keeps floating
  tags rejected.
- **Deriving the version from the IaC code's own constraint** (Terraform's
  `required_version`, Atlantis-style) — tool-specific, and only meaningful
  for one of the three tools. Backlog.
- **Provisioning additional binaries** (helm plugins, cloud CLIs) —
  unchanged from Slice 12, and the reason `plugin` is deliberately *not*
  spent as a schema key here.
- **Per-project Runner resources and timeouts** — `runner` gives them an
  obvious home; this slice adds neither.
- **A workflow or step-sequence concept.** Surveyed rather than assumed:
  across a real multi-repository Atlantis deployment of ~390 projects and
  ~144 custom workflows, 143 of 144 carried no behavior at all — they
  existed to pass arguments, and 142 of those to pass a single
  directory-derived backend key. The one exception printed a version
  string. Named `with:` keys cover that need. Should step sequences ever
  be required, `uses:` is the natural slot for them, as it is in the
  syntax this schema borrows from — so nothing here forecloses it.
- **Cross-project execution ordering** — a real gap the same survey
  surfaced, now Slice 30 in `roadmap.md`. Deliberately given no schema
  key here: it needs sequencing logic in the orchestrator, and a key that
  parses but does nothing is the silent-failure class Requirement 5
  exists to remove.
- **Migrating consuming repositories.** Ordering is a deployment concern
  and, unlike the `schemaVersion` migration, no file value is accepted by
  both the old and new Server at once (see Requirement 6.5).

## Glossary

Terms additional to the global spec's glossary:

- **Tool_Reference**: the value of a Project's `uses` field, naming an
  IaC_Tool and optionally a version, as `<tool>` or `<tool>@<version>`.
- **Override_Path**: a dotted path naming one Project field an operator
  may permit a repository to set, e.g. `runner.serviceAccount`.

## Requirements

### Requirement 1: Tool Selection via `uses`

**User Story:** As a developer, I want to name the tool and version my
Project runs in one place, so that I don't have to look in two levels of
the file to learn what will execute.

#### Acceptance Criteria

1. THE Project schema SHALL name its IaC_Tool in a `uses` field, whose
   value is a Tool_Reference formatted `<tool>` or `<tool>@<version>`
2. THE parser SHALL reject a Project with no `uses` field
3. IF the tool portion is not a supported IaC_Tool, THEN THE parser SHALL
   reject the Project, naming both what it found and the supported values
4. WHERE a Tool_Reference carries no version portion, THE Server SHALL
   resolve the documented default version for that IaC_Tool
5. THE parser SHALL accept a version portion written with or without a
   leading `v`, normalizing it before the vendor image tag is formed —
   `terraform@v1.9.5` and `terraform@1.9.5` SHALL resolve identically
6. IF the version portion is present but not well-formed, THEN THE parser
   SHALL reject the Project naming what it found; a floating tag such as
   `latest` is not well-formed
7. THE parsed Project SHALL expose the tool and the resolved version as
   separate values, so that code reading a Project's tool is unaffected by
   the two being written as one string

### Requirement 2: Tool Configuration via `with`

**User Story:** As a developer, I want the tool's own settings in one
block that holds nothing else, so that I can tell what reaches the tool
and what doesn't.

#### Acceptance Criteria

1. THE Project schema SHALL carry Plugin configuration in a `with` field,
   a map of string to string
2. WHERE the IaC_Tool is Terraform, THE `with` field SHALL support
   `workspace` to specify the Terraform workspace name, and
   `backendConfig` to specify backend configuration the Plugin passes to
   the tool's initialization step. A survey of a real multi-repository
   Atlantis deployment found a per-project backend key to be the reason
   142 of 144 custom workflows existed at all; a named key removes that
   need without exposing which underlying command receives the argument
3. WHERE the IaC_Tool is Pulumi, THE `with` field SHALL support `stack` to
   specify the Pulumi stack name
4. WHERE the IaC_Tool is Helmfile, THE `with` field SHALL support
   `environment` to specify the Helmfile environment
5. THE Server SHALL pass the `with` field to the Plugin when executing
   Operations
6. THE `with` field SHALL be optional; a Project whose tool needs no
   configuration SHALL carry no `with` block
7. THE `with` field SHALL carry no setting that turnip itself reads —
   every key in it is a Plugin's to interpret

### Requirement 3: Runner Configuration via `runner`

**User Story:** As a developer, I want the settings that shape the Runner
Pod grouped together, so that identity and environment aren't scattered
among the tool's settings.

#### Acceptance Criteria

1. THE Project schema SHALL carry Runner Pod settings in a `runner` field
2. THE `runner` field SHALL support `serviceAccount`, naming the
   Kubernetes ServiceAccount the Runner Pod runs as, subject to
   Requirement 4
3. THE `runner` field SHALL support `env`, a map of string to string set
   on the IaC_Tool's process, carrying forward Slice 12's Requirement 2
   unchanged in behavior
4. THE parser SHALL reject an `env` name beginning with `TURNIP_`, and the
   name `PATH`, reporting every offending name rather than the first
5. THE `runner` field SHALL be optional, as SHALL each of its keys

### Requirement 4: Operator-Controlled Overrides

**User Story:** As a platform operator, I want to choose which Project
settings a repository may set for itself, so that adding a new sensitive
setting doesn't mean adding a new environment variable to gate it.

#### Acceptance Criteria

1. THE Server SHALL read `TURNIP_ALLOWED_OVERRIDES`, a comma-separated
   list of Override_Paths a repository's `turnip.yaml` may set
2. THE default SHALL permit nothing, which is what shipping turnip does
   today: a repository has never been able to choose its own
   ServiceAccount
3. IF a Project sets an Override_Path the operator has not permitted, THEN
   THE Server SHALL refuse the Operation with an error comment naming the
   path and the variable that would permit it, without creating a Runner
   Job
4. `TURNIP_ALLOWED_OVERRIDES` SHALL replace
   `TURNIP_RUNNER_SERVICE_ACCOUNT_ALLOW_FROM_CONFIG`, which SHALL no
   longer be read
5. THE mechanism SHALL gate only settings the Server itself also
   provides, an override being by definition a repository replacing a
   Server-supplied value. `uses` SHALL NOT be gateable: turnip has no
   Server-side tool to fall back to, so what a Project runs can only come
   from the repository, and gating the one field every Project must set
   has no coherent meaning. Naming `uses` in the list SHALL be rejected
   as an unknown path

### Requirement 5: Unrecognized Keys Are Rejected

**User Story:** As a developer, I want turnip to tell me when it doesn't
understand something I wrote, so that a misplaced key is a failed check
rather than a silently ignored line.

#### Acceptance Criteria

1. THE parser SHALL reject any key it does not recognize, both at the
   file's top level and within a Project, naming the offending key
2. THE rejection SHALL be reported through the same accumulating
   `ValidationErrors` path as every other schema problem, so that several
   unknown keys are reported together
3. IF `schemaVersion` is absent or unsupported, THEN THE parser SHALL
   report that alone and SHALL NOT also report unknown keys — a file on
   the previous schema would otherwise produce a cascade of unknown-key
   errors whose real cause is the version
4. THE parser SHALL apply this check in place of Slice 12's Decision 4,
   under which an unrecognized key was ignored like any other
5. THE parser SHALL continue to resolve YAML merge keys (`<<`) rather
   than reporting `<<` as an unrecognized key. Real configurations use
   anchors and merge keys precisely to avoid the repetition a per-project
   schema otherwise forces, so rejecting them would break the files that
   most need them

### Requirement 6: Schema Version and Migration

**User Story:** As a developer, I want a file on the old schema to fail
with a clear statement of why, so that I'm not left reading errors about
fields I didn't write.

#### Acceptance Criteria

1. THE only accepted `schemaVersion` SHALL be `v1alpha2`
2. IF a file declares `v1alpha1`, THEN THE parser SHALL reject it naming
   both the version found and the version supported
3. THE Server SHALL carry no compatibility machinery for `v1alpha1`: no
   translation, no defaulting of removed fields, no deprecation window
4. THE schema SHALL NOT retain `tool`, `config`, or a Project-level `env`
   in any form; each is an unrecognized key under Requirement 5
5. THE migration SHALL be documented as requiring the consuming
   repository's file and the Server to move together: unlike the
   `v1alpha1` migration, no single file value is accepted by both the
   currently-deployed Server and the new one

### Requirement 7: Documentation

**User Story:** As a developer adopting turnip, I want the configuration
page to describe the schema that actually ships, so that I can write a
correct file without reading the parser.

#### Acceptance Criteria

1. `docs/configuration.md` SHALL document `uses`, `with` and `runner`,
   including the `<tool>@<version>` form and the optional leading `v`
2. `docs/configuration.md` SHALL document that an unrecognized key is an
   error, and SHALL state which keys are recognized
3. `docs/configuration.md` SHALL document `TURNIP_ALLOWED_OVERRIDES`,
   including its default, replacing the boolean it supersedes
4. THE three unrelated meanings of "version" SHALL remain distinguished:
   the schema's version, the tool's version, and turnip's own release
5. `docs/configuration.md` and `docs/troubleshooting.md` SHALL name the
   accepted configuration file locations, both of which currently name
   `.github/turnip.yaml`

### Requirement 8: Config File Discovery

**User Story:** As a developer, I want turnip's configuration to live in a
directory I can keep related files in, so that the tool's own config and
the files it references sit together rather than scattered across my
repository root.

#### Acceptance Criteria

1. THE Server SHALL look for the repository's configuration at
   `.turnip/config.yaml` first, and at `turnip.yaml` in the repository
   root second
2. THE Server SHALL use whichever is found first and SHALL NOT merge the
   two. The dedicated directory is checked first so that a repository
   migrating to it takes effect by adding the new file, without a step to
   remove the old one
3. THE Server SHALL accept only the `.yaml` extension; `.yml` is not a
   path the Server checks at either location
4. THE Server SHALL no longer read `.github/turnip.yaml`, which earlier
   versions accepted. This is breaking, and is carried by the same
   `v1alpha2` release as the schema reshape rather than shipped separately
5. WHERE neither location holds a file, THE message reported to the PR
   SHALL name both accepted paths, so that a repository with a misspelled
   path or a `.yml` extension can see the accepted spelling. Two messages
   carry this list today — `ErrConfigMissing` and the "not found" PR
   comment — and a test asserts the comment body byte-for-byte
6. THE number of content requests made for a repository with no turnip
   configuration SHALL NOT exceed the two this Requirement defines. This
   handler runs on every pull request open and synchronize in every
   installed repository, including those that never onboard, so the
   not-found path is the one paid most often
7. THE Server SHALL read no file in `.turnip/` other than `config.yaml`.
   Other files a repository keeps there are referenceable from
   `runner.env` through the fixed workspace path
   (`/turnip/src/.turnip/…`), but SHALL NOT be discovered, parsed or
   interpreted by turnip
