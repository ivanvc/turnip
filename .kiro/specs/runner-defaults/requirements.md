# Requirements: Runner Settings Shared Across Projects (Slice 43)

## Introduction

Runner settings are per-Project today. `runner.serviceAccount` and
`runner.env` live under each entry in `projects:`, with only the Server's
`TURNIP_RUNNER_SERVICE_ACCOUNT` beneath them. A repository whose Projects
share settings repeats them in every Project.

Repeating them was tolerable while they were incidental. It stops being
tolerable once a shared setting decides *where* a Project deploys. The
recipe for reaching an EKS cluster turnip is not running in
(`docs/configuration.md`) selects the cluster with `KUBECONFIG`, set
through `runner.env` — and a Project that is missing it does not fail. With
no kubeconfig, helm and helmfile fall back to in-cluster configuration, and
plan and apply against **the cluster turnip itself runs in**.

YAML anchors and merge keys can share the block today — the parser expands
them before its strict check — but they make that hazard easy to hit. A
Project that writes its own `env:` without also writing `<<: *env` replaces
the shared map outright, silently dropping `KUBECONFIG`. Nothing in the
file looks wrong. The anchor must also live inside one of the Projects,
since a dedicated top-level key is rejected as unknown, so deleting or
reordering that Project breaks the rest.

This slice adds a top-level `runner:` block beside `clone:`, merged into
each Project's, so a Project states only what differs.

**A shared value must mean the same thing in every Project.** The tool
runs in its Project's directory, so a relative path in `env` resolves
differently per Project: the EKS recipe's `KUBECONFIG: kubeconfig` names
a different file in each. A shared `KUBECONFIG` has to be absolute, and
turnip already documents a fixed workspace path for exactly this
(`/turnip/src`, "Where the Runner puts things"), so
`KUBECONFIG: /turnip/src/.turnip/kubeconfig` works from every Project.

**The gate is unchanged, and follows the value rather than the line.**
`runner.serviceAccount` is gated because it picks an identity and
`turnip.yaml` is read from the pull request's own head commit. Moving the
setting to the top of the same file changes neither fact.

This slice builds on Slice 13 (`project-schema-v1alpha2`), which
introduced `runner:`, and Slice 16 (`clone-submodules`), which introduced
the first repository-scoped block and the route by which one reaches
execution.

## Glossary

- **Repository_Runner**: the top-level `runner:` block in `turnip.yaml`.
- **Project_Runner**: a Project's own `runner:` block, as today.
- **Effective_Runner**: the settings a Project's Runner Job actually runs
  with, after merging the Project_Runner over the Repository_Runner over
  the Server's defaults.

## Requirements

### Requirement 1: A repository-wide `runner:` block

**User Story:** As a repository maintainer, I want to state Runner
settings once for every Project, so that a Project only states what
differs and cannot lose a shared setting by omission.

#### Acceptance Criteria

1. `turnip.yaml` SHALL accept an optional top-level `runner:` block
2. THE Repository_Runner SHALL accept the same fields as a Project_Runner,
   with the same meaning
3. A Repository_Runner SHALL NOT change the schema version; a file without
   one SHALL behave exactly as today

*Rationale for 1.2: one shape for both levels is what makes "state it
once, override it where it differs" readable. A field valid in one place
and not the other would be a second schema to learn.*

### Requirement 2: `serviceAccount` — the most specific setting wins

#### Acceptance Criteria

1. THE Effective_Runner's `serviceAccount` SHALL be the Project_Runner's
   when it sets one
2. OTHERWISE it SHALL be the Repository_Runner's when it sets one
3. OTHERWISE it SHALL be the Server's `TURNIP_RUNNER_SERVICE_ACCOUNT`, as
   today

### Requirement 3: `env` — merged per variable

#### Acceptance Criteria

1. THE Effective_Runner's `env` SHALL contain every variable the
   Repository_Runner sets, and every variable the Project_Runner sets
2. WHERE both set the same variable, THE Project_Runner's value SHALL win
3. A Project_Runner that sets `env` SHALL NOT thereby drop any variable it
   does not name
4. A variable set to an empty string SHALL be set, to the empty string;
   this slice provides no way for a Project to remove a variable the
   Repository_Runner sets

*Rationale for 3.3: this is the anchor hazard the introduction describes,
removed by construction. A Project adding one variable is the common case,
and it must not cost every shared one.*

*Rationale for 3.4: an empty value is honest — it is what the tool sees —
and needs no new syntax. Removal is ruled out, not deferred (decided
2026-09-26): the top-level block is for what every Project uses. A
setting only some Projects share does not belong there; those Projects
share it with a YAML anchor instead.*

