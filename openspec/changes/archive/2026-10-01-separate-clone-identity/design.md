# Design

## Context

See proposal.md for why. Today every Runner Pod has one projected token
volume (audience `turnip.ivan.vc`, 10 minutes), mounted into the clone
init container and the main `runner` container. The Server validates a
token with a `TokenReview` naming that audience, binds it to the
Operation's Pod by uid, and then any RPC (the result stream or
`FetchCloneCredential`) is admitted. Only the clone container's git ever
asks for a credential, through the Runner binary acting as git's
credential helper; the main container never needs one.

## Goals / Non-Goals

**Goals:**
- Code running in the tool container cannot obtain a GitHub credential
- No new timing window, Server state or setting

**Non-Goals:**
- Preventing the tool container from reporting a forged result for its
  own Operation (see proposal, out of scope)

## Decisions

### Decision 1: One audience per container

| Container | Volume | Audience | Accepted for |
|---|---|---|---|
| `clone` (init, turnip's image) | clone token | `turnip.ivan.vc/clone` | `FetchCloneCredential`, the result stream |
| `runner` (main, runs the tool) | runner token | `turnip.ivan.vc` | the result stream only |

Each volume is mounted only into its own container, at the same path
(`/turnip/run/secrets/token`), so `TURNIP_TOKEN_FILE` and the Runner's
token handling do not change. A container cannot read a volume that is
not mounted into it, and the clone container has exited before the main
container starts.

The Server's `TokenReview` names both audiences and the authenticator
records which one the review reported. `FetchCloneCredential` refuses
any caller without the clone audience with `PermissionDenied` and logs
it at error level; the stream accepts either. Pod-uid binding is
unchanged and applies to both.

*Alternative considered*: refuse `FetchCloneCredential` once the
Operation has started (its first log line, or the stream's start
message).
*Rejected because*: it depends on ordering between the Runner opening
its stream and the tool starting, a window to reason about and test;
and git's credential helper legitimately asks several times during the
clone (fetch, base, each submodule), which rules out a "first call
only" rule too. Separate audiences have no window: the tool container
never holds a token the mint accepts.

*Alternative considered*: drop the token from the main container.
*Rejected because*: the Runner in it must authenticate to report the
result.

### Decision 2: The clone audience is a path under the same domain

`turnip.ivan.vc/clone` rather than a second domain: an audience is an
opaque string to Kubernetes, and keeping both under the domain Slice 49
renames means that rename moves both at once.

## Risks / Trade-offs

- [An in-flight Operation during upgrade presents the old single token
  to the new Server] → its clone container's token has the runner
  audience only, so its credential fetch is refused and that Operation
  fails; re-running it succeeds. Pre-1.0: upgrade while idle, as the
  operator docs already advise for audience changes.
- [A future container needing a credential] → it gets the clone
  audience deliberately; the table above is the place it is decided.
- [The Runner's ServiceAccount can mint tokens or reach into Pods:
  `create` on `serviceaccounts/token`, `pods/exec`, or
  `pods/ephemeralcontainers` in the Runner namespace] → code in the
  tool container can request a clone-audience token bound to its own
  Pod, read the clone identity of another Operation's running clone
  container, or mount the clone identity's volume into an ephemeral
  container. The Server cannot tell any of these from the clone identity
  the kubelet projected, so the Goal above holds only without those
  grants. `cluster-admin`, which the Helmfile docs recommend for a
  Runner that applies, holds all three. Stated as a precondition in
  `docs/operating/runner-access.md`, `SECURITY.md` and the spec delta
  rather than enforced: turnip ships no Runner RBAC and cannot see what
  the operator grants.

## Migration Plan

None beyond deploying the Server and Runner image together.
