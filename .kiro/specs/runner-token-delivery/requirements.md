# Requirements: How the Runner Receives Its GitHub Token (Slice 24)

## Introduction

Four surfaces carry the GitHub App installation token, and they are not
the same problem:

| Surface | Where | Who can read it |
|---|---|---|
| the Job/Pod spec | `build.go:209` — `corev1.EnvVar{Name: "TURNIP_GITHUB_TOKEN", Value: op.GitHubToken}` | anything with pod or job read in the namespace; etcd |
| `.git/config` in the Workspace | `clone.go`'s `git remote add origin <authenticated URL>` | any code running in the tool container |
| reported output | `clone.go`'s error paths, through the Runner's stream, into the pull request comment | anyone who can read the pull request |
| the `OperationStart` message | `reporter.go`'s `startMessage`, field 8 | plaintext pod-to-pod traffic, on the clone-failure path only |

No Kubernetes Secret is involved in the Runner's Job path — no
`secretKeyRef`, no `EnvFrom`, no `corev1.Secret` in `internal/jobs`. The
Server's own credentials do come from a Secret
(`deploy/base/deployment.yaml`), which is exactly the treatment the
Runner's token does not get.

**The second surface is the only one a pull request can reach by running
code.** The tool container mounts the Workspace, and what it runs is
chosen by the repository — so a token left in `.git/config` is readable by
code turnip was *asked to execute*, with no cluster access at all.

**The third is the widest audience and the one already defended.** A
credential in an error message travels the Runner's stream into a pull
request comment, where anyone with read access to the repository sees it —
a lower bar than the other three by a wide margin. It is closed today, and
only, by `clone.go`'s `redact`/`redactArgs` (`redact_test.go`). It is
listed because this slice changes the delivery path those functions guard,
and a surface defended by one helper is a surface that can be reopened by
an edit that forgets it.

The first and fourth require an attacker who already has access to the
cluster or its network.

