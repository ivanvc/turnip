# Configuring turnip

Two things need configuring: the **`turnip.yaml`** each repository carries
(which projects exist, what triggers them) and the **Server itself**
(GitHub App credentials, Redis, Kubernetes namespace; set via environment
variables, normally through the overlay values `docs/deployment.md`
describes).

## `turnip.yaml`

Committed as **`.turnip/config.yaml`**, or as **`turnip.yaml`** in the
repository root. The dedicated directory is checked first; whichever is
found first is used, and the two are never merged. Only the `.yaml`
extension is read; `.yml` is not checked at either location.

`.turnip/` also gives you somewhere to keep files the *tool* reads (an
AWS config, say) next to turnip's own; turnip reads nothing in there
besides `config.yaml`, and a project can point at a sibling through the
fixed workspace path described under "Where the Runner puts things".

```yaml
schemaVersion: v1alpha3
projects:
  - name: web            # optional; defaults to `directory` if omitted
    directory: infra/web
    whenModified:
      - "infra/web/**"
    uses: helmfile@v1.7.4
    with:
      environment: staging
    runner:
      env:
        AWS_PROFILE: web-deployer
```

| Field | Required | Notes |
|---|---|---|
| `schemaVersion` (top-level) | **yes** | the version of this file's schema; must be `v1alpha3`; see below |
| `projects[].name` | no | defaults to `directory`; must be unique across the file once defaulted |
| `projects[].directory` | **yes** | the tool's working directory, relative to the repo root |
| `projects[].uses` | **yes** | what to run: `<tool>@<tag>`, or a fully qualified `<image>@<tag>` the operator allows; see below |
| `projects[].whenModified` | no | a list of glob patterns (full `**` support, [doublestar](https://github.com/bmatcuk/doublestar) syntax), matched against the paths GitHub reports as changed; a PR whose changed files match none of a project's patterns never triggers it. **Omitted, the project is never planned automatically**, nor by a command that names no project; it runs only when a command names it (`/helmfile diff web`) or asks for every project (`*`). Matching is textual and **does not follow symlinks**: git records a change under the file's real path, so a project reached through a symlinked directory must also list the link target (e.g. both `env/prod/**` and `shared/modules/**`) |
| `projects[].with` | no | how to call it: configuration the tool itself reads; see below |
| `projects[].runner` | no | where it runs: settings for the Runner Pod; see below |

Every violation across the whole file is reported together (a typo in
project 3 doesn't hide a missing `directory` in project 1), so you'll see
every problem in one pass, not one-at-a-time.

**A key turnip doesn't recognize is an error, not a silent no-op.** Every
field above is checked by name, so a misspelling or a setting written at
the wrong level fails the check with the offending key and its line
number rather than being read and quietly discarded. The one deliberate
exception is inside `with`, which exists precisely to carry keys turnip
does not define.

### `schemaVersion`

Required, and exactly one value is accepted today: `v1alpha3`. A file
missing it, or carrying anything else (including the previous
`v1alpha2`), is rejected naming both what it found and what this turnip
supports, and it is reported *on its own*,
without also listing every field the older schema used, since the version
is the one fact that explains them.

"Version" means three unrelated things around turnip, so to be explicit
about which one this is:

| | What it versions |
|---|---|
| `schemaVersion` (top-level) | the shape of this file (this field) |
| the tag after `@` in `uses` (per project) | which image, and so which release of the tool, the Runner runs |
| turnip's own release | the Server and Runner images you deploy |

The `alpha` suffix is load-bearing, not decoration. Before turnip 1.0 the
schema is expected to break, and advancing within alpha (`v1alpha3`,
`v1alpha4`, …) costs no turnip release and promises nobody a migration
window. It graduates to `v1` at turnip 1.0, at which point the schema
version and the project's major version coincide. There is no
compatibility shim in the meantime: a file on an older schema is
rejected outright, never silently upgraded.

### `clone`: how the repository is checked out

Top-level, beside `projects:` rather than inside one: a pull request
produces a single clone that every matched project then runs in, so this
describes the checkout itself rather than any one project.

```yaml
schemaVersion: v1alpha3

clone:
  submodules: recursive

projects:
  - directory: infrastructure
    uses: helmfile@v1.1.7
```

**`submodules`** decides how far submodule initialization goes:

| Value | Behavior |
|---|---|
| `none` | submodules are not initialized; the directory stays empty |
| `top-level` | one level of submodules (the default) |
| `recursive` | submodules of submodules too |

Not `shallow`: in git that word means a depth-limited fetch, and turnip
deliberately does the opposite: submodules are fetched at full depth, so
the commit the parent pins is certainly present.

**Only honored when the Server's `TURNIP_ALLOWED_OVERRIDES` includes
`clone.submodules`**; otherwise the operation is refused with a comment on
the PR and no Runner Job is created. The gate here is about cost rather
than safety, unlike `runner.serviceAccount`'s (fetching a submodule the
App can already read grants no permission the repository doesn't already
have), but an operator may still have reason to refuse an expensive fetch,
so it reuses the same list rather than inventing a second mechanism.
What enabling it costs is stated in [`SECURITY.md`](../SECURITY.md).

Submodules are fetched with the same installation credential as the
repository itself, and therefore reach **only repositories your GitHub App
is installed on**. A submodule on the repository's own host works whatever
form `.gitmodules` writes it in (SSH, `git://`, an explicit port) because
turnip rewrites those to HTTPS; it holds no SSH key and never will. A submodule hosted anywhere else is reported by name *before*
anything is fetched, rather than failing later as a generic authentication
error.

### `uses`: what to run

One of two forms, each with exactly one tag after the `@`:

```yaml
    uses: helmfile@v1.7.4                   # a built-in alias
    uses: ghcr.io/org/helmfile-aws@v1.7.4   # a fully qualified image
```

- **`<alias>@<tag-spec>`**: an alias is the name of a tool this Server
  has a Plugin for (see "Tool support" below), standing for that tool's
  vendor image. `helmfile@v1.7.4` runs `ghcr.io/helmfile/helmfile:v1.7.4`.
- **`<image>@<tag-spec>`**: any other image, written in full, registry
  host included (`ghcr.io/…`, `registry.example.com:5000/…`,
  `localhost/…`). It runs only if the Server's operator allows it; see
  "Running your own image" below.

The part after the last `@` is the **tag-spec**: one exact **tag**, or a
**digest** written `sha256:` followed by 64 lowercase hex characters.
Nothing else: no pattern, no version range, and no digest algorithm but
`sha256`. It is used **exactly as written**: turnip never adds or removes a
`v`. Write the tag the vendor publishes, the one you would `docker pull`.
helmfile's tags carry a `v`, so it is `helmfile@v1.7.4`, not
`helmfile@1.7.4`.

`uses:` never names the tool for an image: the operator's entry allowing
the image says which tool it runs (so `ghcr.io/org/helmfile-aws@v1.7.4`
runs helmfile because the entry allowing it says `helmfile:`).

The tag-spec is required: turnip keeps no default version for any tool,
so what a project runs is always stated in the repository and changes
only in a diff someone reviews. A bare `uses: helmfile` is a validation
error that says to name a version.

**Pasting a reference from a registry.** `uses:` separates the tag with
`@`, not `:`. A line written the way a registry prints it is a validation
error that shows the corrected line:

```yaml
    uses: ghcr.io/org/helmfile-aws:v1.7.4   # rejected
    uses: ghcr.io/org/helmfile-aws@v1.7.4   # what the error tells you to write
```

A digest pasted as the registry prints it
(`ghcr.io/org/helmfile-aws@sha256:…`) is already the right form.

**Which tags each alias allows.** An alias allows only full versions and
digests of its vendor's image, so it can never move to a new release, or
to a floating tag, with no change to `turnip.yaml`:

| Alias | Image | Allowed tag-specs |
|---|---|---|
| `helmfile` | `ghcr.io/helmfile/helmfile` | `v*.*.*`, `sha256:*` |

So `helmfile@latest`, `helmfile@v1.7` and `helmfile@1.7.4` are all
validation errors, and the error names the tags the alias does allow. An
operator cannot redefine an alias, so `helmfile@v1.7.4` means the same
image on every turnip Server. The vendor's image under any other tag
(its own `latest`, say) runs only written out in full and allowed by the
operator, where a reviewer sees it.

All of this is checked when the file is parsed, so it appears as a
validation error on the pull request, with every other violation, rather
than as a failed Job. turnip keeps no list of "supported" versions: any tag
the alias's patterns match is passed straight to the vendor's image, so a
tool's new release works the moment the vendor publishes it. If a tag
matches but the vendor never published it, the Job fails when its image
can't be pulled, reported as a normal operation failure.

#### The apply runs what the plan ran

Whatever the tag, turnip records the exact **image digest** a plan's Pod
ran, as Kubernetes reports it, and an apply (or sync) of that plan runs that
digest, never the tag resolved again. A tag can move (`latest` by design,
and any tag a registry re-pushes), so this is what makes any allowed tag,
`latest` included, safe between plan and apply. A plan pulls a tag on
every start (`imagePullPolicy: Always`) so it sees what the tag means now;
a digest never changes, so it uses the node's cache.

If the digest could not be recorded, the apply is refused and asks you to
re-plan. If the recorded digest can no longer be pulled (deleted from the
registry since the plan), the apply fails saying so, and a re-plan
records one that exists.

#### Running your own image

An image built `FROM` a vendor's with something added (a cloud CLI, a
helm plugin), or a build the vendor has not released, runs only when the
Server's operator lists it in **`TURNIP_ALLOWED_IMAGES`** (see the Server
settings table below). This is an opt-in: each allowed image is code the
operator vouches for, running with the Runner's credentials for any pull
request that names it. What that costs is stated in
[`SECURITY.md`](../SECURITY.md).

