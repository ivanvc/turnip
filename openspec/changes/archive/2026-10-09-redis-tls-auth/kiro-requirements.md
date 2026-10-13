# Requirements: Authenticated and Encrypted Redis (Slice 27)

## Introduction

turnip's Server connects to Redis/Valkey with nothing but an address
(`redis.NewClient(&redis.Options{Addr: cfg.RedisAddr})`,
`cmd/server/main.go`): no AUTH, no TLS. The operator documentation
(`docs/operating/deployment.md`, prerequisite 2) says so, and tells an
operator that a managed instance such as ElastiCache "works only if you
disable its AUTH/in-transit-encryption requirement". That is the
deployment guide telling a reader to turn off encryption in transit to
run this project.

**Why now** (2026-09-30): the repository owner is moving turnip's Redis
out of the cluster and into Amazon ElastiCache, after an in-cluster
Redis without persistence lost every recorded plan on a restart. An
ElastiCache instance with in-transit encryption and AUTH is exactly what
turnip cannot connect to today.

**The blast radius is the Server alone.** It holds the only Redis
client; the Runner never touches Redis, and no Redis credential reaches
a Job, a Runner Pod, or anything that runs repository-supplied code.

The roadmap entry for this slice settled four decisions, reconciled here
against the code as it stands after Slices 43 to 48. go-redis is at
v9.22.0, whose `ParseURL` the decisions build on.

## Glossary

- **Redis_URL**: `TURNIP_REDIS_ADDR` written as a URL, `redis://…` or
  `rediss://…`, as opposed to a bare `host:port`.
- **Transport**: whether the connection is TLS, and how it
  authenticates (none, password, or username and password).

## Requirements

### Requirement 1: TLS from the address

#### Acceptance Criteria

1. WHERE `TURNIP_REDIS_ADDR` is a Redis_URL with the scheme `rediss`,
   THE Server SHALL connect over TLS, verifying the server's certificate
2. WHERE it is a Redis_URL with the scheme `redis`, THE Server SHALL
   connect without TLS
3. WHERE it is a bare `host:port`, THE Server SHALL connect exactly as it
   does today
4. THE Server SHALL NOT accept a separate setting that turns TLS on or
   off

*Rationale for 1.4: a second switch would be a second source of truth
for a fact the address already states, and the two could disagree.*

### Requirement 2: The transport is visible

#### Acceptance Criteria

1. AT startup, THE Server SHALL log the Transport it connected with: the
   address (without any credential), whether TLS is on, and whether it
   authenticated with a password, a username and password, or neither
2. THE readiness endpoint SHALL report the same

*Rationale: inferring TLS from one letter of the scheme means a typo,
`redis://` for `rediss://`, silently yields plaintext. Stating what was
actually negotiated turns that typo into something visible.*

### Requirement 3: A username in the address, never a password

#### Acceptance Criteria

1. A Redis_URL MAY carry a username (`rediss://turnip@host:6379`), which
   THE Server SHALL use for AUTH
2. WHERE a Redis_URL carries a password, THE Server SHALL refuse to
   start, and the error SHALL name `TURNIP_REDIS_PASSWORD_PATH` and
   `TURNIP_REDIS_PASSWORD` as where the password goes
3. NO credential SHALL appear in any log line or readiness response

*Rationale for 3.2: `TURNIP_REDIS_ADDR` is set through the Server's
ConfigMap in turnip's own manifests, so a password written there lands
in a ConfigMap, not a Secret: readable under a different RBAC verb, and
echoed by the startup line Requirement 2 adds.*

### Requirement 4: The password from a file or a value

#### Acceptance Criteria

1. THE Server SHALL read the password from `TURNIP_REDIS_PASSWORD_PATH`,
   a file, or from `TURNIP_REDIS_PASSWORD`, a value
2. WHERE both are set, THE Server SHALL refuse to start
3. THE documentation SHALL recommend the file, mounted from a Secret
4. NEITHER setting SHALL be required: an unauthenticated Redis stays
   supported

