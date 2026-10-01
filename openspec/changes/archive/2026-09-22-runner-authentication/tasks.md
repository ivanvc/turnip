# Implementation Plan: Authenticating the Runner to the Server (Slice 25)

## Overview

This is a two-sided change, and the ordering rule is that **every step
before the last is additive**.

The Server and the Runner must agree, and the moment the Server starts
enforcing, a Runner that does not present a credential fails. So the
credential is put in place and presented first, while nothing reads it;
enforcement lands last, by which point everything it needs is already
flowing. Tasks 1 and 2 are each expected to change no behavior at all,
and their checkpoints say so — a test failing there means something was
read that should not have been.

**Every step is stageable, and that is the point of the split.** An
earlier version of this plan carried TLS as one unstageable cutover;
encryption now has its own slice (`runner-server-tls`), leaving nothing
here that both sides must adopt simultaneously. A new Runner talking to
an old Server is ignored-metadata, a no-op. A new Server talking to an
old Runner refuses — so deploy the Runner image first, confirm nothing
changed, then the Server. **Rollback is Server-only**: revert that image
and both Runner versions work again.

The forgery test in task 5 is the one that matters. An implementation that
authenticates the caller but does not bind it to the Operation passes
every other test in this plan.

## Tasks

- [x] 1. The credential reaches the Pod, and nothing reads it
  - [x] 1.1 Project an audience-scoped token
    - A `ServiceAccountToken` projection on the Pod's volumes, with
      turnip's own audience and a short expiry
    - A field of the Pod spec turnip already writes — **no ServiceAccount
      object changes**, which is what keeps Slice 13's
      `runner.serviceAccount` override orthogonal
    - _Requirements: 3.1, 3.3_

- [x] 2. Checkpoint - the token is mounted and unused
  - `go build ./...`, `go test -race ./...` and golangci-lint pass.
  - Behavior is deliberately unchanged: nothing reads the projected
    token, and the Server has no interceptor. A failure here means the
    projection disturbed the Pod spec in some other way.

- [x] 3. The Runner presents it, and the Server ignores it
  - [x] 3.1 Attach token and operation id as metadata
    - Read the token file inside `openStream` (`reporter.go`), not once at
      construction: kubelet rotates a projected token in place, and a
      reconnect after rotation must present the current one
    - The operation id travels beside it, because the interceptor runs
      before any message is read and must bind the pair together
    - _Requirements: 1.1_
  - [x] 3.2 Still additive
    - The Server has no interceptor yet and ignores unknown metadata, so
      this changes nothing observable

- [x] 4. Checkpoint - presented, still unenforced
  - `go build ./...`, `go test -race ./...` and golangci-lint pass.
  - Behavior unchanged again. Two consecutive `openStream` calls read the
    token file twice — asserted on reads, not on timing — so a token
    replaced between them is presented as replaced.
  - _Requirements: 3.1_

- [x] 5. The Server establishes who is calling, and binds it
  - [x] 5.1 A stream interceptor, not a handler check
    - `NewServer` gains options; the check goes where every RPC passes
      through it, so one added later is authenticated without its author
      remembering
    - _Requirements: 4.1, 4.2_
  - [x] 5.2 Validate the token
    - `TokenReview`, verifying turnip's audience. A token for any other
      audience is refused — including the Pod's default API-server token,
      which is the mistake that would let the Server act as the Runner's
      ServiceAccount
    - A validation failure, an expired token, or an absent credential
      refuses the stream
    - _Requirements: 1.2, 3.2_
  - [x] 5.3 Bind the caller to the Operation
    - Compare `authentication.kubernetes.io/pod-uid` against the uid of
      the one Pod belonging to this Operation's Job — resolved through the
      `turnip.ivan.vc/operation-id` label, the `batch.kubernetes.io/job-name`
      selector, and `BackoffLimit: 0`
    - Compare uids, not names: a Pod name can be reused after deletion
    - Nothing about the ServiceAccount enters the decision
    - _Requirements: 1.1, 2.1, 2.2, 2.3_
  - [x] 5.4 The forgery test
    - A caller presenting a **valid** token for one Operation's Pod, while
      naming a **different** Operation, is rejected
    - This is the attack the slice exists to stop, and the case an
      implementation that authenticates without binding would pass
    - _Requirements: 1.3, 2.1_

- [x] 6. The authenticated id supersedes the message's
  - [x] 6.1 Take the operation id from the context
    - `server.go`'s `operationID = payload.Start.GetOperationId()` goes;
      the id comes from what the interceptor verified
    - _Requirements: 1.3_
  - [x] 6.2 Say so at the site
    - `OperationStart.operation_id` becomes decorative and is **not**
      removed — pruning the message is a protocol question Slice 24
      records, not a security one. A comment states why the field is
      ignored, or a later reader restores the assignment as a
      simplification and reopens the gap

- [x] 7. Refuse rather than degrade
  - [x] 7.1 No Pod identity, no stream
    - A `TokenReview` response without `pod-uid` refuses, naming the
      Kubernetes version requirement
    - Exercised with a fake reviewer, since it depends on the cluster
      rather than on anything turnip controls
    - _Requirements: 5.2, 5.3_