Each entry is `tool:image@glob`, comma-separated:

| Entry | Allows |
|---|---|
| `helmfile:ghcr.io/org/helmfile-aws@*` | any tag or digest of that image |
| `helmfile:ghcr.io/org/helmfile-aws@v*.*.*` | full versions only |
| `helmfile:ghcr.io/org/helmfile-aws@latest` | exactly `latest` |
| `helmfile:ghcr.io/org/helmfile-aws@sha256:*` | digests only |
| `helmfile:ghcr.io/helmfile/helmfile@latest` | the vendor's own `latest`, written in full: `uses: ghcr.io/helmfile/helmfile@latest` |

- **The tool** is the text before the first `:`, and must be a tool this
  Server has a Plugin for. It is how turnip knows which Plugin drives the
  image, so one image can be listed for only one tool (including the
  aliases' vendor images: `ghcr.io/helmfile/helmfile` is helmfile's).
- **The image** is fully qualified and carries no tag or digest of its
  own.
- **The glob** is matched against the tag-spec's text: `*` matches any
  characters, `?` one, `[…]` a class. It is a glob, not a version range,
  so it blocks floating tags by shape: `@1.*.*` cannot match `1.16` (a
  floating minor tag) or `latest`, only full versions such as `1.16.4`.

Entries for the same tool may overlap freely. An invalid entry, or an
image listed for two tools, stops the Server from starting, naming every
offending entry at once. A `uses:` line no entry matches is a validation
error of `turnip.yaml`, naming the image and `TURNIP_ALLOWED_IMAGES`.

