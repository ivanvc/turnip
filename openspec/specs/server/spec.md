# server Specification

## Purpose
Defines how the turnip Server process is configured, starts, serves GitHub webhooks and shuts down, and indexes every `TURNIP_*` setting it reads to the capability that owns its behavior. Operators rely on it to configure and run the Server; GitHub relies on its webhook HTTP surface.

## Requirements

### Requirement: Settings loader
The Server SHALL read its configuration once, at startup, from environment variables, and SHALL NOT reload it while running. An empty value SHALL count as unset. These settings SHALL be required, and every unset one SHALL be named together in a single startup error (`missing required environment variable(s): <names>`): `TURNIP_GITHUB_APP_ID`, `TURNIP_GITHUB_WEBHOOK_SECRET`, `TURNIP_REDIS_ADDR`, `TURNIP_K8S_NAMESPACE`, `TURNIP_HTTP_ADDR`, `TURNIP_GRPC_ADDR`, `TURNIP_RUNNER_SERVER_ADDR`, `TURNIP_RUNNER_IMAGE`, and one of `TURNIP_GITHUB_PRIVATE_KEY` (inline PEM) or `TURNIP_GITHUB_PRIVATE_KEY_PATH` (a file holding the PEM). When both key settings are set, `TURNIP_GITHUB_PRIVATE_KEY_PATH` SHALL be used. A malformed value SHALL stop the Server from starting with an error naming the setting; this includes a non-integer `TURNIP_GITHUB_APP_ID`, an unreadable key file, a `TURNIP_MINIMIZE_OUTDATED_PLAN_COMMENTS` that is not a boolean, and an unrecognized value of any enumerated setting (`TURNIP_CLONE_SUBMODULES`, `TURNIP_ALLOWED_OVERRIDES`, `TURNIP_MUTATION_REQUIREMENTS`, `TURNIP_ALLOWED_IMAGES`). The one exception SHALL be `TURNIP_LOG_LEVEL`, where an unrecognized value falls back to `info`. A non-integer `TURNIP_GITHUB_APP_ID` or an unreadable key file SHALL be reported on its own, without the list of unset settings. The remaining values SHALL be checked only once no required setting is unset, and the first malformed one SHALL be reported on its own.

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

### Requirement: Startup
After loading settings, the Server SHALL parse the GitHub App private key and SHALL exit with a non-zero status when it is not a valid key. It SHALL use in-cluster Kubernetes credentials when available and otherwise the standard kubeconfig (`KUBECONFIG` or `~/.kube/config`), exiting when neither loads. It SHALL serve HTTP on `TURNIP_HTTP_ADDR` and gRPC for Runners on `TURNIP_GRPC_ADDR`, and SHALL exit when either cannot listen. It SHALL NOT wait for Redis at startup; Redis reachability is reported through readiness (see the `observability` capability). A settings error SHALL be logged as `loading config` and any later startup failure as `running server`, both at error level, before the process exits with status 1.

#### Scenario: Malformed private key
- **WHEN** `TURNIP_GITHUB_PRIVATE_KEY` holds text that is not a PEM private key
- **THEN** the Server logs `running server` with an error about parsing the App private key and exits with status 1

#### Scenario: gRPC port taken
- **WHEN** another process already listens on `TURNIP_GRPC_ADDR`
- **THEN** the Server stops its HTTP listener too and exits with status 1

### Requirement: Webhook HTTP surface
The Server SHALL accept GitHub webhook deliveries only as `POST /github/webhook` on `TURNIP_HTTP_ADDR`. Any other method on that path SHALL be answered `405`, and any path the Server does not serve (including `/`) SHALL be answered `404` without reaching webhook handling. Besides the webhook, the HTTP listener SHALL serve only `GET /healthz`, `GET /readyz` and `GET /metrics` (see the `observability` capability). Each delivery SHALL be answered as follows:

| Delivery | Status |
|---|---|
| `X-Hub-Signature-256` missing or not matching `TURNIP_GITHUB_WEBHOOK_SECRET` | `401`, nothing processed |
| a correctly signed event other than `pull_request` or `issue_comment` (for example `ping`) | `200`, ignored |
| an `issue_comment` on an issue that is not a pull request | `200`, ignored |
| a `pull_request` or `issue_comment` payload that cannot be parsed | `400` |
| a handled event turnip declines to act on (a refusal, such as a fork or a closed pull request) | `200` |
| a handled event whose handling fails | `500`, so GitHub may redeliver |
| a handled event handled successfully | `200` |

Which `pull_request` actions and comments act is owned by the `pull-request-lifecycle` and `commands` capabilities. The Server SHALL answer a delivery once it has decided what to run, and SHALL run Operations in the background rather than holding the delivery open until they finish.

#### Scenario: Wrong secret
- **WHEN** a delivery arrives signed with a secret other than `TURNIP_GITHUB_WEBHOOK_SECRET`
- **THEN** the Server answers `401` and acts on nothing

#### Scenario: Root path
- **WHEN** GitHub posts a delivery to `https://turnip.example.com/`
- **THEN** the Server answers `404`

#### Scenario: GET on the webhook path
- **WHEN** a client sends `GET /github/webhook`
- **THEN** the Server answers `405`

#### Scenario: Ping event
- **WHEN** GitHub sends a correctly signed `ping`
- **THEN** the Server answers `200` and does nothing else

#### Scenario: Fork pull request refused
- **WHEN** a correctly signed `pull_request` delivery is for a pull request from a fork
- **THEN** turnip refuses it (see the `pull-request-lifecycle` capability) and the Server answers `200`

### Requirement: Shutdown
On `SIGTERM` or `SIGINT`, the Server SHALL stop accepting HTTP requests and give in-flight requests up to 10 seconds to finish, SHALL stop accepting new Runner RPCs and let open ones end, and SHALL stop its background sweep. It SHALL NOT wait for Operations it dispatched in the background; what happens to them is owned by the `operation-lifecycle` capability. When any one of the HTTP listener or the gRPC listener stops on its own, the Server SHALL stop the others as well. A signal-initiated shutdown that completes within those limits SHALL log `server stopped` and exit with status 0; a listener failure, or in-flight HTTP requests outlasting the 10 seconds, SHALL exit with status 1.

#### Scenario: Rolling restart
- **WHEN** Kubernetes sends `SIGTERM` to a Server Pod
- **THEN** the Server stops listening, lets in-flight requests finish for up to 10 seconds, logs `server stopped` and exits 0

#### Scenario: HTTP listener fails
- **WHEN** the HTTP listener stops with an error while the Server runs
- **THEN** the gRPC listener and the sweep stop too and the Server exits with status 1

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
