# Requirements: Move the Webhook Off the Root Path (Slice 29)

## Introduction

The Server mounts its webhook handler at `/`
(`cmd/server/main.go:85`), which makes it a catch-all: every path not
claimed by `/healthz`, `/readyz` or `/metrics` reaches the webhook handler
and is rejected there for failing signature verification. This slice gives
the webhook an explicit path of its own and lets `/` stop being a
catch-all.

The motivation is not tidiness. The webhook must be reachable from the
internet, because GitHub delivers to it; the surfaces Slices 26 and 28 add
— live operation output, and a control that releases Locks — must not be.
While both live under `/`, an Ingress cannot separate them, because path
rules are what Ingress controllers route on. Giving the webhook its own
path turns "expose the webhook publicly, keep everything else internal"
into an ordinary Ingress rule.

**This slice amends no global requirement.** The global `requirements.md`
pins the *configuration file* location (Requirement 1.1) and the
webhook's failure behavior (Requirement 15.5), but names no HTTP path;
the global `design.md` mentions none at all. So this fills a gap rather
than overturning a decision — unlike Slice 21, which had to amend
Requirement 5.3.

**The cost of this change only grows.** The path lives in each GitHub
App's own settings, so moving it is a manual edit per installation. There
is one installation today. A missed update is also silent: deliveries 404
and nothing on turnip's side notices.

## Glossary

(Inherited from the global spec glossary.)

- **Webhook Path**: the HTTP path on the Server at which GitHub delivers
  Webhook_Events. `/` today; `/github/webhook` after this slice.
- **Published URL**: the Webhook URL an operator enters in the GitHub App
  settings, which the documentation instructs them to construct.

## Requirements

### Requirement 1: The webhook has an explicit, forge-namespaced path

**User Story:** As an operator, I want the webhook to have a path of its
own, so that I can expose it publicly without also exposing everything
else the Server serves.

#### Acceptance Criteria

1. THE Server SHALL serve the webhook handler at `/github/webhook`
2. THE Server SHALL NOT serve the webhook handler at `/`
3. THE path SHALL be namespaced by forge (`/github/...`) rather than by
   resource (`/webhook`), so that a second forge — or another
   GitHub-specific endpoint such as an OAuth callback — needs no further
   migration of a published URL
4. THE health, readiness and metrics paths SHALL be unchanged

### Requirement 2: `/` stops being a catch-all

**User Story:** As an operator debugging a delivery problem, I want an
unmatched path to say it was not found, so that a typo does not reach me
as a signature-verification failure.

#### Acceptance Criteria

1. WHERE a request arrives at a path the Server does not serve, THE Server
   SHALL respond 404 rather than routing it to the webhook handler
2. THE Server SHALL respond 404 at `/` itself
3. THE 404 response SHALL NOT invoke webhook signature verification, so
   that an unmatched request produces no authentication error in the logs

### Requirement 3: No compatibility shim

**User Story:** As a maintainer, I do not want a transition path that
outlives its purpose.

#### Acceptance Criteria

1. THE Server SHALL NOT continue accepting deliveries at `/` after this
   slice
2. THE change SHALL be deployed together with the GitHub App's Webhook URL
   update, since deploying either alone stops deliveries

*Rationale: a compatibility shim's value is letting many installations
migrate on their own schedule. There is one. A shim kept past its purpose
becomes code nobody will delete because nobody can prove it is unused.*

### Requirement 4: Documentation states the new path

**User Story:** As an operator setting turnip up, I want the documentation
to tell me the right Webhook URL, so that I do not configure a path that
404s.

#### Acceptance Criteria

1. `docs/configuration.md` SHALL state the Webhook URL as
   `https://<your-hostname>/github/webhook`
2. THE documentation SHALL NOT continue to state that the webhook is at
   the root path — the current text asserts it emphatically ("the root
   path (`/`), not `/webhook` or any other sub-path"), so it must be
   rewritten rather than adjusted
3. `docs/deployment.md` SHALL state the same URL wherever it instructs an
   operator to set or verify the Webhook URL
4. THE documentation SHALL note that an existing installation must update
   its GitHub App's Webhook URL when upgrading, and that deliveries stop
   until it does

## Out of Scope

- **What `/` serves later.** A 404 now. A UI index becomes possible once
  Slice 26 or 28 lands; this slice commits only to the 404.
- **Any second forge.** The path is namespaced so that adding one needs no
  URL migration, but nothing here implements GitLab or Gitea. Signature
  verification differs per forge (a secret-token header rather than
  GitHub's HMAC), and that is the expensive part.
- **Ingress or network policy.** This slice makes path-based separation
  *possible*; configuring it belongs to whoever runs the cluster.
- **The OAuth callback** at `/github/oauth/callback` — Slice 28 adds it if
  it needs it. This slice only ensures the prefix is free.