**What the image must provide.** A custom image always runs as the Job's
main container, with turnip's runner binary handed to it (run-in-image,
see "How a tool reaches the Runner" below), whatever strategy its tool
normally uses. So it must have the tool on its `PATH`, and everything the
tool's Plugin shells out to: for helmfile, `helm` and the plugins its
commands use (helm-diff for `diff`, and helm-secrets and `sops` if your
helmfiles decrypt anything). Building `FROM` the vendor's image keeps all
of that and adds only what you need; the EKS recipe below does exactly
this.

**Pulling from a private registry.** turnip handles no registry
credentials. The Runner Pod pulls the way any Pod does:

- through the **node's own credentials**, for example ECR on EKS, where
  the node role's ECR read access lets every Pod pull from that account's
  registries with nothing else to configure; or
- through an **`imagePullSecrets`** on the Runner's ServiceAccount (the
  one `TURNIP_RUNNER_SERVICE_ACCOUNT` or `runner.serviceAccount` selects),
  which Kubernetes applies to every Pod running as it:

```sh
kubectl -n <runner-namespace> create secret docker-registry registry-pull \
  --docker-server=ghcr.io --docker-username=<user> --docker-password=<token>
kubectl -n <runner-namespace> patch serviceaccount turnip-runner \
  -p '{"imagePullSecrets":[{"name":"registry-pull"}]}'
```

A plan naming a tag pulls on every start, so the credential has to keep
working, not only for the first pull.

### `with`: how to call it

Configuration for the tool itself. Nothing turnip reads lives here: every
key is passed to the plugin for that tool, which is why unrecognized keys
inside `with` are accepted rather than rejected.

| Key | Tool | Meaning |
|---|---|---|
| `environment` | Helmfile | helmfile's own `--environment` flag |
| `workspace` | Terraform | the Terraform workspace name |
| `backendConfig` | Terraform | backend configuration for the tool's initialization step |
| `stack` | Pulumi | the Pulumi stack name |

### `runner`: where it runs

Settings that shape the Runner Pod rather than the tool inside it:

```yaml
    runner:
      serviceAccount: turnip-runner
      env:
        AWS_PROFILE: web-deployer
```

- **`serviceAccount`**: the Kubernetes ServiceAccount this Project's
  Runner Pod runs as: the identity EKS Pod Identity/IRSA maps to an IAM
  role, and the one in-cluster API calls authenticate with. **Only
  honored when the Server's `TURNIP_ALLOWED_OVERRIDES` includes
  `runner.serviceAccount`**; otherwise the operation is refused with a
  comment on the PR and no Runner Job is created. The gate exists because
  turnip reads this file from the pull request's own head commit, and a
  plan needs only collaborator access; without it, anyone able to open a
  PR could pick any ServiceAccount in the Runner namespace and use its
  permissions. What enabling it costs is stated in
  [`SECURITY.md`](../SECURITY.md).
- **`env`**: environment variables for the IaC tool's process. Unlike
  `with`, these are never interpreted by turnip at all. They are simply
  present in the environment the tool runs in, which makes this the place
  for anything the tool's own ecosystem reads.

