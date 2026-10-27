# Helmfile

[Using Helmfile](#using-helmfile) is for people writing `turnip.yaml` and commenting on pull requests.
[Running Helmfile](#running-helmfile) is for operators deploying and configuring the Server.

Everything Helmfile-specific about turnip is on this page. The general
mechanics (`turnip.yaml`, trigger comments, locks, the `turnip` check) are
in [`using/turnip-yaml.md`](../using/turnip-yaml.md),
[`using/commands.md`](../using/commands.md) and
[`using/pull-requests.md`](../using/pull-requests.md), and how the plugin
works inside is in
[`development/README.md`](../development/README.md#the-helmfile-plugin).

## Using Helmfile

```yaml
projects:
  - directory: infra/web
    uses: helmfile@v1.7.4
    with:
      environment: staging
```

Its trigger word is `/helmfile` (`/helmfile diff web`), beside `/turnip`,
which considers projects of every tool.

### Operations

| Operation | Kind | What it does |
|---|---|---|
| `diff` | plan | `helmfile diff`: shows what each release would change, and changes nothing |
| `apply` | mutating | `helmfile apply`: diffs again and upgrades only the releases that changed |
| `sync` | mutating | `helmfile sync`: upgrades every release, whether or not it changed |

`diff` is the plan command: the one turnip runs automatically on a pull
request, and the only one that takes extra arguments. `apply` and `sync`
are mutating commands: they need write access and a plan from this pull
request, may be held back by the operator's mutation requirements, and
replay the scope the `diff` recorded. See "Plan commands and mutating
commands" in [`using/commands.md`](../using/commands.md#plan-commands-and-mutating-commands).

turnip runs Helmfile as
`helmfile --no-color [--environment <environment>] <operation> [arguments...]`,
in the project's `directory`. Color is always off: a comment cannot
render it, and its codes would show up as text such as `[1;34m`.

#### `sync` acts even when the diff is empty

`helmfile sync` upgrades every release regardless of the diff, so it can
change infrastructure precisely when `diff` found nothing. Helmfile is
therefore a tool that *can act without changes*, and two things follow:

- **A Helmfile project keeps its lock even when its diff comes back
  clean.** For a tool that cannot act without changes, a plan with nothing
  to apply releases the lock; for Helmfile it holds it, so `sync` stays
  available. See "What releases a lock" in
  [`using/pull-requests.md`](../using/pull-requests.md#what-releases-a-lock).
- **A Helmfile project always needs its apply or sync to satisfy the
  `turnip` check.** A plan that finds nothing to apply otherwise counts as
  done, but not for Helmfile, whose `sync` acts even when the diff is
  empty. See "Requiring turnip before merge" in
  [`using/pull-requests.md`](../using/pull-requests.md#requiring-turnip-before-merge).

#### Removing a release

There is no `destroy` for Helmfile. `helmfile destroy` has no dry-run and
uninstalls everything its selector matches regardless of `installed:`, so
no plan could show you what it would remove, and turnip only runs what a
plan described.

To remove a release, mark it `installed: false`. `diff` reports it as a
pending removal and `apply` carries it out, reviewed like any other
change.

### The `helmfile` alias

`uses: helmfile@<tag>` runs the vendor's image, `ghcr.io/helmfile/helmfile`,
at that tag: `helmfile@v1.7.4` runs `ghcr.io/helmfile/helmfile:v1.7.4`.

| Alias | Image | Allowed tag-specs |
|---|---|---|
| `helmfile` | `ghcr.io/helmfile/helmfile` | `v*.*.*`, `sha256:*` |

The tag is used exactly as written, and Helmfile's tags carry a `v`, so
it is `helmfile@v1.7.4`, not `helmfile@1.7.4`. `helmfile@latest`,
`helmfile@v1.7` and `helmfile@1.7.4` are all validation errors, and the
error names the tags the alias does allow. How tags and digests are
checked, and why an apply runs the digest its plan ran, is under
"`uses`: what to run" in
[`using/turnip-yaml.md`](../using/turnip-yaml.md#uses-what-to-run).

Any other image (the vendor's under another tag, or one built from it)
is written in full, `uses: ghcr.io/org/helmfile-aws@v1.7.4`, and runs only
if the Server's operator allows it; see
[Running a custom Helmfile image](#running-a-custom-helmfile-image).

### `with:`

| Key | Meaning |
|---|---|
| `environment` | Helmfile's own `--environment` flag, passed before the operation (`helmfile --no-color --environment staging diff`) |

Omitted, no `--environment` is passed and Helmfile uses its own default.
Other keys under `with:` are accepted and ignored.

### Reaching a cluster

A project with no `KUBECONFIG` in its environment acts on **the cluster
turnip runs in**, as whatever ServiceAccount the operator gave the Runner;
there is nothing to configure. Any other cluster needs a kubeconfig,
which you commit to the repository and point `KUBECONFIG` at through
`runner.env`. turnip does not generate one, and doesn't need to.

A kubeconfig that must stay out of git can come from the repository's
Secret instead (see
[Keys from the repository's Secret](#keys-from-the-repositorys-secret)).
List its key in `runner.secrets`, point `KUBECONFIG` at
`/turnip/repository-secrets/<key>`, and select the context with
helmfile's `kubeContext`, which passes `--kube-context`:

```yaml
projects:
  - directory: web
    runner:
      secrets: [web-kubeconfig]
      env:
        KUBECONFIG: /turnip/repository-secrets/web-kubeconfig
```

The mounted file is read-only, so never run anything that writes the
kubeconfig, such as `kubectl config use-context` or
`aws eks update-kubeconfig`: it fails. Read the warning in
[What that grant means](#what-that-grant-means) first, because a pull
request's plan can read every key of the Secret.

#### Targeting an EKS cluster: the repository side

This is the half of the recipe that lives in the repository. The other
half, the identity the Runner signs with and the image it runs, is the
operator's: see
[Targeting an EKS cluster: the operator side](#targeting-an-eks-cluster-the-operator-side).

No step has to run before the tool. A kubeconfig produced by
`aws eks update-kubeconfig` does not contain credentials: it tells the
client to run `aws eks get-token` whenever it needs one. helm, helmfile
and kubectl all run that command themselves, on demand. So the
kubeconfig is generated **once**, committed to the repository, and the
Runner only needs `aws` on its PATH and an AWS identity to sign with.

**Generating the kubeconfig**, once, on your own machine, from the
repository root, with any credentials allowed to call
`eks:DescribeCluster`:

```sh
aws eks update-kubeconfig --name <cluster> --region <region> \
  --kubeconfig .turnip/kubeconfig --alias <cluster>
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

Commit it as `.turnip/kubeconfig`, beside turnip's own `config.yaml`.

**Pointing every Project at it**, once, in the top-level `runner.env`,
and choosing the context in helmfile (`kubeContext`, under
`helmDefaults` or per release):

```yaml
schemaVersion: v1alpha3

runner:
  env:
    KUBECONFIG: /turnip/src/.turnip/kubeconfig

projects:
  - directory: infra/web
    uses: ghcr.io/org/helmfile-aws@v1.7.4
  - directory: infra/api
    uses: ghcr.io/org/helmfile-aws@v1.7.4
```

The path has to be absolute. Every Project inherits this value, and the
tool runs in each Project's own directory, so a relative `KUBECONFIG`
would name a different file, most likely a missing one, in each.
`/turnip/src` is the repository root in every Runner (see "Where the
Runner puts things" in
[`using/turnip-yaml.md`](../using/turnip-yaml.md#where-the-runner-puts-things)),
so this path names the committed file from any Project, and helm, which
helmfile runs per release, reads the same one.

Setting it once also closes a quiet failure. A Project that is missing
`KUBECONFIG` does not fail: with no kubeconfig, helm and helmfile fall
back to in-cluster configuration, and plan and apply against **the
cluster turnip itself runs in**. Set at the top level, a Project cannot
lose it by omission, and one that writes its own `env:` keeps it.

**The `uses:` line** names an image with the AWS CLI in it, since the
vendor's has none, in place of the alias:

```yaml
    uses: ghcr.io/org/helmfile-aws@v1.7.4
```

The operator builds that image and allows it; until they do, this line
is a validation error naming `TURNIP_ALLOWED_IMAGES`.

**When a role *is* assumed.** Only if a cluster must be reached as a role
other than the Pod's, typically a cluster in another AWS account whose
access entries don't accept the Pod's role. Add `--role-arn <arn>` to that
user's `args`, and have the operator allow the Pod's role to assume it.

Helm plugins authenticate separately, and to different things.
`helm-secrets` decrypting through a cloud KMS, or a chart pulled from a
private registry, needs *cloud* credentials rather than cluster ones.
Those come from the Pod's identity (the operator's side) and from
`runner.env` for whatever the plugin reads out of its environment.

### Keys from the repository's Secret

The operator can give the repository a Kubernetes Secret for anything that
must stay out of git (see
[The repository's Secret](../operating/runner-access.md#the-repositorys-secret)).
Each key a Project lists in `runner.secrets` is a read-only file,
`/turnip/repository-secrets/<key>`, and a helmfile can use it in several
ways:

- **A kubeconfig**, through `KUBECONFIG`; see
  [Reaching a cluster](#reaching-a-cluster).
- **A value for a release**, through helmfile's built-in
  [vals](https://github.com/helmfile/vals) support.
  `ref+file:///turnip/repository-secrets/<key>` (three slashes, since the
  path is absolute) reads the whole file, and `fetchSecretValue` resolves
  it in a `.gotmpl` helmfile (`expandSecretRefs` resolves a map of them):

  ```yaml
  # api/helmfile.yaml.gotmpl
  environments:
    default:
      values:
        - db:
            password: ref+file:///turnip/repository-secrets/api-db-password
  ---
  releases:
    - name: api
      chart: ./charts/api
      values:
        - db:
            password: {{ .Values.db.password | fetchSecretValue | quote }}
  ```

  Put such a value only into a Kubernetes Secret resource. The plan's
  diff hides a Secret's data, but it prints every other kind as it is (a
  ConfigMap, a container's `env` value, an annotation), and the diff is
  the output turnip posts to the pull request.
- **A Secret generated by kustomize.** helmfile builds a release whose
  `chart:` is a kustomization directory with kustomize's
  `--load-restrictor=LoadRestrictionsNone`, so a `secretGenerator` there
  can read the mounted file by its absolute path:

  ```yaml
  # lab/db/kustomization.yaml
  secretGenerator:
    - name: db
      files:
        - password=/turnip/repository-secrets/lab-db-password
  ```

  `kustomize build` run by hand refuses the same file (`security; file
  ... is not in or below ...`) unless given that flag too. helmfile runs
  `kustomize`, or `kubectl kustomize` when only `kubectl` is on the
  image's `PATH`, so the image needs one of them (see
  [What a custom Helmfile image must provide](#what-a-custom-helmfile-image-must-provide)).
- **A decryption key**, through an environment variable. helm-secrets
  with SOPS and age reads the key from `SOPS_AGE_KEY_FILE`:

  ```yaml
  runner:
    secrets: [sops-age-key]
    env:
      SOPS_AGE_KEY_FILE: /turnip/repository-secrets/sops-age-key
  ```

Whatever the use, a pull request's plan can read every key of the Secret;
read [What that grant means](#what-that-grant-means) before putting one
there.

### Gotchas

#### Change counts

The counts on a check's title and in the PR comment (`+1 ~4 -2`) come
from Helmfile's own output, read differently for the plan and for the
mutating commands.

**`diff` counts releases with a diff, and the ones it will remove.**
Every release whose diff shows something counts as changed (`~`). A
release set to `installed: false` that is still deployed has no diff:
Helmfile lists it as `DELETED` in the `Affected releases are:` summary
it prints after the diffs, and turnip counts it as destroyed (`-`) from
that summary. A `diff` never reports `+`.

**`apply` and `sync` count what Helmfile reports acting on**: the
releases its closing summary names as updated, reinstalled or deleted,
rather than anything recomputed from the plan. Updated and reinstalled
releases count as changed (`~`), deleted ones as destroyed (`-`); failed
releases are not counted. A helmfile with sub-helmfiles reports on each,
and the counts are summed. An apply or sync that changed nothing shows
`no changes`, exactly as a plan does.

**`sync` counts every release it ran**, since it upgrades all of them
whether or not they changed, not only the ones that did.

**A release whose diff is "empty after suppression" still counts.** When
Helmfile suppresses a diff's content (a Secret's, say), `diff` still
prints a line for the release, so it counts as changed, and `apply`
still upgrades it and reports it. It is counted once, in both.

When the counts can't be read, the run renders as `succeeded`, with no
counts, rather than as `no changes`. `succeeded` never means no changes.
It happens when:

- **the plan was passed `-q`, `--quiet` or `--log-level warn` (or
  higher).** Helmfile prints its summaries at its info log level, so
  these silence them: the plan itself reads `succeeded`, since the
  releases it would remove can't be seen, and so does the apply or sync,
  which replays the plan's arguments. `--debug` overrides them, as it
  does in Helmfile. Leave them off to get counts.
- **the image runs a Helmfile older than v1.6.0**, whose summary turnip
  does not read. The operation still succeeds or fails normally. See
  [Gotchas for operators](#gotchas-for-operators).

**Don't change Helmfile's log level for a Project**, neither with flags
nor with `HELMFILE_LOG_LEVEL` or `HELMFILE_QUIET` in the image. turnip
reads its counts from what Helmfile prints, so with less printed the
counts, and the next steps the pull request offers from them, may not be
accurate. A flag at least makes the run read `succeeded`; a variable in
the environment isn't seen at all.

If an apply or sync that changed something, or a plan that removes a
release, reads `no changes`, the image may be silencing Helmfile's
summary through its environment; see
[Gotchas for operators](#gotchas-for-operators).

#### A failed operation shows Helmfile's messages, not the diffs

When a `diff`, `apply` or `sync` fails, its output in the comment is
what Helmfile wrote to standard error alone: its progress, its release
tables and every error. What it wrote to standard output, the diffs, is
left out, and a line says how much:

```diff
@@ turnip: infra/web, helmfile v1.7.4 @@
@@ turnip: helmfile --no-color diff @@
@@ turnip: 30000 lines of standard output omitted because the operation failed @@
...Helmfile's messages and errors...
@@ turnip: exit 1 in 41s @@
```

The diffs of a plan that failed cannot be applied, and on a first
install they are megabytes with the errors somewhere after them. Fix
what the errors name and plan again to see the diffs. An operator can
read the omitted lines in the Runner's log while its Pod exists; see
[Reading a Runner's log](../operating/troubleshooting.md#reading-a-runners-log).

A successful operation shows everything, in the order Helmfile wrote it.

#### `--color` is refused

turnip always passes `--no-color`, and Helmfile refuses the two flags
together (`--color and --no-color cannot be specified at the same
time`). A command that adds `--color` fails with that message.

#### `--detailed-exitcode` makes a plan with changes fail

With `--detailed-exitcode`, Helmfile exits 2 when a diff found changes.
turnip reads any exit code but 0 as a failure, so such a plan shows as
failed, cannot be applied, and shows no diffs (see above). Leave the
flag off.

#### Several files in `helmfile.d` are written at once

Helmfile processes the files of a `helmfile.d` directory in parallel, so
one file's diffs and another's messages are written at the same moment.
In a successful operation's output those lines can land either way
round, as they do in a terminal. Pass `--sequential-helmfiles` to have
Helmfile process the files one at a time.

#### Don't use helmfile hooks

A helmfile hook runs a command of the helmfile's choosing inside turnip's
Runner, with the Runner's credentials. The `prepare` and `cleanup` hooks
run for every command, `diff` included, so a plan runs them as soon as a
pull request opens, before anyone reviews it. A plan is not read-only.

Move that work into the cluster instead: a Kubernetes `Job` or `CronJob`
your releases deploy runs under its own ServiceAccount, with only the
permissions you give it, and exists only once an apply has deployed it,
which is the reviewed step. turnip does not stop a helmfile from using
hooks, so keeping them out is up to the repository. See
[What that grant means](#what-that-grant-means) for what a hook can do.

#### A relative `KUBECONFIG` resolves per project

Helmfile runs in each Project's `directory`, so a relative path in
`runner.env` (`KUBECONFIG: kubeconfig`) names a different file for each
Project. A `KUBECONFIG` set in the top-level `runner:` block has to be
absolute, under `/turnip/src`, to name one file for every Project. See
"Shared settings: the top-level `runner:` block" in
[`using/turnip-yaml.md`](../using/turnip-yaml.md#shared-settings-the-top-level-runner-block)
and [the repository side of the EKS recipe](#targeting-an-eks-cluster-the-repository-side).

#### A missing `KUBECONFIG` targets turnip's own cluster

A Project with no `KUBECONFIG` does not fail: helm and helmfile fall back
to in-cluster configuration and act on the cluster turnip runs in. Set it
once at the top level; see
[the repository side of the EKS recipe](#targeting-an-eks-cluster-the-repository-side).

#### A clean diff keeps its lock

A Helmfile project holds its lock, and needs its apply or sync for the
`turnip` check, even when its diff is clean; see
[`sync` acts even when the diff is empty](#sync-acts-even-when-the-diff-is-empty).

#### A silent `--wait` is stopped

turnip stops a tool that writes nothing for the idle timeout (30 minutes
by default, set by the operator with `TURNIP_RUNNER_IDLE_TIMEOUT`). A
`--wait` that stays silent for longer than that is stopped: the check
fails titled `helmfile stopped: no output for 30m0s`. Helmfile is
interrupted first, so it can release its locks. See
[A tool stopped for silence](../using/troubleshooting.md#a-tool-stopped-for-silence).

#### There is no `destroy`

Remove a release with `installed: false` and apply; see
[Removing a release](#removing-a-release).

### Troubleshooting

Entries that are Helmfile-specific. Everything else is in
[`using/troubleshooting.md`](../using/troubleshooting.md).

- **A tag the `helmfile` alias does not allow** (`helmfile@latest`,
  `helmfile@v1.7`, `helmfile@1.7.4` missing its `v`): the validation error
  names the tags the alias allows: full versions `v*.*.*` and digests.
  Write the full version the vendor publishes, e.g. `helmfile@v1.7.4`.
  See [The `helmfile` alias](#the-helmfile-alias).
- **`helmfile exited 1`** (or another code): Helmfile itself ran and
  failed. The reason is in its output, in the results comment, which for
  a failed operation is Helmfile's messages without the diffs (see
  [A failed operation shows Helmfile's messages, not the diffs](#a-failed-operation-shows-helmfiles-messages-not-the-diffs));
  turnip does not interpret it. See "Reading a failed check's title" in
  [`using/troubleshooting.md`](../using/troubleshooting.md#reading-a-failed-checks-title).
- **`helmfile could not be started`**: the `helmfile` binary was missing
  from the image (a custom image without it on its `PATH`), or the
  operation isn't one of `diff`, `apply` and `sync`. The results comment carries the
  error, including when the project's working directory could not be
  entered. See "Plugin execution errors" in
  [`using/troubleshooting.md`](../using/troubleshooting.md#plugin-execution-errors).
- **`exec: "helm": executable file not found in $PATH`, or Helmfile
  reporting it can't find a helm plugin**: the image lacks something
  Helmfile shells out to. Ask the operator to check it against
  [What a custom Helmfile image must provide](#what-a-custom-helmfile-image-must-provide).
- **`secrets is forbidden: User "system:serviceaccount:..." cannot list
  resource "secrets"`**: the Runner's ServiceAccount has no RBAC in the
  namespace a release lives in. Helm keeps release state in a Secret in
  that namespace, so even a read-only `diff` must list Secrets there. This
  is the operator's to fix; see [What to grant it](#what-to-grant-it).
- **An apply or sync shows `succeeded` instead of counts, or `no changes`
  when something changed**: see [Change counts](#change-counts).
- **A plan or apply acted on the cluster turnip runs in rather than the
  one you meant**: the Project has no `KUBECONFIG`; see
  [A missing `KUBECONFIG` targets turnip's own cluster](#a-missing-kubeconfig-targets-turnips-own-cluster).

## Running Helmfile

### The vendor image

The `helmfile` alias runs the vendor's image, `ghcr.io/helmfile/helmfile`,
at a full version (`v*.*.*`) or a digest, as the repository writes it. An
operator cannot redefine the alias, so `helmfile@v1.7.4` means the same
image on every turnip Server, and turnip keeps no list of supported
versions: a new Helmfile release works the moment the vendor publishes
it.

The vendor's image under any other tag (its own `latest`, say) runs only
written out in full in `uses:` and allowed with an entry such as
`helmfile:ghcr.io/helmfile/helmfile@latest` in `TURNIP_ALLOWED_IMAGES`.

The vendor's image bundles helm, sops and the standard helm plugins,
which is everything turnip needs from it. It has no cloud CLI.

### Running a custom Helmfile image

An image built `FROM` the vendor's with something added (a cloud CLI, a
helm plugin) runs when you allow it with a `helmfile:` entry in
`TURNIP_ALLOWED_IMAGES`, for example
`helmfile:ghcr.io/org/helmfile-aws@v*.*.*`. The entry syntax, what
allowing an image costs, and pulling from a private registry are in
"Running a custom image" in
[`operating/runner-access.md`](../operating/runner-access.md#running-a-custom-image).

#### What a custom Helmfile image must provide

turnip runs Helmfile *inside* its image (run-in-image, see "How a tool
reaches the Runner" in
[`development/README.md`](../development/README.md#how-a-tool-reaches-the-runner)),
with turnip's runner binary handed to it, rather than copying one binary
out. Helmfile needs that: it shells out to `helm`, and helm finds its
plugins through an environment its image sets. So Helmfile's plugins and
helpers come from the image, and a custom image must have `helmfile` on
its `PATH` and everything Helmfile shells out to:

- `helm`;
- the helm plugins its commands use: helm-diff for `diff`, and
  helm-secrets if the repository's helmfiles decrypt anything;
- `sops`, if they do;
- `kustomize` or `kubectl`, if a release's `chart:` is a kustomization.

Needing one the vendor's image doesn't bundle means building an image
that has it and allowing it, not configuring turnip. Building `FROM` the
vendor's image keeps all of the above and adds only what you need; the
[EKS recipe](#targeting-an-eks-cluster-the-operator-side) below does
exactly this. A helper missing from a version the vendor's own image ships
is a turnip bug worth reporting.

### Authentication and cluster access

turnip ships no ServiceAccount and no RBAC for the Runner (see "What the
Runner's ServiceAccount needs" in
[`operating/runner-access.md`](../operating/runner-access.md#what-the-runners-serviceaccount-needs)).
This section is what a Helmfile Runner needs.

#### Inside the cluster turnip runs in

Inside the cluster turnip runs in there is nothing to configure: the
Runner Pod carries a projected ServiceAccount token, and helm's client
finds it the way any in-cluster client does. No kubeconfig, no cloud
credentials, no `aws eks update-kubeconfig`. *Which* ServiceAccount comes
from `TURNIP_RUNNER_SERVICE_ACCOUNT`, or from `runner.serviceAccount`
(top-level or a Project's) where you permit that override.

Targeting any **other** cluster needs a kubeconfig, which the repository
commits; see
[Targeting an EKS cluster: the repository side](#targeting-an-eks-cluster-the-repository-side).

#### Targeting an EKS cluster: the operator side

This is the half of the recipe that lives in the deployment: the
identity the Runner signs with and the image it runs. The kubeconfig,
the `runner:` block and the `uses:` line are the repository's; see
[Targeting an EKS cluster: the repository side](#targeting-an-eks-cluster-the-repository-side).

**The identity.** With EKS Pod Identity, the Runner Pod already *is* the
IAM role associated with its ServiceAccount; nothing assumes a role. The
Pod Identity agent injects `AWS_CONTAINER_CREDENTIALS_FULL_URI` and
`AWS_CONTAINER_AUTHORIZATION_TOKEN_FILE`, and the AWS CLI's default
credential chain uses them. The target cluster grants that role access
through an access entry. Select the ServiceAccount with
`TURNIP_RUNNER_SERVICE_ACCOUNT`, or let the repository choose it with
`runner.serviceAccount` (once at the top level, or per Project) where the
override is permitted.

**When a role *is* assumed.** Only if a cluster must be reached as a role
other than the Pod's, typically a cluster in another AWS account whose
access entries don't accept the Pod's role. The repository adds
`--role-arn <arn>` to that user's `args` in its kubeconfig; allow the
Pod's role to assume it.

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
`ghcr.io/org/helmfile-aws:v1.7.4`), then allow it:

```
TURNIP_ALLOWED_IMAGES=helmfile:ghcr.io/org/helmfile-aws@v*.*.*
```

The repository then names it in `uses:` in place of the alias. If the
image lives in ECR in the cluster's own account, the nodes can usually
pull it with no further setup; otherwise see "Pulling from a private
registry" under "Running a custom image" in
[`operating/runner-access.md`](../operating/runner-access.md#pulling-from-a-private-registry).

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
or CRDs. **Granting only Secrets is not the fix either**: it clears the
error above, and then the next one arrives for a CRD, a namespace, or
whatever kind a chart's `lookup` consults.

For a Runner that applies, bind `cluster-admin` and know that you did:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: turnip-runner
subjects:
  - kind: ServiceAccount
    name: turnip-runner
    namespace: turnip-runners   # the Runner namespace: TURNIP_RUNNER_NAMESPACE, or the Server's namespace when unset
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

Two things make a hook more than a change to the IaC:

- **It gets around mutation requirements.** A plan is never gated by
  [`TURNIP_MUTATION_REQUIREMENTS`](../operating/server-settings.md#mutation-requirements).
  Under `approved`, someone with write access cannot apply their own
  change without another person's approval, but a `prepare` hook in
  their pull request runs with the Runner's credentials as soon as the
  pull request opens. If those credentials can change the cluster, the
  hook is an unreviewed apply.
- **Its output can reach the pull request.** A hook with `showlogs: true`
  prints its command's output, and turnip includes the tool's output in
  the results comment without redacting it. Whatever the hook can read in
  the Runner container, it can print where the pull request's author
  reads it.

`presync` and `postsync` hooks run only on `apply` and `sync`, so they
are gated like any mutating command; `prepare` and `cleanup` are the ones
a plan runs.

**Keys mounted from the repository's Secret are readable during `diff`.**
A `prepare` or `cleanup` hook can read them, and so can templating with no
hook at all: `exec` or `readFile` in a `.gotmpl` helmfile or values file
runs during `diff`. Either can print a key into the results comment (a
hook with `showlogs: true`, or a value rendered into a manifest that the
diff prints). That comment is read by everyone who can read the repository,
a wider group than those trusted with the cluster, and the Runner Pod's
log mirrors the output unredacted. Blocking hooks does not close this,
because templating needs no hook, and neither does leaving a key out of
the default branch's `runner.secrets`: a pull request can list any key of
its repository's Secret. Put a key in the Secret only if you trust every
collaborator with it. See
[The repository's Secret](../operating/runner-access.md#the-repositorys-secret).

Short of removing hooks (see
[Don't use helmfile hooks](#dont-use-helmfile-hooks)), this is the same
trust boundary the committed IaC already sits on (a chart can redirect a
resource as readily as a hook can run one), but it is sharper than it
looks, and two things follow:

- **Run turnip only on repositories whose contributors you already trust
  with the cluster.** turnip refuses to run anything on a pull request
  from a fork, on both trigger paths (see "Pull requests from forks" in
  [`using/pull-requests.md`](../using/pull-requests.md#pull-requests-from-forks)), so this is about
  the people who can push a branch to the repository itself. For them,
  opening a pull request is enough to run a hook with the Runner's
  credentials.
- **Scope by cluster, not by rule set.** The practical blast radius is
  decided when you choose which cluster turnip runs in and which
  repository drives it, not by trimming permissions that will grow back
  the next time a chart adds a resource kind.

See also [`SECURITY.md`](../../SECURITY.md).

### Gotchas for operators

- **Counts after an `apply` or `sync` need Helmfile v1.6.0 or later.**
  turnip reads what changed from the summary Helmfile prints when it
  finishes, in the form v1.6.0 introduced. An older version still runs,
  and the operation still succeeds or fails normally; it renders as
  succeeded with no counts rather than as `no changes`. See
  [Change counts](#change-counts).
- **Don't set `HELMFILE_QUIET` or `HELMFILE_LOG_LEVEL`** in an image
  turnip runs. Helmfile prints its summary at its info log level, and
  turnip sees the command line, not the environment, so a level set this
  way silences the summary without the `succeeded` fallback: the run then
  reads `no changes`, even for a plan that removes a release. The same holds for either variable set through a
  repository's `runner.env`.
