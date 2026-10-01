# runner-security Specification

## Purpose
Establishes which Operation a Runner may write to and how a Runner obtains the GitHub credential its clone needs, so that reading a Pod spec yields no credential and knowing an Operation id is not enough to forge its result. Operators rely on it as the boundary between Runner Pods and the Server.

## Requirements

### Requirement: Runner identity token
Every Runner Pod SHALL carry a Kubernetes ServiceAccount token projected with the audience `turnip.ivan.vc`, an expiration of 10 minutes (rotated in place by the kubelet), mounted read-only at `/turnip/run/secrets/token`, and named to the Runner by `TURNIP_TOKEN_FILE`. Only the containers that talk to the Server (the clone init container and the `runner` container) SHALL mount it. The Runner SHALL read the file afresh each time it opens a connection to the Server, so a rotation during an Operation does not interrupt it. The projection SHALL NOT modify any ServiceAccount object, so it holds whichever ServiceAccount the Pod runs as (see the `runner-access` capability). A Runner started without `TURNIP_TOKEN_FILE` SHALL exit naming it as a missing required variable.

#### Scenario: Token rotated mid-Operation
- **WHEN** an Operation runs longer than the token's lifetime and the kubelet rotates the file
- **THEN** the next stream the Runner opens presents the rotated token and is accepted

#### Scenario: Tool provisioning container has no token
- **WHEN** a Runner Job is built
- **THEN** the init container that copies a tool or the runner binary mounts no identity token

### Requirement: Every RPC is authenticated and bound to one Operation
The Server SHALL authenticate every Runner RPC, streaming and unary alike, before any handler acts on it. A caller SHALL present gRPC metadata with exactly one `authorization` value of the form `Bearer <token>` and exactly one non-empty `turnip-operation-id` value. The Server SHALL validate the token with a Kubernetes `TokenReview` naming the audience `turnip.ivan.vc`, SHALL require the review to report that audience and exactly one `authentication.kubernetes.io/pod-uid` extra, and SHALL admit the call only when that Pod uid equals the uid of the single Pod of the single Job labeled `turnip.ivan.vc/operation-id=<id>` in `TURNIP_K8S_NAMESPACE`. Pods SHALL be compared by uid, never by name. The Operation an admitted call writes to SHALL be the authenticated one; an Operation id inside the stream's start message SHALL be ignored, and a disagreeing one only logged as a warning. A Server with no authenticator configured SHALL refuse every call.

Refusals SHALL use these gRPC codes:

| Condition | Code | Server log |
|---|---|---|
| metadata missing, duplicated, not a bearer credential, or empty | `Unauthenticated` | warning `refusing rpc: malformed credential` |
| review failed or unauthenticated, audience absent, no pod-uid extra (clusters below Kubernetes 1.32), or not exactly one Job or Pod for the Operation | `Unauthenticated` | warning `refusing rpc: credential not accepted` |
| a valid token for a different Pod than the Operation's | `PermissionDenied` | error `refusing rpc: caller is not this operation's runner` |

#### Scenario: Forged result from another Pod
- **WHEN** a Pod holding a valid `turnip.ivan.vc` token sends a result claiming Operation `op-a`, which runs on a different Pod
- **THEN** the stream is refused with `PermissionDenied`, nothing is recorded for `op-a`, and the Server logs `refusing rpc: caller is not this operation's runner` at error level

#### Scenario: Default ServiceAccount token presented
- **WHEN** a caller presents the Pod's default ServiceAccount token (audience of the API server)
- **THEN** the review does not report `turnip.ivan.vc` and the call is refused with `Unauthenticated`

#### Scenario: Cluster older than 1.32
- **WHEN** the API server's review carries no `authentication.kubernetes.io/pod-uid` extra
- **THEN** every Runner stream is refused with `Unauthenticated`, with an error saying turnip requires Kubernetes v1.32 or later, and no Operation reports a result

