# deployment Specification

## Purpose
Describes what an operator installs to run turnip: the Kustomize base and overlays, the cluster permissions the Server needs, its Service and probes, the released images, and the prerequisites turnip expects to find. Operators rely on it to install, upgrade and scale a Server.

## Requirements

### Requirement: Prerequisites
A turnip installation SHALL require: a Kubernetes cluster at v1.32 or later (below it, every Runner stream is refused; see the `runner-security` capability); a Redis or Valkey instance, which SHALL hold every Lock and recorded plan and therefore needs persistence to keep them across a restart; public HTTPS routing to the Server's webhook path; and a GitHub App installed on the repositories, with the Checks (read and write), Contents (read), Issues (read and write) and Pull requests (read and write) repository permissions, subscribed to the `Pull request` and `Issue comment` events, its webhook URL set to `https://<host>/github/webhook`. turnip SHALL ship none of these: no Redis, no Ingress, Gateway or LoadBalancer, no DNS or certificate, and no GitHub App.

The Redis instance SHALL be reachable as a single endpoint, and MAY require TLS, AUTH, or both (see the `server` capability for the address forms and credentials). It SHALL be Redis 5.0 or later, and 6.0 or later when a username is used. It SHALL NOT be clustered: on Amazon ElastiCache, cluster mode disabled (a primary, optionally with replicas), not cluster mode enabled. turnip connects to one endpoint; pointed at a clustered Redis, its operations fail with `MOVED` errors.

#### Scenario: Redis without persistence restarts
- **WHEN** the Redis instance restarts without persisted data
- **THEN** every Lock and recorded plan is gone, and every pull request must plan again before it can apply

#### Scenario: No ingress configured
- **WHEN** an operator applies `deploy/base` and adds no ingress
- **THEN** the Server is reachable only inside the cluster through its `ClusterIP` Service and GitHub cannot deliver webhooks

#### Scenario: A clustered endpoint
- **WHEN** turnip is pointed at an ElastiCache cluster-mode-enabled endpoint
- **THEN** its Redis operations fail with `MOVED` errors

### Requirement: Kustomize base
`deploy/base` SHALL contain exactly: a Deployment `turnip-server` (1 replica, label `app.kubernetes.io/name: turnip-server`, ServiceAccount `turnip-server`, one container `server` running `ghcr.io/ivanvc/turnip-server:latest` with ports `http` 8080 and `grpc` 9090); a `ClusterIP` Service `turnip-server` exposing ports 8080 (`http`) and 9090 (`grpc`); the ServiceAccount, Role, RoleBinding, ClusterRole and ClusterRoleBinding below; and a generated ConfigMap `turnip-server-config` loaded whole into the container's environment. The container SHALL take `TURNIP_GITHUB_WEBHOOK_SECRET` and `TURNIP_GITHUB_PRIVATE_KEY` from the keys `webhook-secret` and `private-key` of a Secret `turnip-github-app`, which the base SHALL NOT create. The ConfigMap SHALL preset `TURNIP_LOG_LEVEL=info`, `TURNIP_HTTP_ADDR=:8080`, `TURNIP_GRPC_ADDR=:9090`, `TURNIP_K8S_NAMESPACE=default`, `TURNIP_RUNNER_SERVER_ADDR=turnip-server:9090`, `TURNIP_MINIMIZE_OUTDATED_PLAN_COMMENTS=false` and `TURNIP_RUNNER_IMAGE=ghcr.io/ivanvc/turnip-runner:latest`, and SHALL leave `TURNIP_REDIS_ADDR`, `TURNIP_GITHUB_APP_ID`, `TURNIP_RUNNER_SERVICE_ACCOUNT`, `TURNIP_CLONE_SUBMODULES`, `TURNIP_ALLOWED_OVERRIDES`, `TURNIP_ALLOWED_IMAGES` and `TURNIP_MUTATION_REQUIREMENTS` unset, so every opt-in is off. The base SHALL set no namespace; it assumes `default`, and installing elsewhere requires both a Kustomize `namespace:` and a matching `TURNIP_K8S_NAMESPACE`, because Kustomize does not rewrite ConfigMap values. The base SHALL create no ServiceAccount or RBAC for the Runner (see the `runner-access` capability).

#### Scenario: Remote base with operator values
- **WHEN** an operator's `kustomization.yaml` references `github.com/ivanvc/turnip//deploy/base?ref=vX.Y.Z`, merges `TURNIP_REDIS_ADDR`, `TURNIP_GITHUB_APP_ID` and a pinned `TURNIP_RUNNER_IMAGE` into `turnip-server-config`, generates `turnip-github-app`, and runs `kubectl apply -k .`
- **THEN** the Server starts with every required setting present

#### Scenario: Secret missing
- **WHEN** the base is applied without a `turnip-github-app` Secret
- **THEN** the Server container is never created and the Pod reports `CreateContainerConfigError`

