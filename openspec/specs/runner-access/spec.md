# runner-access Specification

## Purpose
Decides which Kubernetes ServiceAccount a Runner Pod runs as, gates a repository's ability to choose that identity (or its own submodule depth) through `TURNIP_ALLOWED_OVERRIDES`, and states what access turnip deliberately leaves to the operator. Operators rely on it to bound what a pull request can reach.

## Requirements

### Requirement: ServiceAccount resolution
turnip SHALL run every Runner Pod for a Project as exactly one ServiceAccount, resolved in this order: the `runner.serviceAccount` of the Project's effective `runner:` settings (its own value, else the value inherited from the top-level `runner:` block; the merge itself belongs to the `configuration` capability) when the Server permits that override; otherwise the Server's `TURNIP_RUNNER_SERVICE_ACCOUNT`; otherwise no ServiceAccount at all, which leaves Kubernetes to apply the namespace's `default` ServiceAccount. The resolved name SHALL be used as given on the Pod (see the `runner-job` capability), and turnip SHALL NOT check that the ServiceAccount exists.

#### Scenario: Server default applies
- **WHEN** `TURNIP_RUNNER_SERVICE_ACCOUNT=turnip-runner` is set and `turnip.yaml` sets no `runner.serviceAccount` at either level
- **THEN** the Runner Pod runs as `turnip-runner`

#### Scenario: Nothing set anywhere
- **WHEN** `TURNIP_RUNNER_SERVICE_ACCOUNT` is unset and `turnip.yaml` sets no `runner.serviceAccount`
- **THEN** the Runner Pod names no ServiceAccount and runs as the namespace's `default` ServiceAccount

#### Scenario: Permitted Project value wins
- **WHEN** `TURNIP_ALLOWED_OVERRIDES` includes `runner.serviceAccount`, the top-level `runner:` block sets `serviceAccount: deployer` and Project `api` sets `serviceAccount: api-deployer`
- **THEN** Project `api`'s Runner Pod runs as `api-deployer`
- **AND** every other Project's Runner Pod runs as `deployer`

### Requirement: The TURNIP_ALLOWED_OVERRIDES setting
The Server SHALL read `TURNIP_ALLOWED_OVERRIDES` as a comma-separated list of the `turnip.yaml` fields a repository may set for itself. Whitespace around each entry SHALL be trimmed and empty entries skipped. The recognized paths SHALL be `runner.serviceAccount` and `clone.submodules`. Unset or blank SHALL permit nothing. An unrecognized path SHALL stop the Server from starting with an error naming the path and listing the known paths (`unknown override path "<path>"; known paths are clone.submodules, runner.serviceAccount`). `uses:` and `TURNIP_MUTATION_REQUIREMENTS` SHALL NOT be override paths.

#### Scenario: Default permits nothing
- **WHEN** `TURNIP_ALLOWED_OVERRIDES` is unset
- **THEN** neither `runner.serviceAccount` nor `clone.submodules` may be set by a repository

#### Scenario: Misspelled path refuses startup
- **WHEN** `TURNIP_ALLOWED_OVERRIDES=runner.serviceaccount`
- **THEN** the Server exits at startup with an error naming `runner.serviceaccount` and the two known paths

#### Scenario: Both paths permitted
- **WHEN** `TURNIP_ALLOWED_OVERRIDES=runner.serviceAccount, clone.submodules`
- **THEN** a repository may set both fields

### Requirement: Refusing an override the Server does not permit
WHEN a Project's effective `runner.serviceAccount` is set (at either level) and `TURNIP_ALLOWED_OVERRIDES` does not include `runner.serviceAccount`, or when `turnip.yaml` sets `clone.submodules` and `TURNIP_ALLOWED_OVERRIDES` does not include `clone.submodules`, turnip SHALL refuse that Project's Operation before any Lock is acquired and before any Runner Job is created. The refusal SHALL apply identically to the automatic plan and to every operation triggered by a comment, plan or mutating, and SHALL apply even when the requested ServiceAccount equals the Server's default. Other Projects in the same pull request that set no refused field SHALL proceed. The refusal reason, shown in the results comment (see the `pr-comment` capability) and the Project's check summary (see the `project-checks` capability, whose title is `<setting> is not permitted`), SHALL name where the value came from and the setting that permits it:

- a Project's own value: `Project "<name>" requested runner.serviceAccount "<sa>", which this turnip deployment does not permit. Add "runner.serviceAccount" to TURNIP_ALLOWED_OVERRIDES on the Server to let turnip.yaml choose its own ServiceAccount.`
- an inherited value: `Project "<name>" inherits runner.serviceAccount "<sa>" from the top-level runner: block, which this turnip deployment does not permit. Add "runner.serviceAccount" to TURNIP_ALLOWED_OVERRIDES on the Server to let turnip.yaml choose its own ServiceAccount.`
- submodules: `turnip.yaml requested clone.submodules "<mode>", which this turnip deployment does not permit. Add "clone.submodules" to TURNIP_ALLOWED_OVERRIDES on the Server to let turnip.yaml choose how submodules are cloned.`

A refused plan SHALL be recorded as refused for the `turnip` check (see the `merge-check` capability).

#### Scenario: Top-level value refused on the automatic plan
- **WHEN** a pull request opens, `turnip.yaml`'s top-level `runner:` block sets `serviceAccount: deployer`, Project `web` matches the changed files, and the override is not permitted
- **THEN** no Lock is taken and no Job is created for `web`
- **AND** the results comment says `Project "web" inherits runner.serviceAccount "deployer" from the top-level runner: block, which this turnip deployment does not permit.` followed by the instruction to add it to `TURNIP_ALLOWED_OVERRIDES`

#### Scenario: Refused on a comment-triggered command
- **WHEN** a collaborator comments `/turnip plan web` and Project `web` sets `runner.serviceAccount` without the override permitted
- **THEN** the Operation is refused with the same reason and nothing runs for `web`

#### Scenario: Repository-wide submodule override refused
- **WHEN** `turnip.yaml` sets `clone.submodules: recursive` and `TURNIP_ALLOWED_OVERRIDES` does not include `clone.submodules`
- **THEN** every Project the pull request would run is refused with the `clone.submodules` reason

#### Scenario: Permitted submodule override used
- **WHEN** `TURNIP_ALLOWED_OVERRIDES` includes `clone.submodules` and `turnip.yaml` sets `clone.submodules: none`
- **THEN** the clone uses `none` instead of `TURNIP_CLONE_SUBMODULES` (see the `checkout` capability)

### Requirement: Access turnip does not provide
turnip SHALL NOT create a ServiceAccount, Role, RoleBinding or any other RBAC for the Runner, SHALL NOT generate, inject or rotate any cloud credential, and SHALL NOT handle container registry credentials. A Runner Pod SHALL receive cloud and cluster access only through the ServiceAccount it runs as (for example EKS Pod Identity, IRSA or GKE Workload Identity bound to it) and SHALL pull images only through the node's own credentials or an `imagePullSecrets` attached to that ServiceAccount. The only RBAC `deploy/base` grants is the Server's own (see the `deployment` capability).

#### Scenario: Runner with no grants
- **WHEN** an operator installs `deploy/base` and creates no ServiceAccount or RBAC for the Runner
- **THEN** Runner Pods run as the namespace's `default` ServiceAccount with whatever that account already holds, and turnip adds nothing to it

#### Scenario: Private image pull
- **WHEN** an allowed custom image lives in a private registry
- **THEN** the Runner Pod can pull it only if the node's credentials or the Runner ServiceAccount's `imagePullSecrets` grant access
