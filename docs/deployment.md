# Deploying turnip

The manifests themselves live under [`deploy/`](../deploy/) as a
Kustomize base plus environment-specific overlays. This guide covers
installing a real, versioned release; `deploy/overlays/kind/` (referenced
later, under "Local development") is a separate, local-testing-only
example — not something to copy as a starting point for a real
deployment.

## Prerequisites

1. **A Kubernetes cluster** — any cluster `kubectl apply -f` can reach.
2. **Redis or Valkey** — turnip's own manifests deliberately don't include
   one (`deploy/base/kustomization.yaml` ships no `TURNIP_REDIS_ADDR`
   default at all): the Server is stateless, and its only state lives in
   whichever Redis/Valkey instance you point it at. Bring your own
   (managed or self-hosted) — but check the constraint below before
   picking one: **`TURNIP_REDIS_ADDR` is a plain `host:port`, nothing
   else**. The Server connects with `redis.Options{Addr:
   cfg.RedisAddr}` (`cmd/server/main.go`) — no password/AUTH field, no
   TLS. It has to be an unauthenticated, non-TLS-only Redis/Valkey
   reachable from the cluster: a bare in-cluster `Deployment`+`Service`
   works (e.g. `TURNIP_REDIS_ADDR=redis.redis-ns.svc.cluster.local:6379`
   for a Service named `redis` in namespace `redis-ns`); a managed
   instance (ElastiCache, Memorystore, etc.) works only if you disable
   its AUTH/in-transit-encryption requirement, or put an unauthenticated
   proxy in front of it — pointing this at an AUTH- or TLS-required
   endpoint as-is will fail to connect.