### Requirement 4: The same validation at both levels

#### Acceptance Criteria

1. THE Repository_Runner's `env` names SHALL be validated by the rules a
   Project_Runner's are: no name beginning `TURNIP_`, and not `PATH`
2. A violation SHALL be reported with its own path — `runner.env["X"]` for
   the Repository_Runner, the Project's path for a Project_Runner — so the
   author can tell which block to fix
3. Violations at both levels SHALL be reported together, in one pass, as
   the parser already reports every violation rather than the first

### Requirement 5: The gate follows the value

#### Acceptance Criteria

1. WHERE a Project's Effective_Runner takes its `serviceAccount` from
   `turnip.yaml` — from the Project_Runner or the Repository_Runner — THE
   Server SHALL refuse the Operation unless `TURNIP_ALLOWED_OVERRIDES`
   includes `runner.serviceAccount`, exactly as it refuses a Project_Runner's
   today
2. THE refusal SHALL say where the value came from: the Project's own
   `runner:`, or the top-level `runner:`
3. `env` SHALL remain ungated at both levels
4. THE refusal SHALL remain a Configuration_Refusal (Slice 36): its
   Project_Check Title stays `runner.serviceAccount is not permitted`,
   and where the value came from is in the check's summary and the
   comment

*Rationale for 5.1: the whole file comes from the pull request, so the
top-level block is exactly as much the pull request's choice as a
Project's. A gate that looked only at `projects[].runner` would be
bypassed by moving one line.*

*Rationale for 5.2: a refusal for a value the Project does not visibly
set would send its author searching the Project for a setting that is
somewhere else.*

### Requirement 6: Every Runner field has a stated merge rule

#### Acceptance Criteria

1. EACH field of the Runner block SHALL have an explicit merge rule —
   replaced as a whole by the more specific level, or merged per key
2. Adding a field to the Runner block without stating its merge rule SHALL
   fail a test
3. EVERY Runner setting `turnip.yaml` accepts, now or in a later slice,
   SHALL be a field of the Runner block, and so available at both levels;
   none SHALL be Project-only or repository-only

*Rationale for 6.3 (decided 2026-09-26): the two levels are one
mechanism, so a later slice adds a field, states its merge rule and its
gate, and inherits the rest. A setting shaped per slice (a Project-only
resources block, a repository-only workspace size) would bring back the
repetition this slice removes, one field at a time.*

*Rationale for 6.1 and 6.2: Slices 31 (node selectors and tolerations)
and 39 (resources) both add Runner fields, and neither has an obvious
rule — a toleration list could reasonably be replaced or appended. Making the rule a required
decision stops the next field from inheriting whatever the merge code
happens to do with an unfamiliar type.*

### Requirement 7: Every path runs with the Effective_Runner

#### Acceptance Criteria

1. THE automatic plan and every comment-triggered Operation SHALL run with
   the Effective_Runner
2. EVERYWHERE turnip reads a Project's Runner settings SHALL see the
   Effective_Runner, not the Project_Runner alone

*Rationale for 7.2: a merge that reaches the Job but not the gate —
or the reverse — would run one identity while checking another.*

### Requirement 8: Documentation

#### Acceptance Criteria

1. `docs/configuration.md` SHALL document the top-level `runner:` block,
   the precedence for each field, and that the gate applies wherever the
   `serviceAccount` is written
2. THE EKS recipe SHALL show `KUBECONFIG` set once in the top-level block,
   as an absolute path under the fixed workspace path
3. THE documentation SHALL state that a Project cannot remove a shared
   `env` variable, only override it
4. THE documentation SHALL state that a relative path in a shared `env`
   value resolves in each Project's own directory
5. `SECURITY.md` SHALL state that the `runner.serviceAccount` opt-in
   covers the top-level block as well as a Project's

## Out of Scope

- **Removing an inherited `env` variable**, deliberately, not merely
  deferred. Requirement 3.4 explains why.
- **A default `uses:`**, deliberately, not merely deferred (decided
  2026-09-26). `uses:` decides what runs, not where the Runner runs, and
  every Project states it: a Project that forgot it must fail validation,
  not silently run a shared tool. Repeating a custom image across many
  Projects is served by a YAML anchor on the scalar, which has none of the
  map hazard this slice removes.
- **Top-level defaults for other Project fields** — `with`,
  `whenModified`. Different questions: `with` is interpreted by the tool,
  and a shared `whenModified` would change which Projects a pull request
  affects. Each would need its own case.
- **Inheritance between Projects** or by directory — only one level,
  repository to Project.
- **Per-repository settings on the Server side.** The Backlog's
  per-repository server configuration is a different layer: an operator's
  choice, not the pull request's.
