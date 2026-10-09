# observability Specification

## Purpose
The logs, Prometheus metrics, health endpoints and Grafana dashboard operators rely on to run turnip, follow a pull request through the Server, and diagnose an incident without reading code.

## Requirements

### Requirement: Structured Server logs
The Server SHALL write its logs to standard error as JSON records, one per line, each with a timestamp, a level, a message and the context at hand (such as `owner`, `repo`, `pr_number`, `project`, `operation`, `operation_id`, `lock_key`, `delivery_id`, `actor`) as separate fields, and an error as an `error` field rather than inside the message. `TURNIP_LOG_LEVEL` SHALL set the minimum level: `debug`, `info`, `warn` or `error`, case-insensitive; unset or unrecognized SHALL mean `info` and SHALL NOT stop the Server from starting. A settings error found at startup SHALL be logged at `error` as `loading config`, at `info` since the level is not yet known, and the Server SHALL exit non-zero.

#### Scenario: Default level
- **WHEN** the Server starts with `TURNIP_LOG_LEVEL` unset
- **THEN** it logs JSON records at `info` and above, and no `debug` records

#### Scenario: Typo in the level
- **WHEN** `TURNIP_LOG_LEVEL` is `verbose`
- **THEN** the Server starts and logs at `info`

### Requirement: Key Server log events
At the default level the Server SHALL log enough to follow a pull request through turnip. It SHALL log at `info`: `server starting` with `http_addr`, `grpc_addr`, `namespace` and `log_level`; `server stopped` on a clean shutdown; `handling webhook delivery` for every dispatched delivery, with `event`, `action`, `owner`, `repo`, `delivery_id` and `pr_number`; `runner job created` with `operation_id` and `job`; `operation result received` with the Lock event and the transition (`lock_event`, `lock_from`, `lock_to`, `lock_released`); `operation finished` with `outcome` and `duration`; Lock releases (`lock released via unlock command`, `lock released on PR close`); and refusals that are an ordinary decision (a non-collaborator's trigger, an unlock without write permission, a closed pull request, unmet mutation requirements). It SHALL log at `warn`: `rejecting webhook delivery: signature verification failed` and `rejecting webhook delivery: unparseable payload`; `refusing operation on a pull request from another repository` with `head_owner`, `head_repo` and `actor`; `operation rejected` with the `reason` shown on the pull request; `operation timed out before its runner reported`; and refused Runner RPCs. Every failure the Server answers or works around (a webhook handler error answered 500, a GitHub, Redis or Kubernetes call that failed) SHALL be logged at `error`. Skipped deliveries SHALL be logged only at `debug`.

#### Scenario: Following a plan
- **WHEN** a push triggers a plan for `web` that succeeds
- **THEN** the Server's log at `info` shows `handling webhook delivery`, `runner job created`, `operation result received` and `operation finished` for it

#### Scenario: Handler failure
- **WHEN** handling a delivery fails and the Server answers 500
- **THEN** it logs `handling webhook delivery failed` at `error` with the delivery's fields and the error

### Requirement: Runner logs
The Runner SHALL mirror the tool's standard output and standard error to its own, line by line as they arrive, unredacted and with absolute paths intact, so `kubectl logs` on the Runner Pod shows exactly what the tool printed. Its own messages SHALL be plain-text lines prefixed `runner: ` on standard error, including `runner: operation result: ...` once the Operation finishes, whether or not the Server acknowledged it. A Runner settings error SHALL be logged as a JSON record `loading config` and the Runner SHALL exit non-zero. The Runner reads `TURNIP_LOG_LEVEL` the same way as the Server, but turnip does not set it on Runner Pods and `runner.env` cannot set it (names beginning `TURNIP_` are reserved; see the `configuration` capability), so it SHALL have no effect in practice.

#### Scenario: Reading a Runner's output
- **WHEN** an operator runs `kubectl logs` on a finished Runner Pod's tool container
- **THEN** it shows the tool's own output followed by a `runner: operation result:` line

### Requirement: Prometheus metrics
The Server SHALL serve Prometheus text-format metrics at `GET /metrics` on `TURNIP_HTTP_ADDR`; the Runner SHALL expose none. The Server SHALL export:

| Metric | Type | Labels | Counts |
|---|---|---|---|
| `turnip_webhook_events_total` | counter | `event_type`, `outcome` | every delivery: `rejected` for a bad signature (with `event_type` `unknown`), an unparseable payload, or a delivery turnip refused; `skipped` for an event type other than `pull_request` and `issue_comment`, or a comment on a plain issue; `dispatched` for every other delivery, including one whose handling failed |
| `turnip_operations_dispatched_total` | counter | `tool`, `operation`, `outcome` | each Operation turnip tried to run: `success` or `failure` as its result reported, `rejected` when it was refused before or while running |
| `turnip_lock_attempts_total` | counter | `outcome` | each plan's Lock acquisition: `acquired`, or `rejected` when another pull request held it or Redis failed |
| `turnip_operation_duration_seconds` | histogram | `tool`, `operation` | the time from an Operation starting on the Server to its outcome, refusals included |
| `turnip_runner_job_start_latency_seconds` | histogram | none | the time from an Operation being recorded, just before its Runner Job is created, to the first output line the Server receives from its Runner |

Histograms SHALL use the Prometheus client's default buckets. Projects refused during selection (an operation the tool does not recognize, a mutating command without write permission) SHALL NOT be counted in `turnip_operations_dispatched_total`.

#### Scenario: Bad signature
- **WHEN** a delivery arrives with an invalid signature
- **THEN** `turnip_webhook_events_total{event_type="unknown",outcome="rejected"}` increases by one

#### Scenario: Refused fork delivery
- **WHEN** a `pull_request` delivery from a fork is refused
- **THEN** `turnip_webhook_events_total{event_type="pull_request",outcome="rejected"}` increases by one and the `dispatched` series does not

#### Scenario: Lock contention
- **WHEN** a plan is refused because another pull request holds the Project's Lock
- **THEN** `turnip_lock_attempts_total{outcome="rejected"}` and `turnip_operations_dispatched_total{outcome="rejected"}` each increase by one

### Requirement: Health endpoints
The Server SHALL answer `GET /healthz` with 200 whenever the process is serving HTTP, without checking any dependency, and `GET /readyz` with 200 only while a Redis `PING` succeeds and 503 otherwise. `GET /readyz` SHALL return a JSON body reporting the Redis transport in use under a `redis` field: the address (host and port, with no credential), whether TLS is on, how it authenticated (`none`, `username`, `password`, or `username+password`), and whether a custom CA bundle was used. The body SHALL never contain a credential. Both endpoints SHALL be served on `TURNIP_HTTP_ADDR`. The Kubernetes probes that use them are the `deployment` capability's.

#### Scenario: Redis unreachable
- **WHEN** the Server is running but cannot reach Redis
- **THEN** `/healthz` answers 200 and `/readyz` answers 503

#### Scenario: Readiness reports the transport
- **WHEN** the Server connected to Redis over TLS with a password, and `/readyz` is requested
- **THEN** its body reports `tls` true and `auth` `password`, with the address and no credential

### Requirement: Grafana dashboard
turnip SHALL ship a Grafana dashboard titled `turnip` (uid `turnip-server`) with a Prometheus data source variable and panels for: webhook throughput and error rate by `event_type` and `outcome`; Operation dispatch rate by `tool`, `operation` and `outcome`; Lock acquisition rejection rate; Runner Job start latency (p99); and Operation duration (p50 and p99) by `tool` and `operation`. It SHALL be installable as a ConfigMap generated from the name `turnip-grafana-dashboard` (Kustomize appends a content hash) and labeled `grafana_dashboard: "1"` through the opt-in Kustomize component `deploy/components/grafana-dashboard`, which is not part of the default install.

#### Scenario: Installing the dashboard
- **WHEN** an operator applies `deploy/components/grafana-dashboard` to a cluster whose Grafana discovers dashboards by the `grafana_dashboard` label
- **THEN** the `turnip` dashboard appears with its five panels

### Requirement: Redis transport logged at startup
After its first successful Redis `PING`, the Server SHALL log, once, at `info`, the transport it connected with: the address (host and port, no credential), whether TLS is on, how it authenticated (`none`, `username`, `password`, or `username+password`), and whether a custom CA bundle was used. The line SHALL never contain a credential. This is what makes a mistyped scheme (`redis://` where `rediss://` was meant, which connects in plaintext) visible rather than silent.

#### Scenario: TLS connection logged
- **WHEN** the Server connects to Redis over `rediss://` with a password
- **THEN** it logs `redis connected` with `tls=true` and `auth=password`, and no credential

#### Scenario: A plaintext typo is visible
- **WHEN** an operator meant `rediss://` but set `redis://`
- **THEN** the startup line reports `tls=false`, so the plaintext connection can be seen
