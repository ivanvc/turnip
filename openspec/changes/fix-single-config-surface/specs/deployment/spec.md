## MODIFIED Requirements

### Requirement: Kustomize base
`deploy/base` SHALL contain exactly: a Deployment `turnip-server` (1 replica, label `app.kubernetes.io/name: turnip-server`, ServiceAccount `turnip-server`, Kubernetes' Service environment variables turned off (`enableServiceLinks: false`), one container `server` running `ghcr.io/ivanvc/turnip-server:latest` with ports `http` 8080 and `grpc` 9090); a `ClusterIP` Service `turnip-server` exposing ports 8080 (`http`) and 9090 (`grpc`); the ServiceAccount, Role, RoleBinding, ClusterRole and ClusterRoleBinding below; and a ConfigMap `turnip-server-config`, loaded whole into the container's environment. The ConfigMap's name SHALL be exactly `turnip-server-config`, with no suffix derived from its contents, so that a patch or transformer selecting it by name finds it whether it runs in the operator's kustomization or on the base already rendered, as Helmfile applies a release's transformers.

All of the Server's secret material SHALL come from one Secret, `turnip-server-secrets`, which the base SHALL NOT create: the container SHALL take `TURNIP_GITHUB_WEBHOOK_SECRET` and `TURNIP_GITHUB_PRIVATE_KEY` from its keys `webhook-secret` and `private-key`, and SHALL mount the whole Secret read-only at `/etc/turnip/secrets`, so a setting naming a file reads any further key from there (for the Redis password, key `redis-password` with `TURNIP_REDIS_PASSWORD_PATH=/etc/turnip/secrets/redis-password`). Where the base references the Secret, it SHALL state the Secret's name, its required keys and where the rest are mounted. The operator creates the Secret in their own files, next to the base: with a `secretGenerator` in their kustomization, or as their own resource.

The container SHALL also set `TURNIP_RUNNER_SERVER_ADDR` in its own `env`, formed from the namespace the Server Pod runs in: an earlier entry SHALL take that namespace from the Downward API (`metadata.namespace`), and `TURNIP_RUNNER_SERVER_ADDR` SHALL be `turnip-server.$(<that entry>).svc:9090`, so the Runner's address for the Server follows the Server namespace with nothing for the operator to set. The ConfigMap SHALL preset `TURNIP_LOG_LEVEL=info`, `TURNIP_HTTP_ADDR=:8080`, `TURNIP_GRPC_ADDR=:9090`, `TURNIP_MINIMIZE_OUTDATED_PLAN_COMMENTS=false` and `TURNIP_RUNNER_IMAGE=ghcr.io/ivanvc/turnip-runner:latest`, and SHALL leave `TURNIP_REDIS_ADDR`, `TURNIP_GITHUB_APP_ID`, `TURNIP_RUNNER_NAMESPACE`, `TURNIP_RUNNER_SERVICE_ACCOUNT`, `TURNIP_CLONE_SUBMODULES`, `TURNIP_ALLOWED_OVERRIDES`, `TURNIP_ALLOWED_IMAGES` and `TURNIP_MUTATION_REQUIREMENTS` unset, so every opt-in is off.

An operator SHALL be able to configure the Server without patching the Deployment: every setting in `turnip-server-config`, every secret in `turnip-server-secrets`, the Server image through Kustomize's image transformer, and the replica count through Kustomize's replica transformer. The one exception SHALL be pointing Runners at an address other than the Server's own Service (below). The Server reads its settings once, at startup (see the `server` capability), so a changed ConfigMap or Secret SHALL take effect only once the Server restarts; nothing in the base restarts it, and the operator documentation SHALL say to run `kubectl rollout restart deployment/turnip-server` wherever it shows a setting or secret being changed.

The base SHALL place every namespaced resource in `turnip-system`, which it SHALL NOT create, and SHALL bind its subjects to `turnip-server` there; with `TURNIP_RUNNER_NAMESPACE` unset, Runner Jobs run in that same namespace, so the base runs with one namespace and nothing else to create. turnip SHALL ship two Kustomize transformers for an operator's kustomization: `deploy/transformers/namespaces`, which points the bindings' subjects at the namespace of the `turnip-server` ServiceAccount and needs no setting; and `deploy/transformers/runner-namespace`, which does the same and also places the Role and RoleBinding in the namespace named by the `TURNIP_RUNNER_NAMESPACE` the operator sets in `turnip-server-config`. An operator SHALL be able to choose the Server namespace with their kustomization's `namespace:` and the first transformer, and to separate the Runner namespace by also setting `TURNIP_RUNNER_NAMESPACE` and using the second transformer in place of the first; `TURNIP_RUNNER_SERVER_ADDR` follows the Server namespace on its own. The operator creates every namespace they name. The base SHALL create no ServiceAccount or RBAC for the Runner (see the `runner-access` capability).

#### Scenario: Remote base with operator values
- **WHEN** an operator's `kustomization.yaml` references `github.com/ivanvc/turnip//deploy/base?ref=vX.Y.Z`, merges `TURNIP_REDIS_ADDR`, `TURNIP_GITHUB_APP_ID` and a pinned `TURNIP_RUNNER_IMAGE` into `turnip-server-config`, generates `turnip-server-secrets` with a `secretGenerator`, and runs `kubectl apply -k .`
- **THEN** the Server starts with every required setting present, reading the GitHub App's credentials from the generated Secret

#### Scenario: Secret missing
- **WHEN** the base is applied without a `turnip-server-secrets` Secret
- **THEN** the Server container is never created and the Pod reports `CreateContainerConfigError`

#### Scenario: Required values missing
- **WHEN** the base is applied with the Secret but without `TURNIP_REDIS_ADDR` or `TURNIP_GITHUB_APP_ID`
- **THEN** the Server exits at startup naming them and the Pod enters `CrashLoopBackOff`

#### Scenario: The default namespaces
- **WHEN** an operator creates the namespace `turnip-system` and applies the base with their values
- **THEN** the Server and its Runner Jobs both run in `turnip-system`, and Runner Pods reach the Server at `turnip-server.turnip-system.svc:9090`

#### Scenario: Renaming the Server namespace
- **WHEN** an operator's kustomization sets `namespace: ops` and lists `deploy/transformers/namespaces`
- **THEN** the Server, the Role and RoleBinding and Runner Jobs are in `ops`, and both bindings' subjects are `turnip-server` in `ops`

#### Scenario: Installing outside default
- **WHEN** an operator's kustomization sets `namespace: ops`, merges `TURNIP_RUNNER_NAMESPACE=ops-runners` and lists `deploy/transformers/runner-namespace`
- **THEN** the Server runs in `ops`, `TURNIP_RUNNER_NAMESPACE` is `ops-runners`, the Server's `TURNIP_RUNNER_SERVER_ADDR` is `turnip-server.ops.svc:9090` without the operator setting it, the Role and RoleBinding are in `ops-runners`, and the bindings' subjects are `turnip-server` in `ops`

#### Scenario: Pointing Runners at another address
- **WHEN** an operator needs Runners to reach the Server at an address other than its Service in its own namespace
- **THEN** they patch `TURNIP_RUNNER_SERVER_ADDR` in the Deployment's `env`, because a value set in `turnip-server-config` is overridden by the container's own `env`

#### Scenario: A namespace not created
- **WHEN** an operator separates the Runner namespace `ops-runners` and applies before creating it
- **THEN** the apply fails for the Role and RoleBinding in it, and no Runner Job can be created until the operator creates it

#### Scenario: Transformers applied to the rendered base
- **WHEN** the base is rendered first, and a patch setting `TURNIP_REDIS_ADDR` and `TURNIP_RUNNER_NAMESPACE=ops-runners` in the ConfigMap selected by the name `turnip-server-config`, followed by `deploy/transformers/runner-namespace`, is then applied to the rendered output, as Helmfile does
- **THEN** the ConfigMap holds both values under the name `turnip-server-config`, the Deployment still references it, and the Role and RoleBinding are in `ops-runners`

#### Scenario: The Redis password from the Secret
- **WHEN** `turnip-server-secrets` holds a key `redis-password` and `turnip-server-config` sets `TURNIP_REDIS_PASSWORD_PATH=/etc/turnip/secrets/redis-password`
- **THEN** the Server authenticates to Redis with that password, with no patch to the Deployment

#### Scenario: A Secret without the optional keys
- **WHEN** `turnip-server-secrets` holds only `webhook-secret` and `private-key`
- **THEN** the Server starts, and `/etc/turnip/secrets` holds just those two files

#### Scenario: Choosing the Server image
- **WHEN** an operator's kustomization, or a Helmfile release's transformers, sets the image `ghcr.io/ivanvc/turnip-server` to another name and tag with Kustomize's image transformer
- **THEN** the Deployment runs that image, with no patch to the Deployment

#### Scenario: A changed setting
- **WHEN** an operator changes `TURNIP_MAX_COMMENTS_PER_PROJECT` in `turnip-server-config` while the Server runs
- **THEN** the running Server keeps the old value, and uses the new one after `kubectl rollout restart deployment/turnip-server`

### Requirement: Overlays and release manifest
`deploy/overlays/release` SHALL be the base with the Server image tag and `TURNIP_RUNNER_IMAGE` set to the release tag; each GitHub Release SHALL attach it rendered as a single file, `turnip-install.yaml`, which, like the base, carries no `TURNIP_REDIS_ADDR`, no `TURNIP_GITHUB_APP_ID` and no Secret, uses the base's single namespace, `turnip-system`, and therefore needs the operator to create it before applying it, and to set those values in `turnip-server-config` and create `turnip-server-secrets` after. `deploy/overlays/kind` SHALL be a local-testing example only: one replica, the Server image tagged `dev`, `TURNIP_REDIS_ADDR=redis:6379`, the placeholder `TURNIP_GITHUB_APP_ID=0`, and the Runner namespace separated as `turnip-runners` through `deploy/transformers/runner-namespace`, so local testing exercises the separated layout; it expects a pre-existing `redis` Service and `turnip-server-secrets` Secret. `deploy/components/grafana-dashboard` SHALL be an opt-in Kustomize component adding a ConfigMap `turnip-grafana-dashboard` labeled `grafana_dashboard: "1"` (the dashboard's content is owned by the `observability` capability); neither the base nor `turnip-install.yaml` SHALL include it.

#### Scenario: Quick install
- **WHEN** an operator creates `turnip-system`, applies `turnip-install.yaml` from release `vX.Y.Z`, sets `TURNIP_REDIS_ADDR` and `TURNIP_GITHUB_APP_ID` in `turnip-server-config`, creates `turnip-server-secrets` and restarts the Deployment
- **THEN** the Server and the Runner image both run tag `vX.Y.Z`

#### Scenario: Dashboard opt-in
- **WHEN** an operator applies `deploy/components/grafana-dashboard`
- **THEN** a ConfigMap labeled `grafana_dashboard: "1"` holding the turnip dashboard appears for Grafana to discover
