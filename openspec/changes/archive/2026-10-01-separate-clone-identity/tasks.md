# Tasks

**The checkpoint that matters is task 3.** Unit tests on the authenticator
and the Job builder can each pass while the two never meet; only a test
that builds the real Job, takes each container's mounted audience, and
calls the real Server with it shows the tool container cannot get a
credential.

## 1. Two identities in the Runner Job (`internal/jobs`)

- [x] 1.1 A second projected token volume with audience `turnip.ivan.vc/clone`, mounted at `/turnip/run/secrets/token` in the clone init container only; the `runner` container keeps the `turnip.ivan.vc` volume; both audiences as exported constants beside `TokenAudience`
- [x] 1.2 Tests: each container mounts exactly its own token volume; provisioning init containers mount neither; the pinned Job test updated

## 2. Audience-aware authentication (`internal/runnerauth`, `internal/rpc`)

- [x] 2.1 The `TokenReview` names both audiences; the authenticator returns which one the review reported, alongside the bound Pod uid
- [x] 2.2 The authenticated context carries the audience; `FetchCloneCredential` refuses the runner audience with `PermissionDenied` and the error log `refusing rpc: credential fetch requires the clone identity`; the result stream accepts either
- [x] 2.3 Tests: a review reporting each audience, neither, and both; the credential fetch refused for the runner audience and admitted for the clone audience; the stream admitted for both

## 3. Checkpoint: the tool container cannot get a credential

- [x] 3.1 A test building the real Job, reading each container's projected audience from it, and calling the real authenticated Server (with a fake `TokenReview` that reports the audience a token was minted for): the `runner` container's identity is refused a credential and allowed to report; the clone container's identity gets a credential and may report a clone failure
- [x] 3.2 `go build ./...`, `make test`, `make lint` pass

## 4. Specs and documentation

- [x] 4.1 `docs/operating/runner-access.md` and `SECURITY.md`: the tool container cannot obtain a GitHub credential; remove any statement implying otherwise
- [ ] 4.2 The archive step applies this change's deltas to `openspec/specs/runner-security/spec.md`, removing its known-issue line, and to `openspec/specs/runner-job/spec.md`

## Repository owner

- Keep this change, the `runner-security` baseline and the fix uncommitted until the fix is ready, then commit them together
- Deploy the Server and Runner image together, while no Operation runs

## Deviations recorded at implementation

- The authenticator returns an `rpc.Identity` (none, report, clone) rather than the raw audience string, so `internal/rpc` stays free of `internal/jobs`; `internal/runnerauth` maps each audience to it.
- The interceptor refuses (`Unauthenticated`) an Authenticator that admits a caller with no identity, and `FetchCloneCredential` refuses a context without one, so a lost identity never defaults to allowed.
- The Job's volumes are named `turnip-clone-identity` and `turnip-report-identity`; `TokenAudience` was replaced by `ReportAudience` and `CloneAudience`.
- The Makefile `docs-check` target now excludes `openspec/` from the link check, as it does `.kiro/`.
- The change also carries a `runner-job` delta spec (`specs/runner-job/`), not listed in the original task text.
- `deploy/base/clusterrole.yaml` and several comments in `internal/runner` were reworded for the new terminology; no behavior change.

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1.1", "1.2", "2.1", "2.2", "2.3"] },
    { "id": 1, "tasks": ["3.1", "3.2"] },
    { "id": 2, "tasks": ["4.1", "4.2"] }
  ]
}
```
