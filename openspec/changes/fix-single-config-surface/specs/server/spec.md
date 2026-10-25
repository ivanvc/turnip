## MODIFIED Requirements

### Requirement: Settings index
Every `TURNIP_*` setting the Server reads SHALL be one of those below, with its behavior owned by the named capability:

| Setting | Required | Owner |
|---|---|---|
| `TURNIP_GITHUB_APP_ID` | yes | `server` |
| `TURNIP_GITHUB_PRIVATE_KEY` or `TURNIP_GITHUB_PRIVATE_KEY_PATH` | one of them | `server` |
| `TURNIP_GITHUB_WEBHOOK_SECRET` | yes | `server` |
| `TURNIP_REDIS_ADDR` | yes | `server` (connection), `locking` (what is stored) |
| `TURNIP_REDIS_PASSWORD` or `TURNIP_REDIS_PASSWORD_PATH` | no | `server` |
| `TURNIP_REDIS_CA_PATH` | no | `server` |
| `TURNIP_HTTP_ADDR` | yes | `server` |
| `TURNIP_GRPC_ADDR` | yes | `server` |
| `TURNIP_RUNNER_NAMESPACE` | no (the Server's own namespace) | `runner-job` |
| `TURNIP_RUNNER_SERVER_ADDR` | yes | `runner-job` |
| `TURNIP_RUNNER_IMAGE` | yes | `runner-job` |
| `TURNIP_LOG_LEVEL` | no | `observability` |
| `TURNIP_MINIMIZE_OUTDATED_PLAN_COMMENTS` | no | `pr-comment` |
| `TURNIP_MAX_COMMENTS_PER_PROJECT` | no | `pr-comment` |
| `TURNIP_RUNNER_SERVICE_ACCOUNT` | no | `runner-access` |
| `TURNIP_ALLOWED_OVERRIDES` | no | `runner-access` |
| `TURNIP_CLONE_SUBMODULES` | no | `checkout` |
| `TURNIP_ALLOWED_IMAGES` | no | `tool-images` |
| `TURNIP_MUTATION_REQUIREMENTS` | no | `authorization` |

`TURNIP_REDIS_ADDR` SHALL be a bare `host:port`, a `redis://` URL (no TLS) or a `rediss://` URL (TLS), as the Redis connection requirement describes. The Runner's own `TURNIP_*` variables are set by the Server on each Job (see the `runner-job` capability) and SHALL NOT be operator settings.

A `TURNIP_*` variable in the Server's environment that is not in this index SHALL NOT change what the Server does, and SHALL NOT stop it from starting. The Server SHALL log one warning at startup naming every such variable, so a stale or mistyped setting is visible. The warning SHALL leave out the variables Kubernetes injects for a Service whose name begins with `turnip-` (names ending in `_SERVICE_HOST`, `_SERVICE_PORT`, `_SERVICE_PORT_<port name>` or `_PORT`, or of the form `<prefix>_PORT_<number>_<protocol>`, optionally followed by `_PROTO`, `_PORT` or `_ADDR`), since those are not settings; the deployment base turns them off in any case (see the `deployment` capability).

#### Scenario: Setting not in the index
- **WHEN** an operator sets `TURNIP_RUNNER_TIMEOUT` on the Server
- **THEN** the Server starts, ignores the value, and logs a warning at startup naming `TURNIP_RUNNER_TIMEOUT`

#### Scenario: A setting no longer read
- **WHEN** an operator still sets `TURNIP_K8S_NAMESPACE`
- **THEN** the Server starts, and its startup warning names `TURNIP_K8S_NAMESPACE`

#### Scenario: Every variable known
- **WHEN** every `TURNIP_*` variable in the Server's environment is in the index
- **THEN** the Server logs no such warning

#### Scenario: A Service's injected variables
- **WHEN** the Server runs with Service environment variables enabled, in a namespace holding a Service named `turnip-redis`, so its environment has `TURNIP_REDIS_PORT` and `TURNIP_REDIS_SERVICE_HOST`
- **THEN** the startup warning names neither

#### Scenario: Redis address
- **WHEN** `TURNIP_REDIS_ADDR=redis.example.svc:6379`
- **THEN** the Server connects to that address with no AUTH and no TLS

#### Scenario: Redis address over TLS
- **WHEN** `TURNIP_REDIS_ADDR=rediss://cache.example.com:6379`
- **THEN** the Server connects to that address over TLS, verifying the server's certificate
