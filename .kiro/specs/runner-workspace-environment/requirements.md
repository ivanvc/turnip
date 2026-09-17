# Requirements Document: Runner Workspace & Project Environment (Slice 12)

## Introduction

This slice gives the Runner a **predictable filesystem layout** and lets a
Project **declare environment variables** for its Operation.

It exists because of a concrete adoption blocker. A Runner Pod starts with
whatever identity its ServiceAccount confers (Slice 6 added
`TURNIP_RUNNER_SERVICE_ACCOUNT` and the per-Project `serviceAccount`
override). That is sufficient where the workload's identity *is* the
identity it needs — but not where an operation must act as a second,
more privileged identity that the first is merely trusted to assume. On
AWS that second step cannot be expressed in environment variables alone:
`role_arn` pairs only with `source_profile`, `credential_source` or
`web_identity_token_file`, and the first two exist solely as shared
*config file* settings. The file must therefore be readable from inside
the Runner, and something must point the tool at it.

Terraform and Pulumi express this in their own provider/stack
configuration and need nothing from turnip. Helmfile has no equivalent
hook — its secret and state lookups go through the plain cloud SDK
credential chain — which is what makes this a turnip-level concern rather
than a repository-level one.

**Why these two capabilities are one slice.** A Project can already commit
a config file to its own repository; what it cannot do is *reference* it,
because the clone lives in a randomly-named `os.MkdirTemp` directory whose
path isn't known when the Job spec is built. Declaring environment
variables without a fixed layout would require turnip to expand a
workspace placeholder at Runner startup — logic the fixed layout removes
entirely. Splitting the work would mean building that expansion and then
deleting it. (Reversible decision: the halves are separable if the fixed
layout is rejected.)

**A third capability, folded in deliberately.** Reviewing the above
surfaced that turnip.yaml's top-level `version` field is parsed and then
never used — `docs/configuration.md` says as much: "parsed, not currently
validated against anything". Written as `version: 1` it also reads as a
product version (1.0.0) for a tool that has cut no releases. Since this
slice already changes the turnip.yaml schema by adding `env`, fixing the
version field here means one schema change in one release rather than two
migrations for consumers. Requirement 4 covers it. (Separable: it could
be its own spec if this slice is judged too wide.)

**This supersedes a criterion in a completed slice.** `config-parsing`'s
Requirement 2 states "THE Config Parser SHALL parse a top-level `version`
field" — parse, with no validation and no stated meaning, which is the
behaviour Requirement 4 replaces. That slice's own documents are left
untouched: amending a completed spec in place is the habit this slice
exists to stop. The supersession is recorded here instead, and
`config-parsing`'s requirements should be read with this note beside
them. No *numbered global requirement* mandates the field, so nothing in
the north-star spec conflicts — only an illustrative snippet
(`multi-iac-automation-platform/design.md:335`) still shows `version: 1`.

**Depends on** Slice 1 (`config-parsing`, for the turnip.yaml schema) and
Slice 5 (`grpc-runner`, for the Job builder and the Runner). Independent
of Slice 7.

**Global requirements**: this slice implements no numbered global
requirement. It extends the mechanism global Requirement 14.2a describes
(the shared volume an initContainer copies a tool binary onto) and sits
adjacent to global Requirement 18 (tool-specific configuration in
turnip.yaml) without changing that requirement's `config` map.

**Explicitly out of scope**, each deferred to named future work:

- **Provisioning additional binaries** — helm plugins, `aws`, `gcloud`,
  `kubelogin`. This is the next blocker for the Helmfile pilot and
  warrants its own slice; nothing here depends on it.
- **Remote-cluster authentication** — a kubeconfig for a cluster the
  Runner is not running in, and that cluster's access entries.
- **Azure Workload Identity pod labels** — recorded in `roadmap.md`'s
  Backlog; that gap is in pod metadata, not in the workspace or
  environment.
- **Any cloud-specific logic in turnip.** turnip writes an environment
  and a filesystem layout; it does not learn what a profile, project or
  subscription is. Cloud-specific *content* stays in the repository.

## Glossary

(Inherited from the global spec glossary.)

- **Workspace**: the directory a Runner clones the repository into for one
  Operation. Currently created per-run by `os.MkdirTemp`.
- **Tools directory**: the shared volume an initContainer copies the
  IaC_Tool binary onto, which the Runner prepends to its `PATH`
  (`TURNIP_TOOLS_DIR`, global Requirement 14.2a).

## Requirements

### Requirement 1: Predictable Runner Filesystem Layout

**User Story:** As a developer onboarding a repository, I want the paths
inside a Runner to be fixed and documented, so that configuration I commit
can reference a file in my own repository without knowing where turnip
happened to put the clone.

#### Acceptance Criteria

