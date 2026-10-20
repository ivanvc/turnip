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

- The Server and its Secret run in one namespace; Runner Jobs can be
  created in another, named by `TURNIP_RUNNER_NAMESPACE`
- **BREAKING** `TURNIP_K8S_NAMESPACE` is renamed `TURNIP_RUNNER_NAMESPACE`
  (2026-10-17): once the namespaces are split, "the Kubernetes
  namespace" no longer says which one, and the new name sits with the
  Server's other Runner settings (`TURNIP_RUNNER_IMAGE`,
  `TURNIP_RUNNER_SERVICE_ACCOUNT`, `TURNIP_RUNNER_SERVER_ADDR`). No
  transition: the old name is no longer read
- `TURNIP_RUNNER_NAMESPACE` is optional (2026-10-17): unset, Runner
  Jobs run in the Server's own namespace, as before this change. The
  base leaves it unset (2026-10-19), so a first install needs one
  namespace and nothing else; the operator docs recommend separating
  the Runner namespace and say what sharing one gives up
- The Server's Job and Pod permissions are granted in the Runner
  namespace (a Role and RoleBinding there, bound to the Server's
  ServiceAccount in its own namespace); the Server needs nothing else in
  the Runner namespace
- `deploy/base` installs into one namespace; two Kustomize transformers
  let an operator rename it and separate the Runner namespace from it,
  and the operator docs explain why and how
- **BREAKING** for existing installations (acceptable pre-1.0): the base
  layout changes

## Decisions (2026-10-17)

1. **Names.** The base uses `turnip-system`, and an operator chooses
   the Server namespace and, separately, a Runner namespace (the docs
   suggest `turnip-runners`) in their own kustomization; every value
   naming a namespace follows the choice (the bindings' subjects, the
   Role's namespace; the Runner's address for the Server follows on its
   own, see 4). Revised 2026-10-19: the base no longer separates them
   out of the box, because a default needing a second namespace to
   exist fails a first install that has not created it.
2. **Who creates the namespaces.** The operator, every one they name,
   before applying: the Runner namespace is where they attach their own
   policies, quotas and cloud identity bindings, and its lifetime should
   not be tied to turnip's manifests.
3. **Anything else the Server reads from the Runner namespace.**
   Nothing. The Server creates and reads Jobs and Pods there
   (`internal/jobs`, and `internal/runnerauth` to tie a token to its
   Pod), all through the one setting, and the token review is
   cluster-scoped. The repository's Secret is mounted by the Runner Pod
   and never read by the Server.
4. **The Runner's address for the Server becomes fully qualified**
   (`turnip-server.<namespace>.svc:9090`), since a short Service name
   resolves only within its own namespace. The base forms it from the
   namespace the Server Pod runs in (the Downward API, expanded in the
   Deployment's `env`), so nothing has to be kept in step with the
   Server namespace and no Server code changes for it.

## Capabilities

### New Capabilities

### Modified Capabilities
- `deployment`: one namespace by default, the transformers that rename it and separate the Runner namespace, the RBAC across them
- `runner-access`: the Runner namespace holds nothing of the Server's
- `server`: `TURNIP_RUNNER_NAMESPACE` in place of `TURNIP_K8S_NAMESPACE`, optional, defaulting to the Server's own namespace
- `runner-job`: the namespace Jobs are created in, by its new setting
- `runner-security`: the namespace the Server looks a Runner's Pod up in, by its new setting

## Impact

`deploy/base`, new `deploy/transformers/namespaces` and
`deploy/transformers/runner-namespace`,
`deploy/overlays/*`, the release install manifest, `make test-kind` and
its tests, `docs/operating/deployment.md`,
`docs/operating/runner-access.md`, `docs/getting-started/operators.md`.
Server code changes only for the setting's new name, its default
(`cmd/server`, `internal/orchestrator` configuration and the settings
Reference test) and the startup log line that reports it. Out of
scope: per-repository
Runner namespaces (one Runner namespace per Server, as today).

## Dependencies

- **Overlaps `runner-server-tls`**, which also changes the `deployment` capability;
  best done before it: that change gives each Runner Job a certificate authority, simpler to design once the Runner's namespace is settled.
- **Overlaps `org-images`**, which rewrites every file; see its
  Dependencies.