Two kinds of `env` name are rejected, with every offending name in the
file reported together rather than one per attempt:

- **anything beginning with `TURNIP_`**: the Runner reads its own
  configuration out of that namespace, so a Project setting one would be
  reconfiguring the Runner rather than the tool.
- **`PATH`**: the Runner composes it at startup so the provisioned tool
  binary is found first; replacing it wholesale would hide the very
  binary the operation needs.

Values reach the tool byte-for-byte, including values containing `$(…)`;
turnip escapes them on the way through, since Kubernetes would
otherwise expand `$(VAR)` inside an environment value before the tool
ever saw it.

One thing worth being deliberate about: this file is read from the pull
request's own head commit, so `runner.env` is something a PR author can
change. That is the same trust boundary the tool's own committed
configuration already sits on (a `.tf` or helmfile in the branch can
redirect a backend or a role just as readily), but if that boundary
matters to you, it's the Server-side settings below, not `env`, that
decide what credentials the Runner holds in the first place.

### What the Runner's ServiceAccount needs

turnip ships no ServiceAccount and no RBAC for the Runner, deliberately.
The Runner runs *your* IaC, so what it needs is whatever your IaC
manages, and anything turnip shipped would have to carry elevated access
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

Targeting any **other** cluster needs a kubeconfig. turnip does not
generate one, and doesn't need to; see the next section.

#### Targeting an EKS cluster turnip is not running in

No step has to run before the tool. A kubeconfig produced by
`aws eks update-kubeconfig` does not contain credentials: it tells the
client to run `aws eks get-token` whenever it needs one. helm, helmfile
and kubectl all run that command themselves, on demand. So the
kubeconfig is generated **once**, committed to the repository, and the
Runner only needs `aws` on its PATH and an AWS identity to sign with.

**The identity.** With EKS Pod Identity, the Runner Pod already *is* the
IAM role associated with its ServiceAccount; nothing assumes a role. The
Pod Identity agent injects `AWS_CONTAINER_CREDENTIALS_FULL_URI` and
`AWS_CONTAINER_AUTHORIZATION_TOKEN_FILE`, and the AWS CLI's default
credential chain uses them. The target cluster grants that role access
through an access entry. Select the ServiceAccount with
`TURNIP_RUNNER_SERVICE_ACCOUNT`, or with `runner.serviceAccount` where the
override is permitted.

**Generating the kubeconfig**, once, on your own machine, with any
credentials allowed to call `eks:DescribeCluster`:

```sh
aws eks update-kubeconfig --name <cluster> --region <region> \
  --kubeconfig ./kubeconfig --alias <cluster>
```

Repeat for each cluster; each run adds a context to the same file.

**Before committing it, check the `users` section.** If a profile was in
use when you ran the command (`--profile`, or `AWS_PROFILE` set),
`update-kubeconfig` records it:

```yaml
users:
- name: <cluster>
  user:
    exec:
      apiVersion: client.authentication.k8s.io/v1beta1
      command: aws
      args: [--region, <region>, eks, get-token, --cluster-name, <cluster>, --output, json]
      env:                      # remove this block
      - name: AWS_PROFILE
        value: <your-local-profile>
```

Remove that `env` block: the Runner Pod has no profiles, and `get-token`
would fail looking for one. The rest of the file (endpoints, CA
certificates, the `exec` stanza) contains no secrets.

**Pointing a Project at it**, with `runner.env`, and choosing the context
in helmfile (`kubeContext`, under `helmDefaults` or per release):

```yaml
    runner:
      env:
        KUBECONFIG: kubeconfig   # relative to the Project's directory
```

The tool runs in the Project's directory, so a relative path resolves
there. Confirm it with the first plan: helm, which helmfile runs per
release, has to resolve it the same way.

**When a role *is* assumed.** Only if a cluster must be reached as a role
other than the Pod's, typically a cluster in another AWS account whose
access entries don't accept the Pod's role. Add `--role-arn <arn>` to that
user's `args`, and allow the Pod's role to assume it.

**The image** needs the AWS CLI next to the tool, and a version recent
enough to read Pod Identity's token file; older versions behave as if no
credentials existed. EKS's Pod Identity documentation lists the minimum
versions. The vendor's helmfile image has no AWS CLI, so build one `FROM`
it that adds the CLI and keeps everything else (helm, its plugins,
`sops`):

```dockerfile
# The vendor's image is Alpine-based; Alpine's aws-cli package is v2.
FROM ghcr.io/helmfile/helmfile:v1.7.4
RUN apk add --no-cache aws-cli && aws --version
```

Push it under a tag that tracks the helmfile it was built from (here
`ghcr.io/org/helmfile-aws:v1.7.4`), then have the Server's operator allow
it (see "Running your own image" above):

```
TURNIP_ALLOWED_IMAGES=helmfile:ghcr.io/org/helmfile-aws@v*.*.*
```

