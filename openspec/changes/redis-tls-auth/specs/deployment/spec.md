## MODIFIED Requirements

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
