# Troubleshooting

What you'll actually see (usually a PR comment or check run) for each
common failure, and what to do about it.

## Configuration errors

**Symptom**: a PR comment saying configuration is missing, or a parse
error with a line number, instead of a plan.

- **No configuration found**: the comment names both accepted locations.
  Add `.turnip/config.yaml`, or `turnip.yaml` in the repository root.
  Only the `.yaml` extension is read, and `.github/turnip.yaml` — accepted
  by turnip before schema `v1alpha2` — is not read any more.
- **Invalid YAML syntax**: the comment includes the parse error and line
  number — fix that line and push again (a new commit re-triggers the
  plan).
- **Unrecognized field**: the comment names the key and its line. turnip
  checks every field it defines by name, so this is usually a misspelling
  or a setting written at the wrong level (the one place arbitrary keys
  are allowed is inside `with`).
- **Unsupported `schemaVersion`**: reported on its own, without also
  listing the fields of the older schema. Migrate the file rather than
  changing the version alone — the field names changed too.
- **Invalid project configuration**: the comment names the specific
  project and the validation error (e.g. an unsupported tool in `uses`, or
  a project missing its required `directory`). Fix that project's entry.

## Lock contention

**Symptom**: a PR comment saying a project is locked by another PR,
instead of a plan or apply running.

- **Locked by another PR**: the comment names the PR holding the lock.
  Either wait for that PR to merge/close (which releases the lock
  automatically) or have someone with write access comment
  `/turnip unlock <project>` on this PR to force it — only appropriate if
  you're sure the other PR's plan is stale or abandoned.
- **Apply rejected — no plan data / wrong PR holds the lock**: this means
  either no plan has run yet for this PR, or the lock is held by a
  *different* PR. Comment `/turnip plan <project>` (or push a commit that
  touches the project) to get a fresh plan on this PR before applying.
- **Locks don't expire on their own**: unlike some similar tools, turnip
  never times out a lock — it only releases on PR merge, PR close, or a
  manual `/turnip unlock`. If a PR is abandoned without being closed, its
  locks stay held until someone unlocks them.

## GitHub API errors

**Symptom**: a check run or comment never appears at all, or appears much
later than expected, with no explanation in the PR.