#### Scenario: Operation id known but no token
- **WHEN** a caller sends only `turnip-operation-id` with no `authorization`
- **THEN** the call is refused with `Unauthenticated`

### Requirement: Clone credential delivery
A Runner Pod SHALL carry no GitHub credential in any container's environment, arguments or volumes. When git needs a credential during the clone, it SHALL ask the Runner binary, configured as git's credential helper through per-invocation environment rather than any config file, which SHALL fetch one from the Server over the authenticated channel and return it to git on a pipe as username `x-access-token` and the token as password. The credential SHALL NOT be written to a file, placed in a URL, or passed as a command-line argument. The helper SHALL answer only `get` requests for the host of the Operation's own repository URL; requests for other hosts and `store`/`erase` requests SHALL receive no credential and succeed silently. The fetch request SHALL carry no Operation identity: the Server SHALL mint the credential for the Operation the caller authenticated as, and SHALL NOT return one when it has no record of that Operation or authentication fails. A fetch the Server cannot satisfy SHALL make the helper exit non-zero with `runner: credential: <error>` on standard error, so the git operation fails (reported per the `checkout` capability).

Known issue: the credential is available to any container in the Operation's Pod that holds the identity token, including the container running the repository's tool, so repository code can fetch it. Fix proposed in change `separate-clone-identity`.

#### Scenario: Reading the Pod yields nothing
- **WHEN** someone with Pod read access inspects a Runner Pod's spec
- **THEN** no GitHub token or other GitHub credential appears in any container's environment, arguments or volumes

#### Scenario: Submodule on another host
- **WHEN** git asks the helper for a credential for a host other than the Operation's repository host
- **THEN** the helper returns no credential

#### Scenario: Operation record gone
- **WHEN** a Runner asks for a credential after its Operation's record was swept
- **THEN** the Server refuses with `Internal` (`rpc: could not mint a clone credential`) and the clone fails

### Requirement: Clone credential scope
The Server SHALL mint the clone credential as a GitHub App installation token at the moment the Runner asks for it, never at dispatch, with only the `contents: read` permission. Its repository scope SHALL be the set computed at dispatch from the resolved submodule mode: under `none`, the Operation's repository alone; under `top-level`, that repository plus every repository named by a `url` in `.gitmodules` at the head commit that is relative, or on the same host and owner as the Operation's repository; under `recursive`, or when `.gitmodules` exists but cannot be read, every repository the installation covers (permissions still `contents: read`). Submodules on another host or owner SHALL be left out of the scope. When GitHub rejects the scoped mint, the Server SHALL NOT fall back to a broader token; the fetch fails.

#### Scenario: Same-owner submodule included
- **WHEN** an Operation on `example/web` runs with `top-level` submodules and `.gitmodules` names `../api.git` and `https://github.com/other/lib.git`
- **THEN** the credential covers `web` and `api` only, with `contents: read`

#### Scenario: Recursive mode widens repositories only
- **WHEN** the resolved submodule mode is `recursive`
- **THEN** the credential covers every repository of the installation, still with `contents: read` only

#### Scenario: Mint rejected
- **WHEN** GitHub refuses to mint a token for the computed scope
- **THEN** the Runner receives no credential and the clone fails rather than proceeding with an unscoped token

### Requirement: Plaintext Runner channel
The Runner-to-Server gRPC channel, for both the operation stream and the credential fetch, SHALL be unencrypted. Authentication SHALL NOT depend on encryption: a captured identity token is limited to the `turnip.ivan.vc` audience, expires within minutes, and admits writes to exactly one Operation, while a captured clone credential is limited as stated above. Plan and log output, and both credentials, SHALL be readable by anyone able to capture pod-to-pod traffic.

#### Scenario: Traffic capture
- **WHEN** an attacker with network position on the cluster captures a Runner's stream
- **THEN** they can read the Operation's output and the token presented, and that token is not accepted by the Kubernetes API for any other audience
