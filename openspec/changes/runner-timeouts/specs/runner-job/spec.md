## MODIFIED Requirements

### Requirement: Runner environment
The `clone` and `runner` containers SHALL both receive these variables: `TURNIP_SERVER_ADDR` (from the Server's `TURNIP_RUNNER_SERVER_ADDR`), `TURNIP_OPERATION_ID`, `TURNIP_PROJECT_NAME`, `TURNIP_PROJECT_DIR`, `TURNIP_TOOL`, `TURNIP_TOOL_VERSION` (the tag the Project's `uses:` names), `TURNIP_OPERATION`, `TURNIP_REPO_URL`, `TURNIP_COMMIT_SHA`, `TURNIP_BASE_REF`, `TURNIP_TOOL_CONFIG` (the Project's `with:` as JSON), `TURNIP_EXTRA_ARGS` (JSON array), `TURNIP_PLAN_DATA` (base64), `TURNIP_WORKSPACE_DIR=/turnip/src` and `TURNIP_TOKEN_FILE=/turnip/run/secrets/token`.

Only the `clone` container SHALL receive `TURNIP_CLONE_SUBMODULES` (see the `checkout` capability). Only the `runner` container SHALL receive `TURNIP_IDLE_TIMEOUT`, the Server's `TURNIP_RUNNER_IDLE_TIMEOUT` as a duration (see the `operation-lifecycle` capability); unset or empty, the Runner SHALL use 30 minutes. Only a copy-out `runner` container SHALL receive `TURNIP_TOOLS_DIR=/turnip/tools`, which the Runner prepends to its own `PATH` at startup.

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

### Requirement: No retries and a bounded afterlife
A Runner Job SHALL never be retried by Kubernetes: the Job SHALL set `backoffLimit: 0` and the Pod `restartPolicy: Never`, so a failed Pod is never replaced and a mutating operation never runs twice. A finished Job and its Pod SHALL be removed by Kubernetes 900 seconds (15 minutes) after finishing, through `ttlSecondsAfterFinished`. The Job SHALL set `activeDeadlineSeconds` to the Server's `TURNIP_RUNNER_DEADLINE` (4 hours by default; see the `server` capability), so Kubernetes stops every Runner by then even when no Server is running, and the Pod SHALL set `terminationGracePeriodSeconds: 60`, which leaves the Runner time to interrupt its tool, wait for it, and report (see the `operation-lifecycle` capability). A Job stopped at its deadline is finished, so the same 15 minutes apply. The Server SHALL NOT delete Runner Jobs itself.

#### Scenario: A Runner Pod fails
- **WHEN** a Runner Pod's container exits with a non-zero status
- **THEN** no new Pod is created for that Job

#### Scenario: A finished Job
- **WHEN** a Runner Job completes or fails
- **THEN** it and its Pod remain inspectable for 15 minutes and are then garbage-collected by Kubernetes

#### Scenario: A Runner past its deadline
- **WHEN** a Runner Job has run for `TURNIP_RUNNER_DEADLINE`
- **THEN** Kubernetes stops its Pod, giving it 60 seconds after `SIGTERM`, and marks the Job failed with reason `DeadlineExceeded`
