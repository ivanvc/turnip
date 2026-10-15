# Proposal

## Why

Runners report log lines and results to the Server over gRPC in
plaintext. Runner authentication established who is calling (a
projected, audience-scoped ServiceAccount token bound to the Operation's
Pod) without encrypting anything. What that leaves exposed to anything
able to capture pod-to-pod traffic:

- plan and diff output, and every log line
- the Runner's token in the stream's metadata, short-lived and good for
  one Operation
- the Server's identity, which a Runner does not verify: whoever answers
  at the Server's Service name is taken for the Server

Capturing traffic needs node access or `CAP_NET_RAW`, a higher bar than
the pod-read exposure runner authentication closed, which is why this
was split from it.

This is roadmap Slice 38, carried over from its Kiro draft. The drafted
requirements, the survey of how other projects provision such a
certificate, and the account of an implementation that was written and
removed travel with this change as `kiro-requirements.md` (Kiro's
format, unchanged).

## What Changes

- The Runner-to-Server gRPC channel is encrypted, for the operation
  stream and the credential fetch alike
- The Runner verifies the Server's identity before presenting its token
  (the Server presents a certificate; no client certificate)
- No setting disables encryption or skips verification
- The Server serves the certificate currently in its configured source,
  not the one present when it started, and each Runner Job is given the
  certificate authority current when the Job is created, so a renewal
  needs no restart
- The Server reads its certificate from one source, whatever filled it

## Open question, to settle before the spec deltas

**Where the certificate comes from.** Four options were weighed and none
chosen:

| Option | Who issues | Who renews | Cost in turnip |
|---|---|---|---|
| A | turnip, on first start | nobody; long expiry | certificate code, no lifecycle |
| B | turnip | turnip (leaf only) | A plus rotation |
| C | cert-manager | cert-manager | reading a mounted file |
| D | the operator, once, by hand | nobody | reading a mounted file; one documented command |

C is the least code, but turnip cannot assume cert-manager is installed.
The answer decides whether turnip only reads a certificate or also
writes one, so it comes before the specs.

## Not in this change

- Mutual TLS, and SPIFFE or SPIRE: a client certificate is itself a
  credential with the delivery problem the Runner token already solved
- Encrypting anything else: the Server's HTTP listener sits behind the
  operator's ingress, and Redis has its own settings (`redis-tls-auth`,
  archived)
- Rotation as a lifecycle turnip owns, unless the option chosen makes
  turnip the issuer

## Capabilities

### New Capabilities

### Modified Capabilities
- `runner-security`: the channel is encrypted and the Runner verifies the Server
- `runner-job`: what each Job is given to verify the Server
- `server`: the certificate's source
- `deployment`: the manifests that supply the certificate

## Impact

`internal/rpc` (Server credentials), `internal/runner` (the reporter's
and the credential helper's connections), `internal/jobs`, `cmd/server`,
`deploy/`, `docs/operating/`, `SECURITY.md` (the plaintext channel is
described there today). The in-process test harnesses that dial without
credentials need a certificate, since nothing turns encryption off.

## Dependencies

- **Overlaps `split-server-runner-namespaces`**, which also changes the `deployment` capability;
  best done after it: each Runner Job is given the certificate authority, which is simpler to design once the Runner's namespace is settled.
- **Overlaps `org-images`**, which rewrites every file; see its
  Dependencies.
