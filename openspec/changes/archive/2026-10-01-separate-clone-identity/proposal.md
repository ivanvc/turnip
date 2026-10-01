# Proposal

## Why

The container that runs the repository's tool holds the same Runner
identity as the clone container (the same Kubernetes projected
ServiceAccount token), because the Runner binary in it needs a Runner
identity to report results. The Server mints a GitHub credential for any
caller authenticated as the Operation's Pod, so the repository's own code
(a Helmfile `prepare` hook, for instance) can read the Runner identity's
file and fetch that GitHub credential.

Usually the GitHub credential is `contents: read` on the repository the
code already sees. But in `recursive` submodule mode, or when
`.gitmodules` exists and cannot be read, the GitHub credential is
installation-wide: read access to every repository the GitHub App is
installed on. The second case needs no operator opt-in, which
`SECURITY.md` defines as a vulnerability. It also contradicts what
Slice 24 (runner-token-delivery) required of the GitHub credential: it
"SHALL remain unavailable to the container that runs the tool".

Found 2026-09-30 by the baseline review of current behavior.

## What Changes

- Every Runner Pod carries two Runner identities, each mounted only into
  its own container: the clone identity in the clone init container and
  the report identity in the `runner` container that runs the tool. They
  differ by audience (`turnip.ivan.vc/clone` and `turnip.ivan.vc`)
- The Server mints a GitHub credential only for a caller presenting the
  clone identity; the report identity is accepted for reporting only
- Reporting (the result stream) accepts either identity, since the clone
  container reports clone failures and the `runner` container reports
  results

## Capabilities

### New Capabilities

### Modified Capabilities
- `runner-security`: two Runner identities per Runner Pod; the GitHub credential is minted only for the clone identity, which holds only while the Runner's ServiceAccount cannot mint a clone identity or reach into a Runner Pod; a captured clone identity on the plaintext channel can also fetch its Operation's GitHub credential
- `runner-job`: the single projected token volume becomes two Runner identity volumes, `turnip-clone-identity` (clone container only) and `turnip-report-identity` (`runner` container only), at the same path

## Impact

`internal/jobs` (a second projected Runner identity volume and
audience), `internal/runnerauth` and `internal/rpc` (which identity a
call presented, and per-method acceptance), the server and runner tests.
No setting changes; no change to the Runner's identity path
(`TURNIP_TOKEN_FILE` names whichever Runner identity its container
mounts). The operator docs and `SECURITY.md` name the Runner
ServiceAccount grants (`create` on `serviceaccounts/token`,
`pods/exec`, `pods/ephemeralcontainers`) that let tool code mint or
read a clone identity anyway: turnip cannot tell such a clone identity
from the one the kubelet projected.

Out of scope:
- The `runner` container forging its own Operation's result (the report
  identity can report a false success for its own Operation). That is a
  narrower problem, its own change if wanted.
- Narrowing the installation-wide GitHub credential in `recursive` mode:
  still the documented cost of that opt-in, and no longer reachable by
  tool code once this lands.
- Renaming the audience domain: Slice 49 (`org-images`) moves both
  identities' audiences to the organization's domain.

**Disclosure**: this change, the `runner-security` baseline that
mentions the flaw, and the fix are kept uncommitted until the fix is
ready, then committed together, as `SECURITY.md` asks of others.
