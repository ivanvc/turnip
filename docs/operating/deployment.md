# Deploying turnip

> **For operators**: deploying and configuring the Server. End users: see
> [`turnip.yaml`](../using/turnip-yaml.md).

The manifests themselves live under [`deploy/`](../../deploy/) as a
Kustomize base plus environment-specific overlays. This guide covers
installing a real, versioned release; `deploy/overlays/kind/` (referenced
in [Local development](../development/README.md#local-development)) is a
separate, local-testing-only example, not something to copy as a
starting point for a real deployment.

## Prerequisites

1. **A Kubernetes cluster, v1.32 or later**: any cluster `kubectl apply
   -f` can reach.

   The version floor comes from how the Server identifies a Runner. Each
   Runner container presents a Runner identity, a ServiceAccount token
   projected with one of turnip's two audiences (the clone identity in
   the clone container, the report identity in the container that runs
   the tool; see
   [Runner access](runner-access.md#how-a-runner-gets-its-github-credential)),
   and the Server validates it with a `TokenReview`. What makes a Runner
   identity bind to *one* operation rather than merely prove "some
   Runner" is the `authentication.kubernetes.io/pod-uid` extra the API
   server reports alongside it. That extra exists from v1.29 but sits
   behind a feature gate until v1.32, and a gate turnip cannot detect
   from a response is not a version it can claim to support, because an
   older cluster simply answers without the extra, which is
   indistinguishable from a Runner identity that never carried one.
   turnip refuses such a stream rather than relaxing the check, so on a
   cluster below v1.32 no operation will ever report its result.
2. **Redis or Valkey, with persistence enabled**: turnip's own manifests
   deliberately don't include one (`deploy/base/kustomization.yaml` ships
   no `TURNIP_REDIS_ADDR` default at all): the Server is stateless, and
   its only state, every Lock and every recorded plan, lives in whichever
   Redis/Valkey instance you point it at. Bring your own (managed or
   self-hosted), but check two things before picking one:

   - **It must persist its data.** Redis without persistence loses
     everything it holds on a restart: every Lock a pull request holds,
     and every plan turnip has recorded for it, gone at once. A pull
     request that held a four-day-old plan has to plan again before it
     can apply, with nothing in the pull request itself hinting that
     happened. Turn on append-only file persistence or periodic
     snapshots, on a volume that survives the Redis Pod being recreated
     (not the Pod's own ephemeral storage); a managed service such as
     Amazon ElastiCache gives you this as a configuration option rather
     than something you set up yourself. Either way, the point is the
     same: **losing Redis's data is indistinguishable, from the pull
     request's side, from turnip forgetting every plan and lock it ever
     held**, so persistence is not an optimization, it's what keeps
     "planned" meaning something between now and when someone applies.
   - **`TURNIP_REDIS_ADDR` is a plain `host:port`, nothing else**. The
     Server connects with `redis.Options{Addr: cfg.RedisAddr}`
     (`cmd/server/main.go`): no password/AUTH field, no TLS. It has to be
     an unauthenticated, non-TLS-only Redis/Valkey reachable from the
     cluster: a bare in-cluster `Deployment`+`Service` works (e.g.
     `TURNIP_REDIS_ADDR=redis.redis-ns.svc.cluster.local:6379` for a
     Service named `redis` in namespace `redis-ns`); a managed instance
     (ElastiCache, Memorystore, etc.) works only if you disable its
     AUTH/in-transit-encryption requirement, or put an unauthenticated
     proxy in front of it. Pointing this at an AUTH- or TLS-required
     endpoint as-is will fail to connect.
3. **Public HTTPS ingress for the webhook path**: turnip ships no
   Ingress, Gateway API resource, or LoadBalancer Service (`deploy/base`
   has only a plain `ClusterIP` one, `deploy/base/service.yaml`). GitHub
   needs to reach the Server's `/github/webhook` path over HTTPS (the root
   path returns 404), so you need your own
   Ingress/Gateway/LoadBalancer (whatever your cluster already uses)
   routing a real, publicly-resolvable hostname to the `turnip-server`
   Service's HTTP port (`8080` by default), plus DNS and a TLS
   certificate for that hostname. See
   [The GitHub App](github-app.md#webhook)'s Webhook URL section for the
   exact shape this feeds into (`https://<your-hostname>/github/webhook`).
   Route `/healthz` too if you want the public check in
   [Verify your deployment](#verify-your-deployment) to work.
4. **A GitHub App**, installed on whichever repositories should trigger
   turnip. You'll need its App ID, its private key (PEM), and a webhook
   secret you choose yourself. See [The GitHub App](github-app.md) for
   the exact permissions, webhook events, and how to generate the key.

## Installing turnip

### With Kustomize (recommended)

Rather than applying a static file and then patching in your
environment's values with follow-up `kubectl` commands, point your own
`kustomization.yaml` at turnip's released base and let Kustomize generate
everything (the Deployment, Service, RBAC, your config, and your
credentials Secret) as one consistent set, applied in one command.

**Step 1**: in a directory of your choice (ideally one you keep under
version control), reference turnip's base as a remote resource, pinned to
a real release tag (never an unpinned branch), so an upgrade is a
deliberate edit to this file, not something that happens underneath you:

```yaml
# kustomization.yaml
resources:
  - github.com/ivanvc/turnip//deploy/base?ref=vX.Y.Z
```

**Step 2**: set your Server's own image tag to match, and three
ConfigMap values: the two that have no sane shared default (every
environment's Redis and GitHub App are different, so the base ships
neither), and the Runner image, which the base presets to `:latest` and
you pin to the same release tag as the Server:

```yaml
images:
  - name: ghcr.io/ivanvc/turnip-server
    newTag: vX.Y.Z

configMapGenerator:
  - name: turnip-server-config
    behavior: merge
    literals:
      - TURNIP_REDIS_ADDR=<your-redis-host>:6379
      - TURNIP_GITHUB_APP_ID=<your-github-app-id>
      - TURNIP_RUNNER_IMAGE=ghcr.io/ivanvc/turnip-runner:vX.Y.Z
```

Every other setting either comes preset by `deploy/base` or is optional;
[Server settings](server-settings.md) lists them all. Two of the optional ones are opt-ins the base
leaves deliberately unset, and each widens what a pull request can
choose: `TURNIP_ALLOWED_OVERRIDES` (letting `turnip.yaml` pick the
Runner's ServiceAccount or its submodule depth) and `TURNIP_ALLOWED_IMAGES`
(letting `uses:` name a tool image other than the built-in aliases, such
as one with a cloud CLI added). Add them to the same `literals:` only
after reading what each costs in [`SECURITY.md`](../../SECURITY.md).

**Choosing a namespace.** `deploy/base` sets no namespace of its own, and
assumes `default`: it presets `TURNIP_K8S_NAMESPACE=default` (where
Runner Jobs are created, which must be the namespace the Server's Role is
in), and its TokenReview ClusterRoleBinding names the Server's
ServiceAccount in `default`. To install anywhere else, set `namespace:`
in your `kustomization.yaml`, which Kustomize applies to every resource
and to that binding's subject, and set `TURNIP_K8S_NAMESPACE` to the same
value in the `literals:` above: Kustomize does not rewrite a ConfigMap
value.

```yaml
namespace: turnip

configMapGenerator:
  - name: turnip-server-config
    behavior: merge
    literals:
      # ...the values from step 2, plus:
      - TURNIP_K8S_NAMESPACE=turnip
```

**Step 3**: generate the GitHub App credentials Secret from local files:
`files:`, not `literals:`, for both, so the actual secret material never
has to appear inline in this YAML at all (keep those two files outside
version control; if this `kustomization.yaml` itself lives in a Git
repo, add them to its `.gitignore`):

```yaml
secretGenerator:
  - name: turnip-github-app
    files:
      - webhook-secret=./secrets/webhook-secret.txt
      - private-key=./secrets/private-key.pem
```

**Step 4**: deploy. Kustomize's generators mean the Deployment's
existing `secretKeyRef`/`configMapRef` names are automatically rewritten
to match what gets generated, so there's nothing left to patch by hand:

```sh
kubectl apply -k .
```

### Quick install (single file)

If you'd rather not write any Kustomize yourself, every
[GitHub Release](https://github.com/ivanvc/turnip/releases) also ships a
single, already-rendered manifest, `turnip-install.yaml`, built by CI
from `deploy/overlays/release/` with that release's own image tag already
baked in:

```sh
# A specific version:
kubectl apply -f https://github.com/ivanvc/turnip/releases/download/vX.Y.Z/turnip-install.yaml

# Always the latest release:
kubectl apply -f https://github.com/ivanvc/turnip/releases/latest/download/turnip-install.yaml
```

Unlike the Kustomize path above, this one *does* need follow-up
imperative commands: a single static file can't generate anything at
apply time, so the two required ConfigMap values and the credentials
Secret have to be layered on afterward:

```sh
kubectl set env deployment/turnip-server \
  TURNIP_REDIS_ADDR=<your-redis-host>:6379 \
  TURNIP_GITHUB_APP_ID=<your-github-app-id>

kubectl create secret generic turnip-github-app \
  --from-literal=webhook-secret=<your-webhook-secret> \
  --from-file=private-key=<path-to-your-app-private-key.pem>
```

Order doesn't matter between the three commands above: the Pod won't
come up `Ready` until all of it is in place, and it'll retry on its own
until then.

The single file assumes the `default` namespace, as `deploy/base` does
(see "Choosing a namespace" under
[With Kustomize](#with-kustomize-recommended)), so apply
it and run the commands above with `-n default`, or use the Kustomize
path to install anywhere else.

## Verify your deployment

The Server exposes `/healthz` (always 200 once the process is up) and
`/readyz` (200 only when it can currently reach Redis). Port-forward and
check both (add `-n <namespace>` to every `kubectl` command here if you
installed outside your current namespace):

```sh
kubectl port-forward svc/turnip-server 8080:8080 &
curl -i localhost:8080/healthz   # expect: HTTP/1.1 200 OK
curl -i localhost:8080/readyz    # expect: HTTP/1.1 200 OK once Redis is reachable
```

A `503` from `/readyz` with a `200` from `/healthz` means the process is
up but can't currently reach Redis. Check `TURNIP_REDIS_ADDR` and that
your Redis is actually reachable from the cluster. If neither endpoint
responds at all, check the Server Pod:

- **A missing `turnip-github-app` Secret** (step 3 above): the container
  is never created, so there is nothing in `kubectl logs`. The Pod shows
  `CreateContainerConfigError`; `kubectl describe pod` (or
  `kubectl get events`) names the missing Secret.
- **A missing `TURNIP_REDIS_ADDR` or `TURNIP_GITHUB_APP_ID`**: the
  process starts and exits at once, and the Pod goes into
  `CrashLoopBackOff`. `kubectl logs` shows "missing required environment
  variable(s): ..." naming every one that is unset.

The port-forward check above only proves the Pod itself works; it says
nothing about the public ingress from Prerequisites #3. Confirm that
separately, from outside the cluster: `curl -i https://<your-hostname>/healthz`
should also return `200`. Once it does, go set the GitHub App's Webhook
URL to `https://<your-hostname>/github/webhook` and check **Active**
([The GitHub App](github-app.md#webhook)'s Webhook section); that's the
last piece needed for GitHub to actually start delivering events.

Then confirm a delivery arrived. The App's **Advanced** tab lists recent
deliveries with the response each got: GitHub's `ping`, if it sent one
when the webhook was set up, and otherwise the event from opening or
pushing to a pull request in the repository. Each should show `200`. A
`401` means the webhook secret the App sends does not match
`TURNIP_GITHUB_WEBHOOK_SECRET`; a `404` means the URL's path is not
`/github/webhook`.

### Upgrading an installation created before the webhook moved

turnip used to serve webhooks at the root path. If your GitHub App still
points at `https://<your-hostname>/`, its deliveries will start returning
404 as soon as you deploy a version with this change: the root path is no
longer served at all.

**There is no ordering that avoids a short gap.** Updating the App first
sends deliveries to a path the running Server does not yet serve; deploying
first leaves the App pointing at a path the new Server no longer serves.
Either way the window is however long the two steps are apart.

The gap is recoverable, so keep it short rather than trying to eliminate
it:

1. Deploy the new version.
2. Change the App's **Webhook URL** to
   `https://<your-hostname>/github/webhook`.
3. Redeliver anything that failed in between. GitHub keeps recent
   deliveries under the App's **Advanced** tab, each with a **Redeliver**
   button.

A delivery that 404s is not retried by GitHub on its own, so step 3 is how
a pull request that was opened mid-window gets its plan.

## What turnip asks of your cluster

Two grants, in `deploy/base`:

| Scope | Grant | Why |
|---|---|---|
| Namespaced (`Role`) | `jobs` create/delete/get/list/watch, `pods` get/list | dispatching Runners, diagnosing ones that never start, and resolving which Pod belongs to which operation |
| Cluster (`ClusterRole`) | `tokenreviews` create | validating a Runner identity against turnip's two audiences (clone and report) |

The `tokenreviews` grant is turnip's only cluster-scoped ask, and it has
to be: `TokenReview` is a cluster-scoped API, so the permission to create
one cannot live in a namespaced Role. turnip ships its own narrow
`ClusterRole` rather than binding the conventional `system:auth-delegator`,
which also carries `subjectaccessreviews`; turnip asks the cluster *who
is this*, never *may they do X*.

## Runner-to-Server traffic is not encrypted

Runners report their logs and results to the Server over gRPC, and that
channel is **plaintext** today. Encryption is tracked separately and is
not part of the authentication described in
[Runner access](runner-access.md#how-a-runner-gets-its-github-credential).

What that does and doesn't mean:

- A Runner proves which Pod it is before it can write anything, so
  knowing an operation's id is no longer enough to forge a plan result or
  release a lock. That check does not depend on encryption.
- The Runner identity a Runner presents is scoped to one of turnip's two
  audiences, expires in minutes, and is accepted for exactly one
  operation. It is useless against the Kubernetes API or anything else.
  The report identity (the container that runs the tool) authorizes only
  writing that operation's result; the clone identity (the clone
  container) also authorizes fetching that operation's GitHub credential
  (see
  [Runner access](runner-access.md#how-a-runner-gets-its-github-credential)).
- Someone able to capture pod-to-pod traffic can read your plan and diff
  output, and can capture a Runner identity within its lifetime. A
  captured clone identity can be traded for the operation's GitHub
  credential until it expires, and the GitHub credential itself crosses
  the same channel when the clone fetches it. That requires network
  position on the cluster (node access or `CAP_NET_RAW`) rather than the
  ability to read a Pod spec.

If your cluster carries infrastructure detail you consider sensitive in
plan output, run turnip inside a service mesh that provides transport
encryption, or restrict who can attach to the pod network, until turnip
offers TLS of its own.

## Optional: the Grafana dashboard

`deploy/components/grafana-dashboard` packages turnip's Grafana dashboard
as a ConfigMap labeled `grafana_dashboard: "1"` (the convention most
Grafana operators auto-discover dashboards by). It isn't part of
`turnip-install.yaml`; apply it separately if you want it:

```sh
kubectl apply -k https://github.com/ivanvc/turnip/deploy/components/grafana-dashboard?ref=vX.Y.Z
```

(Or, from a local clone: `kubectl apply -k deploy/components/grafana-dashboard`.)

## Scaling up

The Server is stateless and safe to run at more than one replica: all
state lives in Redis, so there's no coordination between replicas to
configure:

```sh
kubectl scale deployment/turnip-server --replicas=3
```

## Next

Once the Server is up and `/readyz` is green, add a `turnip.yaml` to a
repository the GitHub App is installed on; see
[`turnip.yaml`](../using/turnip-yaml.md) for its schema, and
[Pull requests](../using/pull-requests.md) for what happens once a PR
opens.