- **No response to a webhook at all**: check the Server's logs for the
  installation ID in question — a token-generation failure returns
  HTTP 500 to GitHub, which retries the delivery automatically, so a
  transient failure usually self-heals within GitHub's own retry window.
  If it persists, check the GitHub App's credentials
  (`docs/deployment.md`'s prerequisites) haven't been rotated/revoked.
- **Check run never updates, but a comment does appear**: check-run
  failures are soft — the Server logs the error and falls back to
  comment-only status, so the Operation still completed normally. This is
  a GitHub API hiccup, not a turnip bug; the PR comment is the
  authoritative result either way.
- **Comment missing after a successful-looking run**: comment posting
  retries up to 3 times with backoff before giving up (and logging). If
  every retry failed, GitHub's API was very likely degraded at that
  moment — check GitHub's own status page.

## Runner execution errors

**Symptom**: the check run/comment reports a failure with no tool output,
or nothing happens for several minutes before a timeout is reported.

- **"creating Runner Job" failure**: the Kubernetes API rejected Job
  creation — almost always an RBAC or resource-quota problem in the
  cluster. Check the Server's own ServiceAccount has the permissions
  `deploy/base/role.yaml` grants (Jobs create/delete/get/list/watch, Pods
  get/list) and that the namespace isn't at a resource quota limit.
- **Timeout ("didn't start within 5 minutes")**: the Job never got a Pod
  scheduled, or the Pod never reached the Server over gRPC in time — check
  `kubectl get pods` in the Runner namespace for `Pending`/`ImagePullBackOff`
  (a scheduling or image-pull problem) or `CrashLoopBackOff` (check the
  Pod's own logs). A network policy blocking Runner→Server gRPC traffic
  produces this symptom too — see `deploy/overlays/kind/`'s Service for
  what that path should look like.
- **Clone failure** (e.g. `clone failed: merge conflict`): the PR's
  branch conflicts with its base, or the Runner's GitHub token couldn't
  read the repository. A merge conflict needs resolving in the PR the
  normal way (no turnip-side action); anything else, check the GitHub
  App's repository access.
- **A tool complaining about a path or repository you know exists** (e.g.
  `Error: repo .. not found` from `helm pull ../charts/...`, or a file a
  tool insists is missing): most often an **uninitialised submodule**. The
  directory is present but empty, so the tool reaches through the gap and
  reports whatever its own parser made of the path — a message that names
  neither the submodule nor the repository it came from. Check whether the
  path lives in a submodule, then whether the Server has
  `TURNIP_CLONE_SUBMODULES=none`, or the repository set
  `clone.submodules: none` in its own config file. A submodule nested
  inside another needs `recursive`; `top-level` fetches only the first
  layer.
- **`remote: Repository not found` for a submodule on the same host**:
  the fetch *was* authenticated — turnip rewrites submodule URLs to carry
  the installation token — so this almost never means the repository is
  missing or misspelled. GitHub returns the same answer for a repository
  the credential cannot see, deliberately, so it doesn't reveal which
  private repositories exist. Check that the **GitHub App is installed on
  the submodule's repository**: an installation set to "only select
  repositories" commonly includes the parent but not a shared chart or
  module repository next to it. turnip appends a hint saying as much.
- **`submodule ... is hosted on <host>, which this installation token
  cannot authenticate`**: the submodule lives somewhere the GitHub App
  isn't installed — another forge, or a self-hosted server. turnip has
  only the installation token, which authenticates nothing off the
  repository's own host, so this is reported before any fetch is attempted
  rather than surfacing later as a generic authentication error. A
  submodule on the *same* host works whatever URL form it uses: SSH and
  other schemes are rewritten to authenticated HTTPS automatically.

## Plugin execution errors

**Symptom**: the check run/comment shows a failure with the tool's own
stdout/stderr attached.

- **Tool not installed**: the comment names the missing tool/exit code.
  A *malformed* version never gets this far — it's rejected when the file
  is parsed, as a validation comment naming `uses`. What reaches here is a
  well-formed version the vendor doesn't actually publish, so the
  initContainer had nothing to pull: check the `@version` in `uses`
  against the tool vendor's own published image tags.
- **A helper binary or plugin is missing** (e.g. `exec: "helm":
  executable file not found in $PATH`, or the tool reporting it can't find
  a plugin): for a run-in-image tool these come from the vendor's image
  rather than from turnip — see "How a tool reaches the Runner" in
  `docs/configuration.md`. The fix is an image that bundles what you need,
  not a turnip setting. Seeing this for Helmfile on a version the vendor's
  own image ships is a turnip bug worth reporting, since that image
  contains helm, sops and the standard plugins.
- **Tool command failed** (e.g. `terraform plan` itself errored): the
  full stdout/stderr is in the PR comment — this is the tool telling you
  something real about your infrastructure code, not a turnip failure.
  Fix what the tool reports and push again.
- **Output parsing failed, but the comment shows zero changes**: the
  operation itself succeeded; turnip just couldn't extract a change count
  from the tool's output format. Check the comment's full output section
  for what actually happened rather than trusting the summary count in
  this case.

## Authorization errors

**Symptom**: a reply comment saying you don't have permission, instead of
the operation running.

- **Non-collaborator trigger**: only repository collaborators can trigger
  operations via comment at all. Ask a maintainer to add you as a
  collaborator (or push a commit instead, which still auto-triggers a
  plan for anyone who can open a PR).
- **Insufficient permission for apply/destroy/unlock**: these need write
  access specifically (not just any collaborator access, e.g. triage or
  read). Ask a maintainer to grant write access, or ask them to run the
  command themselves.

## High-availability / multi-instance behavior

If you're running more than one Server replica (see `docs/deployment.md`),
symptoms should be indistinguishable from running one — that's the entire
point of keeping all state in Redis rather than in-process. If you ever
observe *different* behavior depending on which replica happens to handle
a request — a lock that seems to work in one replica but not another, or
a plan visible to one replica's apply but not another's — that's a
genuine regression worth filing, not expected behavior.
