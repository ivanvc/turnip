# Server settings

> **For operators**: everything the Server reads from its environment at
> startup. End users: see [`turnip.yaml`](../using/turnip-yaml.md).

Read once, at startup, by `internal/orchestrator.ConfigFromEnv`, which
fails fast, naming every missing required variable together rather than
one restart-and-discover-the-next-one at a time.

| Variable | Required | Default | Meaning |
|---|---|---|---|
| `TURNIP_GITHUB_APP_ID` | yes | | [your GitHub App's numeric ID](#the-github-app-credentials) |
| `TURNIP_GITHUB_PRIVATE_KEY` or `TURNIP_GITHUB_PRIVATE_KEY_PATH` | yes (one of) | | [the App's private key](#the-github-app-credentials): inline PEM or a file path |
| `TURNIP_GITHUB_WEBHOOK_SECRET` | yes | | [the secret your GitHub App's webhook is configured with](#the-github-app-credentials) (HMAC-verified on every delivery) |
| `TURNIP_REDIS_ADDR` | yes | | [Redis/Valkey address](deployment.md#prerequisites); the Server's only state store |
| `TURNIP_K8S_NAMESPACE` | yes, preset by `deploy/base` (`default`) | | namespace Runner Jobs are created in |
| `TURNIP_HTTP_ADDR` | yes, preset by `deploy/base` (`:8080`) | | HTTP bind address (webhooks, `/healthz`, `/readyz`, `/metrics`) |
| `TURNIP_GRPC_ADDR` | yes, preset by `deploy/base` (`:9090`) | | gRPC bind address (Runner to Server log/result streaming) |
| `TURNIP_RUNNER_SERVER_ADDR` | yes, preset by `deploy/base` (`turnip-server:9090`) | | address a Runner Pod dials to reach this Server: a Service DNS name, not the bind address above |
| `TURNIP_RUNNER_IMAGE` | yes, preset by `deploy/base` (`ghcr.io/ivanvc/turnip-runner:latest`) | | the Runner container image a deployed Server creates Jobs with |
| `TURNIP_LOG_LEVEL` | no | `info` | [the Server's own log level](#log-level): `debug`, `info`, `warn` or `error`, case-insensitive; unset or unrecognized defaults to `info` |
| `TURNIP_MINIMIZE_OUTDATED_PLAN_COMMENTS` | no | `false` | collapse an older plan comment on the same pull request once a newer one supersedes it |
| `TURNIP_RUNNER_SERVICE_ACCOUNT` | no | unset | [ServiceAccount every Runner Pod runs as](runner-access.md#which-serviceaccount): how a Runner gets cloud credentials (EKS Pod Identity/IRSA) and in-cluster API permissions. Unset leaves Pods on the namespace's `default` ServiceAccount, which normally has neither |
| `TURNIP_CLONE_SUBMODULES` | no | `top-level` | [how far the clone initializes submodules](#clone-submodules): `none`, `top-level`, or `recursive` |
| `TURNIP_ALLOWED_OVERRIDES` | no | permits nothing | [comma-separated list of the fields a repository's own config file may set](#allowed-overrides) |
| `TURNIP_ALLOWED_IMAGES` | no | only the built-in aliases | [comma-separated `tool:image@glob` entries](runner-access.md#running-a-custom-image), each allowing a fully qualified image for one tool |
| `TURNIP_MUTATION_REQUIREMENTS` | no | requires nothing | [comma-separated conditions a pull request must meet](#mutation-requirements) before turnip runs a mutating command |

"Default" is what the Server uses when the variable is unset; a
required setting has none. "Preset by `deploy/base`" means the base's
ConfigMap already supplies a value, so the setting is required by the
Server but not something you must write yourself. Of those,
`TURNIP_RUNNER_IMAGE` should be pinned to the same release tag as the
Server, and `TURNIP_K8S_NAMESPACE` changed if you install outside
`default` (see [Installing turnip](deployment.md#installing-turnip)).

In `deploy/base`, the two credential-shaped values
(`TURNIP_GITHUB_WEBHOOK_SECRET`, `TURNIP_GITHUB_PRIVATE_KEY`) come from a
Secret; everything else comes from a ConfigMap an overlay fills in; see
[Installing turnip](deployment.md#installing-turnip) for the two values
(`TURNIP_REDIS_ADDR`, `TURNIP_GITHUB_APP_ID`) that have no preset and a
real deployment always needs to set itself, whichever install path you
use.

The Runner container's own environment variables (`TURNIP_OPERATION_ID`,
`TURNIP_REPO_URL`, and so on) are internal plumbing the Server sets
automatically on every Job it creates, with nothing here for an operator
to configure directly.

**No GitHub credential is among them.** See
[How a Runner gets its GitHub credential](runner-access.md#how-a-runner-gets-its-github-credential).

## The GitHub App credentials

`TURNIP_GITHUB_APP_ID`, `TURNIP_GITHUB_WEBHOOK_SECRET`, and one of
`TURNIP_GITHUB_PRIVATE_KEY`/`TURNIP_GITHUB_PRIVATE_KEY_PATH`, come from
the GitHub App you create; see [The GitHub App](github-app.md) for how to
get each value.

## Log level

`TURNIP_LOG_LEVEL` sets the Server's own log level: `debug`, `info`,
`warn` or `error`, case-insensitive. An empty or unrecognized value
defaults to `info` rather than stopping the Server from starting, since a
typo'd log level should degrade to a sane default, not block startup.

## Clone submodules

`TURNIP_CLONE_SUBMODULES` decides how far submodule initialization goes
for every repository this Server clones, unless a repository overrides it
with `clone.submodules` in its own `turnip.yaml` and that path is
permitted by `TURNIP_ALLOWED_OVERRIDES`. It takes `none`, `top-level`
(the default) or `recursive`; what each does is under
[`clone`](../using/turnip-yaml.md#clone-how-the-repository-is-checked-out).

Defaults on, unlike `actions/checkout`: turnip clones specifically to run
IaC that may reference submodule paths, so defaulting off would make
every repository with a submodule fail confusingly before anything
worked. An unrecognized value is a startup error.

## Allowed overrides

`TURNIP_ALLOWED_OVERRIDES` is a comma-separated list of the fields a
repository's own `turnip.yaml` may set for itself. The paths accepted
today are `runner.serviceAccount` and `clone.submodules`. The default
matches what turnip has always done: a repository cannot choose the
identity its Runner assumes, since this file is read from the pull
request's own head commit. An unrecognized path is a startup error rather
than a silently ineffective setting.

`uses` is *not* an override: turnip has no Server-side tool to fall back
to, so what a project runs is always the repository's to say, within the
images `TURNIP_ALLOWED_IMAGES` allows.

Each path is an opt-in whose cost [`SECURITY.md`](../../SECURITY.md)
states; see [Runner access](runner-access.md) for what each grants.

## Mutation requirements

A **mutating command** is any operation a tool offers other than its
plan: Helmfile's `apply` and `sync`, but not its `diff` (see
[Plan commands and mutating commands](../using/commands.md#plan-commands-and-mutating-commands)).

By default, turnip admits a mutating command on two facts: the commenter
has write access to the repository, and the project holds a plan from
this pull request. It asks nothing about the pull request itself. Nobody
need have approved the change, so one collaborator can plan and apply
their own pull request with no second pair of eyes.

`TURNIP_MUTATION_REQUIREMENTS` closes that gap. It names conditions the
pull request must meet before turnip runs any mutating command,
Helmfile's `sync` as much as its `apply`. Both are **off by default**; an
operator enables one by naming it:

| Name | Met when |
|---|---|
| `approved` | an account other than the pull request's author, with write access to the repository, has an approving review that GitHub still reports as approving |
| `mergeable` | GitHub reports the pull request has no merge conflict with its base branch |

To require both, which is what most repositories that review changes
want:

```sh
TURNIP_MUTATION_REQUIREMENTS=approved,mergeable
```

Whitespace around each name is trimmed, and a repeated name counts once.
A misspelled name stops the Server from starting, and the error lists the
recognized names. Accepting it would gate nothing while looking like it
gated something.

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
approvals when new commits are pushed"**. With it on, GitHub dismisses
the old approval on every push and turnip no longer counts it; with it
off, the approval stands, as it would for GitHub's own merge button.
Enabling that setting is how a repository requires approval of the
commit being applied. turnip then applies exactly when the repository's
review rules would let the change merge, no earlier and no stricter.

When a requirement is not met, turnip replies once per command, naming
every unmet requirement, and runs nothing: no lock changes, no Runner Job
starts, and no check run is created or updated. What each line of that
reply means, and what to do about it, is in
[`troubleshooting.md`](../using/troubleshooting.md) under "Mutation
requirements not met".
