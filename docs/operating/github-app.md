# The GitHub App

> **For operators**: deploying and configuring the Server. End users: see
> [`turnip.yaml`](../using/turnip-yaml.md).

turnip authenticates to GitHub as a GitHub App, never as a user or a
personal access token. This page walks through creating one and getting
its three credentials into the Server's settings
([`TURNIP_GITHUB_APP_ID`](server-settings.md), and one of
`TURNIP_GITHUB_PRIVATE_KEY`/`TURNIP_GITHUB_PRIVATE_KEY_PATH`, plus
`TURNIP_GITHUB_WEBHOOK_SECRET`).

Start the creation form under the GitHub organization/account whose
repositories should trigger turnip: **Settings → Developer settings →
GitHub Apps → New GitHub App**. The form has several sections turnip
doesn't use at all; they're covered below so nothing is left ambiguous.

**GitHub App name**: required, and must be unique across *all* of
GitHub (not just your org). If your first choice is taken, add a
suffix. This name is only ever visible in GitHub's own UI (installed-App
lists, PR check-run author, etc.); it never ends up in this repository,
so name it however makes sense to you. For example,
`turnip [your GitHub organization]`.

**Description**: optional; skip it.

**Homepage URL**: required by the form, but turnip has no user-facing
page. Point it at this repository (`https://github.com/ivanvc/turnip`)
or your own org's site; the value isn't otherwise meaningful to turnip.

**Identifying and authorizing users** (Callback URL, "Expire user
authorization tokens", "Request user authorization (OAuth) during
installation", "Enable Device Flow"): turnip has no user-facing login;
it never initiates a user OAuth flow. Leave the Callback URL field
blank and every checkbox in this section unchecked (the form's
defaults).

**Post installation** (Setup URL, "Redirect on update"): also unused;
leave the Setup URL blank and the checkbox unchecked.

## Webhook

- **Active**: check this once you have the URL described next; if you
  don't yet, **uncheck it instead of typing a placeholder**: GitHub
  only requires the URL field when Active is checked, and you can come
  back to this same settings page to check Active and fill in the real
  URL once it exists.
- **Webhook URL**: `https://<your-hostname>/github/webhook`, that exact
  path, not the root and not a sub-path beneath it. The Server serves
  webhook deliveries there and nowhere else; every other path returns
  404. `<your-hostname>` is a real, publicly-resolvable DNS name that
  routes HTTPS traffic to the `turnip-server` Service's HTTP port (`8080`
  by default, `TURNIP_HTTP_ADDR`). **turnip provisions none of this for
  you**: `deploy/base` has no Ingress, Gateway API, or LoadBalancer
  Service in it, only a plain `ClusterIP` Service
  (`deploy/base/service.yaml`). You need your own
  Ingress/Gateway/LoadBalancer resource (whatever your cluster already
  uses for exposing HTTP services) routing to that Service, plus a DNS
  record and a TLS certificate for the hostname, e.g.
  `https://turnip.example.com/github/webhook` if your ingress controller
  fronts that hostname and forwards to `turnip-server:8080`. None of that
  is part of this repository; it's entirely your own cluster's ingress
  setup. See [Prerequisites](deployment.md#prerequisites) for the rest of
  what that ingress needs.

  The path is namespaced by forge on purpose. It leaves `/github/` free
  for other GitHub-specific endpoints, and leaves the rest of the server's
  paths free for surfaces that should *not* be publicly reachable, which
  is also why giving the webhook its own path matters: an Ingress can
  route on it, so you can expose this one path and keep everything else
  internal.
- **Webhook secret**: type in a secret you generate yourself (e.g.
  `openssl rand -hex 32`); this exact value is what
  `TURNIP_GITHUB_WEBHOOK_SECRET` must match, so generate it first and
  paste the same string into both places.

**Permissions** (inferred directly from the GitHub API calls turnip
makes; double-check against the current GitHub App permissions UI when
you create the App, since GitHub occasionally renames these). Expand
only **Repository permissions**; leave Organization permissions and
Account permissions untouched (every entry "No access"):

| Permission | Level | Why |
|---|---|---|
| Checks | Read & write | creating/updating the per-project check runs and the `turnip` check |
| Contents | Read-only | fetching `turnip.yaml` and diffing changed files |
| Issues | Read & write | GitHub represents PR comments as issue comments under the hood; this is what lets turnip post and edit them |
| Pull requests | Read & write | reading PR metadata, listing changed files |

**How commenters are checked.** turnip asks GitHub whether the comment's
author is a collaborator, and when GitHub's answer does not list them it
asks what permission they hold. That second answer reports access granted
through a team, so a member with write access through a team can trigger
plans and applies with the permissions above and no Organization
permission. On a public or internal repository only triage or above
counts, since GitHub reports `read` for every account there (every
enterprise member, on an internal one). When GitHub cannot be
asked, turnip runs nothing and replies that it could not check the
author's permission; the GitHub error is in the Server's logs.

**Subscribe to events**: each repository permission above unlocks its
matching event checkbox in this list once set. Check `Pull request`
and `Issue comment` (nothing else).

**Where can this GitHub App be installed?**: choose **Only on this
account**. "Any account" would let *any* GitHub user install this App
on their own repositories, which is never what you want for turnip.

Click **Create GitHub App**. You land on the App's own settings page;
install it on the specific repositories you want from here (or from
**Install App** in the sidebar), and note the App ID shown near the top
(`TURNIP_GITHUB_APP_ID`).

**Generate a private key**: on this same settings page (General tab),
scroll to the **Private keys** section, below Webhook and Permissions,
and distinct from the **Client secrets** section just above it (turnip
authenticates as the App via JWT, not OAuth, so it needs the private
key, not a client secret). Click **Generate a private key**; your
browser immediately downloads a `.pem` file. This is a one-time
download. GitHub never shows or re-offers the raw key again (a lost
key means generating a new one; there's no `gh` CLI equivalent for this
step). Move the downloaded file somewhere private, outside any git
repo. You'll pass its path as `TURNIP_GITHUB_PRIVATE_KEY_PATH` (or
inline its contents as `TURNIP_GITHUB_PRIVATE_KEY`), and it's the same
file [Installing turnip](deployment.md#installing-turnip)'s
`kubectl create secret` command consumes.

Who can actually *use* an installed App is a separate, per-comment
authorization check; see
[Who can trigger what](../using/commands.md#who-can-trigger-what).
