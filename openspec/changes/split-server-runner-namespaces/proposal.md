# Proposal

## Why

`deploy/base` runs the Server in the same namespace its Runner Jobs are
created in (`TURNIP_K8S_NAMESPACE=default`, and the Server's own
resources land in whatever namespace the install targets). The Runner
runs a pull request's code, and an operator commonly grants its
ServiceAccount broad permissions so the IaC can do its job. Any such
grant that reaches Secrets or Pods in the Runner's namespace then also
reaches the Server: the `turnip-github-app` Secret holding the GitHub
App's private key, and the Server Pod itself. The App key is worth more
than anything the Server mints for a Runner.

`docs/operating/runner-access.md` ("Grants that reach the GitHub
credential") documents this exposure. Separating the namespaces removes
it structurally, so a Runner's grant in its own namespace says nothing
about the Server.

Found 2026-09-30 by the review of `separate-clone-identity`.

## What Changes

- The Server and its Secret run in one namespace; Runner Jobs are created
  in another, named by `TURNIP_K8S_NAMESPACE`
- The Server's Job and Pod permissions are granted in the Runner
  namespace (a Role and RoleBinding there, bound to the Server's
  ServiceAccount in its own namespace); the Server needs nothing else in
  the Runner namespace
- `deploy/base` and its overlays ship the two namespaces separated by
  default; the operator docs explain why, and how to choose the names
- **BREAKING** for existing installations (acceptable pre-1.0): the base
  layout changes

## Open questions (to settle before the specs)

1. **Names and defaults.** Fixed names in the base (e.g. `turnip` and
   `turnip-runners`), or left to the overlay with placeholders?
2. **Who creates the Runner namespace.** The base, or the operator (so
   it can carry their own policies, quotas and cloud identity bindings)?
3. **Anything else the Server reads from the Runner namespace.** The
   TokenReview is cluster-scoped and already granted; whether any other
   Server call assumes one namespace needs checking.

## Capabilities

### New Capabilities

### Modified Capabilities
- `deployment`: the Server and Runner namespaces separated, the RBAC across them
- `runner-access`: the Runner namespace holds nothing of the Server's

## Impact

`deploy/base`, `deploy/overlays/*`, the release install manifest,
`docs/operating/deployment.md`, `docs/operating/runner-access.md`,
`docs/getting-started/operators.md`. No Server code change is expected
beyond confirming the open question 3. Out of scope: per-repository
Runner namespaces (one Runner namespace per Server, as today).
