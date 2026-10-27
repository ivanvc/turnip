# Troubleshooting (operators)

> **For operators**: deploying and configuring the Server. End users: see
> [Troubleshooting](../using/troubleshooting.md) for what shows up on a
> pull request.

What to check in the Server's own logs and the cluster when something
goes wrong beneath what a pull request sees.

## The Server Pod does not start

**Symptom**: `kubectl get pods` shows the Server Pod in
`CreateContainerConfigError`, or in `ContainerCreating` for a long time.

- **`secret "turnip-server-secrets" not found`** (in `kubectl describe pod`): the
  Server reads the GitHub App's credentials from a Secret named
  `turnip-server-secrets`, which you create beside the base. Create it with the
  keys `webhook-secret` and `private-key`; see
  [Installing turnip](deployment.md#installing-turnip).
- **`couldn't find key webhook-secret in Secret <namespace>/turnip-server-secrets`**
  (or the same for `private-key`): the Secret exists but lacks a required
  key. Add it, then run `kubectl rollout restart deployment/turnip-server`.

## An unknown settings warning at startup

**Symptom**: the Server runs, but its log has an `unknown settings`
warning with `names=[...]`.

- A `TURNIP_*` variable in the Server's environment is mistyped or no
  longer read. The Server ignores it and starts anyway, so the setting
  you meant to change keeps its default. Fix the name; see
  [Server settings](server-settings.md).

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
- <a id="forbidden-creating-a-runner-job"></a>**`jobs.batch is forbidden`
  when creating a Runner Job** (the message names the Server's
  ServiceAccount and a namespace): the Server may not create Jobs in the
  Runner namespace. Three causes, all in your `kustomization.yaml` (see
  [Choosing namespaces](deployment.md#choosing-namespaces)):
  - **You renamed `namespace:` without a transformer line.** Both
    bindings' subjects still name `turnip-system`, so the Server may not
    create Jobs, and its token reviews are forbidden as well: it logs
    `token review failed` with
    `tokenreviews.authentication.k8s.io is forbidden` and refuses every
    Runner stream. List the `namespaces` transformer under
    `transformers:`; that fixes both.
  - **You separated the Runner namespace but list `namespaces` instead
    of `runner-namespace`.** The Role and RoleBinding stay in the Server
    namespace. List `runner-namespace` in its place.
  - **`TURNIP_RUNNER_NAMESPACE` names a namespace other than the one the
    Role is in**, or one that does not exist. The Server's startup line
    shows `runner_namespace`; check that
    `kubectl get role,rolebinding -n <that namespace>` lists
    `turnip-server`, and that the namespace exists.

  The opposite mistake, `runner-namespace` with no
  `TURNIP_RUNNER_NAMESPACE` merged, never reaches the cluster: the build
  fails with ``fieldPath `data.TURNIP_RUNNER_NAMESPACE` is missing``.
  Merge the setting, or list `namespaces` instead.
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

### Reading a Runner's log

`kubectl -n <namespace> logs <pod> -c runner` shows, in order:

1. **The tool's output, once**, every line as the tool printed it,
   unredacted, with the Pod's absolute paths intact and any bytes that
   are not valid UTF-8 as written (the pull request shows them as `�`). turnip cuts
   nothing here, but the node rotates a container's log when it grows
   past its size limit (10 MiB by default), and `kubectl logs` shows only
   the newest file, so a very long output loses its start here too.
2. **`runner: operation error: <message>`**, only when the result
   carries an error message: why the tool could not start, say, or why
   the result could not be delivered.
3. **One summary line** for the result, which never repeats the output:

```text
runner: operation result: success=true exit_code=0 category=none add=12 change=0 destroy=0 output_bytes=5069133 sent_bytes=1048502
```

| Field | Meaning |
|---|---|
| `success` | whether the result reports success |
| `exit_code` | the tool's exit code; `-1` when no tool process ran |
| `category` | which step failed (`tool_exited`, `tool_not_started`, `workspace_failed`, `clone_failed`), `none` on success or an uncategorized failure |
| `add`, `change`, `destroy` | the change counts, read from the whole output |
| `output_bytes` | the size of the whole output |
| `sent_bytes` | the size of the output sent with the result |

The Runner sends at most 1 MiB of output. When `sent_bytes` is smaller
than `output_bytes`, the start of the output was cut, and what the pull
request shows has a `@@ turnip: <K> earlier lines omitted (<size>) @@`
line right after its opening `@@ turnip:` lines (what end users see is
under
[Output with "earlier lines omitted"](../using/troubleshooting.md#output-with-earlier-lines-omitted)).
The lines that were cut are in this log only while the Pod exists: its
Job is deleted 15 minutes after it finishes.

**`the result was too large to deliver (<size>; the Server accepts at
most 4 MiB)`**: the result's message, plan data included, did not fit
what the Server accepts, so the Runner sent a failed result with that
message instead, with no output and no plan data. The output alone never
causes this (it is cut first); a tool's plan data that large does.

### A Project lists keys of a Secret and the Runner never started

**Symptom**: after the 5 minute start timeout, the Project's output in
the results comment ends with this line, with the placeholders filled in:

```text
This project mounts keys <keys> of Secret <secret> in namespace <namespace>; if that Secret or one of those keys is missing, the Pod cannot start.
```

It means the Project lists keys of
[the repository's Secret](runner-access.md#the-repositorys-secret) in
`runner.secrets`, and its Runner Pod was still `Pending` at the deadline.
The kubelet cannot mount a volume whose Secret or key does not exist, so
the Pod waits with no container started, not even the `clone` init
container, and reports every container as waiting with reason
`PodInitializing`. The check's title is then
`Job <name>: container stuck (PodInitializing)`. The line is a condition, not a finding: turnip never reads
Secrets, so it does not know the Secret is missing, and a Pod can also
stay `Pending` for other reasons (no node fits it, for one).

**Check**:

- `kubectl -n <namespace> describe secret <secret>` lists the Secret's
  keys and their sizes, not their values. A "not found" error means the
  Secret does not exist (check the id in its name against
  `gh api repos/OWNER/REPO --jq .id`); a key missing from the list is one
  to add.
- `kubectl -n <namespace> describe pod <pod>` shows `FailedMount` events
  naming the missing Secret or key when that is the cause, for a missing
  key `MountVolume.SetUp failed for volume "repository-secrets" :
  references non-existent secret key: <key>`. Other events there (for
  example `FailedScheduling`) point to the other reasons.

## Runner timeouts

**Symptom**: a per-project check failed with a title that says a tool or
Runner stopped. The end-user side of each is under
[Reading a failed check's title](../using/troubleshooting.md#reading-a-failed-checks-title).
Two settings bound a Runner: `TURNIP_RUNNER_IDLE_TIMEOUT` (30 minutes by
default) and `TURNIP_RUNNER_DEADLINE` (4 hours by default); see
[Server settings](server-settings.md).

### A tool stopped for silence

**Symptom**: the check is titled `<tool> stopped: no output for 30m0s`,
and the project's output ends with `The tool wrote nothing for 30m0s, so
turnip stopped it.`

- The tool wrote nothing on stdout or stderr for the idle timeout. Only
  the tool's own lines count; turnip's annotation lines do not. The Runner
  sent the tool `SIGINT`, then `SIGKILL` 30 seconds later if it was still
  running. Read the Runner's log and the output for where it went quiet
  (see [Reading a Runner's log](#reading-a-runners-log)).
- A tool that is healthy but silent longer than the timeout (a long
  `--wait`, a slow provider) needs a larger `TURNIP_RUNNER_IDLE_TIMEOUT`.
  Raise it on the Server and restart it; the value must stay shorter than
  `TURNIP_RUNNER_DEADLINE`.

### A Runner told to stop

**Symptom**: the check is titled `<tool> stopped: the Runner was told to
stop`, and the output ends with `The Runner was told to stop (its
deadline, an eviction or a deletion), so turnip stopped the tool.`

- The Runner received `SIGTERM`: the Job's deadline passed, the node
  evicted the pod, or the pod was deleted. Check `kubectl get events` in
  the Runner namespace, and the Job's conditions for `DeadlineExceeded`.
  If operations legitimately run past the deadline, raise
  `TURNIP_RUNNER_DEADLINE`.

### A Runner that stopped without reporting

**Symptom**: the check is titled with a diagnostic such as `Job <name>:
stopped at its deadline of 4h0m0s`, `Job <name>: container <container>
terminated (<reason>)`, `Job <name>: Pod evicted` or `Job <name>:
finished without reporting a result`, and its summary is `The Runner
stopped before reporting a result.`

- The Runner had started, then its Job ended or its pod disappeared
  without a result reaching the Server, and the sweep finalized the
  operation, within about 30 seconds of the Job ending. The title is what
  the sweep read from Kubernetes: a deadline, a container terminated with
  a reason (`OOMKilled` means the Runner pod ran out of memory: turnip
  sets no resources on the Runner, so its limit is whatever the Runner
  namespace's LimitRange gives it, or else the node's memory), an
  eviction, or a Job that ended quietly. `No Job/Pod found for this operation; it may have been deleted
  or never successfully scheduled` means the Job or pod was removed.
- This is different from the start timeout (the Runner never started; see
  [Runner execution errors](#runner-execution-errors)).

## Redis connection and startup errors

**Symptom**: the Server exits at startup with one of these messages
(`kubectl logs` on the Server Pod), or `/readyz` returns 503. None of
them print the password.

- **`must not carry a password in the URL`**: `TURNIP_REDIS_ADDR` has
  `user:password@`. The address is delivered through a ConfigMap, so a
  password there would not be secret. Keep the username if you need one
  (`rediss://user@host:6379`) and set `TURNIP_REDIS_PASSWORD_PATH` (a
  file in the mounted `turnip-server-secrets` Secret, such as
  `/etc/turnip/secrets/redis-password`) or `TURNIP_REDIS_PASSWORD`.
  After changing a setting, run
  `kubectl rollout restart deployment/turnip-server`.
- **`must not set skip_verify`**: the address has `skip_verify` with any
  value. turnip always verifies the Redis certificate. Remove it; for a
  private CA, set `TURNIP_REDIS_CA_PATH` instead.
- **`scheme ... is not supported`**: the scheme is neither `redis` nor
  `rediss` (`unix://`, `http://` and a typo all land here). Use
  `redis://`, `rediss://` or a bare `host:port`.
- **`TURNIP_REDIS_CA_PATH is set but ... does not use rediss://`**: a CA
  bundle only matters for TLS. Switch the address to `rediss://` or unset
  the CA path.
- **`set TURNIP_REDIS_PASSWORD_PATH (a file) or TURNIP_REDIS_PASSWORD (a
  value), not both`**: pick one; the file form is recommended.
- **`reading TURNIP_REDIS_CA_PATH` or `holds no PEM certificate`**: the
  CA file is missing, unreadable, or contains no PEM certificate. Check
  the mount path and the file's contents.
- **`reading TURNIP_REDIS_PASSWORD_PATH`**: the password file is missing
  or unreadable. It is checked at startup and again for each new
  connection, so a Secret that later disappears also fails there.
- **An unknown query parameter** in the address: go-redis refuses
  parameters it does not recognize. See its
  [connection URL](https://pkg.go.dev/github.com/redis/go-redis/v9#ParseURL)
  list.
- **`MOVED` errors** in the Server's logs or in a failed
  operation's results comment:
  the endpoint is a clustered Redis (ElastiCache cluster mode enabled),
  which turnip does not support. Point it at a cluster-mode-disabled
  primary endpoint. See [Prerequisites](deployment.md#prerequisites).
- **Connected, but plaintext when you expected TLS**: the startup line
  and `/readyz` show `tls=false`. The address is `redis://` or bare;
  change it to `rediss://`.

## Failed checks that are the Server's to fix

**Symptom**: a per-project check failed with one of these titles. The
change being planned is not the cause; the end-user side of each is under
[Reading a failed check's title](../using/troubleshooting.md#reading-a-failed-checks-title).

- **`lock could not be acquired`**: turnip could not reach Redis (or
  Redis refused the write) while taking the project's lock; the results
  comment carries the error. A re-plan usually succeeds; if it keeps
  happening, check Redis and the Server's logs.
- **`operation could not be recorded`**: the lock was taken but saving
  the Operation Record to Redis failed, so no Runner was started. If it
  recurs, check Redis and the Server's logs.
- **`Runner Job could not be built`**: the Server could not assemble the
  Runner Job's spec, so nothing was sent to Kubernetes. The results
  comment carries the error. If it recurs, check the Runner settings in the
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

That includes a replica going away. A trigger's progress (its commands,
the results so far, which Operations are still running) is in Redis
under a `trigger:<id>` key, and the replica that receives a Runner's
result starts the comment's next command or posts the results comment,
whichever replica dispatched it.

**A results comment that never appears** therefore means a replica was
killed in the middle of a step, not between steps: while it was
dispatching a command's Runner Jobs, or after it took a Runner's result
and before it posted. A normal stop (a rollout, a node drain) lets those
steps finish. An out-of-memory kill or a lost node does not. The checks
and the locks are still right, because they are updated first, so read
the checks for what ran; a later command of the same comment that was
never started has no check, and has to be commented again.

Nothing recovers such a trigger. Its `trigger:<id>` key stays in Redis
until it expires, 25 hours after its latest command began, and can be
left alone or deleted:

```sh
redis-cli --scan --pattern 'trigger:*'
```

A key that is there while Runner Jobs are still running is a trigger in
progress, not a stranded one.
