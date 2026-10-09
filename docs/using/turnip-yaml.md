# `turnip.yaml`

> **For end users**: people writing `turnip.yaml` and commenting on pull
> requests. Looking for the Server's own settings instead? See
> [Server settings](../operating/server-settings.md).

## Reference

| Field | Required | Notes |
|---|---|---|
| `schemaVersion` | **yes** | the version of this file's schema; must be `v1alpha3`; see [`schemaVersion`](#schemaversion) |
| `clone` | no | settings for the checkout itself, shared by every project; see [`clone`: how the repository is checked out](#clone-how-the-repository-is-checked-out) |
| `clone.submodules` | no | how far submodule initialization goes; see [`clone`: how the repository is checked out](#clone-how-the-repository-is-checked-out) |
| `runner` | no | Runner settings every project starts from, with the same fields as `projects[].runner`; a project's own block is merged over it; see [`runner`: where it runs](#runner-where-it-runs) |
| `runner.serviceAccount` | no | the Kubernetes ServiceAccount the Runner Pod runs as; see [`runner`: where it runs](#runner-where-it-runs) |
| `runner.env` | no | environment variables for the tool's process; see [`runner`: where it runs](#runner-where-it-runs) |
| `runner.secrets` | no | keys of the repository's Secret to mount as files in the tool's container; see [`runner`: where it runs](#runner-where-it-runs) |
| `projects` | no | the list of projects this file defines; see [Projects](#projects) |
| `projects[].name` | no | defaults to `directory`; must be unique across the file once defaulted; see [Projects](#projects) |
| `projects[].directory` | **yes** | the tool's working directory, relative to the repo root; see [Projects](#projects) |
| `projects[].whenModified` | no | glob patterns matched against changed files; see [Projects](#projects) |
| `projects[].uses` | **yes** | what to run: `<tool>@<tag>`, or a fully qualified `<image>@<tag>` the operator allows; see [`uses`: what to run](#uses-what-to-run) |
| `projects[].with` | no | how to call it: configuration the tool itself reads; see [`with`: how to call it](#with-how-to-call-it) |
| `projects[].runner` | no | where it runs: settings for the Runner Pod, merged over the top-level `runner`; see [`runner`: where it runs](#runner-where-it-runs) |
| `projects[].runner.serviceAccount` | no | see [`runner`: where it runs](#runner-where-it-runs) |
| `projects[].runner.env` | no | see [`runner`: where it runs](#runner-where-it-runs) |
| `projects[].runner.secrets` | no | see [`runner`: where it runs](#runner-where-it-runs) |

**A key turnip doesn't recognize is an error, not a silent no-op.** Every
field above is checked by name, so a misspelling or a setting written at
the wrong level fails the check with the offending key and its line
number rather than being read and quietly discarded. The one deliberate
exception is inside `with`, which exists precisely to carry keys turnip
does not define. Every violation across the whole file is reported
together (a typo in project 3 doesn't hide a missing `directory` in
project 1), so you'll see every problem in one pass, not one at a time.

## The file's location

Committed as **`.turnip/config.yaml`**, or as **`turnip.yaml`** in the
repository root. The dedicated directory is checked first; whichever is
found first is used, and the two are never merged. Only the `.yaml`
extension is read; `.yml` is not checked at either location.

`.turnip/` also gives you somewhere to keep files the *tool* reads (an
AWS config, say) next to turnip's own; turnip reads nothing in there
besides `config.yaml`, and a project can point at a sibling through the
fixed workspace path described under
[Where the Runner puts things](#where-the-runner-puts-things).

## Example

```yaml
schemaVersion: v1alpha3
runner:                  # optional; settings every project starts from
  env:
    AWS_REGION: us-west-2
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
        AWS_PROFILE: web-deployer   # added to AWS_REGION, not replacing it
```

## Projects

Each entry in `projects:` answers three separate questions: what to run
(`uses`), how to call it (`with`), and where it runs (`runner`). Three
fields are simpler and covered here rather than in their own section:

- **`name`**: optional, defaulting to `directory` when omitted. Once
  defaulted, it must be unique across the file: two projects sharing a
  directory but no `name` collide the same way two writing the same
  `name` would.
- **`directory`**: required; the tool's working directory, relative to
  the repository root.
- **`whenModified`**: a list of glob patterns (full `**` support,
  [doublestar](https://github.com/bmatcuk/doublestar) syntax), matched
  against the paths GitHub reports as changed. A pull request whose
  changed files match none of a project's patterns never triggers it.
  **Omitted, the project is never planned automatically**, nor by a
  command that names no project; it runs only when a command names it
  (`/helmfile diff web`) or asks for every project (`*`; see
  [Which projects a command targets](commands.md#which-projects-a-command-targets)).
  Matching is textual and **does not follow symlinks**: git records a
  change under the file's real path, so a project reached through a
  symlinked directory must also list the link target (e.g. both
  `env/prod/**` and `shared/modules/**`).

## `schemaVersion`

Required, and exactly one value is accepted today: `v1alpha3`. A file
missing it, or carrying anything else (including the previous
`v1alpha2`), is rejected naming both what it found and what this turnip
supports, and it is reported *on its own*, without also listing every
field the older schema used, since the version is the one fact that
explains them.

"Version" means three unrelated things around turnip, so to be explicit
about which one this is:

| | What it versions |
|---|---|
| `schemaVersion` (top-level) | the shape of this file (this field) |
| the tag after `@` in `uses` (per project) | which image, and so which release of the tool, the Runner runs |
| turnip's own release | the Server and Runner images you deploy |

The `alpha` suffix is load-bearing, not decoration. Before turnip 1.0 the
schema is expected to break, and advancing within alpha (`v1alpha3`,
`v1alpha4`, ...) costs no turnip release and promises nobody a migration
window. It graduates to `v1` at turnip 1.0, at which point the schema
version and the project's major version coincide. There is no
compatibility shim in the meantime: a file on an older schema is
rejected outright, never silently upgraded.

## `clone`: how the repository is checked out

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
so it reuses the same list rather than inventing a second mechanism; see
[Server settings](../operating/server-settings.md). What enabling it
costs is stated in [`SECURITY.md`](../../SECURITY.md).

Submodules are fetched with the same installation credential as the
repository itself, and therefore reach **only repositories your GitHub App
is installed on**. A submodule on the repository's own host works
whatever form `.gitmodules` writes it in (SSH, `git://`, an explicit
port) because turnip rewrites those to HTTPS; it holds no SSH key and
never will. A submodule hosted anywhere else is reported by name *before*
anything is fetched, rather than failing later as a generic authentication
error.

## `uses`: what to run

One of two forms, each with exactly one tag after the `@`:

```yaml
    uses: helmfile@v1.7.4                   # a built-in alias
    uses: ghcr.io/org/helmfile-aws@v1.7.4   # a fully qualified image
```

- **`<alias>@<tag-spec>`**: an alias is the name of a tool this Server
  has a Plugin for (see [the tools turnip runs](../plugins/README.md)),
  standing for that tool's vendor image. `helmfile@v1.7.4` runs
  `ghcr.io/helmfile/helmfile:v1.7.4`.
- **`<image>@<tag-spec>`**: any other image, written in full, registry
  host included (`ghcr.io/...`, `registry.example.com:5000/...`,
  `localhost/...`). It runs only if the Server's operator allows it; see
  [Writing your own image](#writing-your-own-image).

The part after the last `@` is the **tag-spec**: one exact **tag**, or a
**digest** written `sha256:` followed by 64 lowercase hex characters.
Nothing else: no pattern, no version range, and no digest algorithm but
`sha256`. It is used **exactly as written**: turnip never adds or removes
a `v`. Write the tag the vendor publishes, the one you would `docker
pull`; each tool's page shows its vendor's tag format (see
[Helmfile's](../plugins/helmfile.md#the-helmfile-alias)).

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
(`ghcr.io/org/helmfile-aws@sha256:...`) is already the right form.

**Which tags each alias allows.** An alias allows only full versions and
digests of its vendor's image, so it can never move to a new release, or
to a floating tag, with no change to `turnip.yaml`. Each tool's page lists
the exact tag-specs its alias allows
([Helmfile](../plugins/helmfile.md#the-helmfile-alias)). Any other tag is
a validation error, and the error names the tags the alias does allow. An
operator cannot redefine an alias, so `helmfile@v1.7.4` means the same
image on every turnip Server. The vendor's image under any other tag
(its own `latest`, say) runs only written out in full and allowed by the
operator, where a reviewer sees it.

All of this is checked when the file is parsed, so it appears as a
validation error on the pull request, with every other violation, rather
than as a failed Job. turnip keeps no list of "supported" versions: any
tag the alias's patterns match is passed straight to the vendor's image,
so a tool's new release works the moment the vendor publishes it. If a
tag matches but the vendor never published it, the Job fails when its
image can't be pulled, reported as a normal operation failure.

### The apply runs what the plan ran

Whatever the tag, turnip records the exact **image digest** a plan's Pod
ran, as Kubernetes reports it, and an apply (or sync) of that plan runs
that digest, never the tag resolved again. A tag can move (`latest` by
design, and any tag a registry re-pushes), so this is what makes any
allowed tag, `latest` included, safe between plan and apply. A plan pulls
a tag on every start (`imagePullPolicy: Always`) so it sees what the tag
means now; a digest never changes, so it uses the node's cache.

If the digest could not be recorded, the apply is refused and asks you to
re-plan. If the recorded digest can no longer be pulled (deleted from the
registry since the plan), the apply fails saying so, and a re-plan
records one that exists.

### Writing your own image

An image built `FROM` a vendor's with something added (a cloud CLI, a
helm plugin), or a build the vendor has not released, runs only when
whoever runs turnip allows it (see
[Running a custom image](../operating/runner-access.md#running-a-custom-image)).

Once the operator has allowed it, name it in `uses:` exactly as any other
fully qualified image, the tool's own alias replaced:

```yaml
    uses: ghcr.io/org/helmfile-aws@v1.7.4
```

What the image itself must provide (the tool on its `PATH`, and anything
the tool shells out to) is on the tool's own page, since it depends on
how that tool reaches the Runner (for Helmfile, see
[What a custom Helmfile image must provide](../plugins/helmfile.md#what-a-custom-helmfile-image-must-provide)).
If no entry in `TURNIP_ALLOWED_IMAGES` matches yet, ask whoever operates
turnip to add one; see [Runner access](../operating/runner-access.md).

## `with`: how to call it

Configuration for the tool itself. Nothing turnip reads lives here: every
key is passed to the plugin for that tool, which is why unrecognized keys
inside `with` are accepted rather than rejected.

Each tool's page lists the keys its plugin reads (see
[Helmfile's](../plugins/helmfile.md#with)).

## `runner`: where it runs

Settings that shape the Runner Pod rather than the tool inside it:

```yaml
    runner:
      serviceAccount: turnip-runner
      env:
        AWS_PROFILE: web-deployer
        KUBECONFIG: /turnip/repository-secrets/lab-kubeconfig
      secrets:
        - lab-kubeconfig
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
  permissions; see [Server settings](../operating/server-settings.md).
  What enabling it costs is stated in [`SECURITY.md`](../../SECURITY.md).
- **`env`**: environment variables for the IaC tool's process. Unlike
  `with`, these are never interpreted by turnip at all. They are simply
  present in the environment the tool runs in, which makes this the place
  for anything the tool's own ecosystem reads.
- **`secrets`**: keys of the repository's Secret. Each listed key
  becomes a read-only file, `/turnip/repository-secrets/<key>`, in the
  tool's container, and nothing else of the Secret is mounted. Use it
  from the tool's own configuration, or point `runner.env` at it, for
  example `KUBECONFIG: /turnip/repository-secrets/lab-kubeconfig` (for
  Helmfile's other uses, such as release values and kustomize, see
  [Keys from the repository's Secret](../plugins/helmfile.md#keys-from-the-repositorys-secret)).
  A key is 1 to 253 letters, digits, `-`, `_` or `.`, other than `.`
  and not starting with `..`, and may be listed once. A Project's list
  replaces the top-level one, and `secrets: []` mounts nothing. **Any
  pull request's plan can read every key of the Secret**: this file is
  read from the pull request itself, so it can list any key, and the plan
  runs the pull request's own code. The operator creates the Secret; see
  [the repository's Secret](../operating/runner-access.md#the-repositorys-secret).

### Shared settings: the top-level `runner:` block

A `runner:` block at the top level, beside `clone:` and `projects:`,
holds settings every Project starts from, so each Project states only
what differs. It accepts exactly the fields a Project's `runner:` does,
with the same meaning, and every Runner setting turnip adds later will be
accepted at both levels.

```yaml
schemaVersion: v1alpha3

runner:
  serviceAccount: turnip-deployer
  env:
    AWS_REGION: us-west-2
  secrets:
    - shared-kubeconfig

projects:
  - directory: infra/web
    uses: helmfile@v1.7.4
  - directory: infra/api
    uses: helmfile@v1.7.4
    runner:
      serviceAccount: api-deployer   # replaces turnip-deployer
      env:
        AWS_PROFILE: api             # added beside AWS_REGION
      secrets: []                    # replaces [shared-kubeconfig]: mounts nothing
```

Each field has its own rule for combining the two levels:

| Field | Rule | What the Project runs with |
|---|---|---|
| `serviceAccount` | replaced | the Project's own if it sets one, else the top level's, else the Server's `TURNIP_RUNNER_SERVICE_ACCOUNT` |
| `env` | merged per variable | every top-level variable, plus every Project variable; where both set the same variable, the Project's value wins |
| `secrets` | replaced | the Project's own list if it sets one (even an empty one), else the top level's, else none |

A Project that writes its own `env:` keeps every top-level variable it
does not name. That is the difference from sharing the block with a YAML
anchor, where a Project writing `env:` without `<<: *env` silently
replaces the whole map.

**A Project cannot remove a shared variable, only override it.** Setting
it to an empty value (`X: ""`, or `X:` with nothing after it) overrides
it with the empty string, which is what the tool then sees; the variable
is still set. There is no syntax to unset one, deliberately.

**The top level is for what every Project uses.** A setting only some
Projects share does not belong there, since the others could not drop it.
Share it among those Projects with a YAML anchor instead, which the
parser expands before it checks the file:

```yaml
projects:
  - directory: infra/web
    uses: helmfile@v1.7.4
    runner: &eu
      env:
        AWS_REGION: eu-west-1
  - directory: infra/api
    uses: helmfile@v1.7.4
    runner: *eu
```

**A relative path in a shared `env` value resolves in each Project's own
directory.** The tool runs in its Project's `directory`, so `KUBECONFIG:
kubeconfig` at the top level names `infra/web/kubeconfig` for one Project
and `infra/api/kubeconfig` for the next. A path meant to name one file
for every Project has to be absolute; the fixed workspace path
`/turnip/src` (see
[Where the Runner puts things](#where-the-runner-puts-things)) is the
repository root in every Runner, so `/turnip/src/.turnip/kubeconfig`
means the same file wherever it is inherited.

**The `runner.serviceAccount` gate applies at either level.** The whole
file comes from the pull request, so a top-level `serviceAccount` is as
much the pull request's choice as a Project's, and moving the line to the
top of the file does not get past the gate. Without the override
permitted, a top-level `serviceAccount` refuses every Project that
inherits it. The check's Title is `runner.serviceAccount is not
permitted` either way; its summary and the PR comment say where the value
came from, so the author knows which block to edit:

```
Project "web" inherits runner.serviceAccount "turnip-deployer" from the top-level runner: block, which this turnip deployment does not permit. ...
```

where a Project's own value reads `Project "web" requested
runner.serviceAccount "turnip-deployer", ...`. `env` is ungated at both
levels.

### Reserved `env` names

Two kinds of `env` name are rejected, at either level, with every
offending name in the file reported together rather than one per attempt.
A name in the top-level block is reported once, against the file
(`turnip.yaml: runner.env["PATH"]`), not once per Project that inherits
it:

- **anything beginning with `TURNIP_`**: the Runner reads its own
  configuration out of that namespace, so a Project setting one would be
  reconfiguring the Runner rather than the tool.
- **`PATH`**: the Runner composes it at startup so the provisioned tool
  binary is found first; replacing it wholesale would hide the very
  binary the operation needs.

Values reach the tool byte-for-byte, including values containing `$(...)`;
turnip escapes them on the way through, since Kubernetes would otherwise
expand `$(VAR)` inside an environment value before the tool ever saw it.

One thing worth being deliberate about: this file is read from the pull
request's own head commit, so `runner.env` is something a PR author can
change. That is the same trust boundary the tool's own committed
configuration already sits on (a `.tf` or helmfile in the branch can
redirect a backend or a role just as readily), but if that boundary
matters to you, it's the Server-side settings, not `env`, that decide
what credentials the Runner holds in the first place; see
[Runner access](../operating/runner-access.md).

## Where the Runner puts things

Everything turnip creates inside a Runner Pod lives under `/turnip`:

| Path | Contents | Present when |
|---|---|---|
| `/turnip/src` | the repository, cloned at the pull request's head commit; a project's `directory` is relative to this | always |
| `/turnip/tools` | the provisioned IaC tool binary, prepended to the Runner's `PATH` | copy-out tools only |
| `/turnip/bin` | turnip's own runner binary, handed to the tool's image | run-in-image tools and custom images only |

A *copy-out* tool is a single binary copied out of the vendor's image
into turnip's own; a *run-in-image* tool runs inside the vendor's image
itself. Helmfile is run-in-image, so for it `/turnip/tools` is absent and
`/turnip/bin` is present (see
[How a tool reaches the Runner](../development/README.md#how-a-tool-reaches-the-runner)
for the detail).

`/turnip/src` is a fixed path rather than a per-run temporary directory
specifically so committed configuration may reference it: an absolute
path in a helmfile or a `.tfvars` resolves the same way on every run.
It's the one worth naming directly; the other two are turnip's own
plumbing and may move.

The repository is cloned by an initContainer running turnip's image, not
by the container that runs your tool, which is what lets that container
be the tool vendor's own image (or one built from it), needing nothing
from it but the tool.

## Next

- [Commands](commands.md): what you can trigger by comment, and who can
  trigger it
- [Pull requests](pull-requests.md): what an automatic plan does and what
  you'll see
- [Helmfile](../plugins/helmfile.md): everything specific to Helmfile,
  the one tool turnip runs today
