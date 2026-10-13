# Proposal

## Why

A helmfile hook runs a command of the helmfile's choosing inside
turnip's Runner, with the Runner's credentials. `prepare` and `cleanup`
hooks run for every command, `diff` included, so a plan runs them as soon
as a pull request opens: before review, and outside the mutation
requirements that gate an apply. A hook's output can also reach the pull
request comment. The documentation recommends replacing helmfile hooks
with Kubernetes Jobs or Helm chart hooks (which run in the cluster under
their own ServiceAccount), but a recommendation is not enforced: any
pull request can add a hook back.

An operator who has moved off helmfile hooks should be able to make that
stick, so a pull request that adds one fails instead of running it.

## What Changes

- A Server setting that makes turnip refuse to run a helmfile that
  declares hooks, failing the operation with a message naming the hook
  and pointing to the alternatives
- It is operator-side only: a repository cannot set or relax it, for the
  reason mutation requirements are operator-side (the pull request
  supplies `turnip.yaml` and the helmfile)
- The documentation's "Don't use helmfile hooks" gotcha names the setting

## Open questions (to settle before the specs)

1. **Which hooks.** All helmfile hooks, or only the ones a plan runs
   (`prepare`, `cleanup`)? `presync` and `postsync` run only on `apply`
   and `sync`, so they are already gated like any mutating command.
2. **Default.** Off (today's behavior) or on (secure by default; a
   breaking change for repositories using hooks, acceptable pre-1.0)?
3. **Refuse, or tell Helmfile not to run them.** Refusing fails loudly
   and makes the author remove the hook. If Helmfile offers a way to skip
   hooks, turnip could pass it instead, which runs the rest silently
   without them; whether Helmfile has such an option is to be checked.
4. **How hooks are found.** A helmfile is templated (`.gotmpl`, values,
   environments, sub-helmfiles), so reading the YAML as written can miss
   a hook. Asking Helmfile for its rendered state, in the Runner before
   the operation, sees what Helmfile would run; whether that rendering
   itself runs any hook is to be checked.

## Capabilities

### New Capabilities

### Modified Capabilities
- `plugin-helmfile`: refusing a helmfile that declares hooks, when the operator enables it
- `server`: the new setting in the settings index

## Impact

`internal/plugin` (detecting hooks before running Helmfile), the Server's
settings, the Runner (the setting reaches it on the Job), docs. Out of
scope: equivalent policies for other tools (Terraform's `external` data
source and `local-exec` provisioners also run code during a plan; each
plugin decides its own when it is built, Slice 7).
