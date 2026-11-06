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
`deploy/base` SHALL contain exactly: a Deployment `turnip-server` (1 replica, label `app.kubernetes.io/name: turnip-server`, ServiceAccount `turnip-server`, Kubernetes' Service environment variables turned off (`enableServiceLinks: false`), one container `server` running `ghcr.io/ivanvc/turnip-server:latest` with ports `http` 8080 and `grpc` 9090); a `ClusterIP` Service `turnip-server` exposing ports 8080 (`http`) and 9090 (`grpc`); and the ServiceAccount, Role, RoleBinding, ClusterRole and ClusterRoleBinding below. The base SHALL ship no ConfigMap and no Secret, and no placeholder value for any setting: a value only the operator can give has no default in the base, and every other default lives in the Server (see the `server` capability).

The operator SHALL configure the Server through two objects they create next to the base, by name, the same way: a ConfigMap `turnip-server-config` holding every setting they set, which the container loads whole into its environment; and a Secret `turnip-server-secrets` holding all of the Server's secret material. Under Kustomize they are a `configMapGenerator` and a `secretGenerator` in the operator's kustomization; under Helmfile, or any other tool, they are the operator's own resources. The container SHALL take `TURNIP_GITHUB_WEBHOOK_SECRET` and `TURNIP_GITHUB_PRIVATE_KEY` from the Secret's keys `webhook-secret` and `private-key`, and SHALL mount the whole Secret read-only at `/etc/turnip/secrets`, so a setting naming a file reads any further key from there (for the Redis password, key `redis-password` with `TURNIP_REDIS_PASSWORD_PATH=/etc/turnip/secrets/redis-password`). Where the base references each object, it SHALL state its name and what it must hold. Neither is optional: without either, the Server container is never created.

The container SHALL set three values in its own `env`, which the operator does not set: `POD_NAMESPACE`, the namespace the Pod runs in (Downward API, `metadata.namespace`); `TURNIP_RUNNER_SERVER_ADDR`, `turnip-server.$(POD_NAMESPACE).svc:9090`, so the Runner's address for the Server follows the Server namespace; and `TURNIP_RUNNER_NAMESPACE`, the namespace holding the Server's Role, which is `$(POD_NAMESPACE)` in the base and which the `runner-namespace` transformer sets when the Role moves (below). A container's own `env` takes precedence over its `envFrom`, so either value set in `turnip-server-config` SHALL have no effect.

An operator SHALL be able to configure the Server without patching the Deployment: every setting in `turnip-server-config`, every secret in `turnip-server-secrets`, and where turnip runs through Kustomize's transformers (the Server image through the image transformer, the replica count through the replica transformer, the Runner namespace through the Role's namespace, below). The one exception SHALL be pointing Runners at an address other than the Server's own Service. The Server reads its settings once, at startup (see the `server` capability), so a changed ConfigMap or Secret SHALL take effect only once the Server restarts. A ConfigMap or Secret generated by Kustomize is named with a hash of its contents, so a change gives it a new name and the Deployment restarts by itself; one created any other way keeps its name, and the operator documentation SHALL say to run `kubectl rollout restart deployment/turnip-server` after changing it.

The base SHALL place every namespaced resource in `turnip-system`, which it SHALL NOT create, and SHALL bind its subjects to `turnip-server` there; the Server's Role is there too, so Runner Jobs run in that same namespace, and the base runs with one namespace and nothing else to create. turnip SHALL ship two Kustomize transformers for an operator's kustomization, each needing no value of its own: `deploy/transformers/namespaces`, which points the bindings' subjects at the namespace of the `turnip-server` ServiceAccount; and `deploy/transformers/runner-namespace`, which does the same and also copies the namespace of the Server's Role to its RoleBinding and to the Server's `TURNIP_RUNNER_NAMESPACE`. An operator SHALL choose the Server namespace with their kustomization's `namespace:` and the first transformer, and SHALL separate the Runner namespace by listing, under `transformers:`, a patch setting the Role's namespace, followed by the second transformer in place of the first; the Runner namespace is then stated once, as the namespace of the Server's Role. The operator creates every namespace they name. The base SHALL create no ServiceAccount or RBAC for the Runner (see the `runner-access` capability).