**The fourth is free.** `OperationStart` declares twelve fields and the
Server acts on **none** of them. Since Slice 25 the Operation id comes
from what the interceptor authenticated, and `GetOperationId()` is read
only to log a disagreement (`internal/rpc/server.go`'s `_Start` case). All
twelve — `github_token` among them — are populated on every stream open,
sent, and discarded. The Runner returns to the Server a credential the
Server minted and never reads.

**And the token is far broader than the job needs.**
`GenerateInstallationToken` returns `c.itr.Token(ctx)`: the full
installation token, covering every repository the App is installed on with
every permission it holds. `ghinstallation.Transport` already exposes
`InstallationTokenOptions` with `Repositories` and `Permissions`; turnip
never sets it. Narrowing reduces what a leak on *any* of the four surfaces
is worth.

**The first surface is deliberately not fixed by delivering the token
better.** A per-Job Kubernetes Secret would close pod-read and was the
obvious move, and it is not built here: Slice 25 authenticates the Runner
with an audience-scoped projected ServiceAccount token, which kubelet
mounts and which puts nothing in the Pod spec. Once that channel exists,
the Runner can *fetch* the credential rather than be handed it, and the
Secret — with its `ownerReference` lifecycle and its RBAC — would have
been written only to be deleted. There is one installation and no
urgency, so this slice waits for the channel rather than patching around
its absence.

**What does not help**: that the token is short-lived. An installation
token expires in about an hour, but the exposure window *is* the Job's
lifetime — precisely the interval in which the credential is live.

## Glossary

Terms additional to the global spec's glossary:

- **Installation_Token**: the GitHub App installation token turnip mints
  for a Runner to clone with.
- **Clone_Step**: the part of the Runner's work that authenticates to
  GitHub — the only part that needs the Installation_Token.
- **Workspace**: the volume the clone writes into and the tool container
  mounts.
- **Authenticated_Channel**: the Runner-to-Server gRPC channel once Slice
  25 has made the Server able to tell which Pod is calling.
- **Submodule_Mode**: the resolved `none` / `top-level` / `recursive`
  setting the Clone_Step runs under, from `TURNIP_CLONE_SUBMODULES` and
  any permitted repository override. It determines which repositories
  beyond the Operation's own the Installation_Token must reach.

## Requirements

### Requirement 1: The token can do only what the Operation needs

**User Story:** As an operator, I want a leaked credential to be worth as
little as possible, whatever leaked it.

#### Acceptance Criteria

1. THE Installation_Token SHALL be minted scoped to the repositories the
   Clone_Step will fetch — the Operation's own repository, and every
   submodule repository the configured Submodule_Mode causes it to fetch
2. THE Installation_Token SHALL be minted with only the permissions the
   Clone_Step requires — `contents: read`, which is what cloning needs and
   the whole of it
3. WHERE scoping fails, the Operation SHALL fail rather than fall back to
   an unscoped token
4. WHERE the set in 1.1 cannot be enumerated completely before the token
   is minted, the token SHALL NOT be narrowed in a way that would fail a
   fetch turnip would otherwise have performed

*Rationale: this is the one requirement here that depends on nothing —
not on Slice 25, not on the delivery mechanism, not on the others. It also
reduces the severity of all four surfaces at once, including the two a
pull request can reach, which nothing else in this slice does except
Requirement 3.*

*Rationale for 1.1 — why it is repositories and not "the repository".*
An earlier draft of this criterion said "the repository the Operation acts
on", singular, which would have broken the common case.
`internal/runner/submodules.go` authenticates submodule fetches with the
**same** Installation_Token, through `url.<authenticated>.insteadOf`
rewrites, and `TURNIP_CLONE_SUBMODULES` defaults to `top-level` — so a
repository whose submodules point at sibling private repositories in the
same installation is the ordinary path, not an edge case. Combined with
1.3, a single-repository token would turn every such clone into a hard
failure, on exactly the repositories that use the feature. The narrowing
has to follow what the clone actually does.

Enumerating that set before minting is possible for the top level: the
Server mints the token before it creates the Job
(`execute.go`'s `GenerateInstallationToken`), and it can already read a
file from the repository at the Operation's commit — that is how
`turnip.yaml` is fetched — so `.gitmodules` at the same ref is available
by the same path.

*Rationale for 1.4 — where enumeration stops.* Under
`Submodule_Mode: recursive`, a submodule's own submodules are declared in
a `.gitmodules` that only exists once that submodule has been cloned, so
the complete set is not knowable before minting. 1.4 states the invariant
rather than the mechanism: whatever the design chooses — widening the
repository scope under `recursive` while still narrowing permissions,
or something better — scoping must never be the reason a clone fails.
Narrowing is a defense; it is not worth breaking the product for.

Submodules on a host other than the Operation's already fail with their
own error (`submodules.go`: "is hosted on %s, which this installation
token cannot authenticate"), and submodules in repositories outside the
installation fail regardless of scoping. Neither is affected by this
requirement.

*Rationale for 1.3: a silent fallback to a broader token would make the
narrowing invisible when it stops working, which is the failure mode that
matters — an operator would believe in a control that is not there. Note
that 1.4 is not such a fallback: it is a deliberate, documented width for
a case where the narrow set is unknowable, not a silent retry after a
failure.*

### Requirement 2: The Runner fetches its credential rather than being handed it

**User Story:** As an operator, I want reading a Pod to be useless to
someone hunting for credentials.

#### Acceptance Criteria

1. THE Installation_Token SHALL NOT appear in the Job or Pod
   specification, in any form
2. THE Runner SHALL obtain it over the Authenticated_Channel, at the point
   the Clone_Step needs it
3. THE credential returned SHALL be determined by the caller's
   authenticated binding, and NEVER by any value carried in the request
4. THE Installation_Token SHALL be minted when the fetch arrives, not when
   the Operation is dispatched
5. THE token SHALL remain unavailable to the container that runs the tool,
   as it is today
6. WHERE the channel cannot authenticate the caller, the Server SHALL NOT
   return a credential

*Rationale for 2.2: fetching is only coherent once the caller can be
identified. Keyed on the operation id alone it would be keyed on another
plaintext value from the same Pod spec — trading "pod-read yields the
token" for "pod-read yields the id, which yields the token". Slice 25 is
what makes this requirement implementable, and this slice must not be
started before it.*

*Rationale for 2.3 — this is the requirement most likely to be
implemented wrong.* A fetch RPC that takes an operation id and honors it
reintroduces exactly what Slice 25 closed: a Runner authenticated for one
Operation could request another's token, and under Requirement 1 that
token may belong to a different repository. The result is cross-repository
credential theft available to anyone who can open a pull request that
triggers an operation. Slice 25 already supplies the binding
(`rpc.OperationIDFromContext`); this criterion forbids trusting anything
else. An implementation that authenticates the caller and then reads the
id from the request passes every other criterion here.*

*Rationale for 2.4 — the largest reduction in this slice, and it is not
the one the slice is named for.* The Server mints before it creates the
Job (`execute.go`'s `GenerateInstallationToken`), so the credential is
live through queueing, scheduling, image pulls and both initContainers —
minutes, in the ordinary case, during which it is doing nothing. Fetching
makes minting-at-use possible, which shrinks the window from the Job's
lifetime to the clone's duration. The Introduction's "what does not help"
note observes that expiry cannot shrink that window; this is the thing
that can.*

*Rationale for 2.5: already true and stated so the change cannot regress
it. The existing comment records that only the clone needs the token; that
scoping is right, and only its delivery leaks.*

### Requirement 3: The token is never persisted into the Workspace, nor reported out of it

#### Acceptance Criteria

1. WHEN the Clone_Step completes, the Workspace SHALL NOT contain the
   Installation_Token in any file
2. THE requirement SHALL hold for the repository's git configuration,
   which `git remote add origin <authenticated URL>` writes today
3. THE requirement SHALL hold on the failure path as well as the success
   path
4. THE token SHALL NOT be placed where another process in the Pod could
   read it instead — the requirement is that it not be persisted, not that
   it be moved
5. THE token SHALL NOT appear in anything the Runner reports — a log line,
   an error message, or an Operation's output — on any path

*Rationale: the two surfaces reachable without cluster access. Slice 15
refuses fork pull requests, which narrows who can arrange the first to
someone who can push a branch; it does not close it, and it does not
touch the second at all — reading a pull request comment needs no branch.*

*Rationale for 3.3: a clone that fails part-way has usually already
written the remote, so a fix that tidies up only on success leaves the
credential behind in exactly the runs someone will be examining.*

*Rationale for 3.4: writing the token to a file outside the Workspace, or
passing it in a command line, satisfies the letter of 3.1 while leaving it
readable by anything sharing the Pod's filesystem or process namespace. A
credential helper that never writes it at all is the shape that satisfies
the intent.*

*Rationale for 3.5: reported output reaches a pull request comment, so
this is the surface with the widest audience of the four — and it is held
closed by exactly two functions, `clone.go`'s `redact` and `redactArgs`.
This slice rewrites the delivery path they guard. The criterion exists so
that "the new path does not need redaction" is a claim someone has to test
rather than assume; a credential helper that never puts the token in a URL
would make it true, but it has to be true rather than hoped for.*

### Requirement 4: The token is not sent to the Server

#### Acceptance Criteria

1. THE `OperationStart` message SHALL NOT carry the Installation_Token
2. THE vacated field number SHALL be reserved, so it cannot be reused and
   silently decoded by an older peer
3. No Server behavior SHALL change, because the Server never read it

*Rationale: the Runner returns to the Server a credential the Server
minted, over a channel that authenticates nothing today. It is removed
because it should never have been sent — the Pod spec is the cheaper read
either way.*

### Requirement 5: Documentation

#### Acceptance Criteria

1. THE documentation SHALL state how the Runner obtains its credential and
   what an attacker would need in order to read it
2. THE documentation SHALL state what the token is scoped to
3. THE documentation SHALL NOT claim protection the mechanism does not
   provide — in particular, it does not defend against someone who already
   has cluster-admin, nor against the tool the repository chooses to run

## Out of Scope

- **A per-Job Kubernetes Secret.** Considered and deliberately not built.
  It closes pod-read and would have been the right patch under time
  pressure; there is none. Slice 25's projected ServiceAccount token puts
  nothing in the Pod spec, so once it lands there is no credential to
  deliver and the Secret's `ownerReference` lifecycle and RBAC would exist
  only to be removed. Recorded so that "why is there no Secret here" has
  an answer that is not oversight.
- **The other eleven unused `OperationStart` fields.** Removing
  `github_token` belongs here because it is a credential; that the Server
  acts on none of the twelve is a question about the message's shape.
- **Authenticating the Runner.** Slice 25 entirely. This slice consumes
  the channel and does not build it.
- **Encrypting the channel.** Slice 38 (`runner-server-tls`). It is a
  preference for this slice rather than a dependency, and the distinction
  is worth stating: Slice 25 left the channel in plaintext, so Requirement
  2 moves the token from the Pod spec — reachable passively, at rest, by
  pod-read and etcd — onto a wire where capturing it needs node access or
  `CAP_NET_RAW`. That is a net improvement, and Requirement 1 bounds what
  a capture is worth. What Slice 38 additionally closes is that a Runner
  *asks an unverified peer for a credential*: an attacker who wins routing
  to the Server's Service name becomes a participant rather than an
  observer. If 38 is scheduled when this slice starts, do it first.
- **Encryption at rest for etcd**, which turnip cannot assert, and which
  Requirement 5.3 exists to stop the documentation implying.
- **Token rotation or shorter lifetimes.** Expiry bounds damage after the
  window; Requirement 1 narrows the damage within it, which is the part
  turnip controls.