and point the Project at it in place of the alias:

```yaml
    uses: ghcr.io/org/helmfile-aws@v1.7.4
```

If the image lives in ECR in the cluster's own account, the nodes can
usually pull it with no further setup; otherwise see "Pulling from a
private registry" above.

**Verifying access before the first plan** (optional; the first plan
tells you too): start a throwaway Pod in the cluster turnip runs in, with
the Runner's ServiceAccount and any image that has the AWS CLI and
kubectl, copy the kubeconfig in, and run:

```sh
aws sts get-caller-identity                       # the Pod Identity role
KUBECONFIG=./kubeconfig kubectl --context <cluster> get namespaces
```

If both succeed, the Runner can reach the cluster the same way.

Helm plugins authenticate separately, and to different things.
`helm-secrets` decrypting through a cloud KMS, or a chart pulled from a
private registry, needs *cloud* credentials rather than cluster ones.
Those come from the Pod's identity (EKS Pod Identity/IRSA, GCP Workload
Identity, selected by that same ServiceAccount) and from `runner.env`
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
arrives: a chart's `lookup`, a CRD, a namespace, a cluster-scoped
resource. **Enumerating minimal permissions for helmfile is a losing
game**: a helmfile manages whatever its charts declare, so the permission
set is the union of every kind any chart touches, and it moves whenever a
chart does. `apply` needs create/update/delete across all of it, and a
repository that installs CRDs or namespaces needs cluster-scoped rights to
match.

Neither built-in shortcut works. `view` excludes Secrets deliberately:
*"reading the contents of Secrets enables access to ServiceAccount
credentials in the namespace, which would allow API access as any
ServiceAccount in the namespace (a form of privilege escalation)"*, so it
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

A Runner that only ever diffs can be narrower (Secrets plus read on what
the charts consult, bound per namespace), but expect to revisit it as the
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

That is the same trust boundary the committed IaC already sits on (a
chart can redirect a resource as readily as a hook can run one), but it
is sharper than it looks, and two things follow:

- **Run turnip only on repositories whose contributors you already trust
  with the cluster.** turnip refuses to run anything on a pull request
  from a fork, on both trigger paths (see "Pull requests from forks" in
  `docs/usage.md`), so this is about the people who can push a branch to
  the repository itself. For them, opening a pull request is enough to
  run a hook with the Runner's credentials.
- **Scope by cluster, not by rule set.** The practical blast radius is
  decided when you choose which cluster turnip runs in and which
  repository drives it, not by trimming permissions that will grow back
  the next time a chart adds a resource kind.

### Where the Runner puts things

Everything turnip creates inside a Runner Pod lives under `/turnip`:

| Path | Contents | Present when |
|---|---|---|
| `/turnip/src` | the repository, cloned at the pull request's head commit; a project's `directory` is relative to this | always |
| `/turnip/tools` | the provisioned IaC tool binary, prepended to the Runner's `PATH` | copy-out tools only |
| `/turnip/bin` | turnip's own runner binary, handed to the tool's image | run-in-image tools and custom images only |

`/turnip/src` is a fixed path rather than a per-run temporary directory
specifically so committed configuration may reference it: an absolute
path in a helmfile or a `.tfvars` resolves the same way on every run. It's
the one worth naming directly; the other two are turnip's own plumbing and
may move.

The repository is cloned by an initContainer running turnip's image, not
by the container that runs your tool, which is what lets that container
be the tool vendor's own image (or one built from it), needing nothing
from it but the tool.

### Tool support

The tools `uses:` can run are exactly the ones this Server has Plugins
for, each under its alias or in an image allowed for it. A project naming
any other tool makes `turnip.yaml` invalid: it is reported when the file
is parsed, with every other violation, and fails the `turnip` check as
`invalid turnip.yaml`.

| Tool | Status | Provisioned by | Operations |
|---|---|---|---|
| Helmfile | implemented | run-in-image | `diff` (plan), `apply`, `sync` |
| Terraform | **not yet implemented** | not decided | none; `uses: terraform@…` is a validation error until its Plugin lands |
| Pulumi | **not yet implemented** | not decided | same as Terraform |

Adding a tool is described in [`development.md`](development.md).

### How a tool reaches the Runner

Two strategies, chosen per tool by turnip rather than configured (a
custom image always runs the second):

- **copy-out**: an initContainer copies the tool's binary out of the
  vendor's image onto a shared volume, and turnip's own image runs it.
  Correct for a tool that is a single self-contained binary.
- **run-in-image**: the vendor's image *is* the container that runs, with
  turnip's runner binary handed to it. The tool gets its own image's
  `PATH`, environment and `HOME`, so its helper binaries and plugins are
  simply present.

