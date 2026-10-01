## MODIFIED Requirements

### Requirement: Runner identity token
Every Runner Pod SHALL carry two Kubernetes ServiceAccount tokens, each projected in its own volume with an expiration of 10 minutes (rotated in place by the kubelet), mounted read-only at `/turnip/run/secrets/token` in its own container only, and named to the Runner by `TURNIP_TOKEN_FILE`:

| Container | Audience |
|---|---|
| the clone init container | `turnip.ivan.vc/clone` |
| the `runner` container, which runs the tool | `turnip.ivan.vc` |

No other container SHALL mount either token. The Runner SHALL read the file afresh each time it opens a connection to the Server, so a rotation during an Operation does not interrupt it. The projection SHALL NOT modify any ServiceAccount object, so it holds whichever ServiceAccount the Pod runs as (see the `runner-access` capability). A Runner started without `TURNIP_TOKEN_FILE` SHALL exit naming it as a missing required variable.

#### Scenario: Token rotated mid-Operation
- **WHEN** an Operation runs longer than the token's lifetime and the kubelet rotates the file
- **THEN** the next stream the Runner opens presents the rotated token and is accepted

#### Scenario: Tool provisioning container has no token
- **WHEN** a Runner Job is built
- **THEN** the init container that copies a tool or the runner binary mounts neither token

#### Scenario: The tool container holds no clone-audience token
- **WHEN** a Runner Job is built
- **THEN** the `runner` container mounts only the `turnip.ivan.vc` token, and the `turnip.ivan.vc/clone` token is mounted only into the clone container

### Requirement: Every RPC is authenticated and bound to one Operation
The Server SHALL authenticate every Runner RPC, streaming and unary alike, before any handler acts on it. A caller SHALL present gRPC metadata with exactly one `authorization` value of the form `Bearer <token>` and exactly one non-empty `turnip-operation-id` value. The Server SHALL validate the token with a Kubernetes `TokenReview` naming the audiences `turnip.ivan.vc` and `turnip.ivan.vc/clone`, SHALL require the review to report at least one of them and exactly one `authentication.kubernetes.io/pod-uid` extra, and SHALL admit the call only when that Pod uid equals the uid of the single Pod of the single Job labeled `turnip.ivan.vc/operation-id=<id>` in `TURNIP_K8S_NAMESPACE`. Pods SHALL be compared by uid, never by name. The Server SHALL record which audience an admitted call presented. The result stream SHALL accept either audience; a credential fetch SHALL require `turnip.ivan.vc/clone`. The Operation an admitted call writes to SHALL be the authenticated one; an Operation id inside the stream's start message SHALL be ignored, and a disagreeing one only logged as a warning. A Server with no authenticator configured SHALL refuse every call.

Refusals SHALL use these gRPC codes:

| Condition | Code | Server log |
|---|---|---|
| metadata missing, duplicated, not a bearer credential, or empty | `Unauthenticated` | warning `refusing rpc: malformed credential` |
| review failed or unauthenticated, neither audience reported, no pod-uid extra (clusters below Kubernetes 1.32), or not exactly one Job or Pod for the Operation | `Unauthenticated` | warning `refusing rpc: credential not accepted` |
| a valid token for a different Pod than the Operation's | `PermissionDenied` | error `refusing rpc: caller is not this operation's runner` |
| a credential fetch presenting the `turnip.ivan.vc` audience | `PermissionDenied` | error `refusing rpc: credential fetch requires the clone identity` |

#### Scenario: Forged result from another Pod
- **WHEN** a Pod holding a valid `turnip.ivan.vc` token sends a result claiming Operation `op-a`, which runs on a different Pod
- **THEN** the stream is refused with `PermissionDenied`, nothing is recorded for `op-a`, and the Server logs `refusing rpc: caller is not this operation's runner` at error level

#### Scenario: Default ServiceAccount token presented
- **WHEN** a caller presents the Pod's default ServiceAccount token (audience of the API server)
- **THEN** the review reports neither turnip audience and the call is refused with `Unauthenticated`

#### Scenario: Cluster older than 1.32
- **WHEN** the API server's review carries no `authentication.kubernetes.io/pod-uid` extra
- **THEN** every Runner stream is refused with `Unauthenticated`, with an error saying turnip requires Kubernetes v1.32 or later, and no Operation reports a result

#### Scenario: Operation id known but no token
- **WHEN** a caller sends only `turnip-operation-id` with no `authorization`
- **THEN** the call is refused with `Unauthenticated`

#### Scenario: Tool container asks for a credential
- **WHEN** code in the `runner` container presents its `turnip.ivan.vc` token to fetch a clone credential for its own Operation
- **THEN** the call is refused with `PermissionDenied`, no credential is minted, and the Server logs `refusing rpc: credential fetch requires the clone identity` at error level

#### Scenario: Clone container reports a clone failure
- **WHEN** the clone container presents its `turnip.ivan.vc/clone` token on the result stream to report a failed clone
- **THEN** the stream is accepted and the failure is recorded for the Operation

### Requirement: Clone credential delivery
A Runner Pod SHALL carry no GitHub credential in any container's environment, arguments or volumes. When git needs a credential during the clone, it SHALL ask the Runner binary, configured as git's credential helper through per-invocation environment rather than any config file, which SHALL fetch one from the Server over the authenticated channel and return it to git on a pipe as username `x-access-token` and the token as password. The credential SHALL NOT be written to a file, placed in a URL, or passed as a command-line argument. The helper SHALL answer only `get` requests for the host of the Operation's own repository URL; requests for other hosts and `store`/`erase` requests SHALL receive no credential and succeed silently. The fetch request SHALL carry no Operation identity: the Server SHALL mint the credential for the Operation the caller authenticated as, only when the caller presented the clone identity, and SHALL NOT return one when it has no record of that Operation or authentication fails. Because only the clone init container holds the clone identity, no code running in the container that runs the tool SHALL be able to obtain a credential. A fetch the Server cannot satisfy SHALL make the helper exit non-zero with `runner: credential: <error>` on standard error, so the git operation fails (reported per the `checkout` capability).

#### Scenario: Reading the Pod yields nothing
- **WHEN** someone with Pod read access inspects a Runner Pod's spec
- **THEN** no GitHub token or other GitHub credential appears in any container's environment, arguments or volumes

#### Scenario: Submodule on another host
- **WHEN** git asks the helper for a credential for a host other than the Operation's repository host
- **THEN** the helper returns no credential

#### Scenario: Operation record gone
- **WHEN** a Runner asks for a credential after its Operation's record was swept
- **THEN** the Server refuses with `Internal` (`rpc: could not mint a clone credential`) and the clone fails

#### Scenario: Repository code tries the credential helper
- **WHEN** a repository's tool invocation runs the Runner binary as a credential helper from the `runner` container
- **THEN** the Server refuses the fetch and no credential is returned
