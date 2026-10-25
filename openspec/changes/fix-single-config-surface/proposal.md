# Proposal

## Why

Configuring turnip takes more places than it should, and which one a
value belongs in is not obvious. Installing the Server through Helmfile
(the pilot's way) showed it: the Server's configuration ended up in
seven places, only two of them documented by turnip:

| Value | Where it ended up | Why |
|---|---|---|
| Server image | a JSON patch on the Deployment | the docs show how to set it only through a kustomization's `images:` field |
| Settings | a second ConfigMap, plus a patch adding it to `envFrom` | no documented way to set `turnip-server-config` from Helmfile |
| Redis password | its own Secret, plus a Deployment patch adding an `env` entry, a volume and a mount | the base gives secret material other than the GitHub App's no slot |
| GitHub App credentials | their own Secret | joined to the Deployment only by a name and two keys the base hard-codes and nothing in the operator's files shows |
| Runner namespace | the setting, plus a namespace patch on the Role and RoleBinding | the shipped `runner-namespace` transformer cannot run under Helmfile (below) |
| Settings the Server no longer reads | left in place, silently ignored | the Server says nothing about a `TURNIP_*` variable it does not know |
| A settings change | needs a manual `kubectl rollout restart`, which no page says | under Helmfile, nothing restarts the Server; the restart stays the operator's step, but documented (see Decisions) |

The root of most rows: the base's generated ConfigMap is named with a
hash of its contents. Under plain Kustomize that hash is what lets an
operator merge settings in and what restarts the Server on a change.
Helmfile builds a Kustomize chart in two passes, rendering the base to
a file and then applying the release's transformers to it; by the
second pass the name is already `turnip-server-config-<hash>`, so
anything selecting `turnip-server-config` by name finds nothing, and a
later edit no longer changes the name. Found 2026-10-19 deploying
`split-server-runner-namespaces`, and recorded in its archived tasks.

turnip is pre-1.0 with one installation; this is the moment to make
the configuration surface one an operator cannot get wrong.

## What Changes

The rule this change establishes: **settings go in
`turnip-server-config`, secret material goes in one Secret, and an
operator never patches the Deployment** (apart from replica count and
the image, through Kustomize's own image transformer).

- **BREAKING** The base's ConfigMap is no longer hash-suffixed: it is
  `turnip-server-config` under every install method, so one patch sets
  every setting and the `runner-namespace` transformer works under
  Helmfile
- A settings change takes effect after `kubectl rollout restart
  deployment/turnip-server`, which the docs state beside every place
  they show a setting being changed. The hashed name used to restart
  the Server by itself under Kustomize; Helmfile installs never had
  that. The Server still reads its settings once, at startup
- **BREAKING** One Secret, `turnip-server-secrets`, replacing
  `turnip-github-app`, holds all of the Server's secret material:
  the GitHub App's webhook secret and private key, and, optionally, the
  Redis password. The base mounts it, so the Redis password needs only
  `TURNIP_REDIS_PASSWORD_PATH` in the ConfigMap. The Secret is created
  in the operator's own files, next to the base, by a generator or a
  release, so its name and keys are visible where they are wired; the
  base states the contract (name and keys) where it references it
- The Server logs a warning at startup naming each `TURNIP_*`
  variable it does not read, so a stale or mistyped setting is visible
  instead of silently ignored. Kubernetes injects variables for every
  Service in the namespace, named after it (`turnip-redis` gives
  `TURNIP_REDIS_PORT`), so the base turns those off
  (`enableServiceLinks: false`) and the warning leaves names of that
  shape out
- `docs/operating/deployment.md` documents installing with Helmfile
  beside Kustomize: one release whose transformers are the image
  transformer, one settings patch and, when separating, the Runner
  namespace transformer; and the Secret beside it
- A render test covers a Helmfile-shaped install by rendering the base
  first and applying the fixture's transformers to the result, as
  Helmfile does

## Decisions (2026-10-19)

1. **Restarting after a settings change is the operator's step,** a
   documented `kubectl rollout restart`. A Server that watched its
   mounted settings and restarted itself was considered and dropped:
   a watch, validation of the new values, a stagger between replicas
   and a second watch for the Secret are too much machinery for a
   change the operator makes by hand, pre-1.0, with one installation.
2. **One Secret replaces `turnip-github-app` (2026-10-20),** since it
   no longer holds only the App's credentials. Keeping
   `turnip-github-app` and adding a second, optional Secret for the
   Redis password was rejected: two Secrets to explain, and the next
   secret setting would need a third. **It is named
   `turnip-server-secrets` (2026-10-23),** mirroring
   `turnip-server-config`, so the two names say which holds settings
   and which holds secrets; first named `turnip-server` after the
   Deployment, which matched it but not the ConfigMap.

## Capabilities

### New Capabilities

### Modified Capabilities
- `deployment`: the ConfigMap's fixed name, the one
  Secret for secret material and how the base wires it, and the
  Helmfile install
- `server`: warning about unknown `TURNIP_*` variables (the settings
  index's "Setting not in the index" scenario changes)
- `runner-access`: the Server's Secret is named `turnip-server-secrets` where
  the Runner namespace's requirements name it

## Impact

`deploy/base` (kustomization, Deployment), `deploy/overlays/*`, the
release install manifest, `internal/orchestrator` (the
unknown-variable warning), `test/manifests`
(a two-pass fixture), `make test-kind`, `docs/operating/deployment.md`,
`docs/operating/server-settings.md`, `docs/getting-started/operators.md`
and `docs/operating/troubleshooting.md`.

Out of scope:
- A Helm chart: decided against for now (2026-10-19), since releasing
  one adds work before turnip has a beta; kept in the backlog
- The Runner image following the Server's version, so one version is
  stated once instead of twice: backlog
- An ingress or HTTPRoute for the webhook: the operator's, as today
- Docs warning that separating the Runner namespace reaches the cloud
  identity bindings (an EKS Pod Identity association and its role's
  trust policy, GKE Workload Identity, Azure federated credentials,
  IRSA's `sub` claim): backlog, found at the same deploy

## Dependencies

None open. Builds on the archived `split-server-runner-namespaces`,
whose `runner-namespace` transformer this makes usable under Helmfile.