1. THE Job builder SHALL place every turnip-owned directory under a single
   top-level namespace, `/turnip`, rather than at the filesystem root —
   the convention every comparable system follows (`/drone/src`,
   `/woodpecker/src/...`, `/github/workspace`), because a dedicated
   namespace cannot collide with anything a tool image already installs
2. THE Job builder SHALL mount the Workspace at `/turnip/src` and the
   Tools directory at `/turnip/tools`
3. THE Workspace and the Tools directory SHALL be **separate** `emptyDir`
   volumes, so that the tool-provisioning initContainer has no access to
   the Workspace and cannot seed it before the clone
4. THE Job builder SHALL communicate the Workspace path to the Runner in
   an environment variable, mirroring how `TURNIP_TOOLS_DIR` already
   communicates the Tools directory, rather than both sides hardcoding a
   constant
5. WHERE that environment variable is absent, THE Runner SHALL create a
   temporary directory and use it as the Workspace — preserving today's
   behaviour for unit tests and local runs, which cannot create
   directories at the filesystem root
6. THE Runner SHALL remove a Workspace it created itself, and SHALL NOT
   attempt to remove one it was given: the latter is a volume mount point,
   and deleting it is both wrong and unnecessary, since the Pod is
   ephemeral
7. THE Runner SHALL continue rewriting the Workspace path out of output
   bound for the Server, so a reviewer sees `environments/cicd-2/...`
   rather than an absolute path (grpc-runner's task 24, unchanged by the
   path becoming fixed)
8. THE `/turnip/src` path SHALL be documented, and repositories MAY
   reference it in committed configuration. Relocating it later is a
   release-gated change rather than a permanent commitment: turnip has
   cut no releases at all, so nothing is pinned to it yet, and semantic
   versioning covers the move when one is wanted
9. WHERE the path is relocated, THE release SHALL say so explicitly. A
   version bump reaches the operator who changes the image tag, but the
   breakage lands in the *repositories* turnip serves, whose owners did
   not choose the upgrade — so it cannot be left to be inferred from a
   version number. Exposing the path through a referencing mechanism
   instead (the `GITHUB_WORKSPACE` / `CI_PROJECT_DIR` pattern, which
   would let the path move without breaking anyone) is deferred to the
   Backlog until a second repository onboards; Woodpecker's "Plugins will
   always have the workspace base at `/woodpecker`" is what the deferral
   eventually costs if it is left too long

### Requirement 2: Project-Declared Environment Variables

**User Story:** As a developer, I want my Project to declare environment
variables for its Operation, so that a tool with no configuration hook of
its own can be pointed at credentials, configuration or context that my
repository already carries.

#### Acceptance Criteria

1. THE turnip.yaml parser SHALL support an `env` field on a Project: a map
   of variable name to value, distinct from the existing `config` map,
   which is tool-specific and consumed by Plugins
2. THE Server SHALL ensure a Project's `env` is present in the
   environment the IaC_Tool subprocess runs with. This requirement is
   indifferent to *where* the injection happens; `design.md`'s Decision 3
   settles that
3. THE parser SHALL reject any variable name beginning with `TURNIP_`:
   the Runner reads its own configuration from that namespace, and a
   Project overriding it could redirect the Runner's Server address, its
   operation, or its GitHub token
4. THE parser SHALL reject `PATH`: the Tools directory is prepended to it
   at Runner startup, and overriding it would let a Project shadow the
   provisioned tool binary with one of its own choosing
5. THE parser SHALL report every rejected name in one pass rather than
   stopping at the first, matching `internal/config`'s existing
   accumulate-all-violations behaviour
6. THE IaC_Tool subprocess SHALL receive each value byte-for-byte as
   written in turnip.yaml, with no expansion or substitution — Requirement
   1's fixed layout means a value needing the Workspace path can simply
   contain it. WHERE the delivery mechanism performs expansion of its own
   (Kubernetes expands `$(VAR)` inside container environment values), THE
   Server SHALL escape each value so that expansion cannot alter what the
   tool receives
7. `env` SHALL NOT be gated behind Server configuration, unlike
   `serviceAccount`. A Project that can run its own IaC code can already
   set any environment it likes from within that code, so a gate would
   restrict nothing while implying a protection that does not exist; the
   reserved names in 2.3 and 2.4 are what actually bound it. The rationale
   belongs in `design.md`, not merely in this list

### Requirement 3: Documentation

**User Story:** As an operator or developer, I want both capabilities
documented where I already look, so that neither is discoverable only by
reading Go source.

#### Acceptance Criteria

1. `docs/configuration.md` SHALL document the `env` field, including the
   reserved `TURNIP_` prefix and `PATH`, alongside the existing `config`
   map documentation
