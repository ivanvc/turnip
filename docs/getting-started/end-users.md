# Getting started (end users)

> **For end users**: people writing `turnip.yaml` and commenting on pull
> requests. Operators: see [Getting started](operators.md).

From nothing to a first `turnip.yaml`, a first plan, and a first apply.
This assumes turnip is already deployed and the GitHub App is installed
on your repository (if not, that's
[Getting started (operators)](operators.md)). The examples below use
Helmfile, the one tool turnip runs today; each step links to the page
that explains it.

1. **Write `turnip.yaml`.** Commit `.turnip/config.yaml` or
   `turnip.yaml` at the repository root, naming a project's directory,
   what to run, and which changed files should trigger it:

   ```yaml
   schemaVersion: v1alpha3
   projects:
     - name: web
       directory: infra/web
       whenModified:
         - "infra/web/**"
       uses: helmfile@v1.7.4
       with:
         environment: staging
   ```

   See [`turnip.yaml`](../using/turnip-yaml.md) for every field, and
   [Helmfile](../plugins/helmfile.md#using-helmfile) for `uses:` and
   `with:` as Helmfile reads them. With no `KUBECONFIG`, as here, the
   project acts on the cluster turnip itself runs in, as the Runner's
   ServiceAccount; any other cluster needs `runner.env.KUBECONFIG` (see
   [Reaching a cluster](../plugins/helmfile.md#reaching-a-cluster)).
2. **Open a pull request and read the plan.** Changing a file under
   `infra/web/` triggers `helmfile diff` automatically, no comment
   needed: a check run per project and a PR comment showing what would
   change. See [Automatic plans](../using/pull-requests.md#automatic-plans)
   and [What you'll see on the PR](../using/pull-requests.md#what-youll-see-on-the-pr).
3. **Comment `/turnip apply`** (or `/helmfile apply`) once the plan looks
   right. Applying needs write access to the repository, and reuses
   exactly what the plan computed rather than planning again. See
   [Triggering by comment](../using/commands.md#triggering-by-comment)
   and
   [Plan → apply, and why apply needs a plan first](../using/pull-requests.md#plan--apply-and-why-apply-needs-a-plan-first).
4. **Read the result, and the `turnip` check.** The apply's own check run
   shows what changed; the single `turnip` check sums up whether every
   plan on the pull request has been carried out, and is the one to
   require before merge. See
   [What you'll see on the PR](../using/pull-requests.md#what-youll-see-on-the-pr)
   and
   [Requiring turnip before merge](../using/pull-requests.md#requiring-turnip-before-merge).

## Next

- [`turnip.yaml` reference](../using/turnip-yaml.md): every field the
  schema accepts
- [Commands](../using/commands.md): triggering by comment, targeting
  specific projects, who can run what
- [Pull requests](../using/pull-requests.md): locks, the `turnip` check,
  and what happens when a pull request closes or comes from a fork
- [Helmfile](../plugins/helmfile.md): operations, `with:` keys and
  gotchas specific to that tool
