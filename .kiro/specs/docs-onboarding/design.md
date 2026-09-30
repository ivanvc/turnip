# Design: Documentation a New User Can Follow (Slice 48)

## Overview

A move, two new guides, an entry page, and three tests that keep the
result from rotting: two References checked against the code, and every
relative link checked against its target.

```
docs/
  README.md                  entry: end user / operator / developer
  getting-started/
    end-users.md             first turnip.yaml → first plan → first apply
    operators.md             nothing → Server receiving webhooks
  using/                     end users
    turnip-yaml.md           Reference first, explanations below
    commands.md              triggers, targeting, plan vs mutating, who can run what
    pull-requests.md         automatic plans, what you'll see, checks, locks
    troubleshooting.md
  operating/                 operators
    deployment.md            install, upgrade, scaling, Redis (must persist)
    server-settings.md       Reference first, explanations below
    github-app.md
    runner-access.md         ServiceAccount, cloud credentials, custom images
    troubleshooting.md
  plugins/
    README.md                the tools turnip runs, one line each
    helmfile.md
  development/
    README.md
```

`plugins/README.md` is the one page added to the approved tree: today's
"Tool support" table needs a home that is neither audience's, and it is
the natural index of the Plugin_Pages.

## Every page's opening

Each page opens with one line naming its reader, then what the page is
for, so a reader who arrived from a search knows at once whether they
are in the right place:

> **For end users**: people writing `turnip.yaml` and commenting on pull
> requests. Operators: see [Server settings](../operating/server-settings.md).

A Guide ends with "Next", pointing to the References and the
Plugin_Pages (Requirement 3.3). A Reference opens with its table.

## The section map

Every section of today's five pages, and where it lands (Requirement
5.1). A section whose end-user and operator halves differ is split; the
two halves link to each other, as the Helmfile page's EKS recipe does.

### `configuration.md`

| Section | Lands in |
|---|---|
| intro, the file's location, example, field table | `using/turnip-yaml.md`, the table becoming the Reference |
| `schemaVersion`, `clone`, `with`, `runner` (shared settings, reserved `env` names) | `using/turnip-yaml.md` |
| `uses`: the forms, aliases, the apply running the plan's digest | `using/turnip-yaml.md` |
| `uses` › "Running your own image": `TURNIP_ALLOWED_IMAGES`, what an image must provide, private registries | `operating/runner-access.md`; the end-user half (writing an image in `uses:`) stays in `using/turnip-yaml.md` |
| What the Runner's ServiceAccount needs | `operating/runner-access.md` |
| Where the Runner puts things | `using/turnip-yaml.md`, since end users write `/turnip/src` paths |
| Tool support | `plugins/README.md` |
| How a tool reaches the Runner | `development/README.md`; its image requirements, already on the Plugin_Page, are linked from `operating/runner-access.md` |
| The Server's own configuration | `operating/server-settings.md`, the table becoming the Reference |
| Mutation requirements | `operating/server-settings.md`; what an end user sees when one is unmet stays in `using/troubleshooting.md` and is linked from `using/commands.md` |
| Setting up the GitHub App | `operating/github-app.md` |

### `usage.md`

| Section | Lands in |
|---|---|
| Automatic plans, draft, fork and closed pull requests | `using/pull-requests.md` |
| Triggering by comment, plan and mutating commands, which projects a command targets, removing infrastructure, releasing a lock manually | `using/commands.md` |
| Plan → apply, when a stored plan stops being usable, what releases a lock | `using/pull-requests.md` |
| Who can trigger what, and its subsections | `using/commands.md` |
| Requiring turnip before merge, what you'll see on the PR, what turnip ran | `using/pull-requests.md` |

### `deployment.md`

| Section | Lands in |
|---|---|
| Prerequisites, installing, verifying, upgrading, what turnip asks of your cluster | `operating/deployment.md`, with Redis persistence added to Prerequisites (Requirement 7) |
| How a Runner gets its GitHub credential | `operating/runner-access.md` |
| Runner-to-Server traffic is not encrypted, the Grafana dashboard, scaling up | `operating/deployment.md` |
| Local development (the kind overlay) | `development/README.md` |

### `troubleshooting.md`

Split by **who can fix it**. An entry an end user meets but only an
operator can fix stays on the end-user page as its symptom and one line,
"ask whoever runs turnip", linking to the operator entry.

| Section | Lands in |
|---|---|
| Configuration errors, lock contention, the `turnip` check, plugin execution errors, authorization errors | `using/troubleshooting.md` |
| Reading a failed check's title | split: every title and what an end user does about it in `using/troubleshooting.md`; the three the Server fails on (`lock could not be acquired`, `operation could not be recorded`, `Runner Job could not be built`) explained in full under "Failed checks that are the Server's to fix" in `operating/troubleshooting.md` |
| Mutation requirements not met | split: the reply and each line's fix in `using/troubleshooting.md`; the INFO log line in `operating/troubleshooting.md`, "Mutation requirement refusals" |
| Fork pull requests are refused | split: the symptom in `using/troubleshooting.md`; the WARN log line, its fields and why to watch it in `operating/troubleshooting.md` |
| GitHub API errors, Runner execution errors, high-availability behavior | `operating/troubleshooting.md` |

### The rest

| Today | Lands in |
|---|---|
| `development.md` | `development/README.md` |
| root `README.md`'s documentation table and Plugins list | `docs/README.md`; the root keeps turnip's introduction, a "How it works" of three or four lines, and one link (Requirement 1.2) |
| root `README.md`'s Architecture section | `development/README.md` (decided 2026-09-30): it is what a developer needs and more than someone deciding whether to try turnip reads |

