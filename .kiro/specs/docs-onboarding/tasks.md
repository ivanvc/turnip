# Implementation Plan: Documentation a New User Can Follow (Slice 48)

## Overview

The new pages are written first, while the old ones still exist, so the
agents writing them in parallel only ever read the old pages and never
edit the same file. The old pages are deleted, and every link updated,
in a wave of their own once every new page exists.

**The checkpoint that matters is task 8.** A reorganization that reads
well can still have dropped a section, and nothing but a comparison
against the old pages, section by section, shows that. The design's
section map is the checklist. The link test shows nothing points
nowhere; only this shows nothing went missing.

Tasks in the same wave of the dependency graph (at the end) touch
disjoint files and run in parallel. The tree builds, and the link test
passes, after every wave.

## Tasks

- [x] 1. Every Server setting read in one place
  - [x] 1.1 `TURNIP_LOG_LEVEL` read in `ConfigFromEnv` as a `Config`
    field; `cmd/server` uses it instead of reading the variable itself
  - [x] 1.2 Tests: the level reaches `Config`; unset keeps today's
    default
  - _Requirements: 4.4_

- [x] 2. The link test (`internal/doccheck`)
  - [x] 2.1 A test-only package walking every Markdown file except
    `.kiro/`, `.claude/` and `docs/internal/`, checking each relative
    link's file and `#anchor` by GitHub's anchor rule; external links
    skipped
  - [x] 2.2 The anchor rule tested on its own: backticks, punctuation,
    a repeated heading
  - [x] 2.3 Passes on today's documentation
  - _Requirements: 5.2_

- [x] 3. The end-user pages (`docs/using/`), written from the old pages,
  which are left untouched
  - [x] 3.1 `turnip-yaml.md`: the Reference table first (every schema
    field, first column its path), then the explanations the design's
    map sends here
  - [x] 3.2 `commands.md` and `pull-requests.md`, per the map
  - [x] 3.3 `troubleshooting.md`: the entries an end user can fix; for
    one only an operator can fix, its symptom and a link to
    `operating/troubleshooting.md`
  - [x] 3.4 Each page's opening names its reader, per the design
  - _Requirements: 2.1–2.3_

- [x] 4. The operator pages (`docs/operating/`), written from the old
  pages, which are left untouched
  - [x] 4.1 `deployment.md`, with Redis persistence in Prerequisites:
    Redis must persist its data, since losing it drops every Lock and
    recorded plan and a pull request must plan again before it can apply
  - [x] 4.2 `server-settings.md`: the Reference table first (every
    setting `ConfigFromEnv` reads, `TURNIP_LOG_LEVEL` included, with
    default and whether required), then the explanations the map sends
    here, mutation requirements among them
  - [x] 4.3 `github-app.md`, `runner-access.md`, `troubleshooting.md`,
    per the map
  - [x] 4.4 Each page's opening names its reader; `SECURITY.md` linked
  - _Requirements: 2.1–2.4, 7.1_

- [x] 5. The developer and plugin index pages
  - [x] 5.1 `docs/development/README.md`: today's `development.md`, the
    root README's Architecture section, "How a tool reaches the Runner",
    and the kind overlay's local development
  - [x] 5.2 `docs/plugins/README.md`: the Tool support table
  - _Requirements: 2.1, 6.2_

- [x] 6. Retire the old pages and connect the new ones
  - [x] 6.1 Delete `configuration.md`, `usage.md`, `deployment.md`,
    `troubleshooting.md` and `development.md` from `docs/`
  - [x] 6.2 `docs/README.md`, the entry page; the root `README.md` down
    to turnip's introduction, "How it works" in a few lines, and a link
    to the entry page
  - [x] 6.3 Every link to an old page updated, in every Markdown file
    (`docs/plugins/helmfile.md`, `SECURITY.md` and the new pages among
    them), and in the files the link test does not read: comments in
    `.goreleaser.yaml` and `deploy/`, and any Go string or comment that
    names a docs page (found by searching for `docs/` and each old file
    name)
  - _Requirements: 1.1, 1.2, 5.2, 5.3_

