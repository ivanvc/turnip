# Configuring turnip

Two things need configuring: the **`turnip.yaml`** each repository carries
(which projects exist, what triggers them) and the **Server itself**
(GitHub App credentials, Redis, Kubernetes namespace — set via environment
variables, normally through the overlay values `docs/deployment.md`
describes).

## `turnip.yaml`

Committed to the repository root (or `.github/turnip.yaml` — checked only
if the root file is missing, never both merged):

```yaml
version: 1
projects:
  - name: web            # optional — defaults to `directory` if omitted
    directory: infra/web
    tool: helmfile
    whenModified:
      - "infra/web/**"
    config:
      environment: staging
```

| Field | Required | Notes |
|---|---|---|
| `version` (top-level) | — | parsed, not currently validated against anything |
| `projects[].name` | no | defaults to `directory`; must be unique across the file once defaulted |
| `projects[].directory` | **yes** | the tool's working directory, relative to the repo root |
| `projects[].tool` | **yes** | one of `terraform`, `pulumi`, `helmfile` — see "Tool support" below |
| `projects[].whenModified` | no | a list of glob patterns (full `**` support — [doublestar](https://github.com/bmatcuk/doublestar) syntax); a PR whose changed files match none of a project's patterns never triggers it |
| `projects[].config` | no | a free-form string map, tool-specific — see below |

Every violation across the whole file is reported together (a typo in
project 3 doesn't hide a missing `directory` in project 1) — you'll see
every problem in one pass, not one-at-a-time.

### `config` map — recognized keys

- **`version`**: pins which release of the tool binary the Runner
  provisions (e.g. `terraform 1.9.5`, not whatever `:latest` happens to
  be). Omit it to get the current default. turnip keeps no list of
  "supported" versions to validate against — any value that looks like a
  real version (roughly semver: `1.9.5`, `0.170.1`, `1.7.4-rc1`) is
  accepted and passed straight through to the vendor's own per-version
  image, so a tool's new release works the moment the vendor publishes it,
  with no turnip release required. Only obviously malformed input (a
  floating tag like `latest`, a typo, stray whitespace) is rejected at
  Job-build time; if the value is well-formed but the vendor doesn't
  actually publish that tag, the Job fails when its initContainer can't
  pull the image — reported as a normal operation failure, not caught
  ahead of time.
- **`environment`** (Helmfile only): passed through as helmfile's own
  `--environment` flag.

Anything else in `config` is opaque to turnip itself — a plugin only
reads the keys it understands.

### Tool support

| Tool | Status | Operations |
|---|---|---|
| Helmfile | implemented | `diff` (plan), `apply`, `sync`, `destroy` |
| Terraform | **not yet implemented** | recognized by the config parser (won't reject your `turnip.yaml`), but no Plugin exists yet to actually run it |
| Pulumi | **not yet implemented** | same as Terraform |

A `turnip.yaml` project can name `terraform`/`pulumi` today without
error, but nothing will actually trigger for it until support lands.

## The Server's own configuration

Read from environment variables at startup (`internal/orchestrator.ConfigFromEnv`)
— fails fast, naming every missing required variable together, rather
than one restart-and-discover-the-next-one at a time.

| Variable | Required | Purpose |
|---|---|---|
| `TURNIP_GITHUB_APP_ID` | yes | your GitHub App's numeric ID |
| `TURNIP_GITHUB_PRIVATE_KEY` or `TURNIP_GITHUB_PRIVATE_KEY_PATH` | yes (one of) | the App's private key — inline PEM or a file path |
| `TURNIP_GITHUB_WEBHOOK_SECRET` | yes | the secret your GitHub App's webhook is configured with (HMAC-verified on every delivery) |
| `TURNIP_REDIS_ADDR` | yes | Redis/Valkey address — the Server's only state store |
| `TURNIP_K8S_NAMESPACE` | yes | namespace Runner Jobs are created in |
| `TURNIP_HTTP_ADDR` | yes | HTTP bind address (webhooks, `/healthz`, `/readyz`, `/metrics`) |
| `TURNIP_GRPC_ADDR` | yes | gRPC bind address (Runner→Server log/result streaming) |
| `TURNIP_RUNNER_SERVER_ADDR` | yes | address a Runner Pod dials to reach this Server — a Service DNS name, not the bind address above |
| `TURNIP_RUNNER_IMAGE` | yes | the Runner container image a deployed Server creates Jobs with |
| `TURNIP_MINIMIZE_OUTDATED_PLAN_COMMENTS` | no (default `false`) | collapse an older plan comment on the same PR once a newer one supersedes it |

In `deploy/base`, the two credential-shaped values
(`TURNIP_GITHUB_WEBHOOK_SECRET`, `TURNIP_GITHUB_PRIVATE_KEY`) come from a
Secret; everything else comes from a ConfigMap an overlay fills in — see
`docs/deployment.md`'s "Installing turnip" section for the two values
(`TURNIP_REDIS_ADDR`, `TURNIP_GITHUB_APP_ID`) a real deployment always
needs to set itself, whichever install path you use.

The Runner container's own environment variables (`TURNIP_OPERATION_ID`,
`TURNIP_REPO_URL`, `TURNIP_GITHUB_TOKEN`, and so on) are internal
plumbing the Server sets automatically on every Job it creates — nothing
here for an operator to configure directly.

## Setting up the GitHub App

Create the App under the GitHub organization/account whose repositories
should trigger turnip (**Settings → Developer settings → GitHub Apps →
New GitHub App**), then install it on the specific repositories you want.

**Permissions** (inferred directly from the GitHub API calls turnip
makes — double-check against the current GitHub App permissions UI when
you create the App, since GitHub occasionally renames these):

| Permission | Level | Why |
|---|---|---|
| Contents | Read-only | fetching `turnip.yaml` and diffing changed files |
| Pull requests | Read & write | reading PR metadata, listing changed files |
| Issues | Read & write | GitHub represents PR comments as issue comments under the hood — this is what lets turnip post and edit them |
| Checks | Read & write | creating/updating the per-project check runs |

**Webhook events**: subscribe to `Pull request` and `Issue comment`,
delivered to the Server's `/` path over HTTPS, with the webhook secret
matching `TURNIP_GITHUB_WEBHOOK_SECRET`.

**Generate a private key** on the App's settings page and keep the
downloaded `.pem` — you'll pass it as `TURNIP_GITHUB_PRIVATE_KEY_PATH` (or
inline as `TURNIP_GITHUB_PRIVATE_KEY`), and it's the same file
`docs/deployment.md`'s `kubectl create secret` command consumes.

Who can actually *use* an installed App is a separate, per-comment
authorization check — see `docs/usage.md`'s "Who can trigger what"
section.
