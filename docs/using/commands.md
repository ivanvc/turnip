# Commands

> **For end users**: people writing `turnip.yaml` and commenting on pull
> requests. This assumes turnip is already deployed and a repository has
> a [`turnip.yaml`](turnip-yaml.md). For what happens automatically, see
> [Pull requests](pull-requests.md).

## Triggering by comment

Comment on the PR:

```
/turnip <operation> [project...] [args...]
```

or scope it to one tool by using the tool's own name instead of `turnip`:

```
/helmfile <operation> [project...] [args...]
```

The difference: `/turnip ...` considers projects using any tool;
`/helmfile ...` considers only the projects whose `uses:` runs Helmfile.
Each tool this Server has a Plugin for gets its own trigger word, and
today that is only `/helmfile`. `<operation>` must be one the tool
actually supports; each tool's page lists its operations (Helmfile:
[`diff`, `apply`, `sync`](../plugins/helmfile.md#operations)).

### Plan commands and mutating commands

Every tool has exactly one **plan command**, which shows what a change
would do, and any number of **mutating commands**, which change
infrastructure. A mutating command is every operation the tool offers
other than its plan; turnip does not keep a list of them, so a tool's new
operation is mutating unless it is the plan.

| Tool | Plan command | Mutating commands |
|---|---|---|
| [Helmfile](../plugins/helmfile.md#operations) | `diff` | `apply`, `sync` |

The split decides most of what turnip allows:

- **Who can run it.** Any collaborator can run a plan; a mutating command
  needs write access (see [Who can trigger what](#who-can-trigger-what)).
- **What it needs first.** A mutating command runs only on a project this
  pull request has planned, and runs what that plan saw (see
  [Plan → apply, and why apply needs a plan first](pull-requests.md#plan--apply-and-why-apply-needs-a-plan-first)).
- **Extra arguments.** Only a plan takes them; a mutating command's
  arguments come from the plan it follows.
- **What the operator can require.** `TURNIP_MUTATION_REQUIREMENTS` can
  hold every mutating command until the pull request is approved, or has
  no merge conflicts (see
  [Mutation requirements](../operating/server-settings.md#mutation-requirements),
  and what you see when one isn't met in
  [Troubleshooting](troubleshooting.md#mutation-requirements-not-met)).
- **What runs by itself.** Only plans. turnip never runs a mutating
  command without a comment asking for it.

A plan changes no infrastructure, but it is not read-only: it runs the
project's code with the Runner's credentials (Helmfile's `prepare` hooks
run during `diff`; see
[What that grant means](../plugins/helmfile.md#what-that-grant-means)).
See [`SECURITY.md`](../../SECURITY.md).

## Which projects a command targets

Naming projects is optional. What you get when you omit them depends on
the operation:

| Trigger | Targets |
|---|---|
| `/turnip diff` | the projects whose `whenModified` patterns match this pull request's changed files, the same set the automatic plan picks |
| `/turnip apply` | the projects this pull request has already planned |
| `/turnip diff web` | `web`, whether or not the pull request touched it |
| `/turnip diff *` | every project |
| `/turnip diff gcp/*` | every project whose **name** matches the pattern |

A plan that matches nothing, and an apply with nothing planned, each get a
single reply saying so, not one refusal per configured project.

**Patterns match names, not directories.** They use the same glob syntax
as `whenModified`: `*` matches within one path segment and `**` crosses
segments, so `gcp/*` reaches a project named `gcp/project` but not
`gcp/team/project`, where `gcp/**` reaches both. A pattern matching
nothing is reported at the top of the results comment and the rest of the
command still runs, so a mistyped `gpc/*` tells you, rather than quietly
doing less than you asked.

**`*` on its own is a reserved word, not a pattern.** It means *every*
project, which matters because a `*` pattern stops at a `/` and would
skip projects named for their path. It cannot be combined with other
selectors: a trigger either names projects or asks for all of them.

A project name cannot contain `*` or begin with `-`; turnip rejects such
a name when it parses the configuration file, rather than leaving you to
discover that nothing can select it. A name containing whitespace parses
but can never be addressed either, since a trigger line is split on
spaces; avoid it.

**Wrap a trigger carrying two `*` characters in backticks when you type
it.** Markdown renders the text between two asterisks as italics and eats
them, so a comment reading `/turnip diff gcp/* aws/*` displays as
something other than what you wrote. Backticks make the comment show what
you typed; turnip receives the raw text either way, so this affects
whether *you* can check the command, not whether it works.

**Very large pull requests.** GitHub lists at most 3000 changed files, so
beyond that the matched set may be incomplete. turnip says so in the
results comment when it happens; `*` targets every project regardless.

Examples:

```
/turnip diff
/turnip diff web
/helmfile apply web api
/helmfile diff web -l name=ingress
/turnip diff web -- --context=3
```

Arguments are passed straight through to the tool's own CLI; turnip
doesn't interpret them. **The first argument beginning with `-` is where
the project names stop**, so a `--` separator is optional; write one if
you prefer, and a second `--` further along is passed through verbatim.

**Only the plan operation takes arguments.** `apply` and `sync` replay the
scope the plan recorded, so they accept none of their own and refuse a
trigger that supplies any, naming what they refused. That is what makes an
apply match the diff you reviewed: the two cannot disagree, because only
one of them chose a scope.

You can put more than one trigger line in a single comment; each runs in
order (one finishes before the next starts), and a malformed line (e.g.
`/turnip` with nothing after it) is reported back without blocking the
other, well-formed lines in the same comment.

Only `/turnip` and one trigger per tool this Server has a Plugin for
(today `/helmfile`) are turnip's. A line starting with anything else
(`/terraform` and `/pulumi` until their Plugins land, another bot's
command like `/jira`, a `/cc`, or a file path pasted at the start of a
line) is ignored completely: no operation, and no reply saying it was
ignored. turnip stays silent on comments that aren't addressed to it.

### Removing infrastructure

turnip only runs what a plan described, so removing infrastructure goes
through a plan like any other change. Whether a tool offers a command for
it, and how to remove things without one, is on the tool's page. Helmfile
has no `destroy`; see
[Removing a release](../plugins/helmfile.md#removing-a-release).

### Releasing a lock manually

```
/turnip unlock [project...]
```

Normally you'll never need this: a lock releases on its own once an apply
succeeds, when a plan finds nothing to apply, when a plan fails having
recorded nothing, or when the PR merges or closes. Use `unlock` to
abandon a stale plan (e.g. the PR is being reworked and the old plan no
longer applies) without merging or closing the PR first. Needs write
permission (see below), and never runs a tool or creates a Job; it's a
pure Redis operation.

## Who can trigger what

turnip recognizes three roles, and does not distinguish further:

| Role | Who |
|---|---|
| **Outsider** | not a collaborator on the repository |
| **Collaborator** | a collaborator below write: read or triage |
| **Writer** | write, maintain or admin |

Write, maintain and admin are treated identically; nothing turnip does
needs to tell them apart.

| Action | Outsider | Collaborator | Writer |
|---|---|---|---|
| `/turnip diff`, `/helmfile diff` (the plan command) | no | **yes** | yes |
| `/turnip apply`, `/helmfile sync` | no | no | **yes** |
| `/turnip unlock` | no | no | **yes** |

An outsider's comment gets a reply saying so, and nothing runs. A
collaborator asking for an apply gets a reply naming the permission they
are missing.

**"Collaborator" is wider than it sounds.** For an organization-owned
repository, GitHub counts outside collaborators, members who are direct
collaborators, members with access through a team, members with access
through the organization's *default* permission, and organization owners.
If your organization grants its members a default permission on
repositories, every member can trigger a plan.

### Automatic plans have no actor

The table above governs comments. A plan that turnip starts by itself
(on open, push, or reopen) has no one to authorize: it is a consequence
of the commit existing. Its real gate is GitHub's, not turnip's, because
pushing the branch required access in the first place, and a pull request
from a fork is refused outright (see
[Pull requests from forks](pull-requests.md#pull-requests-from-forks)).

So a read-level collaborator can ask for a plan but cannot cause one by
pushing. That asymmetry is deliberate: someone reviewing a change needs to
be able to ask what it would do, and a plan changes no infrastructure.

### This is not configurable, on purpose

There is no setting to raise or lower these levels. An apply that could be
permitted below write would be a way to get it wrong quietly, and a
setting that only ever has one safe value is not a setting. If you need a
plan restricted more tightly than "any collaborator", the lever is
GitHub's (the repository's collaborator list and your organization's
default permission) rather than a turnip key that a repository could
later be permitted to set for itself.

### What else has to be true

Permission is necessary, not sufficient. Independently of who asks, turnip
refuses to act on a pull request from a fork, on one that is closed or
merged, and refuses a mutating operation whose project has no valid plan
recorded. Those are covered in [Pull requests](pull-requests.md).

## Next

- [`turnip.yaml` reference](turnip-yaml.md)
- [Helmfile](../plugins/helmfile.md): the operations, `with:` keys and
  gotchas specific to that tool