Helmfile needs the second: it shells out to `helm`, `helmfile diff` needs
the helm-diff plugin, helm-secrets needs `sops`, and helm finds its plugins
through an environment its image sets. Copying one binary out produced
exactly the failure you'd expect: `exec: "helm": executable file not found
in $PATH`.

The practical consequence: **for a run-in-image tool, its plugins and
helpers come from the image.** Needing one the vendor's image doesn't
bundle means building an image that has it and having the operator allow
it (see "Running your own image"), not configuring turnip.

## The Server's own configuration

Read from environment variables at startup (`internal/orchestrator.ConfigFromEnv`)
and fails fast, naming every missing required variable together, rather
than one restart-and-discover-the-next-one at a time.

| Variable | Required | Purpose |
|---|---|---|
| `TURNIP_GITHUB_APP_ID` | yes | your GitHub App's numeric ID |
| `TURNIP_GITHUB_PRIVATE_KEY` or `TURNIP_GITHUB_PRIVATE_KEY_PATH` | yes (one of) | the App's private key: inline PEM or a file path |
| `TURNIP_GITHUB_WEBHOOK_SECRET` | yes | the secret your GitHub App's webhook is configured with (HMAC-verified on every delivery) |
| `TURNIP_REDIS_ADDR` | yes | Redis/Valkey address; the Server's only state store |
| `TURNIP_K8S_NAMESPACE` | yes | namespace Runner Jobs are created in |
| `TURNIP_HTTP_ADDR` | yes | HTTP bind address (webhooks, `/healthz`, `/readyz`, `/metrics`) |
| `TURNIP_GRPC_ADDR` | yes | gRPC bind address (Runner→Server log/result streaming) |
| `TURNIP_RUNNER_SERVER_ADDR` | yes | address a Runner Pod dials to reach this Server: a Service DNS name, not the bind address above |
| `TURNIP_RUNNER_IMAGE` | yes | the Runner container image a deployed Server creates Jobs with |
| `TURNIP_MINIMIZE_OUTDATED_PLAN_COMMENTS` | no (default `false`) | collapse an older plan comment on the same PR once a newer one supersedes it |
| `TURNIP_RUNNER_SERVICE_ACCOUNT` | no (default unset) | ServiceAccount every Runner Pod runs as: how a Runner gets cloud credentials (EKS Pod Identity/IRSA) and in-cluster API permissions. Unset leaves Pods on the namespace's `default` ServiceAccount, which normally has neither |
| `TURNIP_CLONE_SUBMODULES` | no (default `top-level`) | how far the clone initializes submodules: `none`, `top-level`, or `recursive`. Applies to every repository this Server clones, unless one overrides it with `clone.submodules` *and* that path is permitted below. Defaults on, unlike `actions/checkout`: turnip clones specifically to run IaC that may reference submodule paths, so defaulting off would make every repository with a submodule fail confusingly before anything worked. An unrecognized value is a startup error |
| `TURNIP_ALLOWED_OVERRIDES` | no (default: permits nothing) | comma-separated list of the fields a repository's own config file may set. The paths accepted today are `runner.serviceAccount` and `clone.submodules`. The default matches what turnip has always done: a repository cannot choose the identity its Runner assumes, since this file is read from the PR's own head commit. An unrecognized path is a startup error rather than a silently ineffective setting. Note that `uses` is *not* an override: turnip has no Server-side tool to fall back to, so what a project runs is always the repository's to say, within the images `TURNIP_ALLOWED_IMAGES` allows. Each path is an opt-in whose cost [`SECURITY.md`](../SECURITY.md) states |
| `TURNIP_ALLOWED_IMAGES` | no (default: only the built-in aliases) | comma-separated `tool:image@glob` entries, each allowing a fully qualified image, for one tool, with the tag-specs its glob matches; see "Running your own image". For example `helmfile:ghcr.io/org/helmfile-aws@*` (any tag or digest), `helmfile:ghcr.io/org/helmfile-aws@latest` (exactly `latest`), or `helmfile:ghcr.io/helmfile/helmfile@latest` (the vendor's own `latest`, written in full in `uses:`). A glob is not a version range: `@1.*.*` cannot match `1.16` or `latest`. An entry naming a tool with no Plugin, an unqualified image, or an image with a tag of its own, and an image listed for two tools, are startup errors naming every offending entry. Each allowed image is code the operator vouches for, running with the Runner's credentials; see [`SECURITY.md`](../SECURITY.md) |
| `TURNIP_MUTATION_REQUIREMENTS` | no (default: requires nothing) | comma-separated conditions a pull request must meet before turnip runs a mutating command (any operation that is not the tool's plan, such as helmfile's `apply` or `sync`): `approved`, `mergeable`, or both. Whitespace is trimmed and a repeated name counts once. Operator-side only: a repository cannot set or relax it. An unrecognized name is a startup error that lists the recognized ones. See "Mutation requirements" below |

In `deploy/base`, the two credential-shaped values
(`TURNIP_GITHUB_WEBHOOK_SECRET`, `TURNIP_GITHUB_PRIVATE_KEY`) come from a
Secret; everything else comes from a ConfigMap an overlay fills in; see
`docs/deployment.md`'s "Installing turnip" section for the two values
(`TURNIP_REDIS_ADDR`, `TURNIP_GITHUB_APP_ID`) a real deployment always
needs to set itself, whichever install path you use.

The Runner container's own environment variables (`TURNIP_OPERATION_ID`,
`TURNIP_REPO_URL`, and so on) are internal plumbing the Server sets
automatically on every Job it creates, with nothing here for an operator to
configure directly.

**No GitHub credential is among them.** See
[`docs/deployment.md`](deployment.md#how-a-runner-gets-its-github-credential).

### Mutation requirements

A **mutating command** is any operation a tool offers other than its
plan: helmfile's `apply` and `sync`, but not its `diff` (see "Plan
commands and mutating commands" in `docs/usage.md`).

By default, turnip admits a mutating command on two facts: the commenter has write
access to the repository, and the project holds a plan from this pull
request. It asks nothing about the pull request itself. Nobody need have
approved the change, so one collaborator can plan and apply their own pull
request with no second pair of eyes.

`TURNIP_MUTATION_REQUIREMENTS` closes that gap. It names conditions the
pull request must meet before turnip runs any mutating command,
helmfile's `sync` as much as its `apply`. Both are **off by default**; an operator enables one by naming it:

| Name | Met when |
|---|---|
| `approved` | an account other than the pull request's author, with write access to the repository, has an approving review that GitHub still reports as approving |
| `mergeable` | GitHub reports the pull request has no merge conflict with its base branch |

To require both, which is what most repositories that review changes
want:

```sh
TURNIP_MUTATION_REQUIREMENTS=approved,mergeable
```

A misspelled name stops the Server from starting, and the error lists
the recognized names. Accepting it would gate nothing while looking like
it gated something.

**What is never gated.** A plan runs whatever the requirements say: it is
how a reviewer sees what the change would do, and nobody can approve what
they cannot see. `/turnip unlock` is not gated either, since releasing a
lock is how someone recovers from a pull request that cannot meet them.

**A repository cannot set or relax them.** They are read only from the
Server's environment, never from `turnip.yaml`, and they are not a path
`TURNIP_ALLOWED_OVERRIDES` accepts. The reason is that the pull request
supplies `turnip.yaml`: a requirement exists to constrain the pull
request, so letting the pull request choose it would let the change being
reviewed switch off its own review. One setting applies to every
repository this Server serves.

**`mergeable` is about merge conflicts, not status checks.** turnip reads
GitHub's conflict-only answer, not the merge state that folds in required
status checks and reviews. It does not enforce branch protection: a pull
request whose required checks are failing, or whose required reviews are
missing, still satisfies `mergeable` if it merges cleanly. This is
deliberate. The `turnip` check you mark required cannot pass until every
plan is applied, so a requirement that waited for required checks would
wait for the apply it is gating. Use `approved` for review; branch
protection still decides the merge. GitHub computes mergeability in the
background after a push, so when it has no answer yet turnip reads the
pull request again, up to 3 times a second apart, before replying that
the answer is not available yet.

**`approved` follows GitHub's review state.** Each reviewer's standing is
their latest approving, changes-requested or dismissed review; a plain
comment review leaves it as it was. So an approval followed by a request
for changes from the same reviewer does not count, and neither does a
dismissed one. The approval must come from an account **other than the
author** (an author approving their own change is not a second pair of
eyes) and one **with write access** to the repository. GitHub accepts an
approving review from anyone who can read the repository, which on a
public repository is anyone, so without that line a stranger, or the
author's second account, would satisfy the requirement.

**An approval given before a later push still counts**, unless branch
protection dismisses it. turnip does not look at which commit a review
was given on. Whether a new push voids an approval is the repository's
own policy, set by branch protection's **"Dismiss stale pull request
approvals when new commits are pushed"**. With it on, GitHub dismisses the
old approval on every push and turnip no longer counts it; with it off,
the approval stands, as it would for GitHub's own merge button. Enabling
that setting is how a repository requires approval of the commit being
applied. turnip then applies exactly when the repository's review rules
would let the change merge, no earlier and no stricter.

When a requirement is not met, turnip replies once per command, naming
every unmet requirement, and runs nothing: no lock changes, no Runner Job
starts, and no check run is created or updated. What each line of that
reply means, and what to do about it, is in `docs/troubleshooting.md`
under "Mutation requirements not met".

## Setting up the GitHub App

Start the creation form under the GitHub organization/account whose
repositories should trigger turnip: **Settings → Developer settings →
GitHub Apps → New GitHub App**. The form has several sections turnip
doesn't use at all; they're covered below so nothing is left ambiguous.

**GitHub App name**: required, and must be unique across *all* of
GitHub (not just your org). If your first choice is taken, add a
suffix. This name is only ever visible in GitHub's own UI (installed-App
lists, PR check-run author, etc.); it never ends up in this repository,
so name it however makes sense to you. For example,
`turnip [your GitHub organization]`.

**Description**: optional; skip it.

**Homepage URL**: required by the form, but turnip has no user-facing
page. Point it at this repository (`https://github.com/ivanvc/turnip`)
or your own org's site; the value isn't otherwise meaningful to turnip.

