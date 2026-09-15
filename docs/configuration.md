# Configuring turnip

Two things need configuring: the **`turnip.yaml`** each repository carries
(which projects exist, what triggers them) and the **Server itself**
(GitHub App credentials, Redis, Kubernetes namespace — set via environment
variables, normally through the overlay values `docs/deployment.md`
describes).

## `turnip.yaml`

Committed to the repository root (or `.github/turnip.yaml` — checked only
if the root file is missing, never both merged):

```yaml
version: 1
projects:
  - name: web            # optional — defaults to `directory` if omitted
    directory: infra/web
    tool: helmfile
    whenModified:
      - "infra/web/**"
    config:
      environment: staging
```

| Field | Required | Notes |
|---|---|---|
| `version` (top-level) | — | parsed, not currently validated against anything |
| `projects[].name` | no | defaults to `directory`; must be unique across the file once defaulted |
| `projects[].directory` | **yes** | the tool's working directory, relative to the repo root |
| `projects[].tool` | **yes** | one of `terraform`, `pulumi`, `helmfile` — see "Tool support" below |
| `projects[].whenModified` | no | a list of glob patterns (full `**` support — [doublestar](https://github.com/bmatcuk/doublestar) syntax); a PR whose changed files match none of a project's patterns never triggers it |
| `projects[].config` | no | a free-form string map, tool-specific — see below |

Every violation across the whole file is reported together (a typo in
project 3 doesn't hide a missing `directory` in project 1) — you'll see
every problem in one pass, not one-at-a-time.

### `config` map — recognized keys

- **`version`**: pins which release of the tool binary the Runner
  provisions (e.g. `terraform 1.9.5`, not whatever `:latest` happens to
  be). Omit it to get the current default. turnip keeps no list of
  "supported" versions to validate against — any value that looks like a
  real version (roughly semver: `1.9.5`, `0.170.1`, `1.7.4-rc1`) is
  accepted and passed straight through to the vendor's own per-version
  image, so a tool's new release works the moment the vendor publishes it,
  with no turnip release required. Only obviously malformed input (a
  floating tag like `latest`, a typo, stray whitespace) is rejected at
  Job-build time; if the value is well-formed but the vendor doesn't
  actually publish that tag, the Job fails when its initContainer can't
  pull the image — reported as a normal operation failure, not caught
  ahead of time.
- **`environment`** (Helmfile only): passed through as helmfile's own
  `--environment` flag.
- **`serviceAccount`**: the Kubernetes ServiceAccount this Project's
  Runner Pod runs as — the identity EKS Pod Identity/IRSA maps to an IAM
  role, and the one in-cluster API calls authenticate with. **Only
  honored when the Server sets
  `TURNIP_RUNNER_SERVICE_ACCOUNT_ALLOW_FROM_CONFIG=true`**; otherwise the
  operation is refused with a comment on the PR and no Runner Job is
  created. The gate exists because turnip reads `turnip.yaml` from the
  pull request's own head commit, and a plan needs only collaborator
  access — without it, anyone able to open a PR could pick any
  ServiceAccount in the Runner namespace and use its permissions.

Anything else in `config` is opaque to turnip itself — a plugin only
reads the keys it understands.

### Tool support

| Tool | Status | Operations |
|---|---|---|
| Helmfile | implemented | `diff` (plan), `apply`, `sync`, `destroy` |
| Terraform | **not yet implemented** | recognized by the config parser (won't reject your `turnip.yaml`), but no Plugin exists yet to actually run it |
| Pulumi | **not yet implemented** | same as Terraform |

A `turnip.yaml` project can name `terraform`/`pulumi` today without
error, but nothing will actually trigger for it until support lands.

## The Server's own configuration

Read from environment variables at startup (`internal/orchestrator.ConfigFromEnv`)
— fails fast, naming every missing required variable together, rather
than one restart-and-discover-the-next-one at a time.

| Variable | Required | Purpose |
|---|---|---|
| `TURNIP_GITHUB_APP_ID` | yes | your GitHub App's numeric ID |
| `TURNIP_GITHUB_PRIVATE_KEY` or `TURNIP_GITHUB_PRIVATE_KEY_PATH` | yes (one of) | the App's private key — inline PEM or a file path |
| `TURNIP_GITHUB_WEBHOOK_SECRET` | yes | the secret your GitHub App's webhook is configured with (HMAC-verified on every delivery) |
| `TURNIP_REDIS_ADDR` | yes | Redis/Valkey address — the Server's only state store |
| `TURNIP_K8S_NAMESPACE` | yes | namespace Runner Jobs are created in |
| `TURNIP_HTTP_ADDR` | yes | HTTP bind address (webhooks, `/healthz`, `/readyz`, `/metrics`) |
| `TURNIP_GRPC_ADDR` | yes | gRPC bind address (Runner→Server log/result streaming) |
| `TURNIP_RUNNER_SERVER_ADDR` | yes | address a Runner Pod dials to reach this Server — a Service DNS name, not the bind address above |
| `TURNIP_RUNNER_IMAGE` | yes | the Runner container image a deployed Server creates Jobs with |
| `TURNIP_MINIMIZE_OUTDATED_PLAN_COMMENTS` | no (default `false`) | collapse an older plan comment on the same PR once a newer one supersedes it |
| `TURNIP_RUNNER_SERVICE_ACCOUNT` | no (default unset) | ServiceAccount every Runner Pod runs as — how a Runner gets cloud credentials (EKS Pod Identity/IRSA) and in-cluster API permissions. Unset leaves Pods on the namespace's `default` ServiceAccount, which normally has neither |
| `TURNIP_RUNNER_SERVICE_ACCOUNT_ALLOW_FROM_CONFIG` | no (default `false`) | allow a Project's `config.serviceAccount` to override the above. Off by default: `turnip.yaml` is read from the PR's own head commit, so enabling this lets any PR author choose which ServiceAccount their Runner uses |

In `deploy/base`, the two credential-shaped values
(`TURNIP_GITHUB_WEBHOOK_SECRET`, `TURNIP_GITHUB_PRIVATE_KEY`) come from a
Secret; everything else comes from a ConfigMap an overlay fills in — see
`docs/deployment.md`'s "Installing turnip" section for the two values
(`TURNIP_REDIS_ADDR`, `TURNIP_GITHUB_APP_ID`) a real deployment always
needs to set itself, whichever install path you use.

The Runner container's own environment variables (`TURNIP_OPERATION_ID`,
`TURNIP_REPO_URL`, `TURNIP_GITHUB_TOKEN`, and so on) are internal
plumbing the Server sets automatically on every Job it creates — nothing
here for an operator to configure directly.

## Setting up the GitHub App

Start the creation form under the GitHub organization/account whose
repositories should trigger turnip: **Settings → Developer settings →
GitHub Apps → New GitHub App**. The form has several sections turnip
doesn't use at all — covered below so nothing is left ambiguous.

**GitHub App name**: required, and must be unique across *all* of
GitHub (not just your org) — if your first choice is taken, add a
suffix. This name is only ever visible in GitHub's own UI (installed-App
lists, PR check-run author, etc.); it never ends up in this repository,
so name it however makes sense to you. For example,
`turnip [your GitHub organization]`.

**Description**: optional — skip it.

**Homepage URL**: required by the form, but turnip has no user-facing
page. Point it at this repository (`https://github.com/ivanvc/turnip`)
or your own org's site — the value isn't otherwise meaningful to turnip.

**Identifying and authorizing users** (Callback URL, "Expire user
authorization tokens", "Request user authorization (OAuth) during
installation", "Enable Device Flow"): turnip has no user-facing login —
it never initiates a user OAuth flow. Leave the Callback URL field
blank and every checkbox in this section unchecked (the form's
defaults).

**Post installation** (Setup URL, "Redirect on update"): also unused —
leave the Setup URL blank and the checkbox unchecked.

**Webhook**:
- **Active**: check this once you have the URL described next; if you
  don't yet, **uncheck it instead of typing a placeholder** — GitHub
  only requires the URL field when Active is checked, and you can come
  back to this same settings page to check Active and fill in the real
  URL once it exists.
- **Webhook URL**: `https://<your-hostname>/` — the root path (`/`),
  not `/webhook` or any other sub-path; `<your-hostname>` is a real,
  publicly-resolvable DNS name that routes HTTPS traffic to the
  `turnip-server` Service's HTTP port (`8080` by default,
  `TURNIP_HTTP_ADDR`). **turnip provisions none of this for you** —
  `deploy/base` has no Ingress, Gateway API, or LoadBalancer Service in
  it, only a plain `ClusterIP` Service (`deploy/base/service.yaml`).
  You need your own Ingress/Gateway/LoadBalancer resource (whatever
  your cluster already uses for exposing HTTP services) routing to that
  Service, plus a DNS record and a TLS certificate for the hostname —
  e.g. `https://turnip.example.com/` if your ingress controller fronts
  that hostname and forwards to `turnip-server:8080`. None of that is
  part of this repository; it's entirely your own cluster's ingress
  setup.
- **Webhook secret**: type in a secret you generate yourself (e.g.
  `openssl rand -hex 32`) — this exact value is what
  `TURNIP_GITHUB_WEBHOOK_SECRET` must match, so generate it first and
  paste the same string into both places.

**Permissions** (inferred directly from the GitHub API calls turnip
makes — double-check against the current GitHub App permissions UI when
you create the App, since GitHub occasionally renames these). Expand
only **Repository permissions**; leave Organization permissions and
Account permissions untouched (every entry "No access"):

| Permission | Level | Why |
|---|---|---|
| Checks | Read & write | creating/updating the per-project check runs |
| Contents | Read-only | fetching `turnip.yaml` and diffing changed files |
| Issues | Read & write | GitHub represents PR comments as issue comments under the hood — this is what lets turnip post and edit them |
| Pull requests | Read & write | reading PR metadata, listing changed files |

**Subscribe to events**: each repository permission above unlocks its
matching event checkbox in this list once set — check `Pull request`
and `Issue comment` (nothing else).

**Where can this GitHub App be installed?**: choose **Only on this
account** — "Any account" would let *any* GitHub user install this App
on their own repositories, which is never what you want for turnip.

Click **Create GitHub App**. You land on the App's own settings page —
install it on the specific repositories you want from here (or from
**Install App** in the sidebar), and note the App ID shown near the top
(`TURNIP_GITHUB_APP_ID`).

**Generate a private key**: on this same settings page (General tab),
scroll to the **Private keys** section — below Webhook and Permissions,
and distinct from the **Client secrets** section just above it (turnip
authenticates as the App via JWT, not OAuth, so it needs the private
key, not a client secret). Click **Generate a private key**; your
browser immediately downloads a `.pem` file. This is a one-time
download — GitHub never shows or re-offers the raw key again (a lost
key means generating a new one; there's no `gh` CLI equivalent for this
step). Move the downloaded file somewhere private, outside any git
repo — you'll pass its path as `TURNIP_GITHUB_PRIVATE_KEY_PATH` (or
inline its contents as `TURNIP_GITHUB_PRIVATE_KEY`), and it's the same
file `docs/deployment.md`'s `kubectl create secret` command consumes.

Who can actually *use* an installed App is a separate, per-comment
authorization check — see `docs/usage.md`'s "Who can trigger what"
section.