#### Scenario: Remote base with operator values
- **WHEN** an operator's `kustomization.yaml` references `github.com/ivanvc/turnip//deploy/base?ref=vX.Y.Z`, generates `turnip-server-config` with `TURNIP_REDIS_ADDR`, `TURNIP_GITHUB_APP_ID` and a pinned `TURNIP_RUNNER_IMAGE` with a `configMapGenerator`, generates `turnip-server-secrets` with a `secretGenerator`, and runs `kubectl apply -k .`
- **THEN** the Server starts with every required setting present, reading both generated objects under their hashed names

#### Scenario: Secret missing
- **WHEN** the base is applied without a `turnip-server-secrets` Secret
- **THEN** the Server container is never created and the Pod reports `CreateContainerConfigError`

#### Scenario: Required values missing
- **WHEN** the base is applied with both objects, but `turnip-server-config` lacks `TURNIP_REDIS_ADDR` or `TURNIP_GITHUB_APP_ID`
- **THEN** the Server exits at startup naming them and the Pod enters `CrashLoopBackOff`

#### Scenario: The default namespaces
- **WHEN** an operator creates the namespace `turnip-system` and applies the base with their two objects
- **THEN** the Server and its Runner Jobs both run in `turnip-system`, and Runner Pods reach the Server at `turnip-server.turnip-system.svc:9090`

#### Scenario: Renaming the Server namespace
- **WHEN** an operator's kustomization sets `namespace: ops` and lists `deploy/transformers/namespaces`
- **THEN** the Server, the Role and RoleBinding and Runner Jobs are in `ops`, and both bindings' subjects are `turnip-server` in `ops`

#### Scenario: Installing outside default
- **WHEN** an operator's kustomization sets `namespace: ops` and lists, under `transformers:`, a patch setting the Server's Role's namespace to `ops-runners` followed by `deploy/transformers/runner-namespace`
- **THEN** the Server runs in `ops`, the Role and RoleBinding are in `ops-runners`, the Server's `TURNIP_RUNNER_NAMESPACE` is `ops-runners` and its `TURNIP_RUNNER_SERVER_ADDR` is `turnip-server.ops.svc:9090`, neither set by the operator, and the bindings' subjects are `turnip-server` in `ops`

#### Scenario: Pointing Runners at another address
- **WHEN** an operator needs Runners to reach the Server at an address other than its Service in its own namespace
- **THEN** they patch `TURNIP_RUNNER_SERVER_ADDR` in the Deployment's `env`, because a value set in `turnip-server-config` is overridden by the container's own `env`

#### Scenario: A namespace not created
- **WHEN** an operator separates the Runner namespace `ops-runners` and applies before creating it
- **THEN** the apply fails for the Role and RoleBinding in it, and no Runner Job can be created until the operator creates it

#### Scenario: Transformers applied to the rendered base
- **WHEN** the base is rendered first, and a patch setting the Server's Role's namespace to `ops-runners`, followed by `deploy/transformers/runner-namespace`, is then applied to the rendered output, as Helmfile does
- **THEN** the Role and RoleBinding are in `ops-runners` and the Deployment's `TURNIP_RUNNER_NAMESPACE` is `ops-runners`

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
- **WHEN** an operator changes `TURNIP_MAX_COMMENTS_PER_PROJECT` in a `turnip-server-config` they created as their own resource, while the Server runs
- **THEN** the running Server keeps the old value, and uses the new one after `kubectl rollout restart deployment/turnip-server`

#### Scenario: A changed generated setting
- **WHEN** an operator changes `TURNIP_MAX_COMMENTS_PER_PROJECT` in their kustomization's `configMapGenerator` and applies it
- **THEN** the ConfigMap gets a new hashed name, the Deployment rolls, and the Server uses the new value

#### Scenario: ConfigMap missing
- **WHEN** the base is applied with `turnip-server-secrets` but without `turnip-server-config`
- **THEN** the Server container is never created and the Pod reports `CreateContainerConfigError`

#### Scenario: The base alone
- **WHEN** `deploy/base` is rendered on its own
- **THEN** it holds no ConfigMap and no Secret

#### Scenario: The Runner namespace set in the ConfigMap
- **WHEN** an operator sets `TURNIP_RUNNER_NAMESPACE` in `turnip-server-config`
- **THEN** it has no effect: Runner Jobs are created in the namespace of the Server's Role

