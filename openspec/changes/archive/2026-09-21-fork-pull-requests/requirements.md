# Requirements: Refuse Fork Pull Requests (Slice 15)

## Introduction

turnip has no concept of a fork. `github.Repository` carries `Owner`,
`Name` and `URL`; `github.PullRequest` carries `Number`, `HeadSHA`,
`BaseRef`, `HeadRef` and `Draft`. Nothing identifies the *head*
repository, so there is no field to compare even if a check existed.

That matters because of what the Runner is handed. `execute.go:237-238`
sends the **base** repository's URL together with the pull request's head
SHA; `clone.go:89` fetches `<headSHA>:refs/turnip/head` from that remote,
and GitHub serves a fork's objects out of the base repository's shared
object store — so a fork's tree arrives, is merged with the base branch
(`clone.go:115`), and is executed by the tool container. The configuration
is read from that same head commit (`pullrequest.go:54`), so whoever opened
the pull request also chooses the project list, the directories, and the
`with:` values the tool runs with.

An automatic plan runs on `pull_request`/`opened` with no authorization
check, by design. So today, opening a pull request from a fork is enough
to execute code inside a Pod holding cloud credentials.

**One mitigation already holds and should not be re-litigated**:
`runner.serviceAccount` cannot be chosen from the pull request's own
configuration unless an operator opted in, because
`defaultAllowedOverrides()` returns an empty map. A fork's pull request
inherits the operator's default ServiceAccount rather than selecting one.

## Glossary

Terms additional to the global spec glossary:

- **Base_Repository**: the repository turnip is installed on — the one a
  Webhook_Event's `repository` field identifies.
- **Head_Repository**: the repository a pull request's head branch lives
  in. Equal to Base_Repository for an ordinary branch pull request;
  different for one opened from a fork.
- **Foreign Pull Request**: a pull request whose Head_Repository differs
  from its Base_Repository.

## Requirements

### Requirement 1: Head-repository identity is available on both trigger paths

**User Story:** As a maintainer, I want turnip to know where a pull
request's code comes from, so that it can decline to run code it did not
receive from the repository it is installed on.

#### Acceptance Criteria

1. THE `PullRequest` type SHALL carry the identity of the Head_Repository
2. THE Server SHALL populate it from the `pull_request` webhook payload
3. THE Server SHALL populate it from `GetPullRequest`, which is the only
   source available on the `issue_comment` path — that payload carries a
   pull request number and nothing else
4. THE comparison SHALL be between Head_Repository and Base_Repository
   identity, NOT the repository's `fork` flag

*Rationale for 1.4: a repository can be a fork while its pull requests
still originate from itself — turnip installed on a fork of some upstream
is an ordinary case. Keying on the flag would refuse legitimate work and
detect nothing a comparison does not.*

### Requirement 2: An automatic plan never runs on a Foreign Pull Request

**User Story:** As an operator, I want an unsolicited pull request from
outside my repository to execute nothing, so that opening one is not
enough to run code against my infrastructure.

#### Acceptance Criteria

1. WHERE a `pull_request` event's Head_Repository differs from its
   Base_Repository, THE Server SHALL NOT acquire a Lock, create a Runner
   Job, or create a check run
2. THE refusal SHALL occur before the repository's configuration is read,
   since that file is itself attacker-controlled on a Foreign Pull Request
3. THE refusal SHALL apply only to the paths that execute code. Lock
   release on `closed` SHALL continue to run, because it executes nothing
   and refusing it would strand a Lock

### Requirement 3: A comment trigger never runs on a Foreign Pull Request

**User Story:** As an operator, I want a trusted collaborator to be unable
to run untrusted code by accident, so that authorization of the *trigger*
is not mistaken for trust in the *code*.

#### Acceptance Criteria

1. WHERE an `issue_comment` Trigger Command names a pull request whose
   Head_Repository differs from its Base_Repository, THE Server SHALL
   execute no Operation
2. THE refusal SHALL hold regardless of the commenter's permission level —
   the collaborator check authorizes the trigger, not the code
3. THE refusal SHALL occur before the repository's configuration is read

### Requirement 4: A refusal is silent on the pull request and recorded on the Server

**User Story:** As an operator, I want to see that turnip refused a
Foreign Pull Request, so that I can tell a contributor's mistake from an
attempt to run code against my infrastructure.

#### Acceptance Criteria

1. THE Server SHALL NOT post a comment, and SHALL NOT create or update a
   check run, on a Foreign Pull Request
2. THE Server SHALL write a log entry for every refusal at `WARN`, which
   is visible under the default configuration (`ParseLevel` defaults to
   `INFO`) while remaining distinguishable from routine activity — a
   refusal is a security signal, not a normal outcome to be read past
3. THE log entry SHALL identify the Base_Repository, the pull request
   number, the Head_Repository, and the account that opened the pull
   request or typed the Trigger Command
4. THE Server SHALL count the refusal using the existing webhook-event
   metric with the `rejected` outcome, rather than introducing a new
   outcome label

*Rationale: silence on the pull request denies an attacker feedback. The
log entry is not merely diagnostic — a refusal means someone outside the
repository attempted to have turnip execute their code, and repeated
refusals are the signal that an operator's infrastructure is being probed.
It therefore has to carry enough to act on, not merely record that
something was refused.*

*Accepted cost: a collaborator who types a Trigger Command on a Foreign
Pull Request receives no reply. This deliberately differs from the
missing-configuration case, where the automatic path stays silent but an
explicit command still answers, on the reasoning that a human asked. Here
the attacker may be the one asking.*

### Requirement 5: There is no opt-in

**User Story:** As a maintainer, I do not want a switch whose safety rests
on an operator's unverifiable claim.

#### Acceptance Criteria

1. THE Server SHALL provide no configuration — environment variable,
   `turnip.yaml` key, or allowed-override path — that permits Operations
   on a Foreign Pull Request

*Rationale: an opt-in is only safe for a genuinely credential-free
deployment, and turnip cannot verify that. The Runner's ServiceAccount,
its node's cloud identity, and `runner.env` all sit outside what turnip
can inspect, so the switch would be safe exactly when an operator's
self-assessment happened to be right. The Backlog's stated-security-model
entry is where such an opt-in belongs once there is a model to state it
against.*

### Requirement 6: Documentation

#### Acceptance Criteria

1. THE documentation SHALL state that turnip runs no Operation on a pull
   request opened from a fork, on either trigger path
2. THE documentation SHALL state that the refusal is silent on the pull
   request, and name the log entry an operator can look for

## Out of Scope

- **The installation token in `.git/config`.** `clone.go:97` runs
  `git remote add origin <authenticated URL>`, persisting the token into
  the shared workspace volume the tool container also mounts — which
  defeats `jobs/build.go` keeping `TURNIP_GITHUB_TOKEN` out of that
  container's environment. It is a live credential exposure with no fork
  involved, so it gets its own slice rather than waiting behind this one.
- **A stated security model**, and with it any future opt-in. Backlog.
- **Fork pull requests from a repository the App is also installed on.**
  Head_Repository identity is compared against the Base_Repository of the
  event being handled; whether turnip might legitimately act on a fork it
  is separately installed on is a question this slice does not open.
