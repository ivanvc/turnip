# Runner access

> **For operators**: deploying and configuring the Server. End users: see
> [`turnip.yaml`](../using/turnip-yaml.md), including
> ["Writing your own image"](../using/turnip-yaml.md#writing-your-own-image)
> for the `uses:` side of custom images.

## Which ServiceAccount

`TURNIP_RUNNER_SERVICE_ACCOUNT` sets the Kubernetes ServiceAccount every
Runner Pod runs as by default: the identity EKS Pod Identity/IRSA maps to
an IAM role, and the one in-cluster API calls authenticate with. Unset
leaves Pods on the namespace's `default` ServiceAccount, which normally
has neither.

turnip creates no ServiceAccount for the Runner, so create one in the
namespace Runner Jobs run in (`TURNIP_K8S_NAMESPACE`) and point the
setting at it. The examples on these pages and the tool pages call it
`turnip-runner`:

```sh
kubectl -n <runner-namespace> create serviceaccount turnip-runner
```

and set `TURNIP_RUNNER_SERVICE_ACCOUNT=turnip-runner` in the Server's
ConfigMap (see [Server settings](server-settings.md)).

A repository's own `turnip.yaml` can choose a different ServiceAccount
per project (or for every project, in its top-level `runner:` block) with
`runner.serviceAccount`, but only when
[`TURNIP_ALLOWED_OVERRIDES`](server-settings.md#allowed-overrides)
includes `runner.serviceAccount`. Without it, an operation that sets the
field is refused with a comment on the pull request and no Runner Job is
created.

**Think before enabling this one.** turnip reads `turnip.yaml` from the
pull request's own head commit, so without the gate, anyone able to open
a pull request could pick any ServiceAccount in the Runner namespace and
run with its permissions. Enabling `runner.serviceAccount` widens that to
**every ServiceAccount in the Runner namespace**, and the most privileged
one becomes the ceiling for every pull request. The gate applies whether
the value is set at the top level or on one project: moving the line does
not get past it. See the "Opt-ins that grant capability by design" table
in [`SECURITY.md`](../../SECURITY.md) for the full cost.

## What the Runner's ServiceAccount needs

turnip ships no ServiceAccount and no RBAC for the Runner, deliberately.
The Runner runs *your* IaC, so what it needs is whatever your IaC
manages, and anything turnip shipped would have to carry elevated access
to be useful for anyone. That is a grant an operator should make
knowingly, not one inherited from a manifest they applied in order to
install turnip. `deploy/base` creates only the Server's own
ServiceAccount and Role, which cover creating Jobs and reading Pods and
nothing else.

What the Runner needs, how it authenticates to a cluster, and what that
grant means in practice depend on the tool, so each tool's page covers
them. For Helmfile, this operator's side is under
[Authentication and cluster access](../plugins/helmfile.md#authentication-and-cluster-access)
and the repository's under
[Reaching a cluster](../plugins/helmfile.md#reaching-a-cluster). Whatever
the tool, a plan is not read-only in the security sense: it runs the pull
request's code with the Runner's credentials (for Helmfile, see
[What that grant means](../plugins/helmfile.md#what-that-grant-means)).

### Grants that reach the GitHub credential

Code in the container that runs the tool cannot get a GitHub credential
from the Server (see
[How a Runner gets its GitHub credential](#how-a-runner-gets-its-github-credential)),
but only while the Runner's ServiceAccount can neither mint a clone
identity nor reach into a Runner Pod. Any of these grants in the Runner
namespace (`TURNIP_K8S_NAMESPACE`) lets that code get one:

| Grant | What code in the tool container can do with it |
|---|---|
| `create` on `serviceaccounts/token` | request a token for its own ServiceAccount with the clone identity's audience, bound to its own Pod. The Server cannot tell it apart from the clone identity Kubernetes mounted in the clone container, and mints a GitHub credential for it. |
| `pods/exec` | read the clone identity from another Operation's clone container while that clone runs, and fetch that Operation's GitHub credential |
| `patch` or `update` on `pods/ephemeralcontainers` | add a container to its own Pod that mounts the clone identity's volume |

This applies to `TURNIP_RUNNER_SERVICE_ACCOUNT` and, when
`runner.serviceAccount` is allowed, to every ServiceAccount in the Runner
namespace. `cluster-admin`, which
[Helmfile's page](../plugins/helmfile.md#what-to-grant-it) recommends
for a Runner that applies, includes all three. In `deploy/base` the
Server runs in the Runner namespace too, so a Runner that can exec into
Pods or read Secrets there can also reach the GitHub App's private key
(the `turnip-github-app` Secret), which is worth more than any GitHub
credential the Server mints. Giving a Runner any of these trusts the
pull request's code with the GitHub App as well as with the cluster:
that is the threat model in [`SECURITY.md`](../../SECURITY.md), and
turnip cannot narrow it.

## Cloud credentials

turnip itself hands the Runner nothing beyond the ServiceAccount above:
no cloud credential is generated, injected or rotated by turnip. A
Runner Pod gets cloud credentials the same way any Pod in your cluster
would, through that ServiceAccount: EKS Pod Identity or IRSA on AWS,
Workload Identity on GKE, and so on, all decided by your cluster's own
setup and which ServiceAccount the Pod runs as.
[Targeting an EKS cluster: the operator side](../plugins/helmfile.md#targeting-an-eks-cluster-the-operator-side)
walks through one concrete example end to end (identity, image, and the
access entry the target cluster needs).

## Running a custom image

An image built `FROM` a vendor's with something added (a cloud CLI, a
helm plugin), or a build the vendor has not released, runs only when you
list it in **`TURNIP_ALLOWED_IMAGES`**
(see [Server settings](server-settings.md)). This is an opt-in: each
allowed image is code you vouch for, running with the Runner's
credentials for any pull request that names it. What that costs is
stated in [`SECURITY.md`](../../SECURITY.md).

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
see
["How a tool reaches the Runner"](../development/README.md#how-a-tool-reaches-the-runner)),
whatever strategy its tool normally uses. So it must have the tool on its
`PATH`, and everything the tool's Plugin shells out to, which each tool's
page lists (see
[Helmfile's](../plugins/helmfile.md#what-a-custom-helmfile-image-must-provide)).
Building `FROM` the vendor's image keeps all of that and adds only what
you need;
[Targeting an EKS cluster: the operator side](../plugins/helmfile.md#targeting-an-eks-cluster-the-operator-side)
does exactly this.

### Pulling from a private registry

turnip handles no registry credentials. The Runner Pod pulls the way any
Pod does:

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

## How a Runner gets its GitHub credential

**Nothing to configure. It is listed so you know what is and isn't there.**

A Runner Pod carries no GitHub credential. Its spec has no GitHub
credential in any container, so reading the Pod (or the etcd behind it)
yields nothing worth having. When git needs a credential it asks turnip's
own binary, running as a git credential helper in the clone init
container, which fetches one from the Server over the authenticated gRPC
channel at that moment and hands it to git on a pipe. It is never written
to a file, never put in a URL, and never passed as a command-line
argument, so a failed clone leaves nothing behind in the workspace
either.

**Two identities, one per container.** Each Runner container presents a
short-lived Kubernetes projected ServiceAccount token (its Runner
identity) to the Server, and the Pod has two, each mounted only in its own
container:

| Container | Runner identity | Audience | May |
|---|---|---|---|
| `clone` (init container) | clone identity | `turnip.ivan.vc/clone` | fetch a GitHub credential, report |
| `runner` (runs the tool) | report identity | `turnip.ivan.vc` | report only |

The container that runs the tool holds only the report identity, and the
Server refuses a GitHub credential to any caller without the clone
identity. The clone identity is mounted only in the clone init container,
which has exited before the tool starts. Both identities are validated
against the Pod they were issued to. So the tool cannot obtain a GitHub
credential, **as long as the Runner's ServiceAccount holds none of the
[grants that reach the GitHub credential](#grants-that-reach-the-github-credential)**
(`cluster-admin` holds all of them). The audiences are fixed, so there is
nothing to configure here either.

**What the credential can do.** It is scoped to the repositories that
clone will actually fetch (the pull request's own repository, plus any
submodule repositories declared in `.gitmodules` that live in the same
account), with `contents: read` and nothing else. A leaked credential is
worth one repository's source, not your whole installation.

One exception, and it is deliberate: with
`TURNIP_CLONE_SUBMODULES=recursive`, a submodule's own submodules are not
declared anywhere turnip can read before cloning them, so the repository
scope is left at the installation's full breadth. Permissions are still
narrowed to `contents: read`. Narrowing must never be the reason a clone
that worked yesterday stops working, so this widens rather than guesses.

**What it does not protect against.** Not someone with cluster-admin, who
can read anything. Not the tool in a Runner whose ServiceAccount holds
any of the
[grants that reach the GitHub credential](#grants-that-reach-the-github-credential)
(`cluster-admin` among them): it can then obtain a GitHub credential, its
own Operation's or another's. And not the tool your repository chooses
to run: that executes with the workspace mounted, by design, and turnip
has no way to know what it does with it.