**Old paths are not kept as stubs** (decided 2026-09-30). Pre-1.0, with
one pilot repository, a redirect page per moved file is maintenance for
readers who do not exist; the repository owner updates any link to the
old paths from outside this repository. Links inside the repository are
all updated and checked (Requirement 5.2).

**Files that are not Markdown** also cite docs by path, and the link
test does not read them: comments in `.goreleaser.yaml` and
`deploy/overlays/release/kustomization.yaml` name `docs/deployment.md`.
They are updated by hand, found by a search the tasks name.

## The two Guides

| Guide | From | To | Steps |
|---|---|---|---|
| `getting-started/operators.md` | a cluster and a repository | a Server receiving webhooks from that repository | create the GitHub App; provide Redis, with persistence; install with Kustomize; set the required settings; grant the Runner's ServiceAccount; install the App on the repository; verify |
| `getting-started/end-users.md` | a repository with a Helmfile project, and a Server someone runs | a first apply | write `turnip.yaml`; open a pull request and read the plan; comment `/turnip apply`; read the result and the `turnip` check |

Each step is short and links to the section that explains it; a Guide
repeats no Reference. The end-user Guide uses Helmfile, the one tool
that exists, and says so.

## Keeping the References complete

### `turnip.yaml`

A test walks the configuration schema by reflection, from `config.Config`
through every struct it contains, reading each field's YAML name and
building its path: `schemaVersion`, `clone.submodules`,
`runner.serviceAccount`, `projects[].uses`, and so on. Fields tagged
`yaml:"-"` are skipped, and `with` is a leaf, since its keys belong to
each Plugin_Page. The test reads the first column of the Reference table
in `using/turnip-yaml.md` and requires the two sets to be equal, so a
field missing from the table, or a row naming a field that no longer
exists, fails.

### Server settings

A test calls `ConfigFromEnv` with a `getenv` that records every name it
is asked for, and requires that set to equal the first column of the
Reference table in `operating/server-settings.md`.

That only works if every Server setting is read there, and one is not:
`cmd/server` reads `TURNIP_LOG_LEVEL` itself, and it is documented
nowhere. **`TURNIP_LOG_LEVEL` moves into `ConfigFromEnv`**, as a
`Config` field `cmd/server` reads, and gains its row.

Both tests read their table through one shared helper,
`internal/doccheck/reftable` (the first table's first column, and the
missing/extra comparison), so a fix to the table reader is made once.

### Decision 1: Record what the code reads, rather than scan the source

*Alternative considered*: search the Server's Go source for
`"TURNIP_…"` literals.

*Rejected because*: a literal is not a read. The Runner's variables,
which the Server sets on every Job and no operator configures, appear in
the same packages, and a name built by concatenation would be missed.
Recording what `ConfigFromEnv` actually asks for is exact, and moving
the one stray read into it is a two-line change that also gives the
Server one place its configuration comes from.

## The link test

*Superseded 2026-09-30 by lychee (Decision 2, task 10); kept as the
record of what the check must cover.*

A test walks every Markdown file in the repository except `.kiro/`,
`.claude/` and `docs/internal/`, and checks each relative link:

| Link | Check |
|---|---|
| `path/to/page.md` | the file exists |
| `path/to/page.md#anchor` or `#anchor` | the target file has a heading whose GitHub anchor is `anchor` |
| `http://…`, `https://…`, `mailto:` | not checked: no network in tests |

GitHub's anchor rule: lower-case the heading's text, drop everything but
letters, digits, spaces, `-` and `_`, turn spaces into `-`, and number a
repeated anchor `-1`, `-2`. The test lives in a test-only package,
`internal/doccheck`, so it runs with `go test ./...` and in CI.

### Decision 2: An existing link checker (amended 2026-09-30)

*First decided*: a Go test in `internal/doccheck`, so the check ran
with `go test` and depended on no tool or website.

*Amended because* the repository owner would rather rely on a tool that
already exists than maintain a checker: lychee, run offline
(`--offline --include-fragments`), does not touch the network, and
`make test` depends on `make docs-check`, so the check still runs with
the tests locally and in CI. Before switching, lychee was checked
against the cases the Go test covered (GitHub's anchor rule on an arrow,
backticks, punctuation and repeated headings; wrapped links; links in
code) and matched on each (task 10).

*Alternative considered*: remark-validate-links, which uses GitHub's own
heading-anchor library. Rejected because it brings a Node toolchain into
a Go repository's CI, and lychee matched on every case tested.

## `CLAUDE.md`

A new section, "Where documentation goes", beside the writing-style
rules: the tree above in one table (audience, directory, what goes
there), the rule that a page links rather than repeats, that every
tool has a Plugin_Page with "Using" and "Running" sections, and that the
two References must gain a row with any field or setting a slice adds
(which the tests enforce).

## Testing

- **The schema Reference test**: passes on the finished page; fails, in
  a throwaway check during implementation, when a row is removed and when
  a row is added for a field that does not exist.
- **The settings Reference test**: the same, and `TURNIP_LOG_LEVEL` read
  through `ConfigFromEnv`, with `cmd/server` using the `Config` field.
- **The link test**: its anchor rule tested on its own, against headings
  with backticks, punctuation, and a repeated title; and run over the
  repository.
- **The map**: every section in the tables above found on its new page,
  checked during implementation against the old pages, section by
  section.