2. `docs/configuration.md` SHALL document `/turnip/src` and
   `/turnip/tools` as the Runner's fixed paths, stating that `/turnip/src`
   is stable and may be referenced from committed configuration
3. Documentation SHALL NOT describe a cloud-specific recipe as though it
   were a turnip feature. A worked example may appear in this slice's
   `design.md` to show the mechanism is sufficient, clearly marked as an
   example rather than as behaviour turnip implements

### Requirement 4: An Enforced, Unambiguous Schema Version

**User Story:** As a developer whose turnip.yaml predates a schema change,
I want to be told my file is too old in those words, so that I am not left
deciphering a parse error about a field I never wrote.

#### Acceptance Criteria

1. THE turnip.yaml schema SHALL name its version field `schemaVersion`,
   replacing the current `version`. What is versioned is the *shape of the
   file*, and the name should say so: `version: 1` reads as a product
   version (1.0.0) turnip does not have; `apiVersion` reads as Kubernetes,
   where it pairs with a `kind` and an API group that turnip.yaml has
   neither of; and `scheme` would be the wrong word — a scheme is a plan
   or a URI prefix, a schema is a structure
2. THE `schemaVersion` value SHALL be a string in Kubernetes' maturity
   vocabulary, beginning at `v1alpha1`. That vocabulary is borrowed
   deliberately for *values* while 4.1 declines `apiVersion` as the field
   name and the `kind`/group structure surrounding it there — the words
   are worth reusing, the shape is not
3. THE value SHALL NOT be an integer. turnip is pre-1.0 and this schema
   will break repeatedly before it settles; `alpha` states that in words
   the intended audience already reads correctly, where a bare `1` would
   imply a stability the file does not have. A non-numeric value also
   sidesteps YAML's float trap, in which an unquoted `1.10` parses as
   `1.1`
4. THE parser SHALL accept exactly one `schemaVersion` at a time and
   reject every other value, naming both what it found and what it
   supports. No compatibility window is offered while the schema is
   alpha: supporting two shapes simultaneously is a cost that belongs
   with a stability promise, and alpha makes none. A version field that is
   parsed but not enforced is the state Docker Compose reached and then
   had to abandon — its top-level `version` is now documented as obsolete,
   "only informative", because Compose "always uses the most recent schema
   to validate the Compose file, regardless of the `version` field"
5. WHERE a breaking schema change lands before turnip 1.0, THE version
   SHALL advance within alpha (`v1alpha2`, `v1alpha3`, …), which costs no
   turnip release and promises nobody anything. WHERE turnip reaches 1.0,
   THE schema SHALL graduate to `v1`, at which point the schema version
   and the project major coincide naturally rather than by decree —
   giving the tied, single-number model golangci-lint enjoys at the moment
   it becomes true. THE word `beta` SHALL NOT be used unless its
   obligation is genuinely accepted: Kubernetes defines beta as a
   deprecation window of "9 months or 3 minor releases, whichever is
   longer", with migration instructions provided
6. THE parser SHALL reject a turnip.yaml with no `schemaVersion` at all,
   rather than defaulting it. A default would make the field optional in
   practice, reintroducing exactly the ambiguity 4.1-4.4 remove
7. THE parser SHALL NOT carry compatibility machinery for the previous
   schema. A file still carrying `version: 1` fails because
   `schemaVersion` is absent (4.6), which is a clear and actionable
   error; the old key is ignored like any other unrecognised field.
   Detecting it by name would buy a marginally better message at the cost
   of parser code that exists solely to describe a schema turnip no
   longer accepts — and `alpha` exists precisely so that pre-1.0 breaking
   changes are paid for by consumers updating their files, not by the
   codebase carrying shims
8. THE rejection SHALL be reported through the same accumulate-all-
   violations path as every other validation error (Requirement 2.5's
   convention), not as a special early return
9. `docs/configuration.md` SHALL document `schemaVersion` as the schema
   version of the file, explicitly distinguishing it from turnip's own
   version and from the per-Project `config.version` that pins an IaC_Tool
   release — three unrelated meanings of "version" that the current
   documentation leaves the reader to disentangle. It SHALL state the
   graduation path from 4.5, so a reader understands why the value says
   `alpha` and what will replace it
10. THE migration SHALL be stated in this slice's `tasks.md`, naming every
    place the old field appears rather than only the one external
    consumer: `docs/configuration.md`'s example, the global design's
    illustrative snippet (`multi-iac-automation-platform/design.md:335`),
    and the test fixtures in `internal/config` (`config_test.go`,
    `parse_test.go`, `property_test.go`) and
    `internal/github/client_test.go`, where `version: 1` is used as file
    content. In each, `version: 1` becomes `schemaVersion: v1alpha1`. With
    no releases cut and one repository using turnip, this is the cheapest
    this change will ever be