#### Scenario: Required values missing
- **WHEN** the base is applied with the Secret but without `TURNIP_REDIS_ADDR` or `TURNIP_GITHUB_APP_ID`
- **THEN** the Server exits at startup naming them and the Pod enters `CrashLoopBackOff`

#### Scenario: Installing outside default
- **WHEN** an operator sets `namespace: turnip` in their kustomization and `TURNIP_K8S_NAMESPACE=turnip`
- **THEN** every resource, including the ClusterRoleBinding's subject, lands in or points at `turnip`, and Runner Jobs are created there

### Requirement: Server RBAC
The base SHALL grant the Server's ServiceAccount exactly: through a namespaced Role `turnip-server`, `create`, `delete`, `get`, `list` and `watch` on `jobs` and `get` and `list` on `pods`; and through a ClusterRole `turnip-server-token-review` bound by a ClusterRoleBinding of the same name to `turnip-server` in `default`, `create` on `tokenreviews`. The ClusterRole SHALL be turnip's only cluster-scoped grant, and turnip SHALL NOT bind `system:auth-delegator` or request `subjectaccessreviews`. Runner Jobs SHALL be created in `TURNIP_K8S_NAMESPACE`, which therefore has to be the namespace holding this Role.

#### Scenario: Namespace mismatch
- **WHEN** `TURNIP_K8S_NAMESPACE` names a namespace other than the one holding the `turnip-server` Role
- **THEN** the Server is forbidden from creating Runner Jobs there

#### Scenario: Token reviews
- **WHEN** a Runner opens a stream
- **THEN** the Server can create the `TokenReview` that authenticates it, and holds no other cluster-scoped permission

### Requirement: Probes and scaling
The Deployment SHALL use `GET /readyz` on port `http` as its readiness probe and `GET /healthz` as its liveness probe, each every 5 seconds (their meaning is owned by the `observability` capability). The Server SHALL be safe to run at more than one replica with no coordination to configure, since all shared state lives in Redis.

#### Scenario: Redis unreachable
- **WHEN** a Server Pod cannot reach Redis
- **THEN** it is marked not ready and removed from the Service's endpoints, while its liveness probe keeps passing

#### Scenario: Scaling out
- **WHEN** an operator runs `kubectl scale deployment/turnip-server --replicas=3`
- **THEN** all three replicas serve webhooks and Runner streams against the same Redis

### Requirement: Overlays and release manifest
`deploy/overlays/release` SHALL be the base with the Server image tag and `TURNIP_RUNNER_IMAGE` set to the release tag; each GitHub Release SHALL attach it rendered as a single file, `turnip-install.yaml`, which, like the base, carries no `TURNIP_REDIS_ADDR`, no `TURNIP_GITHUB_APP_ID` and no Secret, assumes the `default` namespace, and therefore needs the operator to set those values and create `turnip-github-app` after applying it. `deploy/overlays/kind` SHALL be a local-testing example only: one replica, the Server image tagged `dev`, `TURNIP_REDIS_ADDR=redis:6379` and the placeholder `TURNIP_GITHUB_APP_ID=0`, expecting a pre-existing `redis` Service and `turnip-github-app` Secret. `deploy/components/grafana-dashboard` SHALL be an opt-in Kustomize component adding a ConfigMap `turnip-grafana-dashboard` labeled `grafana_dashboard: "1"` (the dashboard's content is owned by the `observability` capability); neither the base nor `turnip-install.yaml` SHALL include it.

#### Scenario: Quick install
- **WHEN** an operator applies `turnip-install.yaml` from release `vX.Y.Z`, sets `TURNIP_REDIS_ADDR` and `TURNIP_GITHUB_APP_ID` on the Deployment and creates `turnip-github-app`
- **THEN** the Server and the Runner image both run tag `vX.Y.Z`

#### Scenario: Dashboard opt-in
- **WHEN** an operator applies `deploy/components/grafana-dashboard`
- **THEN** a ConfigMap labeled `grafana_dashboard: "1"` holding the turnip dashboard appears for Grafana to discover

### Requirement: Images
Each release SHALL publish `ghcr.io/ivanvc/turnip-server` and `ghcr.io/ivanvc/turnip-runner` for `linux/amd64`, tagged with the release tag and `latest`. The Server image SHALL be a distroless static image running as a non-root user with the Server as its entrypoint. The Runner image SHALL be Alpine-based with `git` and CA certificates, holding a statically linked Runner binary at `/runner`, so that binary can be copied into and run inside a vendor tool image (see the `runner-job` capability).

#### Scenario: Pinning the Runner image
- **WHEN** an operator installs release `vX.Y.Z`
- **THEN** `ghcr.io/ivanvc/turnip-runner:vX.Y.Z` exists for `TURNIP_RUNNER_IMAGE` to name

#### Scenario: Non-amd64 node
- **WHEN** a cluster's nodes are `arm64` only
- **THEN** no published turnip image runs on them