**Identifying and authorizing users** (Callback URL, "Expire user
authorization tokens", "Request user authorization (OAuth) during
installation", "Enable Device Flow"): turnip has no user-facing login;
it never initiates a user OAuth flow. Leave the Callback URL field
blank and every checkbox in this section unchecked (the form's
defaults).

**Post installation** (Setup URL, "Redirect on update"): also unused;
leave the Setup URL blank and the checkbox unchecked.

**Webhook**:
- **Active**: check this once you have the URL described next; if you
  don't yet, **uncheck it instead of typing a placeholder**: GitHub
  only requires the URL field when Active is checked, and you can come
  back to this same settings page to check Active and fill in the real
  URL once it exists.
- **Webhook URL**: `https://<your-hostname>/github/webhook`, that exact
  path, not the root and not a sub-path beneath it. The Server serves
  webhook deliveries there and nowhere else; every other path returns
  404. `<your-hostname>` is a real, publicly-resolvable DNS name that
  routes HTTPS traffic to the `turnip-server` Service's HTTP port (`8080`
  by default, `TURNIP_HTTP_ADDR`). **turnip provisions none of this for
  you**: `deploy/base` has no Ingress, Gateway API, or LoadBalancer
  Service in it, only a plain `ClusterIP` Service
  (`deploy/base/service.yaml`). You need your own
  Ingress/Gateway/LoadBalancer resource (whatever your cluster already
  uses for exposing HTTP services) routing to that Service, plus a DNS
  record and a TLS certificate for the hostname, e.g.
  `https://turnip.example.com/github/webhook` if your ingress controller
  fronts that hostname and forwards to `turnip-server:8080`. None of that
  is part of this repository; it's entirely your own cluster's ingress
  setup.

  The path is namespaced by forge on purpose. It leaves `/github/` free
  for other GitHub-specific endpoints, and leaves the rest of the server's
  paths free for surfaces that should *not* be publicly reachable, which
  is also why giving the webhook its own path matters: an Ingress can
  route on it, so you can expose this one path and keep everything else
  internal.
