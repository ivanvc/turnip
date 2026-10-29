# Proposal

## Why

A member of the organization whose write access to the repository comes
through a team cannot trigger anything: every comment, a `diff`
included, is answered `@<user> does not have permission to trigger
operations on this repository.` Seen 2026-10-15 on the pilot.

turnip decides whether a comment's author may trigger anything by asking
GitHub whether they are a collaborator
(`GET /repos/{owner}/{repo}/collaborators/{username}`), and the
`authorization` spec requires that answer and no other: "collaborator
status SHALL be taken from GitHub's collaborator answer itself and never
inferred from a permission level". Asked by turnip's GitHub App, with the
repository permissions the setup guide gives it and no organization
permission, that answer leaves out access granted through a team.
Checked as the App with an installation token, 2026-10-15:

| Account | Repository | Collaborator answer | Permission answer |
|---|---|---|---|
| member with write through a team | private | 404, not a collaborator | `write` |
| organization admin | private | yes | `admin` |
| outsider | private | 404 | `none` |
| outsider (asked as a user) | public | 404 | `read` |

The collaborator list the App sees holds only the direct collaborator.
GitHub's documentation says this endpoint counts team members and needs
only the repository's metadata permission, which the App has; the answer
disagrees.

The permission answer (`GET .../collaborators/{username}/permission`),
with the same token, reports the team-granted role correctly. turnip
already uses it, for the write check a mutating command needs; only the
gate before it is wrong.

The spec's rule exists for a real reason, also checked: on a public
repository GitHub reports `read` for any account, so treating any
reported permission as access would let anyone trigger plans there. On a
private repository an account with no access is reported `none`.

A second fault hides the first: when turnip cannot ask GitHub at all
(an error, a rate limit), the "Fail closed" requirement answers with the
same "does not have permission" message as a refusal, so neither the
author nor the operator can tell a missing permission from a failed
check.

## What Changes

- An author is a collaborator when GitHub's collaborator answer says so,
  or when the permission answer reports access the repository's
  visibility cannot explain: on a private repository any role other
  than `none`, on a public one a role above `read` (triage, write,
  maintain, admin). Everyone admitted today is still admitted; the only
  accounts added are those whose access GitHub reports but the
  collaborator answer leaves out, such as team members
- The repository's visibility is read from the webhook (`repository.visibility`); an internal repository is judged like a public one
- When turnip cannot ask GitHub, it still runs nothing (it fails
  closed), but the reply says it could not check the author's
  permission, not that the author lacks it; the error is logged as today
- `docs/operating/github-app.md` explains how commenters are checked,
  and that access through a team works with no organization permission

## Not in this change

- Asking for the organization's "Members" permission: not needed, and
  not verified to change the collaborator answer
- The roles themselves (outsider, collaborator, writer) and what each
  may trigger, which are unchanged

## Capabilities

### New Capabilities

### Modified Capabilities
- `authorization`: collaborator status from the collaborator answer or a permission the repository's visibility cannot explain (Roles); the reply when GitHub cannot be asked (Fail closed)

## Impact

`internal/github` (`Authorizer.IsCollaborator`, the repository's
visibility in the webhook types), `internal/orchestrator` (the comment
gate and its reply), `docs/operating/github-app.md`. Depends on nothing
open, and nothing depends on it. No GitHub App permission changes.
