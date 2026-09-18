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

### `clone` — how the repository is checked out

Top-level, beside `projects:` rather than inside one: a pull request
produces a single clone that every matched project then runs in, so this
describes the checkout itself rather than any one project.

```yaml
schemaVersion: v1alpha2

clone:
  submodules: recursive

projects:
  - directory: infrastructure
    uses: helmfile@1.1.7
```

**`submodules`** decides how far submodule initialisation goes:

| Value | Behaviour |
|---|---|
| `none` | submodules are not initialised; the directory stays empty |
| `top-level` | one level of submodules — the default |
| `recursive` | submodules of submodules too |

Not `shallow`: in git that word means a depth-limited fetch, and turnip
deliberately does the opposite — submodules are fetched at full depth, so
the commit the parent pins is certainly present.

**Only honored when the Server's `TURNIP_ALLOWED_OVERRIDES` includes
`clone.submodules`**; otherwise the operation is refused with a comment on
the PR and no Runner Job is created. The gate here is about cost rather
than safety, unlike `runner.serviceAccount`'s — fetching a submodule the
App can already read grants no permission the repository doesn't already
have — but an operator may still have reason to refuse an expensive fetch,
so it reuses the same list rather than inventing a second mechanism.

Submodules are fetched with the same installation token as the repository
itself, and therefore reach **only repositories your GitHub App is
installed on**. A submodule on the repository's own host works whatever
form `.gitmodules` writes it in — SSH, `git://`, an explicit port — because
turnip rewrites those to authenticated HTTPS; it holds no SSH key and
never will. A submodule hosted anywhere else is reported by name *before*
anything is fetched, rather than failing later as a generic authentication
error.

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

### What the Runner's ServiceAccount needs

turnip ships no ServiceAccount and no RBAC for the Runner, deliberately.
The Runner runs *your* IaC, so what it needs is whatever your IaC
manages — and anything turnip shipped would have to carry elevated access
to be useful for anyone. That is a grant an operator should make
knowingly, not one inherited from a manifest they applied in order to
install turnip. `deploy/base` creates only the Server's own
ServiceAccount and Role, which cover creating Jobs and reading Pods and
nothing else.

#### How helmfile authenticates

Inside the cluster turnip runs in there is nothing to configure: the
Runner Pod carries a projected ServiceAccount token, and helm's client
finds it the way any in-cluster client does. No kubeconfig, no cloud
credentials, no `aws eks update-kubeconfig`. *Which* ServiceAccount comes
from `TURNIP_RUNNER_SERVICE_ACCOUNT`, or from a Project's
`runner.serviceAccount` where the operator permits that override.

Targeting any **other** cluster needs a kubeconfig, which turnip does not
yet produce — see the roadmap's Backlog.

Helm plugins authenticate separately, and to different things.
`helm-secrets` decrypting through a cloud KMS, or a chart pulled from a
private registry, needs *cloud* credentials rather than cluster ones.
Those come from the Pod's identity — EKS Pod Identity/IRSA, GCP Workload
Identity, selected by that same ServiceAccount — and from `runner.env`
for whatever the plugin reads out of its environment.

#### What to grant it

**In practice, administrative access to the cluster.** That is not a
recommendation made lightly, so here is why the smaller grants do not
hold.

Helm keeps each release's state in a Secret in the release's namespace, so
even a read-only `diff` must list Secrets there. That is the first failure
most people meet:

```
Error: query: failed to query with labels: secrets is forbidden:
User "system:serviceaccount:<namespace>:<name>" cannot list resource
"secrets" in API group "" in the namespace "<release-namespace>"
```

Granting exactly that gets past exactly that error, and then the next one
arrives — a chart's `lookup`, a CRD, a namespace, a cluster-scoped
resource. **Enumerating minimal permissions for helmfile is a losing
game**: a helmfile manages whatever its charts declare, so the permission
set is the union of every kind any chart touches, and it moves whenever a
chart does. `apply` needs create/update/delete across all of it, and a
repository that installs CRDs or namespaces needs cluster-scoped rights to
match.

Neither built-in shortcut works. `view` excludes Secrets deliberately —
*"reading the contents of Secrets enables access to ServiceAccount
credentials in the namespace, which would allow API access as any
ServiceAccount in the namespace (a form of privilege escalation)"* — so it
cannot even diff. `edit` covers Secrets but not cluster-scoped resources
or CRDs.