- [x] 8. Checkpoint - the gap is closed
  - `go build ./...`, `go test -race ./...` and golangci-lint pass.
  - The forgery test passes; a wrong audience is refused; an absent
    `pod-uid` refuses.
  - The interceptor suite runs over an insecure transport, which is the
    assertion that authentication does not depend on encryption.
  - Knowing `TURNIP_OPERATION_ID` is no longer sufficient to write to an
    Operation, which is the whole point.
  - _Requirements: 1.3_

- [x] 9. Deployment and documentation
  - [x] 9.1 The cluster-scoped grant
    - A ClusterRole and binding for `create` on
      `authentication.k8s.io/tokenreviews` (the group this task
      originally misnamed as `authentication.kubernetes.io`). It
      **cannot** live in the namespaced Role: `TokenReview` is
      cluster-scoped, and this is turnip's first cluster-scoped ask
    - Shipped as turnip's own narrow ClusterRole rather than a binding to
      the conventional `system:auth-delegator`, which also carries
      `subjectaccessreviews` — recorded as a deviation in `design.md`
    - _Requirements: 6.1, 6.2_
  - [x] 9.2 Say that the channel is not encrypted
    - The deployment documentation states plainly that Runner-to-Server
      traffic is plaintext, what the authentication above still
      guarantees without it, and what an attacker with network position
      can therefore read
    - An operator who considers plan output sensitive is pointed at a
      service mesh or network policy until `runner-server-tls` lands.
      Saying nothing would let an operator assume the opposite from the
      presence of a credential
  - [x] 9.3 Documentation
    - Kubernetes **v1.32 or later**, and why it is not v1.29: the extras
      exist from v1.29 but are feature-gated until v1.32, and a gate
      turnip cannot detect is not a version it can claim to support
    - The cluster-scoped grant, and what it is for
    - _Requirements: 5.1, 6.2_

- [x] 10. Amendments and status
  - [x] 10.1 Slice 5 `grpc-runner`
    - Its service gains an interceptor and a metadata contract, and
      `NewServer`'s signature changes — appended as a task there rather
      than edited into its history
  - [x] 10.2 Roadmap
    - Slice 25 to Complete; record that Slice 24's option B is now
      unblocked, and that encryption moved to `runner-server-tls`

- [x] 11. Checkpoint - the slice is done
  - `go build ./...`, `go test -race ./...` and golangci-lint all pass.
  - **Mutation checks**: removing the binding while keeping authentication
    must fail task 5.4; accepting any audience must fail the wrong-audience
    test; treating an absent `pod-uid` as acceptable must fail task 7.1.
  - The interceptor covers an RPC it was not written for — asserted by
    registering a second method and finding it authenticated with no check
    of its own.

**Outcome** (2026-09-22): `go build ./...`, `go test -race ./...` and
golangci-lint v2 all pass. Six mutants were run and all six were killed:
binding removed while authentication kept (task 5.4 fails), any audience
accepted (the wrong-audience test fails), an absent `pod-uid` treated as
acceptable (task 7.1 fails), the Operation id read back off the Start
message (task 6.1's test fails), the interceptor not installed (six tests
fail), and a missing credential waved through (the malformed-credential
table fails). The cross-RPC assertion is
`TestInterceptor_CoversAnRPCItWasNotWrittenFor`, which registers a
`turnip.test.v1.Probe` service whose handler contains no check and finds
it refused unauthenticated and handed the established Operation id when
authenticated.


- [x] 12. A unary interceptor beside the stream one
      (runner-token-delivery amendment, Slice 24)
  - **This slice's Requirement 4.2 was half true.** `NewServer` installed
    `grpc.StreamInterceptor` and nothing else, so a unary RPC registered
    on it was reached with **no authentication at all**. The claim that
    "an RPC added later is authenticated without its author having to
    remember" held for streaming RPCs and quietly failed for unary ones
  - **The probe test did not catch it because the probe was streaming.**
    `TestInterceptor_CoversAnRPCItWasNotWrittenFor` registered a
    streaming method, so it asserted the property for the half of gRPC it
    happened to use. Slice 24 needed a unary endpoint and found the hole
  - `grpc.UnaryInterceptor` now shares the same check: both interceptors
    call one `authorize` function, so the two kinds cannot drift apart
  - `TestInterceptor_CoversAUnaryRPCItWasNotWrittenFor` is its sibling,
    and was verified to fail with the interceptor removed — a probe that
    passes either way proves nothing
  - Refusals are also logged now, which they were not: a `TokenReview`
    the Server had no permission to make refused every stream in silence,
    on both sides, for as long as it took someone to notice a pull
    request had never been answered
  - _Requirements: 4.1, 4.2 (this slice); Slice 24 task 4_

**Re-verified after the TLS split** (2026-09-22): encryption moved to
Slice 38 (`runner-server-tls`) and everything above was removed from this
slice — `internal/servertls`, `WithCertificate`, the TLS dial,
`TURNIP_CA_CERT`, `OperationParams.CACert`, the `secrets` RBAC grant and
the cert-manager component. Build, `-race` tests, golangci-lint v2 and
all six mutants still pass, with the interceptor suite now dialing
`insecure.NewCredentials()` throughout — which is the demonstration that
none of this slice depended on encryption. Both overlays still render and
the `kind`-tagged harness still vets.
