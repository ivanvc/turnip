# Getting started (operators)

> **For operators**: deploying and configuring the Server. End users: see
> [Getting started](end-users.md).

From nothing to a Server receiving webhooks from one repository. Each
step links to the page that explains it; this page only orders them.

1. **Create the GitHub App and install it on the repository** you want
   turnip to watch. turnip authenticates to GitHub as an App, never as a
   user or a personal access token. Note its App ID, private key and
   webhook secret, and leave its webhook inactive for now (step 8 turns
   it on). See [The GitHub App](../operating/github-app.md).
2. **Provide Redis or Valkey, with persistence enabled.** The Server is
   stateless; every Lock and every recorded plan lives in whichever
   Redis/Valkey instance you point it at, and losing that data drops
   every plan a pull request is holding, which then has to plan again
   before it can apply. See
   [Prerequisites](../operating/deployment.md#prerequisites).
3. **Create the `turnip-system` namespace and install turnip with
   Kustomize**, pointed at a released version tag. `deploy/base` runs the
   Server and its Runner Jobs in that one namespace; your
   `kustomization.yaml` sets `namespace:` and lists the `namespaces`
   transformer. Separating the Runner namespace is recommended, so that a
   Runner's grants cannot reach the GitHub App's private key: see
   [Choosing namespaces](../operating/deployment.md#choosing-namespaces),
   and [Installing turnip](../operating/deployment.md#installing-turnip)
   for the rest.
4. **Set the required settings**: `TURNIP_REDIS_ADDR`,
   `TURNIP_GITHUB_APP_ID`, the GitHub App credentials from step 1, and
   `TURNIP_RUNNER_IMAGE` pinned to the same release tag as the Server.
   The other required settings come preset by `deploy/base`, and the rest
   are optional; see [Server settings](../operating/server-settings.md).
   Put the GitHub App's webhook secret and private key in the
   `turnip-server-secrets` Secret, and every setting in `turnip-server-config`;
   after changing either, run
   `kubectl rollout restart deployment/turnip-server`. Installing with
   Helmfile instead of Kustomize:
   [Installing with Helmfile](../operating/deployment.md#installing-with-helmfile).
5. **Create a ServiceAccount for the Runner** in the Runner namespace, set
   `TURNIP_RUNNER_SERVICE_ACCOUNT` to its name, and grant it whatever
   your infrastructure projects need. Left unset, Runners run as the
   Runner namespace's `default` ServiceAccount. turnip ships no ServiceAccount
   and no RBAC for the Runner, deliberately: what it needs depends on the
   tool your projects run. See
   [Which ServiceAccount](../operating/runner-access.md#which-serviceaccount)
   and
   [What the Runner's ServiceAccount needs](../operating/runner-access.md#what-the-runners-serviceaccount-needs).
6. **Expose the webhook path.** Route `https://<your-hostname>/github/webhook`
   (and `/healthz`, for the check in step 7) to the `turnip-server`
   Service. See [Prerequisites](../operating/deployment.md#prerequisites).
7. **Verify the Server.** Check `/healthz` and `/readyz` through a
   port-forward, then confirm the public hostname answers `/healthz` too.
   See [Verify your deployment](../operating/deployment.md#verify-your-deployment).
8. **Turn on the webhook and confirm a delivery.** Set the App's Webhook
   URL to `https://<your-hostname>/github/webhook` and check **Active**
   (see [the Webhook section](../operating/github-app.md#webhook)). Then,
   in the App's **Advanced** tab, check that a delivery got `200`; a
   `401` means the webhook secret does not match. See
   [Verify your deployment](../operating/deployment.md#verify-your-deployment).

## Next

- [Deploying turnip](../operating/deployment.md): prerequisites,
  upgrading, scaling, and what turnip asks of your cluster
- [Server settings](../operating/server-settings.md): every `TURNIP_*`
  setting, its default and whether it's required
- [Runner access](../operating/runner-access.md): the ServiceAccount,
  cloud credentials, custom images
- [Helmfile](../plugins/helmfile.md): what its Runner needs, the one
  tool turnip runs today