### Requirement: Server RBAC
The base SHALL grant the Server's ServiceAccount, which lives in the Server namespace, exactly: through a Role `turnip-server` in the Runner namespace, bound by a RoleBinding of the same name there to `turnip-server` in the Server namespace, `create`, `delete`, `get`, `list` and `watch` on `jobs` and `get` and `list` on `pods`; and through a ClusterRole `turnip-server-token-review` bound by a ClusterRoleBinding of the same name to `turnip-server` in the Server namespace, `create` on `tokenreviews`. The ClusterRole SHALL be turnip's only cluster-scoped grant, and turnip SHALL NOT bind `system:auth-delegator` or request `subjectaccessreviews`. Runner Jobs SHALL be created in the Runner namespace (see the `server` capability), which therefore has to be the namespace holding this Role: the Server namespace by default, and once the operator separates the Runner namespace, the Server SHALL hold no grant in its own namespace.

#### Scenario: Namespace mismatch
- **WHEN** `TURNIP_RUNNER_NAMESPACE` names a namespace other than the one holding the `turnip-server` Role
- **THEN** the Server is forbidden from creating Runner Jobs there

#### Scenario: Token reviews
- **WHEN** a Runner opens a stream
- **THEN** the Server can create the `TokenReview` that authenticates it, and holds no other cluster-scoped permission

#### Scenario: No grant in the Server namespace
- **WHEN** an operator separates the Runner namespace with `deploy/transformers/runner-namespace`
- **THEN** no Role or RoleBinding in the Server namespace grants the Server's ServiceAccount anything

### Requirement: Probes and scaling
The Deployment SHALL use `GET /readyz` on port `http` as its readiness probe and `GET /healthz` as its liveness probe, each every 5 seconds (their meaning is owned by the `observability` capability). The Server SHALL be safe to run at more than one replica with no coordination to configure, since all shared state lives in Redis.

#### Scenario: Redis unreachable
- **WHEN** a Server Pod cannot reach Redis
- **THEN** it is marked not ready and removed from the Service's endpoints, while its liveness probe keeps passing

#### Scenario: Scaling out
- **WHEN** an operator runs `kubectl scale deployment/turnip-server --replicas=3`
- **THEN** all three replicas serve webhooks and Runner streams against the same Redis

### Requirement: Overlays and release manifest
`deploy/overlays/release` SHALL be the base with the Server image tag set to the release tag; each GitHub Release SHALL attach it rendered as a single file, `turnip-install.yaml`, which, like the base, holds no ConfigMap and no Secret, uses the base's single namespace, `turnip-system`, and therefore needs the operator to create that namespace before applying it and to create `turnip-server-config` (with `TURNIP_REDIS_ADDR`, `TURNIP_GITHUB_APP_ID` and `TURNIP_RUNNER_IMAGE`, the Runner image of the same release) and `turnip-server-secrets` there. `deploy/overlays/kind` SHALL be a local-testing example only: one replica, the Server image tagged `dev`, a generated `turnip-server-config` with `TURNIP_REDIS_ADDR=redis:6379`, the placeholder `TURNIP_GITHUB_APP_ID=0` and the Runner image tagged `dev`, and the Runner namespace separated as `turnip-runners` through a patch of the Role's namespace and `deploy/transformers/runner-namespace`, so local testing exercises the separated layout; it expects a pre-existing `redis` Service and `turnip-server-secrets` Secret. `deploy/components/grafana-dashboard` SHALL be an opt-in Kustomize component adding a ConfigMap `turnip-grafana-dashboard` labeled `grafana_dashboard: "1"` (the dashboard's content is owned by the `observability` capability); neither the base nor `turnip-install.yaml` SHALL include it.

#### Scenario: Quick install
- **WHEN** an operator creates `turnip-system`, applies `turnip-install.yaml` from release `vX.Y.Z`, and creates `turnip-server-config` (with `TURNIP_RUNNER_IMAGE=ghcr.io/ivanvc/turnip-runner:vX.Y.Z`) and `turnip-server-secrets`
- **THEN** the Server starts once both exist, and the Server and the Runner image both run tag `vX.Y.Z`

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
