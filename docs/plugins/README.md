# Tools turnip runs

> This page is for everyone: end users deciding what to write in
> `turnip.yaml`, and operators deciding what to deploy.

The tools `uses:` can run are exactly the ones this Server has Plugins
for, each under its alias or in an image allowed for it. A project naming
any other tool makes `turnip.yaml` invalid: it is reported when the file
is parsed, with every other violation, and fails the `turnip` check as
`invalid turnip.yaml`.

| Tool | Status | Provisioned by | Operations |
|---|---|---|---|
| [Helmfile](helmfile.md) | implemented | run-in-image | `diff` (plan), `apply`, `sync` |
| Terraform | **not yet implemented** | not decided | none; `uses: terraform@…` is a validation error until its Plugin lands |
| Pulumi | **not yet implemented** | not decided | same as Terraform |

Each implemented tool has its own page under [`plugins/`](.), covering
its operations, what `uses:` and `with:` accept for it, how it
authenticates, and its gotchas (including how its mutating commands
report what they changed). Adding a tool is described in
[`development/README.md`](../development/README.md).