3. **Public HTTPS ingress for the webhook path** — turnip ships no
   Ingress, Gateway API resource, or LoadBalancer Service (`deploy/base`
   has only a plain `ClusterIP` one, `deploy/base/service.yaml`). GitHub
   needs to reach the Server's `/` path over HTTPS, so you need your own
   Ingress/Gateway/LoadBalancer (whatever your cluster already uses)
   routing a real, publicly-resolvable hostname to the `turnip-server`
   Service's HTTP port (`8080` by default), plus DNS and a TLS
   certificate for that hostname. See
   [`docs/configuration.md`](configuration.md#setting-up-the-github-app)'s
   Webhook URL section for the exact shape this feeds into
   (`https://<your-hostname>/github/webhook`).
4. **A GitHub App** — installed on whichever repositories should trigger
   turnip. You'll need its App ID, its private key (PEM), and a webhook
   secret you choose yourself. See
   [`docs/configuration.md`](configuration.md#setting-up-the-github-app)
   for the exact permissions, webhook events, and how to generate the key.

## Installing turnip

### With Kustomize (recommended)

Rather than applying a static file and then patching in your
environment's values with follow-up `kubectl` commands, point your own
`kustomization.yaml` at turnip's released base and let Kustomize generate
everything — the Deployment, Service, RBAC, your config, and your
credentials Secret — as one consistent set, applied in one command.

**Step 1**: in a directory of your choice (ideally one you keep under
version control), reference turnip's base as a remote resource, pinned to
a real release tag — never an unpinned branch, so an upgrade is a
deliberate edit to this file, not something that happens underneath you:

```yaml
# kustomization.yaml
resources:
  - github.com/ivanvc/turnip//deploy/base?ref=vX.Y.Z
```

**Step 2**: set your Server's own image tag to match, and the two
ConfigMap values that have no sane shared default — every environment's
Redis and GitHub App are different, so the base ships neither:

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

**Step 3**: generate the GitHub App credentials Secret from local files —
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

**Step 4**: deploy — Kustomize's generators mean the Deployment's
existing `secretKeyRef`/`configMapRef` names are automatically rewritten
to match what gets generated, so there's nothing left to patch by hand:

```sh
kubectl apply -k .
```

### Quick install (single file)

If you'd rather not write any Kustomize yourself, every
[GitHub Release](https://github.com/ivanvc/turnip/releases) also ships a
single, already-rendered manifest — `turnip-install.yaml` — built by CI
from `deploy/overlays/release/` with that release's own image tag already
baked in:

```sh
# A specific version:
kubectl apply -f https://github.com/ivanvc/turnip/releases/download/vX.Y.Z/turnip-install.yaml

# Always the latest release:
kubectl apply -f https://github.com/ivanvc/turnip/releases/latest/download/turnip-install.yaml
```

Unlike the Kustomize path above, this one *does* need follow-up
imperative commands — a single static file can't generate anything at
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

Order doesn't matter between the three commands above — the Pod won't
come up `Ready` until all of it is in place, and it'll retry on its own
until then.

## Verify your deployment

The Server exposes `/healthz` (always 200 once the process is up) and
`/readyz` (200 only when it can currently reach Redis). Port-forward and
check both:

```sh
kubectl port-forward svc/turnip-server 8080:8080 &
curl -i localhost:8080/healthz   # expect: HTTP/1.1 200 OK
curl -i localhost:8080/readyz    # expect: HTTP/1.1 200 OK once Redis is reachable
```

A `503` from `/readyz` with a `200` from `/healthz` means the process is
up but can't currently reach Redis — check `TURNIP_REDIS_ADDR` and that
your Redis is actually reachable from the cluster. If neither endpoint
responds at all, check `kubectl get pods`/`kubectl logs` for the Server
Pod — a missing `turnip-github-app` Secret (above) fails at startup with
a clear "missing required environment variable(s)" message.

The port-forward check above only proves the Pod itself works — it says
nothing about the public ingress from Prerequisites #3. Confirm that
separately, from outside the cluster: `curl -i https://<your-hostname>/healthz`
should also return `200`. Once it does, go set the GitHub App's Webhook
URL to `https://<your-hostname>/github/webhook` and check **Active**
(`docs/configuration.md`'s Webhook section) — that's the last piece
needed for GitHub to actually start delivering events.

### Upgrading an installation created before the webhook moved

turnip used to serve webhooks at the root path. If your GitHub App still
points at `https://<your-hostname>/`, its deliveries will start returning
404 as soon as you deploy a version with this change — the root path is no
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
3. Redeliver anything that failed in between — GitHub keeps recent
   deliveries under the App's **Advanced** tab, each with a **Redeliver**
   button.

A delivery that 404s is not retried by GitHub on its own, so step 3 is how
a pull request that was opened mid-window gets its plan.

## Optional: the Grafana dashboard

`deploy/components/grafana-dashboard` packages turnip's Grafana dashboard
as a ConfigMap labeled `grafana_dashboard: "1"` (the convention most
Grafana operators auto-discover dashboards by). It isn't part of
`turnip-install.yaml` — apply it separately if you want it:

```sh
kubectl apply -k https://github.com/ivanvc/turnip/deploy/components/grafana-dashboard?ref=vX.Y.Z
```

(Or, from a local clone: `kubectl apply -k deploy/components/grafana-dashboard`.)

## Scaling up

The Server is stateless and safe to run at more than one replica — all
state lives in Redis, so there's no coordination between replicas to
configure:

```sh
kubectl scale deployment/turnip-server --replicas=3
```

## Local development

`deploy/overlays/kind/` is a complete, runnable example for a local
[kind](https://kind.sigs.k8s.io/) cluster — useful for trying turnip out
or working on it, not for a real deployment (it pins a locally-built
`dev` image tag and a placeholder GitHub App ID). From a clone of this
repository:

```sh
kubectl create secret generic turnip-github-app \
  --from-literal=webhook-secret=<your-webhook-secret> \
  --from-file=private-key=<path-to-your-app-private-key.pem>

# A Redis/Valkey the overlay can reach as "redis:6379" (test/load/redis.yaml
# is a minimal example) — or edit the overlay's TURNIP_REDIS_ADDR.

kubectl apply -k deploy/overlays/kind
```

## Next

Once the Server is up and `/readyz` is green, add a `turnip.yaml` to a
repository the GitHub App is installed on — see
[`docs/configuration.md`](configuration.md) for its schema, and
[`docs/usage.md`](usage.md) for what happens once a PR opens.
