## MODIFIED Requirements

### Requirement: Settings loader
The Server SHALL read its configuration once, at startup, from environment variables, and SHALL NOT reload it while running. An empty value SHALL count as unset. These settings SHALL be required, and every unset one SHALL be named together in a single startup error (`missing required environment variable(s): <names>`): `TURNIP_GITHUB_APP_ID`, `TURNIP_GITHUB_WEBHOOK_SECRET`, `TURNIP_REDIS_ADDR`, `TURNIP_HTTP_ADDR`, `TURNIP_GRPC_ADDR`, `TURNIP_RUNNER_SERVER_ADDR`, `TURNIP_RUNNER_IMAGE`, and one of `TURNIP_GITHUB_PRIVATE_KEY` (inline PEM) or `TURNIP_GITHUB_PRIVATE_KEY_PATH` (a file holding the PEM). When both key settings are set, `TURNIP_GITHUB_PRIVATE_KEY_PATH` SHALL be used. When `TURNIP_RUNNER_NAMESPACE` is unset, the Runner namespace SHALL be the namespace the Server runs in: in a cluster, `POD_NAMESPACE` when set, otherwise the namespace of the Server's ServiceAccount; outside one, the namespace of the current kubeconfig context, or `default` when it names none. The Server SHALL log the Runner namespace it resolved at startup. `TURNIP_RUNNER_IDLE_TIMEOUT` and `TURNIP_RUNNER_DEADLINE` SHALL be Go durations (`30m`, `4h`), defaulting to 30 minutes and 4 hours, documented as safety bounds rather than sizing choices; each SHALL be positive, and the deadline SHALL be longer than the idle timeout, so a silent tool is stopped with the clearer idle message before the deadline fires. A malformed value SHALL stop the Server from starting with an error naming the setting; this includes a non-integer `TURNIP_GITHUB_APP_ID`, an unreadable key file, a `TURNIP_MINIMIZE_OUTDATED_PLAN_COMMENTS` that is not a boolean, a `TURNIP_RUNNER_IDLE_TIMEOUT` or `TURNIP_RUNNER_DEADLINE` that is not a positive duration, a deadline not longer than the idle timeout (an error naming both), and an unrecognized value of any enumerated setting (`TURNIP_CLONE_SUBMODULES`, `TURNIP_ALLOWED_OVERRIDES`, `TURNIP_MUTATION_REQUIREMENTS`, `TURNIP_ALLOWED_IMAGES`). The one exception SHALL be `TURNIP_LOG_LEVEL`, where an unrecognized value falls back to `info`. A non-integer `TURNIP_GITHUB_APP_ID` or an unreadable key file SHALL be reported on its own, without the list of unset settings. The remaining values SHALL be checked only once no required setting is unset, and the first malformed one SHALL be reported on its own.

#### Scenario: Several required settings missing
- **WHEN** the Server starts with `TURNIP_REDIS_ADDR` and `TURNIP_GITHUB_APP_ID` unset
- **THEN** it logs one error naming both and exits with a non-zero status

#### Scenario: Private key from a file
- **WHEN** `TURNIP_GITHUB_PRIVATE_KEY_PATH=/etc/turnip/key.pem` is set and the file is readable
- **THEN** the Server uses that file's contents as the App's private key, even if `TURNIP_GITHUB_PRIVATE_KEY` is also set

#### Scenario: Unknown enumerated value
- **WHEN** `TURNIP_CLONE_SUBMODULES=deep`
- **THEN** the Server exits at startup with an error naming `TURNIP_CLONE_SUBMODULES` and listing the known modes

#### Scenario: Unknown log level
- **WHEN** `TURNIP_LOG_LEVEL=verbose`
- **THEN** the Server starts and logs at `info`

#### Scenario: Runner namespace omitted
- **WHEN** a Server running in namespace `turnip-system` starts with `TURNIP_RUNNER_NAMESPACE` unset
- **THEN** it creates Runner Jobs in `turnip-system` and logs that namespace at startup

#### Scenario: The old namespace setting
- **WHEN** a Server is started with `TURNIP_K8S_NAMESPACE=ops-runners` and `TURNIP_RUNNER_NAMESPACE` unset
- **THEN** it ignores `TURNIP_K8S_NAMESPACE` and creates Runner Jobs in its own namespace

#### Scenario: Default timeouts
- **WHEN** the Server starts with neither `TURNIP_RUNNER_IDLE_TIMEOUT` nor `TURNIP_RUNNER_DEADLINE` set
- **THEN** Runners stop a tool after 30 minutes with no output, and Runner Jobs have a deadline of 4 hours

#### Scenario: A deadline shorter than the idle timeout
- **WHEN** `TURNIP_RUNNER_IDLE_TIMEOUT=2h` and `TURNIP_RUNNER_DEADLINE=1h`
- **THEN** the Server exits at startup with an error naming both settings

#### Scenario: A malformed duration
- **WHEN** `TURNIP_RUNNER_DEADLINE=four hours`
- **THEN** the Server exits at startup with an error naming `TURNIP_RUNNER_DEADLINE`

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
| `TURNIP_RUNNER_IDLE_TIMEOUT` | no (30 minutes) | `operation-lifecycle` |
| `TURNIP_RUNNER_DEADLINE` | no (4 hours) | `runner-job` |
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
