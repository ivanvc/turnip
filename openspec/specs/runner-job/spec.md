# runner-job Specification

## Purpose
Defines the Kubernetes Job and Pod the Server creates for each Operation: its containers, volumes, paths, environment, labels and lifetime. Operators rely on it to reason about what runs in their cluster, and repository authors rely on its fixed paths and environment.

## Requirements

### Requirement: One Job per Operation
The Server SHALL create exactly one Kubernetes Job for each dispatched Operation, in the namespace named by `TURNIP_K8S_NAMESPACE`, with a name generated from the prefix `turnip-runner-`. The Job SHALL carry the labels `app.kubernetes.io/name: turnip-runner`, `turnip.ivan.vc/operation-id` (the Operation's id) and `turnip.ivan.vc/project` (the Project's name as written, unaltered). The Server SHALL NOT create the Job when the Operation is refused before dispatch (see the `locking`, `authorization` and `runner-access` capabilities).

#### Scenario: A plan for one Project
- **WHEN** a plan for Project `web` is dispatched
- **THEN** one Job named `turnip-runner-<suffix>` is created in the `TURNIP_K8S_NAMESPACE` namespace
- **AND** it is labeled `app.kubernetes.io/name=turnip-runner`, `turnip.ivan.vc/operation-id=<operation id>` and `turnip.ivan.vc/project=web`

#### Scenario: Several Projects in one trigger
- **WHEN** one trigger dispatches an Operation for Projects `web` and `api`
- **THEN** two Jobs are created, one per Project, each with its own Operation id

#### Scenario: A Project name Kubernetes does not accept as a label value
- **WHEN** a Project's name is not a valid Kubernetes label value (for example `infra/web`, the default name of a Project in that directory)
- **THEN** Kubernetes rejects the Job and the Operation is refused as a Runner Job that could not be created (see the `project-checks` capability)

### Requirement: Provisioning strategies shape the Pod
The Pod SHALL take one of two shapes, chosen by the Project's tool or by a custom image (see the `tool-images` and `plugins` capabilities), and in both the single regular container SHALL be named `runner`.

