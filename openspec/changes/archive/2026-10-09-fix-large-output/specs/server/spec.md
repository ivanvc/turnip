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
| `TURNIP_K8S_NAMESPACE` | yes | `runner-job` |
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

#### Scenario: Setting not in the index
- **WHEN** an operator sets `TURNIP_RUNNER_TIMEOUT` on the Server
- **THEN** the Server ignores it

#### Scenario: Redis address
- **WHEN** `TURNIP_REDIS_ADDR=redis.example.svc:6379`
- **THEN** the Server connects to that address with no AUTH and no TLS

#### Scenario: Redis address over TLS
- **WHEN** `TURNIP_REDIS_ADDR=rediss://cache.example.com:6379`
- **THEN** the Server connects to that address over TLS, verifying the server's certificate
