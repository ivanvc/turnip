# Proposal

## Why

turnip's Server connects to Redis/Valkey with nothing but an address: no
AUTH, no TLS. The operator documentation says so and tells an operator
that a managed instance such as ElastiCache works only if its AUTH and
in-transit encryption are disabled: the deployment guide telling a
reader to turn off encryption to run this project.

The repository owner is moving turnip's Redis into Amazon ElastiCache
(2026-09-30), after an in-cluster Redis without persistence lost every
recorded plan on a restart. An ElastiCache instance with in-transit
encryption and an AUTH token is exactly what turnip cannot connect to
today. This is roadmap Slice 27; its roadmap entry and the requirements
drafted before the move to OpenSpec are the source of the decisions here.

The blast radius is the Server alone: it holds the only Redis client, and
no Redis credential reaches a Runner.

## What Changes

- `TURNIP_REDIS_ADDR` accepts a URL: `rediss://` connects over TLS with
  the certificate verified, `redis://` without TLS, and a bare `host:port`
  as today. No separate TLS switch
- A username may be in the URL; a password never is (startup refusal,
  naming the password settings)
- New settings: `TURNIP_REDIS_PASSWORD_PATH` (a file, recommended) or
  `TURNIP_REDIS_PASSWORD` (a value), not both; `TURNIP_REDIS_CA_PATH` for
  a private CA, only with `rediss`
- The URL may carry everything go-redis reads (database number, tuning
  parameters) except a password and `skip_verify`; a scheme other than
  `redis` or `rediss` is refused
- The Server logs, and `/readyz` reports, the transport it connected
  with, never a credential
- **BREAKING** for the documentation only: the advice to disable AUTH
  and TLS is removed; the docs state that turnip needs a single,
  non-clustered Redis endpoint (ElastiCache cluster mode disabled) and
  the version floor (Redis 5.0, 6.0 for a username)

## Capabilities

### New Capabilities

### Modified Capabilities
- `server`: the Redis address forms, the three new settings and their startup refusals
- `observability`: the transport in the startup log and the `/readyz` body
- `deployment`: the Redis prerequisite (TLS and AUTH supported, single endpoint, version floor), and connecting to ElastiCache

## Impact

`internal/orchestrator` (settings, building the Redis options),
`cmd/server` (client construction, startup log), `internal/health`
(`/readyz` body), operator docs. Out of scope: IAM authentication to
ElastiCache (short-lived tokens; its own change if wanted), mutual TLS,
clustered Redis (supporting it later is a change of client, since every
turnip operation touches a single key), and migrating data from an
existing Redis (a new instance starts empty; pull requests plan again).
