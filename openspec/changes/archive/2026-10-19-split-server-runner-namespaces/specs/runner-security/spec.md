## MODIFIED Requirements

### Requirement: Every RPC is authenticated and bound to one Operation
The Server SHALL authenticate every Runner RPC, streaming and unary alike, before any handler acts on it. A caller SHALL present gRPC metadata with exactly one `authorization` value of the form `Bearer <token>`, carrying a Runner identity, and exactly one non-empty `turnip-operation-id` value. The Server SHALL validate the Runner identity with a Kubernetes `TokenReview` naming the audiences of both the clone identity and the report identity, SHALL require the review to report at least one of them and exactly one `authentication.kubernetes.io/pod-uid` extra, and SHALL admit the call only when that Pod uid equals the uid of the single Pod of the single Job labeled `turnip.ivan.vc/operation-id=<id>` in the Runner namespace (see the `server` capability). Pods SHALL be compared by uid, never by name. The Server SHALL record which Runner identity an admitted call presented. The result stream SHALL accept either Runner identity; a GitHub credential fetch SHALL require the clone identity. The Operation an admitted call writes to SHALL be the authenticated one; an Operation id inside the stream's start message SHALL be ignored, and a disagreeing one only logged as a warning. A Server with no authenticator configured SHALL refuse every call.

Refusals SHALL use these gRPC codes:

| Condition | Code | Server log |
|---|---|---|
| metadata missing, duplicated, not a bearer credential, or empty | `Unauthenticated` | warning `refusing rpc: malformed credential` |
| review failed or unauthenticated, neither Runner identity's audience reported, no pod-uid extra (clusters below Kubernetes 1.32), or not exactly one Job or Pod for the Operation | `Unauthenticated` | warning `refusing rpc: credential not accepted` |
| a valid Runner identity for a different Pod than the Operation's | `PermissionDenied` | error `refusing rpc: caller is not this operation's runner` |
| a GitHub credential fetch presenting the report identity | `PermissionDenied` | error `refusing rpc: credential fetch requires the clone identity` |

#### Scenario: Forged result from another Pod
- **WHEN** a Pod holding a valid report identity sends a result claiming Operation `op-a`, which runs on a different Pod
- **THEN** the stream is refused with `PermissionDenied`, nothing is recorded for `op-a`, and the Server logs `refusing rpc: caller is not this operation's runner` at error level

#### Scenario: Default ServiceAccount token presented
- **WHEN** a caller presents the Pod's default ServiceAccount token (audience of the API server) instead of a Runner identity
- **THEN** the review reports neither Runner identity's audience and the call is refused with `Unauthenticated`

#### Scenario: Cluster older than 1.32
- **WHEN** the API server's review carries no `authentication.kubernetes.io/pod-uid` extra
- **THEN** every Runner stream is refused with `Unauthenticated`, with an error saying turnip requires Kubernetes v1.32 or later, and no Operation reports a result

#### Scenario: Operation id known but no token
- **WHEN** a caller sends only `turnip-operation-id` with no `authorization`
- **THEN** the call is refused with `Unauthenticated`

#### Scenario: Tool container asks for a GitHub credential
- **WHEN** code in the `runner` container presents the report identity to fetch a GitHub credential for its own Operation
- **THEN** the call is refused with `PermissionDenied`, no GitHub credential is minted, and the Server logs `refusing rpc: credential fetch requires the clone identity` at error level

#### Scenario: Clone container reports a clone failure
- **WHEN** the clone container presents the clone identity on the result stream to report a failed clone
- **THEN** the stream is accepted and the failure is recorded for the Operation

### Requirement: Clone credential delivery
A Runner Pod SHALL carry no GitHub credential in any container's environment, arguments or volumes. When git needs a GitHub credential during the clone, it SHALL ask the Runner binary, configured as git's credential helper through per-invocation environment rather than any config file, which SHALL fetch one from the Server over the authenticated channel and return it to git on a pipe as username `x-access-token` and the GitHub credential as password. The GitHub credential SHALL NOT be written to a file, placed in a URL, or passed as a command-line argument. The helper SHALL answer only `get` requests for the host of the Operation's own repository URL; requests for other hosts and `store`/`erase` requests SHALL receive no credential and succeed silently. The fetch request SHALL carry no Operation identity: the Server SHALL mint the GitHub credential for the Operation the caller authenticated as, only when the caller presented the clone identity, and SHALL NOT return one when it has no record of that Operation or authentication fails. Because only the clone init container holds the clone identity, no code running in the container that runs the tool SHALL be able to obtain a GitHub credential, provided no ServiceAccount a Runner Pod may run as (`TURNIP_RUNNER_SERVICE_ACCOUNT`, and every ServiceAccount in the Runner namespace when `runner.serviceAccount` is allowed) is granted, in that namespace, `create` on `serviceaccounts/token`, access to `pods/exec`, or `patch` or `update` on `pods/ephemeralcontainers`. Each of those grants lets code in the tool container mint a clone identity bound to its own Pod, or read one from a running clone container, and the Server cannot tell that clone identity apart from one the kubelet projected; the operator documentation SHALL name these grants as ones that remove this protection. A fetch the Server cannot satisfy SHALL make the helper exit non-zero with `runner: credential: <error>` on standard error, so the git operation fails (reported per the `checkout` capability).

#### Scenario: Reading the Pod yields nothing
- **WHEN** someone with Pod read access inspects a Runner Pod's spec
- **THEN** no GitHub credential, in any form, appears in any container's environment, arguments or volumes

#### Scenario: Submodule on another host
- **WHEN** git asks the helper for a credential for a host other than the Operation's repository host
- **THEN** the helper returns no credential

#### Scenario: Operation record gone
- **WHEN** a Runner asks for a GitHub credential after its Operation's record was swept
- **THEN** the Server refuses with `Internal` (`rpc: could not mint a clone credential`) and the clone fails

#### Scenario: Repository code tries the credential helper
- **WHEN** a repository's tool invocation runs the Runner binary as a credential helper from the `runner` container, which holds only the report identity
- **THEN** the Server refuses the fetch and no GitHub credential is returned

#### Scenario: A Runner ServiceAccount allowed to mint tokens
- **WHEN** the Runner's ServiceAccount may `create` `serviceaccounts/token` in the Runner namespace, and code in the `runner` container requests a token for that ServiceAccount with the clone identity's audience, bound to its own Pod
- **THEN** the Server accepts that token as the clone identity and mints a GitHub credential, and the operator documentation lists this grant as one that lets the tool obtain a GitHub credential