*Rationale for 4.3: a mounted Secret file is not readable by anything
that can only read Pods, where an environment variable is. This mirrors
`TURNIP_GITHUB_PRIVATE_KEY` and `TURNIP_GITHUB_PRIVATE_KEY_PATH`.*

### Requirement 5: Certificates are always verified

#### Acceptance Criteria

1. WHERE a Redis_URL carries `skip_verify`, THE Server SHALL refuse to
   start, rather than ignore it or honor it
2. `TURNIP_REDIS_CA_PATH` SHALL name a CA bundle to verify the server's
   certificate against, for a Redis behind a private certificate
   authority; unset, the system's trusted roots are used
3. WHERE `TURNIP_REDIS_CA_PATH` is set and the address is not a `rediss`
   Redis_URL, THE Server SHALL refuse to start

*Rationale for 5.1: go-redis's `ParseURL` honors `?skip_verify=true` and
turns certificate verification off. Adopting `ParseURL` as-is would ship
that switch without anything in turnip saying so; an escape hatch that
disables verification tends to become permanent, and an undocumented one
becomes permanent and invisible. A private CA is served by 5.2 instead.*

*Rationale for 5.3: a CA bundle with no TLS to use it on is a
misconfiguration that would otherwise do nothing, silently.*

### Requirement 6: What else the URL may carry

#### Acceptance Criteria

1. A Redis_URL MAY carry everything go-redis's `ParseURL` reads (a
   database number, and its connection-tuning query parameters such as
   `dial_timeout` and `pool_size`), except a password (Requirement 3)
   and `skip_verify` (Requirement 5)
2. A query parameter `ParseURL` does not recognize SHALL keep failing at
   startup, as `ParseURL` already makes it
3. THE documentation SHALL say the Redis_URL accepts go-redis's
   parameters, link to their list, and name the two it refuses and why

*Rationale (decided 2026-09-30): the rule is that the address may say
anything except a secret or a switch that disables verification. The
tuning parameters are a gain, since turnip exposes no connection tuning
otherwise, and restricting them would mean keeping turnip's own list in
step with go-redis's.*

### Requirement 7: The settings Reference

#### Acceptance Criteria

1. EVERY new setting SHALL be read in `ConfigFromEnv` and have its row
   in `docs/operating/server-settings.md`, which the Reference test
   (Slice 48) enforces

### Requirement 8: Documentation

#### Acceptance Criteria

1. `docs/operating/deployment.md`'s Redis prerequisite SHALL be
   rewritten: it SHALL describe connecting to a Redis that requires TLS,
   AUTH or both, and SHALL NOT tell an operator to disable either
2. THE documentation SHALL give a worked example for Amazon ElastiCache
   with in-transit encryption and an AUTH token
3. THE documentation SHALL state the Redis version turnip needs: 5.0 or
   later, and 6.0 or later for a username (ACL users)
4. THE documentation SHALL state that turnip connects to a single Redis
   endpoint, and so needs a non-clustered Redis: on ElastiCache, cluster
   mode disabled (a primary, optionally with replicas), not cluster mode
   enabled. Whether ElastiCache Serverless works SHALL be checked against
   AWS's documentation during implementation and stated either way
5. THE documentation SHALL say what happens when pointed at a clustered
   Redis, so the symptom (`MOVED` errors) leads back to this limitation

*Rationale for 8.4 (decided 2026-09-30): every operation turnip performs
touches a single key, so supporting a cluster later is a change of
client, not of data layout. Until something needs it, one primary is
ample for turnip's load, and a replica covers failover.*

## Out of Scope

- **IAM authentication to ElastiCache** (short-lived tokens generated
  from the Server's cloud identity). A static AUTH token or ACL user is
  what this slice supports; IAM tokens would need refreshing every
  connection and are their own slice if wanted.
- **Mutual TLS** (a client certificate).
- **Clustered Redis** (ElastiCache cluster mode enabled, Serverless):
  Requirement 8.4 documents the limitation. Supporting it is a change of
  client (go-redis's cluster client), possible later because every
  turnip operation touches a single key.
- **Migrating data** from an existing Redis: a new instance starts
  empty, and every pull request plans again, as after any Redis loss.
