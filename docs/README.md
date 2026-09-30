# turnip documentation

turnip automates infrastructure-as-code operations across a
repository's projects, driven from pull requests. It runs Helmfile
today; Terraform and Pulumi are planned (see [Plugins](plugins/README.md)).

Start here, by what you're doing:

- **End user**: you write `turnip.yaml` and comment on pull requests.
  Start with [Getting started](getting-started/end-users.md).
- **Operator**: you deploy and configure the Server. Start with
  [Getting started](getting-started/operators.md).
- **Developer**: you work on turnip itself. Start with
  [Developing turnip](development/README.md).

## End users

- [Getting started](getting-started/end-users.md): from nothing to a
  first `turnip.yaml`, a first plan and a first apply
- [`turnip.yaml`](using/turnip-yaml.md): every field the schema accepts
- [Commands](using/commands.md): triggering by comment, plan and mutating
  commands, who can run what
- [Pull requests](using/pull-requests.md): automatic plans, what you'll
  see, checks, locks
- [Troubleshooting](using/troubleshooting.md)

## Operators

- [Getting started](getting-started/operators.md): from nothing to a
  Server receiving webhooks from one repository
- [Deploying turnip](operating/deployment.md): prerequisites, installing,
  upgrading, scaling
- [Server settings](operating/server-settings.md): every `TURNIP_*`
  setting, its default and whether it's required
- [The GitHub App](operating/github-app.md): creating it and getting its
  credentials into the Server
- [Runner access](operating/runner-access.md): the ServiceAccount, cloud
  credentials, custom images
- [Troubleshooting](operating/troubleshooting.md)
- [Security](../SECURITY.md)

## Developers

- [Developing turnip](development/README.md): architecture, adding a
  tool, local development

## Plugins

One page per tool turnip runs, split into a section for end users and one
for operators: see the [tools index](plugins/README.md).