- [x] 7. The Guides, the References' tests, and `CLAUDE.md`
  - [x] 7.1 `docs/getting-started/operators.md` and `end-users.md`, per
    the design's table, each step linking to the section that explains
    it, each ending with "Next"
  - [x] 7.2 The `turnip.yaml` Reference test (`internal/config`): the
    schema's paths by reflection against the table's first column, both
    directions
  - [x] 7.3 The Server settings Reference test (`internal/orchestrator`):
    the names `ConfigFromEnv` asks for against the table's first column,
    both directions
  - [x] 7.4 Each Reference test shown to fail, in a throwaway check, for
    a missing row and for a row naming nothing
  - [x] 7.5 `CLAUDE.md`: "Where documentation goes", per the design
  - _Requirements: 3.1–3.3, 4.1–4.3, 6.1_

- [x] 8. Checkpoint: nothing lost
  - [x] 8.1 Every section in the design's map, compared against the old
    pages (from `git show HEAD:docs/...`), found on its new page with its
    substance intact; anything missing restored
  - [x] 8.2 Every page opens by naming its reader; no page repeats
    another audience's material; CLAUDE.md's writing style holds
  - [x] 8.3 `go build ./...`, `go test -race ./...` (the link test and
    both Reference tests among them), golangci-lint (v2) pass
  - _Requirements: 2.2, 2.3, 5.1_

- [x] 9. Final checkpoint
  - Every acceptance criterion maps to a task
  - Roadmap status updated
  - Any intermittent test failure is recorded in the roadmap Backlog's
    intermittent-failure entry, with the full output kept, rather than
    rerun and dropped
  - Deviations recorded at implementation: none found. `go test -race
    ./... -count=1` passed cleanly on three separate runs; no test
    result varied between runs

- [x] 10. Amendment: an existing link checker, not our own
  - [x] 10.1 `internal/doccheck`'s link test replaced by lychee
    (decided 2026-09-30: rely on existing tools rather than maintain a
    checker). `make docs-check` runs it offline with fragment checking
    over the Markdown git tracks, `.kiro/` excluded; `make test` depends
    on it; the Makefile downloads lychee at its pinned version into
    `bin/` on first use, verified against the release's sha256, so CI
    and a local run use the same binary with nothing to install. The
    version is the tag in `build/tools/lychee/Dockerfile`, a file never
    built, kept so Dependabot's docker ecosystem bumps it (tag only: the
    image is never pulled, so its digest would only add noise)
  - [x] 10.2 Verified before switching, against a scratch copy: lychee
    matches GitHub's anchors for an arrow (double hyphen), backticks,
    punctuation and a repeated heading (`-1`), rejects each near miss,
    follows a link wrapped across lines, and ignores links in code; and
    it reports nothing on the current documentation
  - [x] 10.3 The Reference tests' shared table reader moved from
    `internal/doccheck/reftable` to `internal/reftable`; `internal/doccheck`
    deleted
  - [x] 10.4 `CLAUDE.md` (commands, CI order, the documentation rule) and
    `docs/development/README.md` ("Checking the documentation") describe
    the new check

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1.1", "1.2", "2.1", "2.2", "2.3"] },
    { "id": 1, "tasks": ["3.1", "3.2", "3.3", "3.4", "4.1", "4.2", "4.3", "4.4", "5.1", "5.2"] },
    { "id": 2, "tasks": ["6.1", "6.2", "6.3"] },
    { "id": 3, "tasks": ["7.1", "7.2", "7.3", "7.4", "7.5"] },
    { "id": 4, "tasks": ["8.1", "8.2", "8.3"] },
    { "id": 5, "tasks": ["9"] }
  ]
}
```