- **Webhook secret**: type in a secret you generate yourself (e.g.
  `openssl rand -hex 32`); this exact value is what
  `TURNIP_GITHUB_WEBHOOK_SECRET` must match, so generate it first and
  paste the same string into both places.

**Permissions** (inferred directly from the GitHub API calls turnip
makes; double-check against the current GitHub App permissions UI when
you create the App, since GitHub occasionally renames these). Expand
only **Repository permissions**; leave Organization permissions and
Account permissions untouched (every entry "No access"):

| Permission | Level | Why |
|---|---|---|
| Checks | Read & write | creating/updating the per-project check runs and the `turnip` check |
| Contents | Read-only | fetching `turnip.yaml` and diffing changed files |
| Issues | Read & write | GitHub represents PR comments as issue comments under the hood; this is what lets turnip post and edit them |
| Pull requests | Read & write | reading PR metadata, listing changed files |

**Subscribe to events**: each repository permission above unlocks its
matching event checkbox in this list once set. Check `Pull request`
and `Issue comment` (nothing else).

**Where can this GitHub App be installed?**: choose **Only on this
account**. "Any account" would let *any* GitHub user install this App
on their own repositories, which is never what you want for turnip.

Click **Create GitHub App**. You land on the App's own settings page;
install it on the specific repositories you want from here (or from
**Install App** in the sidebar), and note the App ID shown near the top
(`TURNIP_GITHUB_APP_ID`).

**Generate a private key**: on this same settings page (General tab),
scroll to the **Private keys** section, below Webhook and Permissions,
and distinct from the **Client secrets** section just above it (turnip
authenticates as the App via JWT, not OAuth, so it needs the private
key, not a client secret). Click **Generate a private key**; your
browser immediately downloads a `.pem` file. This is a one-time
download. GitHub never shows or re-offers the raw key again (a lost
key means generating a new one; there's no `gh` CLI equivalent for this
step). Move the downloaded file somewhere private, outside any git
repo. You'll pass its path as `TURNIP_GITHUB_PRIVATE_KEY_PATH` (or
inline its contents as `TURNIP_GITHUB_PRIVATE_KEY`), and it's the same
file `docs/deployment.md`'s `kubectl create secret` command consumes.

Who can actually *use* an installed App is a separate, per-comment
authorization check; see `docs/usage.md`'s "Who can trigger what"
section.
