# Tasks

**The checkpoint that matters is task 3.** Unit tests on the option
builder and on the readiness body can each pass while TLS and AUTH are
never actually exercised. Only a test that stands up a Redis requiring
TLS and a password, and connects the real client to it, shows the
connection is encrypted and authenticated rather than merely configured.
miniredis (already a test dependency) serves TLS (`RunTLS`) and requires
AUTH (`RequireAuth`, `RequireUserAuth`), so this runs everywhere with no
external Redis.

## 1. The connection, built from settings (`internal/orchestrator`)

- [x] 1.1 `ConfigFromEnv` reads `TURNIP_REDIS_PASSWORD`,
  `TURNIP_REDIS_PASSWORD_PATH` and `TURNIP_REDIS_CA_PATH`; both password
  forms set is a startup error
- [x] 1.2 `redisOptions(cfg) (*redis.Options, Transport, error)`: the
  address forms; the startup refusals (password in URL, `skip_verify`,
  a non-`redis`/`rediss` scheme, an unknown query parameter, a CA path
  without `rediss`, an unreadable or empty CA bundle); the TLS config
  (verified, system roots or the CA bundle); the password through a
  per-connection credentials provider that re-reads the file, read once
  at startup so a missing file fails then; a `Transport` record (address
  without credential, TLS, auth kind, custom CA)
- [x] 1.3 Unit tests, table-driven: the bare form unchanged; `redis` and
  `rediss`; a username; a database number and a tuning parameter reaching
  the options; each refusal with its message; a CA bundle loaded, empty
  and unreadable; both password forms; the provider re-reading a rewritten
  file; a trailing newline trimmed; no credential in the `Transport`
  - _Deltas: server (Settings index, Redis connection)_

## 2. The transport is observable (`internal/health`)

- [x] 2.1 `/readyz` keeps its 200/503 and returns a JSON body with the
  Redis `Transport` under a `redis` field; no credential in it
- [x] 2.2 Tests: the body for a TLS+password transport; 503 still has no
  credential
  - _Deltas: observability (Health endpoints)_

## 3. Checkpoint: a real encrypted, authenticated connection

- [x] 3.1 A test using miniredis over TLS (a certificate from a CA
  generated in the test) and requiring AUTH: the client built from
  `redisOptions` with `TURNIP_REDIS_CA_PATH` and the password connects
  and pings; it is refused without the CA, with a wrong password, and
  over `redis://` to the TLS port
- [x] 3.2 `go build ./...`, `make test`, `make lint` pass

## 4. Wiring (`cmd/server`)

- [x] 4.1 The Redis client is built from `redisOptions`; the `Transport`
  is logged at `info` after the first successful ping (no credential),
  and passed to `/readyz`
  - _Deltas: observability (Redis transport logged at startup)_

## 5. Documentation

- [x] 5.1 `docs/operating/server-settings.md`: rows for the three new
  settings (the settings Reference test requires them); the
  `TURNIP_REDIS_ADDR` row describes the three forms, links the Redis URL
  parameter list, and names the two refusals (`skip_verify`, a password
  in the URL)
- [x] 5.2 `docs/operating/deployment.md`: the Redis prerequisite
  rewritten (TLS and AUTH supported, a single non-clustered endpoint,
  the version floor), with NO advice to disable AUTH or TLS; a new
  section connecting to ElastiCache (in-transit encryption, an AUTH token
  in a Secret at `TURNIP_REDIS_PASSWORD_PATH`, the `rediss://` address,
  confirming TLS from the startup line or `/readyz`); whether ElastiCache
  Serverless works, checked against AWS's documentation and stated either
  way
- [x] 5.3 `docs/operating/troubleshooting.md`: each startup refusal, and
  `MOVED` as the sign of a clustered endpoint
  - _Deltas: deployment (Prerequisites)_

## 6. Final checkpoint

- [x] 6.1 `go build ./...`, `gofmt -l ./internal ./cmd` clean,
  `go vet ./...`, `make test`, `make lint` pass
- [x] 6.2 Every acceptance criterion in the deltas maps to a test or doc
  change; `openspec validate redis-tls-auth` passes
- [x] 6.3 An intermittent failure is recorded with full output, not
  rerun away

## Repository owner

- Create the ElastiCache instance (cluster mode disabled, in-transit
  encryption, an AUTH token, persistence); store the token in a Secret
  mounted at `TURNIP_REDIS_PASSWORD_PATH`; set `TURNIP_REDIS_ADDR` to the
  `rediss://` endpoint
- Deploy, and confirm `tls=true auth=password` in the startup line or
  `/readyz`

## Deviations recorded at implementation

- The startup line `redis connected` (task 4.1) has no automated test:
  it is emitted from `main` after the first ping, and the transport it
  logs is the same `Transport` value covered by the `redisOptions` and
  `/readyz` tests. Credential absence is asserted on `/readyz` and on
  the `Transport` record, not on captured log output.
- ElastiCache Serverless is documented as unverified in
  `docs/operating/deployment.md` rather than confirmed either way.

- Verification found one more leak path after the workflow: an address with credentials but no scheme (`user:pass@host:6379`) was taken as a bare `host:port` and would have reached `/readyz` and the startup line. A bare address containing `@` is now refused without echoing it (`TestRedisOptions_SchemelessCredentialsDoNotLeak`).

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1.1", "1.2", "1.3", "2.1", "2.2"] },
    { "id": 1, "tasks": ["3.1", "3.2", "4.1"] },
    { "id": 2, "tasks": ["5.1", "5.2", "5.3"] },
    { "id": 3, "tasks": ["6.1", "6.2", "6.3"] }
  ]
}
```