For a Runner that applies, bind `cluster-admin` and know that you did:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: turnip-runner
subjects:
  - kind: ServiceAccount
    name: turnip-runner
    namespace: turnip-system
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: cluster-admin
```

A Runner that only ever diffs can be narrower — Secrets plus read on what
the charts consult, bound per namespace — but expect to revisit it as the
charts change.

#### What that grant means

**A plan is not a read-only operation in the security sense.** helmfile's
`prepare` and `cleanup` hooks run for *every* command, `diff` included;
only `presync`/`postsync` are restricted to the mutating ones, which
helmfile documents as the place for commands that may mutate cluster state
"as it will not be run for read-only operations like `lint`, `diff` or
`template`". A hook runs an arbitrary executable inside the Runner
container, and that container holds the ServiceAccount token.

Since a plan runs automatically when a pull request opens, a pull request
that adds a `prepare` hook runs arbitrary code with the Runner's cluster
credentials before anyone reviews it. If those credentials are
`cluster-admin`, so is the pull request.

That is the same trust boundary the committed IaC already sits on — a
chart can redirect a resource as readily as a hook can run one — but it
is sharper than it looks, and two things follow:

- **Run turnip only on repositories whose contributors you already trust
  with the cluster.** Refusing pull requests from forks is a planned slice
  and is **not implemented yet**; until it is, a fork's pull request is
  treated like any other.
- **Scope by cluster, not by rule set.** The practical blast radius is
  decided when you choose which cluster turnip runs in and which
  repository drives it — not by trimming permissions that will grow back
  the next time a chart adds a resource kind.

### Where the Runner puts things

Everything turnip creates inside a Runner Pod lives under `/turnip`:

| Path | Contents | Present when |
|---|---|---|
| `/turnip/src` | the repository, cloned at the pull request's head commit; a project's `directory` is relative to this | always |
| `/turnip/tools` | the provisioned IaC tool binary, prepended to the Runner's `PATH` | copy-out tools only |
| `/turnip/bin` | turnip's own runner binary, handed to the vendor's image | run-in-image tools only |

`/turnip/src` is a fixed path rather than a per-run temporary directory
specifically so committed configuration may reference it — an absolute
path in a helmfile or a `.tfvars` resolves the same way on every run. It's
the one worth naming directly; the other two are turnip's own plumbing and
may move.

The repository is cloned by an initContainer running turnip's image, not
by the container that runs your tool — which is what lets that container
be the tool vendor's own image, needing nothing from it but the tool.

### Tool support

| Tool | Status | Provisioned by | Operations |
|---|---|---|---|
| Helmfile | implemented | run-in-image | `diff` (plan), `apply`, `sync`, `destroy` |
| Terraform | **not yet implemented** | copy-out | recognized by the config parser (won't reject your config), but no Plugin exists yet to actually run it |
| Pulumi | **not yet implemented** | copy-out | same as Terraform |

A project can name `terraform`/`pulumi` today without error, but nothing
will actually trigger for it until support lands.

### How a tool reaches the Runner

Two strategies, chosen per tool by turnip rather than configured:

- **copy-out** — an initContainer copies the tool's binary out of the
  vendor's image onto a shared volume, and turnip's own image runs it.
  Correct for a tool that is a single self-contained binary.
- **run-in-image** — the vendor's image *is* the container that runs, with
  turnip's runner binary handed to it. The tool gets its own image's
  `PATH`, environment and `HOME`, so its helper binaries and plugins are
  simply present.

Helmfile needs the second: it shells out to `helm`, `helmfile diff` needs
the helm-diff plugin, helm-secrets needs `sops`, and helm finds its plugins
through an environment its image sets. Copying one binary out produced
exactly the failure you'd expect — `exec: "helm": executable file not found
in $PATH`.

The practical consequence: **for a run-in-image tool, its plugins and
helpers come from the vendor's image.** Needing one the image doesn't
bundle means choosing an image that has it, not configuring turnip.

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
| `TURNIP_CLONE_SUBMODULES` | no (default `top-level`) | how far the clone initialises submodules: `none`, `top-level`, or `recursive`. Applies to every repository this Server clones, unless one overrides it with `clone.submodules` *and* that path is permitted below. Defaults on, unlike `actions/checkout` — turnip clones specifically to run IaC that may reference submodule paths, so defaulting off would make every repository with a submodule fail confusingly before anything worked. An unrecognized value is a startup error |
| `TURNIP_ALLOWED_OVERRIDES` | no (default: permits nothing) | comma-separated list of the fields a repository's own config file may set. The paths accepted today are `runner.serviceAccount` and `clone.submodules`. The default matches what turnip has always done: a repository cannot choose the identity its Runner assumes, since this file is read from the PR's own head commit. An unrecognized path is a startup error rather than a silently ineffective setting. Note that `uses` is *not* an override — turnip has no Server-side tool to fall back to, so what a project runs is always the repository's to say |

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
