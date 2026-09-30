# turnip

turnip is a GitHub-integrated platform that automates infrastructure-as-code
operations across a repository's projects. It runs Helmfile today;
Terraform and Pulumi are planned.

## How it works

A Server receives GitHub webhooks, determines which `turnip.yaml`
projects are affected by a pull request's changed files, and dispatches
operations to ephemeral Kubernetes Runner pods over gRPC, each running
the matched tool. Locking keeps a plan and its later apply from drifting,
and a consolidated PR comment and check run report the result.

See [`docs/README.md`](docs/README.md) for the full documentation, by
audience (end user, operator, developer).
