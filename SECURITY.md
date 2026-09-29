# Security

## The threat model

turnip reads `turnip.yaml`, and the IaC code it drives, from the pull
request's own head commit, and runs the tool in a Runner Pod that holds
cloud and cluster credentials. A plan runs automatically when a pull
request opens, and a plan is not read-only in the security sense: a
helmfile `prepare` hook, for one, runs an arbitrary executable during
`diff`. So **anyone who can get code into a pull request turnip plans can
run it with those credentials.** turnip refuses to run anything on a pull
request from a fork, which narrows "anyone" to the people who can push a
branch to the repository itself, but it does not narrow what they can do
once they have. Run turnip only on repositories whose contributors you
already trust with what the Runner can reach, and decide that reach by
which cluster and which credentials you give the Runner
(`docs/configuration.md`, "What that grant means").

## Review before apply is off by default

Out of the box, turnip lets anyone with write access apply a pull request
that holds a plan, including their own. It asks nothing about the pull
request's review: **without the `approved` requirement, one collaborator
can plan and apply their own pull request, with no one else having
approved it**, and the change reaches infrastructure before it could be
merged under branch protection.

Setting **`TURNIP_MUTATION_REQUIREMENTS=approved`** on the Server
prevents it: a mutating command (an apply, a sync, or any other operation
that is not a plan) then needs
an approving review from an account other than the author with write
access to the repository. Add branch protection's "Dismiss stale pull
request approvals when new commits are pushed" to make that approval
cover the commit being applied rather than an earlier one. A repository
cannot set or relax this from `turnip.yaml`. The details, and the
`mergeable` requirement beside it, are in `docs/configuration.md`
("Mutation requirements").

## Opt-ins that grant capability by design

Each of these is off by default, and each is a deliberate widening of what
a pull request can choose. Enabling one is not a vulnerability; it is the
cost below, accepted by the operator.

| Opt-in | Setting (Server) | Default | What enabling it costs |
|---|---|---|---|
| `runner.serviceAccount` | `TURNIP_ALLOWED_OVERRIDES` includes `runner.serviceAccount` | not permitted: every Runner Pod runs as `TURNIP_RUNNER_SERVICE_ACCOUNT` | A pull request can pick **any ServiceAccount in the Runner namespace**, and run its code with that ServiceAccount's cluster permissions and whatever cloud role EKS Pod Identity/IRSA maps to it. The Runner namespace's most privileged ServiceAccount becomes the ceiling for every pull request. The opt-in covers `runner.serviceAccount` wherever `turnip.yaml` writes it, in the top-level `runner:` block as well as a Project's own; without it, a value at either level is refused, so moving the line does not get past the gate. |
| `clone.submodules` | `TURNIP_ALLOWED_OVERRIDES` includes `clone.submodules` | not permitted: the clone follows the Server's `TURNIP_CLONE_SUBMODULES` | A pull request can make the clone fetch submodules recursively and at full depth. The gate is about cost more than safety: submodules are fetched with the GitHub App's installation credential, so they reach only repositories the App is already installed on. |
| Custom tool images | `TURNIP_ALLOWED_IMAGES` lists `tool:image@glob` Entries | empty: only the built-in aliases (each tool's vendor image, full versions or digests) run | Each allowed image is **code the operator vouches for**, running with the Runner's credentials, for any pull request that names it with a tag the glob matches. A floating tag (`@latest`, `@1.*`) is safe between plan and apply, since the apply runs the digest the plan's Pod ran, but the plan runs whatever the tag means when it pulls: whoever can push that tag can run code in the next plan. |

Each opt-in's own documentation is in `docs/configuration.md`.

## What turnip does consider a vulnerability

- **A gate that fails open**: any of the settings above taking effect when
  the operator has not enabled it, or an image running that no built-in
  alias or `TURNIP_ALLOWED_IMAGES` Entry allows; turnip running on a pull
  request from a fork; an operation triggered by someone the collaborator
  check should have refused; an operation other than a plan running on a
  pull request that does not meet a requirement in
  `TURNIP_MUTATION_REQUIREMENTS`, or a repository's own configuration
  changing what that setting requires.
- **A default that grants more than documented**: a Server with no opt-ins
  enabled letting a pull request choose anything this document or
  `docs/configuration.md` says it cannot.
- **A leak reachable with no opt-in**: a credential, token or secret held
  by the Server or the Runner reaching a pull request comment, a check
  run, a log line, or anyone outside the cluster, on a Server with none of
  the opt-ins above enabled.

What a pull request's own code does with the credentials the operator gave
the Runner is the threat model above, not a vulnerability in turnip.

## Reporting a vulnerability

Report it privately through GitHub's private vulnerability reporting on
this repository: the **Security** tab, then **Report a vulnerability**.
Please do not open a public issue or pull request for it. There is no
email address for reports.
