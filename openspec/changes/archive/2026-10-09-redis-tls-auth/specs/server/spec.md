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

## ADDED Requirements

### Requirement: Redis connection
The Server SHALL connect to Redis according to `TURNIP_REDIS_ADDR`:

- a bare `host:port` connects without TLS and without AUTH, as before;
- a `redis://` URL connects without TLS;
- a `rediss://` URL connects over TLS and SHALL verify the server's certificate against the system's trusted roots, or against the PEM bundle at `TURNIP_REDIS_CA_PATH` when set.

A URL MAY carry a username (used for AUTH) and everything else the Redis URL form accepts (a database number, connection-tuning parameters). The Server SHALL refuse to start, naming the fix, when the address: carries a password in the URL (the error names `TURNIP_REDIS_PASSWORD_PATH` and `TURNIP_REDIS_PASSWORD`); carries `skip_verify`, whatever its value; uses a scheme other than `redis` or `rediss`; or carries a query parameter the Redis URL form does not recognize.

The AUTH password SHALL come from `TURNIP_REDIS_PASSWORD_PATH` (a file) or `TURNIP_REDIS_PASSWORD` (a value), never both; setting both SHALL stop the Server from starting. The file SHALL be read on each new connection, so a rotated password is used without a restart, and SHALL be read once at startup so a missing or unreadable file stops the Server from starting. `TURNIP_REDIS_CA_PATH` SHALL name a readable PEM bundle holding at least one certificate; an unreadable or empty bundle, or one set without a `rediss` address, SHALL stop the Server from starting. No Redis credential SHALL appear in any log line or readiness response.

#### Scenario: Password in the URL
- **WHEN** `TURNIP_REDIS_ADDR=rediss://user:secret@cache.example.com:6379`
- **THEN** the Server exits at startup with an error naming `TURNIP_REDIS_PASSWORD_PATH` and `TURNIP_REDIS_PASSWORD`

#### Scenario: Certificate verification cannot be turned off
- **WHEN** `TURNIP_REDIS_ADDR=rediss://cache.example.com:6379?skip_verify=true`
- **THEN** the Server exits at startup rather than skipping verification

#### Scenario: A username but a password from a file
- **WHEN** `TURNIP_REDIS_ADDR=rediss://turnip@cache.example.com:6379` and `TURNIP_REDIS_PASSWORD_PATH` names a readable file
- **THEN** the Server connects over TLS, authenticating as `turnip` with the file's contents

#### Scenario: Both password forms set
- **WHEN** `TURNIP_REDIS_PASSWORD` and `TURNIP_REDIS_PASSWORD_PATH` are both set
- **THEN** the Server exits at startup naming the conflict

#### Scenario: CA bundle without TLS
- **WHEN** `TURNIP_REDIS_CA_PATH` is set and `TURNIP_REDIS_ADDR` is a bare `host:port` or a `redis://` URL
- **THEN** the Server exits at startup, since the bundle would have no TLS to verify

#### Scenario: Rotated password
- **WHEN** the file at `TURNIP_REDIS_PASSWORD_PATH` is updated in place while the Server runs
- **THEN** the next Redis connection the Server opens authenticates with the new value, without a restart
