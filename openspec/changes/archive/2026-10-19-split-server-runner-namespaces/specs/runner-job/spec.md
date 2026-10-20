## MODIFIED Requirements

### Requirement: One Job per Operation
The Server SHALL create exactly one Kubernetes Job for each dispatched Operation, in the Runner namespace (`TURNIP_RUNNER_NAMESPACE`, or the Server's own namespace when it is unset; see the `server` capability), with a name generated from the prefix `turnip-runner-`. The Job SHALL carry the labels `app.kubernetes.io/name: turnip-runner`, `turnip.ivan.vc/operation-id` (the Operation's id) and `turnip.ivan.vc/project` (the Project's name as written, unaltered). The Server SHALL NOT create the Job when the Operation is refused before dispatch (see the `locking`, `authorization` and `runner-access` capabilities).

#### Scenario: A plan for one Project
- **WHEN** a plan for Project `web` is dispatched
- **THEN** one Job named `turnip-runner-<suffix>` is created in the Runner namespace
- **AND** it is labeled `app.kubernetes.io/name=turnip-runner`, `turnip.ivan.vc/operation-id=<operation id>` and `turnip.ivan.vc/project=web`

#### Scenario: Several Projects in one trigger
- **WHEN** one trigger dispatches an Operation for Projects `web` and `api`
- **THEN** two Jobs are created, one per Project, each with its own Operation id

#### Scenario: A Project name Kubernetes does not accept as a label value
- **WHEN** a Project's name is not a valid Kubernetes label value (for example `infra/web`, the default name of a Project in that directory)
- **THEN** Kubernetes rejects the Job and the Operation is refused as a Runner Job that could not be created (see the `project-checks` capability)
