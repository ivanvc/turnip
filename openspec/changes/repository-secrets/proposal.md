# Proposal

## Why

A Runner often needs a credential that cannot be committed: an admin
kubeconfig for a new cluster, a cloud key a cluster's exec plugin signs
with, the static kubeconfig a k0s cluster generates. One turnip
deployment provisions clusters in several clouds, so a Runner in one
cluster routinely needs credentials for clusters that its
ServiceAccount's cloud identity cannot reach. Federation will cover some
of these later; static credentials remain wherever it does not.

Today the only way to hand a Runner such a file is to commit it to the
repository and point `runner.env` at it. Nothing lets an operator give a
repository a credential that stays out of git.

The specs also state the opposite of what operators need: the
`runner-access` capability says a Runner Pod receives cloud and cluster
access "only through the ServiceAccount it runs as". That sentence was
a description of what turnip itself does not provide, hardened into a
restriction during the move to OpenSpec, and it is wrong.

## What Changes

- Each repository may have one Kubernetes Secret of its own in the
  Runner namespace, named by turnip from the repository's GitHub id
  (`turnip-repository-<id>`). The operator creates it and fills it; creating
  it is how a repository is opted in, and no Server setting is involved
- A new `runner.secrets` field lists keys of that Secret. Each listed
  key is mounted read-only (mode `0444`) as a file under a fixed
  directory in `/turnip`, in the tool's container only, never in the
  clone container. `runner.env` points the tool at it, for example
  `KUBECONFIG`
- `runner.secrets` is available at the top level and per Project; a
  Project's list replaces the top-level one, so `secrets: []` opts a
  Project out
- A Runner Pod that never starts because the Secret or a listed key is
  missing times out as today, and the diagnostic names the repository's
  Secret and the keys the Project listed
- The `runner-access` requirement on access turnip does not provide is
  corrected: turnip still creates no RBAC and mints no cloud credential,
  but a Runner's access comes from whatever the operator provides (its
  ServiceAccount's identity, the repository's Secret, or anything the
  tool's own configuration reaches)

Documentation, beyond each setting's Reference row:

- A Runner ServiceAccount allowed to read Secrets in the Runner
  namespace can read every repository's Secret, not only its own
- A plan runs the pull request's own code, so every listed key is
  readable by any pull request a collaborator opens, before review. In
  the Helmfile guide (`docs/plugins/helmfile.md`), beside the existing
  hook warning: a `prepare` or `cleanup` hook runs during `diff` and,
  with `showlogs: true`, prints into the results comment; templating
  does the same without a hook (`exec` or `readFile` in a `.gotmpl`
  helmfile or values file runs during `diff`, and a value it renders
  into a manifest is printed by the diff). The results comment is read
  by everyone who can read the repository, a wider group than those
  trusted with the cluster, and the Runner's own log mirrors the output
  unredacted. Blocking hooks (`helmfile-hook-policy`) does not close
  this
- The repository's Secret lives in the Runner namespace, which until
  `split-server-runner-namespaces` lands also holds turnip's own
  `turnip-github-app` Secret. A repository cannot name that Secret
  (it names only keys), but a Runner grant that reaches Secrets in the
  namespace does

## Decisions (2026-10-08)

- **The Secret is the repository's, named by its id.** A pull request
  controls `turnip.yaml`, so it must not choose which Secret is mounted;
  the repository id comes from the webhook. The id survives renames and
  transfers and needs no conversion to a Kubernetes name. Rejected: a
  Server allowlist of Secret names (an operator setting per Secret, and a
  PR could still pick any allowed one); a name built from `owner/repo`
  (GitHub allows uppercase and `_`, which Kubernetes names do not, so
  the conversion collides, and a rename breaks the mount); gating
  `runner.secrets` in `TURNIP_ALLOWED_OVERRIDES` as
  `runner.serviceAccount` is (a ServiceAccount belongs to the namespace,
  so that gate supplies a repository's consent; the Secret's name
  already carries it, and a gate would be a second switch for the same
  decision that blocks nothing the name does not)
- **One Secret per repository.** Any collaborator's pull request can
  mount any of its keys, including at plan time, where the PR's own code
  runs. Rejected for now: a second Secret mounted only for mutating
  Operations; the name `turnip-repository-<id>` leaves room for it
- **A Project's list replaces the top-level one.** It is the only way a
  Project can opt out of shared keys
- **A missing Secret is reported by a hint in the start-timeout
  diagnostic.** Rejected: reading the Pod's events (a new Server
  permission) and checking the Secret before dispatch (the Server would
  need to read Secrets)
- **Files are mode `0444`.** A tool image running as a non-root user
  must read them; the container holds only the tool

## Capabilities

### New Capabilities

### Modified Capabilities
- `configuration`: `runner.secrets` at both levels, its validation, and its merge rule
- `runner-job`: the Secret volume and its mount in the tool's container
- `runner-access`: the repository's Secret; the corrected statement of access turnip does not provide
- `operation-lifecycle`: the start-timeout diagnostic names the repository's Secret when keys were listed

## Impact

`internal/config` (the field), `internal/jobs` (the volume),
`internal/orchestrator` (the repository id, the diagnostic),
`docs/using/turnip-yaml.md`, `docs/operating/runner-access.md`,
`docs/plugins/helmfile.md`, `SECURITY.md`. The diagnostic touches the same `operation-lifecycle`
requirement as `fix-results-comment-on-replica-loss`; whichever is
archived second rebases its delta. Out of scope: keys as environment
variables, and tokens with a chosen audience for federation (both later
changes).