Under copy-out, the init containers SHALL run in order `provision-<tool>` (the tool's image, running `sh -c` to copy the tool binary into `/turnip/tools/<tool>`) and then `clone`; the `runner` container SHALL run turnip's Runner image (`TURNIP_RUNNER_IMAGE`) with its default entrypoint.

Under run-in-image, the init containers SHALL run in order `copy-runner` (turnip's Runner image, copying its `/runner` binary into `/turnip/bin/runner`) and then `clone`; the `runner` container SHALL run the tool's image with its command overridden to `/turnip/bin/runner`, so the tool runs with that image's own `PATH`, environment and `HOME`.

The `clone` init container SHALL always run turnip's Runner image (see the `checkout` capability). The container running the tool's image SHALL use the pull policy the `tool-images` capability assigns; containers running turnip's Runner image SHALL leave the pull policy unset, so the cluster default applies.

#### Scenario: A copy-out tool
- **WHEN** an Operation runs a copy-out tool named `tool`
- **THEN** the Pod's init containers are `provision-tool` then `clone`
- **AND** the `runner` container runs `TURNIP_RUNNER_IMAGE` and finds the tool on its `PATH` via `/turnip/tools`

#### Scenario: A run-in-image tool
- **WHEN** an Operation runs Helmfile, a run-in-image tool
- **THEN** the Pod's init containers are `copy-runner` then `clone`
- **AND** the `runner` container runs the Helmfile image with command `/turnip/bin/runner`

#### Scenario: Reading a Runner's output
- **WHEN** an operator runs `kubectl logs -c runner` on a Runner Pod
- **THEN** the tool's output is shown, whichever strategy the Pod uses

#### Scenario: A copy-out tool image without a shell
- **WHEN** a copy-out tool's image has no `sh` or `cp`
- **THEN** the `provision-<tool>` init container fails and the Operation is reported through the start timeout (see the `operation-lifecycle` capability)

### Requirement: Fixed paths under /turnip
Everything turnip places in a Runner Pod SHALL live under `/turnip`, on these volumes:

| Path | Volume | Mounted in | Present |
|---|---|---|---|
| `/turnip/src` | emptyDir `workspace` | `clone`, `runner` | always |
| `/turnip/tools` | emptyDir `tools` | `provision-<tool>`, `runner` | copy-out only |
| `/turnip/bin` | emptyDir `bin` | `copy-runner`, `runner` | run-in-image only |
| `/turnip/run/secrets/token` | projected `turnip-clone-identity`, read-only | `clone` | always |
| `/turnip/run/secrets/token` | projected `turnip-report-identity`, read-only | `runner` | always |

`/turnip/src` SHALL be the repository root in every Runner, so a Project's `directory` is resolved relative to it and committed configuration may reference it by absolute path. Each container that talks to the Server SHALL find its own Runner identity at the same path, so `TURNIP_TOKEN_FILE` has one value for both; the contents of the two Runner identity volumes are defined by the `runner-security` capability.

#### Scenario: An absolute path in committed configuration
- **WHEN** a Project's `runner.env` sets `KUBECONFIG: /turnip/src/.turnip/kubeconfig`
- **THEN** that path names the same file in the repository for every Project and every run

#### Scenario: Where a Project's tool runs
- **WHEN** an Operation runs for a Project with `directory: infra/web`
- **THEN** the tool runs with working directory `/turnip/src/infra/web`

#### Scenario: Each container's Runner identity at one path
- **WHEN** a Runner Job is built
- **THEN** `/turnip/run/secrets/token` is the `turnip-clone-identity` volume in the `clone` container and the `turnip-report-identity` volume in the `runner` container, and no other container mounts either

### Requirement: Runner environment
The `clone` and `runner` containers SHALL both receive these variables: `TURNIP_SERVER_ADDR` (from the Server's `TURNIP_RUNNER_SERVER_ADDR`), `TURNIP_OPERATION_ID`, `TURNIP_PROJECT_NAME`, `TURNIP_PROJECT_DIR`, `TURNIP_TOOL`, `TURNIP_TOOL_VERSION` (the tag the Project's `uses:` names), `TURNIP_OPERATION`, `TURNIP_REPO_URL`, `TURNIP_COMMIT_SHA`, `TURNIP_BASE_REF`, `TURNIP_TOOL_CONFIG` (the Project's `with:` as JSON), `TURNIP_EXTRA_ARGS` (JSON array), `TURNIP_PLAN_DATA` (base64), `TURNIP_WORKSPACE_DIR=/turnip/src` and `TURNIP_TOKEN_FILE=/turnip/run/secrets/token`.

Only the `clone` container SHALL receive `TURNIP_CLONE_SUBMODULES` (see the `checkout` capability). Only a copy-out `runner` container SHALL receive `TURNIP_TOOLS_DIR=/turnip/tools`, which the Runner prepends to its own `PATH` at startup.

The Project's merged `runner.env` (see the `configuration` capability) SHALL be set on the `runner` container only, after turnip's own variables, sorted by name, with every `$` doubled so that Kubernetes does not expand `$(VAR)` and the tool receives each value byte for byte.

No container SHALL carry a GitHub credential (see the `runner-security` capability).

#### Scenario: A value containing $(...)
- **WHEN** a Project sets `runner.env` `GREETING: "$(HOME)"`
- **THEN** the tool's process sees `GREETING` with the literal value `$(HOME)`

#### Scenario: runner.env does not reach the clone
- **WHEN** a Project sets `runner.env` `HTTPS_PROXY: http://proxy.example.com`
- **THEN** the variable is present in the `runner` container and absent from the `clone` container

#### Scenario: A Runner started without its environment
- **WHEN** the Runner binary starts with any of `TURNIP_SERVER_ADDR`, `TURNIP_OPERATION_ID`, `TURNIP_PROJECT_NAME`, `TURNIP_PROJECT_DIR`, `TURNIP_TOOL`, `TURNIP_OPERATION`, `TURNIP_REPO_URL`, `TURNIP_COMMIT_SHA`, `TURNIP_BASE_REF` or `TURNIP_TOKEN_FILE` unset or empty
- **THEN** it logs `missing required environment variable(s): ` naming every missing one and exits with status 1 without contacting the Server

### Requirement: ServiceAccount as given
The Pod SHALL run as the ServiceAccount the Server resolved for the Operation (see the `runner-access` capability), set as given. When none is resolved, the Pod SHALL leave the ServiceAccount unset, so the namespace's `default` ServiceAccount applies.

#### Scenario: A resolved ServiceAccount
- **WHEN** the Server resolves ServiceAccount `deployer` for an Operation
- **THEN** the Runner Pod's `serviceAccountName` is `deployer`

#### Scenario: No ServiceAccount resolved
- **WHEN** neither the Server nor `turnip.yaml` names a ServiceAccount
- **THEN** the Runner Pod runs as the namespace's `default` ServiceAccount

### Requirement: No retries and a bounded afterlife
A Runner Job SHALL never be retried by Kubernetes: the Job SHALL set `backoffLimit: 0` and the Pod `restartPolicy: Never`, so a failed Pod is never replaced and a mutating operation never runs twice. A finished Job and its Pod SHALL be removed by Kubernetes 900 seconds (15 minutes) after finishing, through `ttlSecondsAfterFinished`. The Server SHALL NOT delete Runner Jobs itself.

#### Scenario: A Runner Pod fails
- **WHEN** a Runner Pod's container exits with a non-zero status
- **THEN** no new Pod is created for that Job

#### Scenario: A finished Job
- **WHEN** a Runner Job completes or fails
- **THEN** it and its Pod remain inspectable for 15 minutes and are then garbage-collected by Kubernetes
