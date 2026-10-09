## MODIFIED Requirements

### Requirement: Health endpoints
The Server SHALL answer `GET /healthz` with 200 whenever the process is serving HTTP, without checking any dependency, and `GET /readyz` with 200 only while a Redis `PING` succeeds and 503 otherwise. `GET /readyz` SHALL return a JSON body reporting the Redis transport in use under a `redis` field: the address (host and port, with no credential), whether TLS is on, how it authenticated (`none`, `username`, `password`, or `username+password`), and whether a custom CA bundle was used. The body SHALL never contain a credential. Both endpoints SHALL be served on `TURNIP_HTTP_ADDR`. The Kubernetes probes that use them are the `deployment` capability's.

#### Scenario: Redis unreachable
- **WHEN** the Server is running but cannot reach Redis
- **THEN** `/healthz` answers 200 and `/readyz` answers 503

#### Scenario: Readiness reports the transport
- **WHEN** the Server connected to Redis over TLS with a password, and `/readyz` is requested
- **THEN** its body reports `tls` true and `auth` `password`, with the address and no credential

## ADDED Requirements

### Requirement: Redis transport logged at startup
After its first successful Redis `PING`, the Server SHALL log, once, at `info`, the transport it connected with: the address (host and port, no credential), whether TLS is on, how it authenticated (`none`, `username`, `password`, or `username+password`), and whether a custom CA bundle was used. The line SHALL never contain a credential. This is what makes a mistyped scheme (`redis://` where `rediss://` was meant, which connects in plaintext) visible rather than silent.

#### Scenario: TLS connection logged
- **WHEN** the Server connects to Redis over `rediss://` with a password
- **THEN** it logs `redis connected` with `tls=true` and `auth=password`, and no credential

#### Scenario: A plaintext typo is visible
- **WHEN** an operator meant `rediss://` but set `redis://`
- **THEN** the startup line reports `tls=false`, so the plaintext connection can be seen
