# Requirements: Authenticating the Runner to the Server (Slice 25)

## Introduction

`rpc.NewServer` is a bare `grpc.NewServer()` — no credentials, no
interceptors — and the Runner dials with `insecure.NewCredentials()`
(`reporter.go:73`). The Server identifies an Operation solely by the
`operation_id` the client sends, which it looks up in Redis.

So the id is a **bearer capability**, and it is handed to the Pod as a
plaintext environment value (`TURNIP_OPERATION_ID`, `build.go:123`) —
readable by anything with pod-read in the namespace.

This slice establishes *who* is calling. It does not encrypt the channel:
that is `runner-server-tls`, split out deliberately — see Out of Scope.

**Any Pod that can reach the Server and knows an id can stream log lines
and a final result for that Operation.** That means a forged success
carrying attacker-chosen output posted to the pull request, and a Lock
released as though an apply had completed. It is the one gap that lets a
third party write to a pull request's record of what happened.

**The pieces for a precise answer already exist:**

| Step | Mechanism |
|---|---|
| Operation → Job | `turnip.ivan.vc/operation-id` label (`jobs/labels.go`) |
| Job → Pod | `batch.kubernetes.io/job-name` selector (`jobs/status.go:50`) |
| exactly one Pod | `BackoffLimit: 0` on every Runner Job (`build.go:272`) |
| permission to look | `pods` get/list, already in the Server's Role |

**And Kubernetes surfaces the caller's Pod.** A projected ServiceAccount
token bound to a Pod carries that Pod's identity as claims, and
`TokenReview` returns them in `UserInfo.Extra` as
`authentication.kubernetes.io/pod-name` and `.../pod-uid`. First available
in Kubernetes v1.29; **stable and enabled by default since v1.32**.

## Glossary

Terms additional to the global spec's glossary:

- **Runner_Token**: the audience-scoped, Pod-bound ServiceAccount token
  the Runner presents to the Server.
- **Operation_Pod**: the single Pod belonging to the Job turnip created
  for a given Operation.
- **Caller_Identity**: what the Server concludes about who is on the other
  end of a gRPC stream.

## Requirements

### Requirement 1: The Server establishes who is calling, not merely what they claim

**User Story:** As a maintainer, I want an Operation's results to come
only from the Pod that ran it, so that a pull request's record of what
happened cannot be written by anything else.

#### Acceptance Criteria

1. THE Server SHALL derive Caller_Identity from a credential the caller
   presents, NOT from an identifier the caller names
2. THE Server SHALL reject a stream whose credential is absent, expired,
   or fails validation
3. THE `operation_id` SHALL cease to be sufficient on its own to write to
   an Operation

*Rationale for 1.3: the id remains the Operation's reference — it is how
the Server knows which record to write. What changes is that naming it no
longer authorises writing to it.*

### Requirement 2: The identity checked is the Pod, not the ServiceAccount

**User Story:** As an operator who chose a ServiceAccount for my Runners,
I do not want that choice to decide who the Server trusts.

#### Acceptance Criteria

1. THE Server SHALL verify that the caller is the Operation_Pod for the
   Operation the stream names
2. THE verification SHALL NOT depend on the ServiceAccount the Pod runs as
3. WHERE the Pod's identity can be compared by uid, the Server SHALL
   compare the uid rather than the name

*Rationale for 2.2: turnip lets an operator — and where
`TURNIP_ALLOWED_OVERRIDES` permits, a repository — choose
`runner.serviceAccount`. A rule like "the ServiceAccount must be
`turnip-runner`" breaks the moment anyone uses that feature, and where a
repository chooses the ServiceAccount it would let the repository
influence which identities the Server accepts. The Pod-bound rule leaves
the override orthogonal.*

*Rationale for 2.3: a Pod name can be reused after deletion; a uid cannot.
The Server already lists the Operation's Pods to diagnose timeouts, so it
can learn the uid without new permissions.*

### Requirement 3: The credential is worthless anywhere but here

#### Acceptance Criteria

1. THE Runner_Token SHALL be scoped to an audience that identifies
   turnip's Server
2. THE Server SHALL verify that audience, and SHALL reject a token
   presented for any other
3. THE Runner SHALL NOT present the Pod's default API-server token

*Rationale: every Pod automounts a token at
`/var/run/secrets/kubernetes.io/serviceaccount/token`, whose audience is
the API server. Sending that one would let the Server replay it and act as
the Runner's ServiceAccount against the cluster. An audience-scoped token
is useless anywhere but the Server it names — which is the property that
makes presenting it safe at all.*

