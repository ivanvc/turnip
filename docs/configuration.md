# Configuring turnip

Two things need configuring: the **`turnip.yaml`** each repository carries
(which projects exist, what triggers them) and the **Server itself**
(GitHub App credentials, Redis, Kubernetes namespace — set via environment
variables, normally through the overlay values `docs/deployment.md`
describes).

## `turnip.yaml`

Committed as **`.turnip/config.yaml`**, or as **`turnip.yaml`** in the
repository root. The dedicated directory is checked first; whichever is
found first is used, and the two are never merged. Only the `.yaml`
extension is read — `.yml` is not checked at either location.

`.turnip/` also gives you somewhere to keep files the *tool* reads (an
AWS config, say) next to turnip's own; turnip reads nothing in there
besides `config.yaml`, and a project can point at a sibling through the
fixed workspace path described under "Where the Runner puts things".

```yaml
schemaVersion: v1alpha2
projects:
  - name: web            # optional — defaults to `directory` if omitted
    directory: infra/web
    whenModified:
      - "infra/web/**"
    uses: helmfile@1.7.4
    with:
      environment: staging
    runner:
      env:
        AWS_PROFILE: web-deployer
```

| Field | Required | Notes |
|---|---|---|
| `schemaVersion` (top-level) | **yes** | the version of this file's schema — must be `v1alpha2`; see below |
| `projects[].name` | no | defaults to `directory`; must be unique across the file once defaulted |
| `projects[].directory` | **yes** | the tool's working directory, relative to the repo root |
| `projects[].uses` | **yes** | what to run: `<tool>` or `<tool>@<version>` — see below |
| `projects[].whenModified` | no | a list of glob patterns (full `**` support — [doublestar](https://github.com/bmatcuk/doublestar) syntax); a PR whose changed files match none of a project's patterns never triggers it |
| `projects[].with` | no | how to call it: configuration the tool itself reads — see below |
| `projects[].runner` | no | where it runs: settings for the Runner Pod — see below |

Every violation across the whole file is reported together (a typo in
project 3 doesn't hide a missing `directory` in project 1) — you'll see
every problem in one pass, not one-at-a-time.

**A key turnip doesn't recognize is an error, not a silent no-op.** Every
field above is checked by name, so a misspelling or a setting written at
the wrong level fails the check with the offending key and its line
number rather than being read and quietly discarded. The one deliberate
exception is inside `with`, which exists precisely to carry keys turnip
does not define.

### `schemaVersion`

Required, and exactly one value is accepted today: `v1alpha2`. A file
missing it, or carrying anything else, is rejected naming both what it
found and what this turnip supports — and it is reported *on its own*,
without also listing every field the older schema used, since the version
is the one fact that explains them.

"Version" means three unrelated things around turnip, so to be explicit
about which one this is:

| | What it versions |
|---|---|
| `schemaVersion` (top-level) | the shape of this file — this field |
| the `@version` in `uses` (per project) | which release of `terraform`/`helmfile`/`pulumi` the Runner provisions |
| turnip's own release | the Server and Runner images you deploy |

The `alpha` suffix is load-bearing, not decoration. Before turnip 1.0 the
schema is expected to break, and advancing within alpha (`v1alpha2`,
`v1alpha3`, …) costs no turnip release and promises nobody a migration
window. It graduates to `v1` at turnip 1.0, at which point the schema
version and the project's major version coincide. There is no
compatibility shim in the meantime: a file on an older schema is
rejected outright, never silently upgraded.

### `uses` — what to run

`<tool>`, or `<tool>@<version>`:

```yaml
    uses: helmfile              # the documented default version
    uses: helmfile@1.7.4        # pinned
    uses: terraform@v1.9.5      # a leading "v" is accepted and normalized
```

The tool must be one of `terraform`, `pulumi`, `helmfile` (see "Tool
support" below).

Omit the version to get turnip's current default for that tool. When you
do pin one, turnip keeps no list of "supported" versions to validate
against — any value shaped like a real version (roughly semver: `1.9.5`,
`0.170.1`, `1.7.4-rc1`) is accepted and passed straight to the vendor's
own per-version image, so a tool's new release works the moment the
vendor publishes it, with no turnip release required.

Malformed input is rejected when the file is parsed, so it appears as a
validation error on the pull request rather than as a failed Job. That
includes **floating tags**: `latest` is not accepted, because the same
version has to still be there when the plan you approved is applied
later. If a version is well-formed but the vendor doesn't publish that
tag, the Job fails when its initContainer can't pull the image —
reported as a normal operation failure, not caught ahead of time.

### `with` — how to call it

Configuration for the tool itself. Nothing turnip reads lives here: every
key is passed to the plugin for that tool, which is why unrecognized keys
inside `with` are accepted rather than rejected.

| Key | Tool | Meaning |
|---|---|---|
| `environment` | Helmfile | helmfile's own `--environment` flag |
| `workspace` | Terraform | the Terraform workspace name |
| `backendConfig` | Terraform | backend configuration for the tool's initialization step |
| `stack` | Pulumi | the Pulumi stack name |

### `runner` — where it runs

Settings that shape the Runner Pod rather than the tool inside it:

```yaml
    runner:
      serviceAccount: turnip-runner
      env:
        AWS_PROFILE: web-deployer
```

- **`serviceAccount`**: the Kubernetes ServiceAccount this Project's
  Runner Pod runs as — the identity EKS Pod Identity/IRSA maps to an IAM
  role, and the one in-cluster API calls authenticate with. **Only
  honored when the Server's `TURNIP_ALLOWED_OVERRIDES` includes
  `runner.serviceAccount`**; otherwise the operation is refused with a
  comment on the PR and no Runner Job is created. The gate exists because
  turnip reads this file from the pull request's own head commit, and a
  plan needs only collaborator access — without it, anyone able to open a
  PR could pick any ServiceAccount in the Runner namespace and use its
  permissions.
- **`env`**: environment variables for the IaC tool's process. Unlike
  `with`, these are never interpreted by turnip at all — they are simply
  present in the environment the tool runs in, which makes this the place
  for anything the tool's own ecosystem reads.

Two kinds of `env` name are rejected, with every offending name in the
file reported together rather than one per attempt:

- **anything beginning with `TURNIP_`** — the Runner reads its own
  configuration out of that namespace, so a Project setting one would be
  reconfiguring the Runner rather than the tool.
- **`PATH`** — the Runner composes it at startup so the provisioned tool
  binary is found first; replacing it wholesale would hide the very
  binary the operation needs.

Values reach the tool byte-for-byte, including values containing `$(…)`
— turnip escapes them on the way through, since Kubernetes would
otherwise expand `$(VAR)` inside an environment value before the tool
ever saw it.

One thing worth being deliberate about: this file is read from the pull
request's own head commit, so `runner.env` is something a PR author can
change. That is the same trust boundary the tool's own committed
configuration already sits on — a `.tf` or helmfile in the branch can
redirect a backend or a role just as readily — but if that boundary
matters to you, it's the Server-side settings below, not `env`, that
decide what credentials the Runner holds in the first place.

### Where the Runner puts things

Everything turnip creates inside a Runner Pod lives under `/turnip`:

| Path | Contents |
|---|---|
| `/turnip/src` | the repository, cloned at the pull request's head commit; a project's `directory` is relative to this |
| `/turnip/tools` | the provisioned IaC tool binary, prepended to the Runner's `PATH` |

`/turnip/src` is a fixed path rather than a per-run temporary directory
specifically so committed configuration may reference it — an absolute
path in a helmfile or a `.tfvars` resolves the same way on every run.
It's the one of the two worth naming directly; `/turnip/tools` is
reachable through `PATH`, and hard-coding it just pins something turnip
may move.

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
| `TURNIP_ALLOWED_OVERRIDES` | no (default: permits nothing) | comma-separated list of the fields a repository's own config file may set. The only path accepted today is `runner.serviceAccount`. The default matches what turnip has always done: a repository cannot choose the identity its Runner assumes, since this file is read from the PR's own head commit. An unrecognized path is a startup error rather than a silently ineffective setting. Note that `uses` is *not* an override — turnip has no Server-side tool to fall back to, so what a project runs is always the repository's to say |

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
