# Proposal

## Why

The container that runs the repository's tool holds the same Kubernetes
identity token as the clone container, because the Runner binary in it
needs the token to report results. The Server mints a GitHub clone
credential for any caller authenticated as the Operation's Pod, so the
repository's own code (a Helmfile `prepare` hook, for instance) can read
the token file and fetch that credential.

Usually the credential is `contents: read` on the repository the code
already sees. But in `recursive` submodule mode, or when `.gitmodules`
exists and cannot be read, the credential is installation-wide: read
access to every repository the GitHub App is installed on. The second
case needs no operator opt-in, which `SECURITY.md` defines as a
vulnerability. It also contradicts what Slice 24 (runner-token-delivery)
required: the token "SHALL remain unavailable to the container that runs
the tool".

Found 2026-09-30 by the baseline review of current behavior.

## What Changes

- The clone init container and the tool container carry identity tokens
  for different audiences, each mounted only into its own container
- The Server mints a clone credential only for the clone audience; the
  tool container's token is accepted for reporting only
- Reporting (the result stream) accepts either audience, since the clone
  container reports clone failures and the tool container reports results

## Capabilities

### New Capabilities

### Modified Capabilities
- `runner-security`: two identities per Runner Pod; the clone credential is minted only for the clone identity

## Impact

`internal/jobs` (a second projected token volume and audience),
`internal/runnerauth` and `internal/rpc` (the audience a call presented,
and per-method acceptance), the server and runner tests. No setting
changes; no change to the Runner's token path (`TURNIP_TOKEN_FILE` names
whichever token its container mounts).

Out of scope:
- The tool container forging its own Operation's result (it can report
  a false success for itself). That is a narrower problem, its own
  change if wanted.
- Narrowing the installation-wide credential in `recursive` mode: still
  the documented cost of that opt-in, and no longer reachable by tool
  code once this lands.
- Renaming the audience domain: Slice 49 (`org-images`) moves both
  audiences to the organization's domain.

**Disclosure**: this change, the `runner-security` baseline that
mentions the flaw, and the fix are kept uncommitted until the fix is
ready, then committed together, as `SECURITY.md` asks of others.