### Requirement 4: Enforcement is structural, not per-handler

#### Acceptance Criteria

1. THE check SHALL be applied where every RPC passes through it, rather
   than inside individual handlers
2. AN RPC added later SHALL be authenticated without its author having to
   remember

*Rationale: `NewServer` takes no options today, so there is exactly one
place to add an interceptor, and it then fails closed for anything added
afterwards. A check inside `ExecuteOperation` would protect the one RPC
that exists and silently not protect the next one — the same
"a Plugin that forgets" hazard Slice 18 designed around.*

### Requirement 5: The Kubernetes version dependency is explicit

#### Acceptance Criteria

1. THE documentation SHALL state that turnip requires **Kubernetes v1.32
   or later**, and why
2. WHERE the cluster does not surface the caller's Pod identity, the
   Server SHALL refuse rather than fall back to trusting the
   `operation_id`
3. WHERE a weaker binding is implemented as a fallback, its weakness SHALL
   be stated in the documentation rather than left for a reader to infer

*Rationale for 6.1: `pod-name` and `pod-uid` in `UserInfo.Extra` are
stable and enabled by default from Kubernetes v1.32. They first appeared
in v1.29 but are feature-gated between the two, so v1.29–v1.31 works only
where an operator has turned the gate on — a condition turnip cannot
detect at install time and should not depend on. v1.32 is the first
version where the binding is available by default, so it is the stated
minimum rather than v1.29.*

*Rationale for 6.2: a fallback that silently reverts to the current
behaviour would leave an operator believing in a control that is not
there. Refusing is visible; degrading quietly is not.*

*Note on what Kubernetes verifies: the API server checks that the bound
object still exists, by uid, when the token is used. The `node-name` and
`node-uid` extras are documented as **not** verified, so nothing
security-relevant may be derived from them — only the Pod claims carry
weight.*

### Requirement 6: The new cluster permission is deliberate and documented

#### Acceptance Criteria

1. THE permission the Server needs to validate tokens SHALL be the
   narrowest that works
2. THE deployment documentation SHALL state what was added, that it is
   cluster-scoped, and why it is required

*Rationale: `TokenReview` is a cluster-scoped resource, so RBAC for it
cannot live in the namespaced Role turnip uses today — it needs a
ClusterRole and binding, conventionally the built-in
`system:auth-delegator`. That is turnip's first cluster-scoped grant and a
real thing for an operator to accept, so it is stated rather than
discovered during deployment.*

## Out of Scope

- **Fetching the GitHub token over this channel.** Slice 24 consumes what
  this slice builds; it does not belong here.
- **Encrypting the channel.** Split into its own slice
  (`runner-server-tls`) after an earlier draft of this document made it
  Requirement 4 here. The two are genuinely separate axes: authentication
  establishes *who* is calling, and every acceptance criterion above holds
  over a plaintext transport — the interceptor's own test suite runs
  unencrypted. What encryption adds is protection from an attacker with
  network position, which is a far higher bar than the pod-read this slice
  closes, and it carries an unresolved question about where the Server's
  certificate comes from that should not hold up the authentication.

  The cost of deferring, stated plainly: a caller able to capture
  pod-to-pod traffic can read plan output, and can capture a Runner_Token
  within its lifetime. That token is audience-scoped, expires in minutes,
  and authorizes writing to exactly one Operation whose output the captor
  can already see.

- **Mutual TLS and SPIFFE/SPIRE.** mTLS would authenticate as well as
  encrypt, but a client certificate delivered by environment value or
  Secret is itself a credential with the delivery problem Slice 24 exists
  to solve, and issuing per-Job certificates needs something to
  authenticate the request — which lands back on this token. SPIFFE
  resolves it properly at the cost of a large infrastructure dependency
  turnip does not otherwise require.
- **Validating the token locally against the API server's JWKS.** It would
  avoid the cluster-scoped grant of Requirement 7 at the cost of
  implementing signature validation, which has more ways to be subtly
  wrong. Worth weighing in the design; not decided here.
- **Authorising *what* an authenticated Runner may do.** This slice
  establishes that the caller is the Operation's Pod. That Pod may write
  that Operation's results, which is the whole of its business.
- **Removing `TURNIP_OPERATION_ID` from the Pod spec.** It stays as the
  Operation's reference; Requirement 1.3 removes its power rather than its
  presence.
