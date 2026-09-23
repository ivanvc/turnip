# Requirements: Encrypting the Runner-to-Server Channel

## Introduction

Runners report log lines and results to the Server over gRPC, and that
channel is plaintext. Slice 25 (`runner-authentication`) established
*who* is calling — a projected, audience-scoped ServiceAccount token
validated by `TokenReview` and bound to the Operation's Pod uid — without
encrypting anything.

This slice encrypts it.

**Why it is a separate slice.** An earlier draft made encryption
Requirement 4 of Slice 25, on the argument that a bearer token crossing a
plaintext channel makes the authentication decorative. That argument
proved too much. It equates "an attacker with network position could
capture the credential" with "anything holding pod-read can read the
credential", and those are different exposures by a wide margin. Slice 25
holds entirely over an unencrypted transport — its interceptor test suite
runs on `insecure.NewCredentials()` — so tying it to an unresolved
certificate question delayed the part that was settled.

## What is exposed today

| Surface | Who can reach it |
|---|---|
| plan and diff output, log lines | anything able to capture pod-to-pod traffic |
| the Runner_Token in stream metadata | the same; audience-scoped to turnip, expires in minutes, authorizes writing to exactly one Operation |
| the Server's identity | unverified — a caller winning routing to the Server's Service name is indistinguishable from the Server |

Capturing traffic requires node access or `CAP_NET_RAW`, not a namespaced
RBAC grant. That is the bar this slice raises, and it is already higher
than the pod-read bar Slice 25 closed.

## Requirements

### Requirement 1: The channel is encrypted in transit

#### Acceptance Criteria

1. THE Runner-to-Server gRPC channel SHALL be encrypted
2. THE Runner SHALL verify the Server's identity before presenting its
   credential
3. THERE SHALL be no flag that disables encryption and none that skips
   verification

*Rationale for 1.2: presenting a bearer token to an unverified peer hands
it to whoever answered. One-way TLS — the Server presents a certificate,
the Runner verifies it — is sufficient and needs no client PKI.*

*Rationale for 1.3: a switch whose only safe value is off is the argument
that kept apply thresholds out of configuration. The cost is that the
in-cluster test harness needs a certificate rather than a way around one.*

### Requirement 2: The Server does not serve a certificate it read once

#### Acceptance Criteria

1. THE Server SHALL serve the certificate currently in its configured
   source, not the one present when it started
2. THE certificate authority a Runner is given SHALL be the one current
   when its Job was created

*Rationale: this is the defect that made the first implementation wrong
regardless of provenance. It read the certificate once into a static
`tls.Config.Certificates` and stamped a fixed CA into every Job at
`Orchestrator` construction. At a ten-year expiry that is invisible; at
any renewal interval it is a recurring total outage that a restart
appears to fix. Whoever renews, the Server has to notice.*

### Requirement 3: Whatever provisions the certificate, turnip reads one thing

#### Acceptance Criteria

1. THE Server SHALL obtain its certificate from a single source
   regardless of what filled it
2. AN operator supplying their own certificate SHALL NOT require turnip
   to behave differently from the default path

*Rationale: turnip cannot distinguish cert-manager from `openssl` from a
Vault injector, and should not try. Supporting many fillers costs no
branches; the only branch that costs anything is whether turnip writes a
certificate as well as reading one.*

## The open question

**Where does the certificate come from?** Not settled, deliberately. The
options, with what was learned while weighing them:

| | who issues | who renews | turnip's code |
|---|---|---|---|
| A | turnip, on first boot | nobody; long expiry | ~490 lines, no lifecycle |
| B | turnip | turnip, leaf only, CA long-lived | A plus rotation |
| C | cert-manager | cert-manager | ~20 lines: read a mounted file |
| D | the operator, once, by hand | nobody | ~20 lines; one documented `openssl` + `kubectl create secret tls` |

**cert-manager is not installed on the cluster turnip is piloted against**,
which is what keeps C from being the obvious answer.

## Prior art, so it is not re-derived

- **cert-manager's own webhook** cannot bootstrap itself with
  cert-manager, so it generates a self-signed CA into
  `secret/cert-manager-webhook-ca` at startup and signs short-lived
  leaves from it. Defaults: **365-day CA, 7-day leaf**, with a
  `WatchRotation` channel. Published as
  `github.com/cert-manager/webhook-lib/authority` and usable standalone —
  which is the strongest argument against turnip hand-writing PKI.
- **OLM** provisions webhook certificates for operators itself: a
  self-signed CA mounted into the deployment, **two-year** expiry,
  regenerate-and-redeploy at expiry. It answers staleness by restarting
  the workload, which turnip cannot do to itself.
- **ingress-nginx's `kube-webhook-certgen`** generates into a Secret from
  a Helm pre-install hook. Works there because Helm hooks recreate the
  Job; a Job inside a `kubectl apply -f` install file fails re-apply on
  Job-spec immutability.
- **kubebuilder/operator-sdk** do *not* bundle cert-manager — it is a
  documented prerequisite and the scaffolding ships commented out.
- **Argo CD's repo-server** generates a non-persistent self-signed
  certificate every startup and its clients "use a non-validating
  connection". Simplest of all, and unavailable to turnip only because
  more than one Server replica would each serve a different certificate.
- **etcd-operator** exposes `auto` and `cert-manager` behind one
  `Provider` interface, defaulting to `auto`, with BYO as a
  `caBundleSecret` rather than a third provider. Its `auto` provider does
  not hand-roll PKI either — it calls `transport.SelfCert` from etcd's own
  libraries. Validity: 365 days auto, 90 days cert-manager.

Nobody surveyed picked a ten-year expiry, and nobody wrote the
certificate code by hand.

## Work already done, and removed

Option A was designed and implemented in full, then removed from the tree
when this slice was split out. It is recoverable from the history of the
`runner-authentication` work rather than carried here as a draft: an
`internal/servertls` package generating a CA and leaf and creating a
`kubernetes.io/tls` Secret, with concurrent replicas converging through
`Create`'s `AlreadyExists` — **no leader election, no Lease, no lock** —
plus SAN expansion from `TURNIP_RUNNER_SERVER_ADDR`, CA distribution into
each Job's environment, a `secrets` RBAC grant, and a cert-manager
component.

Two things about it are worth keeping whichever option wins: the
optimistic-concurrency convergence, which preserves turnip's
coordination-free property, and the fact that it read the certificate
once, which Requirement 2 exists to prevent.

## Out of Scope

- **Mutual TLS and SPIFFE/SPIRE.** A client certificate delivered by
  environment value or Secret is itself a credential with the delivery
  problem Slice 24 exists to solve. SPIFFE resolves it properly at the
  cost of an infrastructure dependency turnip does not otherwise require.
- **Encrypting anything else.** The Server's HTTP listener is fronted by
  an operator's own ingress, which already terminates TLS. Redis is
  covered by its own note in `docs/deployment.md`.
- **Certificate rotation as a turnip-owned lifecycle**, unless the option
  chosen makes turnip the issuer. Requirement 2 is about *noticing* a
  renewal, not performing one.
