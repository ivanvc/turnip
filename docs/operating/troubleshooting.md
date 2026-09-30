# Troubleshooting (operators)

> **For operators**: deploying and configuring the Server. End users: see
> [Troubleshooting](../using/troubleshooting.md) for what shows up on a
> pull request.

What to check in the Server's own logs and the cluster when something
goes wrong beneath what a pull request sees.

## GitHub API errors

**Symptom**: a check run or comment never appears at all, or appears much
later than expected, with no explanation in the PR.

- **No response to a webhook at all**: check the Server's logs for the
  installation ID in question. A token-generation failure returns
  HTTP 500 to GitHub, which retries the delivery automatically, so a
  transient failure usually self-heals within GitHub's own retry window.
  If it persists, check the GitHub App's credentials
  ([Prerequisites](deployment.md#prerequisites)) haven't been
  rotated/revoked.
- **Check run never updates, but a comment does appear**: check-run
  failures are soft: the Server logs the error and falls back to
  comment-only status, so the Operation still completed normally. This is
  a GitHub API hiccup, not a turnip bug; the PR comment is the
  authoritative result either way.
- **Comment missing after a successful-looking run**: comment posting
  retries up to 3 times with backoff before giving up (and logging). If
  every retry failed, GitHub's API was very likely degraded at that
  moment. Check GitHub's own status page.

## Runner execution errors

**Symptom**: the check run/comment reports a failure with no tool output,
or nothing happens for several minutes before a timeout is reported.

- **"creating Runner Job" failure**: the Kubernetes API rejected Job
  creation, almost always an RBAC or resource-quota problem in the
  cluster. Check the Server's own ServiceAccount has the permissions
  `deploy/base/role.yaml` grants (Jobs create/delete/get/list/watch, Pods
  get/list) and that the namespace isn't at a resource quota limit.
- **Timeout ("didn't start within 5 minutes")**: the Job never got a Pod
  scheduled, or the Pod never reached the Server over gRPC in time. Check
  `kubectl get pods` in the Runner namespace for `Pending`/`ImagePullBackOff`
  (a scheduling or image-pull problem) or `CrashLoopBackOff` (check the
  Pod's own logs). A network policy blocking Runner→Server gRPC traffic
  produces this symptom too: a Runner dials `TURNIP_RUNNER_SERVER_ADDR`
  (see [Server settings](server-settings.md)), which in `deploy/base` is
  the `grpc` port (`9090`) of the Service in `deploy/base/service.yaml`.
- **Clone failure** (e.g. `clone failed: merge conflict`): the PR's
  branch conflicts with its base, or the Runner's GitHub token couldn't
  read the repository. A merge conflict needs resolving in the PR the
  normal way (no turnip-side action); anything else, check the GitHub
  App's repository access.
- **A tool complaining about a path or repository you know exists** (e.g.
  `Error: repo .. not found` from `helm pull ../charts/...`, or a file a
  tool insists is missing): most often an **uninitialized submodule**. The
  directory is present but empty, so the tool reaches through the gap and
  reports whatever its own parser made of the path, a message that names
  neither the submodule nor the repository it came from. Check whether the
  path lives in a submodule, then whether the Server has
  `TURNIP_CLONE_SUBMODULES=none`, or the repository set
  `clone.submodules: none` in its own config file. A submodule nested
  inside another needs `recursive`; `top-level` fetches only the first
  layer.
- **`remote: Repository not found` for a submodule on the same host**:
  the fetch *was* authenticated (turnip rewrites submodule URLs to carry
  the installation token), so this almost never means the repository is
  missing or misspelled. GitHub returns the same answer for a repository
  the credential cannot see, deliberately, so it doesn't reveal which
  private repositories exist. Check that the **GitHub App is installed on
  the submodule's repository**: an installation set to "only select
  repositories" commonly includes the parent but not a shared chart or
  module repository next to it. turnip appends a hint saying as much.
- **`submodule ... is hosted on <host>, which this installation token
  cannot authenticate`**: the submodule lives somewhere the GitHub App
  isn't installed: another forge, or a self-hosted server. turnip has
  only the installation token, which authenticates nothing off the
  repository's own host, so this is reported before any fetch is attempted
  rather than surfacing later as a generic authentication error. A
  submodule on the *same* host works whatever URL form it uses: SSH and
  other schemes are rewritten to authenticated HTTPS automatically.
- **An error from inside the tool about cluster access** (for Helmfile,
  `secrets is forbidden: User "system:serviceaccount:..." cannot list
  resource "secrets"`): the Runner's ServiceAccount lacks what the tool
  needs in the target cluster. What each tool needs, and what that grant
  means, is on its page; for Helmfile, see
  [What to grant it](../plugins/helmfile.md#what-to-grant-it).

## Failed checks that are the Server's to fix

**Symptom**: a per-project check failed with one of these titles. The
change being planned is not the cause; the end-user side of each is under
[Reading a failed check's title](../using/troubleshooting.md#reading-a-failed-checks-title).

- **`lock could not be acquired`**: turnip could not reach Redis (or
  Redis refused the write) while taking the project's lock; the check's
  details carry the error. A re-plan usually succeeds; if it keeps
  happening, check Redis and the Server's logs.
- **`operation could not be recorded`**: the lock was taken but saving
  the Operation Record to Redis failed, so no Runner was started. If it
  recurs, check Redis and the Server's logs.
- **`Runner Job could not be built`**: the Server could not assemble the
  Runner Job's spec, so nothing was sent to Kubernetes. The check's
  details carry the error. If it recurs, check the Runner settings in the
  Server's configuration (image, ServiceAccount, resources) that go into
  the Job spec; see [Server settings](server-settings.md) and
  [Runner access](runner-access.md).

## Mutation requirement refusals

When a pull request does not meet `TURNIP_MUTATION_REQUIREMENTS`, turnip
replies on the pull request (what each line of that reply means is under
[Mutation requirements not met](../using/troubleshooting.md#mutation-requirements-not-met)).
Each refusal is also logged at INFO, with the unmet requirement names
under `unmet`:

```
INFO refusing mutating operation with unmet mutation requirements
  owner=... repo=... pr_number=... actor=... operation=apply unmet=[approved mergeable]
```

When the reply says "(turnip could not check this; try again)" and it
keeps happening, the Server's logs carry the GitHub error, as with the
entries under [GitHub API errors](#github-api-errors).

## Fork pull requests are refused

What an end user sees is under
[Fork pull requests are refused](../using/troubleshooting.md#fork-pull-requests-are-refused):
nothing at all, by design. The refusal is reported in the Server's log
instead:

```
WARN refusing operation on a pull request from another repository
  owner=... repo=... pr_number=... head_owner=... head_repo=... actor=...
```

`head_owner`/`head_repo` identify the repository the branch actually
lives in, and `actor` is whoever caused the attempt: the pull request's
author on the automatic path, the commenter on the comment path. An
empty `head_owner`/`head_repo` means GitHub reported no head repository
at all, usually a fork deleted after the pull request was opened; turnip
refuses that case too rather than guessing.

Worth watching rather than filtering out: a single entry is ordinary
open-source traffic, but repeated entries naming the same `head_owner`
mean someone outside the repository is persistently trying to get turnip
to execute their code with your deployment credentials. That is the
signal this log line exists to give you.

## High-availability / multi-instance behavior

If you're running more than one Server replica (see
[Scaling up](deployment.md#scaling-up)), symptoms should be
indistinguishable from running one; that's the entire point of keeping
all state in Redis rather than in-process. If you ever observe
*different* behavior depending on which replica happens to handle a
request (a lock that seems to work in one replica but not another, or a
plan visible to one replica's apply but not another's), that's a genuine
regression worth filing, not expected behavior.
