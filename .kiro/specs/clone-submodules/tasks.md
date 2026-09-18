# Implementation Plan: Cloning Submodules (Slice 16)

## Overview

Ordered so that each stage is provable before the next depends on it, and
so the slice's security fix cannot be lost if the slice stalls.

The **redaction fix lands first**. It is required by everything that
follows — every rewritten submodule URL carries the token inside a larger
string, which today's redaction does not strip — but it is also valuable
on its own, is confined entirely to `clone.go`, and needs no configuration
to exist. Landing it first means a half-finished slice still leaves the
codebase safer than it found it.

**The clone mechanics land second, before any configuration plumbing.**
`Clone` takes the mode as an ordinary parameter, so the whole feature is
exercisable against real local git fixtures while the Server still has no
idea the setting exists. A failure at that point is unambiguously about
git behaviour rather than about threading a value through six files.

**The plumbing lands last**, in dependency order from the leaf package
outward: schema, then the override gate, then the `Target` threading of
Decision 8, then the Job's environment. Each of those is mechanical once
the stage before it compiles.

## Tasks

- [x] 1. Redaction covers the token itself
  - [x] 1.1 Add the token as a second needle in `redactArgs` and `redact`
    - Both currently match only an argument or substring *exactly equal*
      to `authedURL`, so a token carried inside a larger string reaches a
      pull request comment. Both gain the token alongside `authedURL`
    - All four call sites are inside `clone.go` (the two step-failure
      wrappers, the unshallow-fallback wrapper, and `MergeConflictError`),
      so this causes no cross-package churn
    - **An empty token must be a no-op.** `strings.ReplaceAll(s, "", …)`
      inserts the placeholder between every character, and `Clone` is
      called with an empty token throughout the existing tests and
      wherever the remote is public — guard before replacing, mirroring
      `embedToken`'s existing "empty means no-op" convention
    - _Requirements: 4.3_
  - [x] 1.2 Test both halves
    - A token embedded inside a larger argument and inside subprocess
      output is stripped; an empty token leaves the string byte-identical
    - _Requirements: 4.3_

- [x] 2. Checkpoint - the security fix stands alone
  - `go build ./...` and `go test -race ./internal/runner/...` pass. No
    signature outside `clone.go` has changed yet, so a failure here is
    about redaction alone.

- [x] 3. Submodule initialisation in `clone.go`
  - [x] 3.1 Widen the `gitRunner` seam to carry environment
    - `execGit` leaves `cmd.Env` nil today, inheriting the parent's
      environment. The seam gains an environment argument so exactly one
      call can set `GIT_CONFIG_*`; every existing call passes nothing and
      keeps inheriting, so no current behaviour changes
    - `cloneWith` and `runMerge` both take the seam and are updated
    - _Requirements: 3.1, 3.2_
  - [x] 3.2 Read and classify submodule URLs before anything is fetched
    - `git config -f .gitmodules --get-regexp '^submodule\..*\.url$'`,
      verified to return `submodule.<name>.url <url>` pairs
    - A repository with no `.gitmodules` is a no-op and must not fail —
      the command exits non-zero when the file is absent, which is not an
      error condition here
    - Host extraction cannot be `net/url.Parse` alone: the scp-like form
      has no scheme and is recognised only when no slash precedes the
      first colon, which is what separates `host:org/repo` from the local
      path `./foo:bar`
    - A submodule on a host other than the repository's own is reported
      **before any fetch**, naming the submodule and that host
    - _Requirements: 1.3, 4.4_
  - [x] 3.3 Build the rewrite configuration
    - One **exact-URL** `insteadOf` entry per same-host submodule URL
      found in 3.2, mapping it to its authenticated HTTPS equivalent;
      `embedToken` already builds that equivalent
    - Plus the fixed prefix net for nested submodules, whose own
      `.gitmodules` cannot have been read yet (Decision 3)
    - Supplied as `GIT_CONFIG_COUNT` with `GIT_CONFIG_KEY_n`/
      `GIT_CONFIG_VALUE_n` pairs on the subprocess only — never in `argv`,
      never on disk, never in the Job spec
    - _Requirements: 3.1, 3.4, 3.5_
  - [x] 3.4 Run the initialisation, after the merge
    - `Clone` and `cloneWith` gain the mode; the step runs after
      `runMerge` returns, so the gitlinks resolved are the merged tree's
    - `none` skips entirely; `top-level` is `--init`; `recursive` is
      `--init --recursive`. **No `--depth`** — submodules are fetched at
      full depth so the pinned commit is certainly present
    - A submodule that cannot be fetched fails the clone, naming it. It
      must not leave an empty directory and continue: that is the exact
      behaviour that produced `Error: repo .. not found`
    - _Requirements: 1.1, 1.2, 1.4, 3.3, 4.1, 4.2_
  - [x] 3.5 Update the existing `Clone` call sites
    - Ten of them: eight in `clone_test.go`, two in `property_test.go`.
      (`slices.Clone` in `internal/jobs/build.go` is an unrelated name.)

- [x] 4. Checkpoint - the feature works with no configuration in sight
  - `go test -race ./internal/runner/...` passes, including the new
    submodule tests from task 11.1. At this point the Server still knows
    nothing about the setting; the mode is just a parameter.

- [x] 5. Repository configuration schema
  - [x] 5.1 Add `CloneSpec` and the `Clone` field
    - `Config` gains `Clone CloneSpec` with `yaml:"clone,omitempty"`,
      beside `SchemaVersion` and `Projects`; `CloneSpec` carries
      `Submodules string` with `yaml:"submodules,omitempty"`, mirroring
      how `RunnerSpec` is shaped
    - A constants block for the three modes, beside the existing tool
      constants
    - No change to `parse.go`: `rejectUnknownFields` decodes into this
      same struct, so adding the field is what makes `clone:` accepted —
      and what keeps an older Server rejecting it legibly
    - **No schema version bump** (Decision 7): the key is optional, so
      every file valid today stays valid
    - _Requirements: 2.3, 2.6, 2.7_
  - [x] 5.2 Validate the value
    - An unrecognised mode appends to the accumulated errors rather than
      returning early, with the file-level ref (`clone:` belongs to no
      project) and the field path `clone.submodules`
    - An empty value is valid and means "unset" — the Server's default
      applies
    - _Requirements: 2.2_

- [x] 6. The Server setting and the override gate
  - [x] 6.1 Register the override path
    - A `clone.submodules` path constant beside `runner.serviceAccount`,
      appended to the known-paths list keeping its sorted order so the
      "known paths are …" error message stays stable
    - It is **not** added to the default allowed set, which stays empty:
      the repository override is opt-in, as Decision 6 states
    - _Requirements: 2.4_
  - [x] 6.2 Read `TURNIP_CLONE_SUBMODULES`
    - Parsed in the orchestrator config's post-`missing` section, in the
      same shape as `TURNIP_ALLOWED_OVERRIDES`: a package-local parser
      returning a bare error, wrapped by the caller with the
      `parsing <VAR>` prefix, so a bad value is a startup error rather
      than a silently ignored setting
    - Unset means `top-level`, not empty
    - _Requirements: 2.1, 2.2_
  - [x] 6.3 Resolve the effective mode
    - A new file mirroring `serviceaccount.go`: requested value empty →
      the Server's default; requested but the path not allowed → a
      not-permitted error naming the path and telling the operator to add
      it to `TURNIP_ALLOWED_OVERRIDES`; otherwise the requested value
    - _Requirements: 2.3, 2.4_

- [x] 7. Thread the repository-scoped value to the Job (Decision 8)
  - [x] 7.1 Carry it on `Target`
    - `Target` gains a `Clone config.CloneSpec` field. It carries only a
      `config.Project` today, so the repository-scoped value has no route
      to the execution path at all — this is the gap Decision 8 exists to
      close
    - _Requirements: 2.5_
  - [x] 7.2 Populate it at both target-building sites
    - The pull-request path and the comment path each hold the fetched
      `*config.Config` and must copy `cfg.Clone` in. Missing either leaves
      a silently empty mode on one of the two ways an Operation starts
    - _Requirements: 2.5_
  - [x] 7.3 Resolve and pass it in `executeOne`
    - Immediately beside the ServiceAccount resolution and **before any
      lock is acquired**, so a refusal costs nothing and becomes a
      rejected result rather than a failed Job
    - The resolved value joins the Job's operation parameters
    - _Requirements: 2.4, 2.5_
  - [x] 7.4 Wire the Server default through construction
    - The orchestrator struct and its constructor gain the default, passed
      from the Server's entrypoint

- [x] 8. Checkpoint - the value reaches the Job spec
  - `go build ./...` and `go test -race ./...` pass. Tests that construct
    the orchestrator or an allowed-overrides map directly will need
    updating here; that is expected churn, not a regression.

- [x] 9. The Runner receives it
  - [x] 9.1 Set `TURNIP_CLONE_SUBMODULES` on the clone initContainer only
    - Appended to the clone container's environment, **not** to the shared
      base environment — the tool container has no use for it, matching
      how the installation token is handled after Slice 14
    - _Requirements: 2.1_
  - [x] 9.2 Read it in the Runner's configuration
    - **Optional, never required.** It is absent from the tool container,
      and the required-variable check runs before the mode branch — the
      exact mistake that made every Runner refuse to start during Slice 14
    - An absent value means `top-level`, so a Job built by an older Server
      still initialises submodules
    - _Requirements: 2.1_
  - [x] 9.3 Widen the clone entrypoint's seam
    - The `cloner` function type and the call through it gain the mode.
      Clone mode already rejects an empty workspace directory; that stays
    - _Requirements: 3.1_

- [x] 10. Checkpoint - end to end
  - `go build ./...` and `go test -race ./...` pass.

- [x] 11. Tests
  - [x] 11.1 `internal/runner`: submodules against real local repositories
    - The existing fixtures build real repositories rather than fakes and
      extend naturally: a fixture gains a submodule pointing at a second
      local repository
    - **Every local-submodule git call needs
      `-c protocol.file.allow=always`** — git refuses the `file` transport
      for submodules as CVE-2022-39253 hardening, and it blocks
      `submodule update --init`, not only `submodule add`. This is
      test-only and must never appear in `clone.go`
    - Cases: initialisation happens after the merge; `none` leaves the
      directory empty and does **not** fail, distinguishing "configured
      off" from "failed to fetch"; a fetch failure fails the clone and
      names the submodule
    - _Requirements: 1.1, 1.2, 1.3, 1.4, 3.3, 4.1, 4.2_
  - [x] 11.2 `internal/runner`: the rewrite and its derivation
    - A token embedded in a rewritten submodule URL does not appear in a
      reported error — the assertion whose absence leaks a credential
    - An SSH URL on the repository's own host is rewritten and fetched
      rather than refused
    - **A URL carrying an explicit port is rewritten too.** This is the
      assertion that pins the derivation: a prefix list passes the plain
      SSH case and fails this one, so without it Decision 3's central
      claim goes untested
    - Host extraction handles the scp-like form and does not mistake a
      local path containing a colon for one
    - A foreign host is reported before any fetch is attempted
    - _Requirements: 3.4, 3.5, 4.3, 4.4_
  - [x] 11.3 `internal/config`: the schema
    - A valid `clone.submodules` round-trips; an unrecognised value is a
      validation error naming `clone.submodules`; an absent `clone:` block
      leaves the zero value; an unknown key *inside* `clone:` is rejected
      by the existing strict decode
    - _Requirements: 2.2, 2.3, 2.6, 2.7_
  - [x] 11.4 `internal/orchestrator`: the gate
    - An unrecognised `TURNIP_CLONE_SUBMODULES` is a startup error; the
      override is refused when the path is absent from the allowed set and
      honoured when present; the default applies when the repository says
      nothing
    - _Requirements: 2.1, 2.2, 2.4_
  - [x] 11.5 `internal/jobs`: the Job shape
    - `TURNIP_CLONE_SUBMODULES` appears on the clone initContainer and
      **not** on the main container
    - _Requirements: 2.1_

- [x] 12. Documentation
  - [x] 12.1 `docs/configuration.md`
    - The Server setting in the Server-configuration section, and a
      `clone:` subsection alongside the existing `runner:` one, covering
      the three modes and that the override is gated and opt-in
    - State that submodule fetches use the same installation token and so
      reach only repositories the App is installed on
    - _Requirements: 5.1, 5.3_
  - [x] 12.2 `docs/troubleshooting.md`
    - The symptom this slice was found through — a tool reporting a
      missing path or repository that is really an uninitialised submodule
      — under the Runner-execution section, since that is what the next
      person will search for
    - _Requirements: 5.2_
  - [x] 12.3 Deployment manifest
    - The new variable in the Server's environment literals, beside the
      existing allowed-overrides entry

- [x] 13. Final checkpoint - full verification
  - `go build ./...`, `go test -race ./...`, `go mod tidy` clean,
    `gofmt -l .` clean, and the real golangci-lint v2 via
    `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest run ./...`

- [x] 14. Amendment - name the likely cause of "Repository not found"
  - Found in the pilot after the slice was complete: a private submodule on
    the repository's *own* host failed with git's bare
    `remote: Repository not found`. Read literally that says the repository
    does not exist, when it actually means the credential cannot see it —
    GitHub answers identically for both, so as not to leak which private
    repositories exist
  - turnip has already rewritten the URL to carry the installation token by
    that point, so the fetch was authenticated. The remaining explanation is
    almost always that the App is not installed on the submodule's
    repository; an installation set to "only select repositories" commonly
    covers the parent but not a shared chart or module repository beside it
  - This is the same class of misleading message the slice exists to
    remove — `Error: repo .. not found` was the original one — so it
    belongs here rather than in a later slice
  - The hint is attached only to a "Repository not found" output, with a
    test pinning that an unrelated failure does not acquire it
  - _Requirements: 4.1_

## Notes

- **No new dependencies.** The rewrite is git configuration, the mode is a
  string, and the Job shape uses `corev1` types already in use.
- **Two mechanisms were verified experimentally before being specified**,
  rather than taken from documentation: that an *exact full URL* works as
  an `insteadOf` value supplied through `GIT_CONFIG_*` (proven against an
  SSH URL with an explicit port — the form a prefix rule misses), and that
  `git config -f .gitmodules --get-regexp` reads the URLs as Decision 4
  assumes. Both were confirmed on git 2.55.
- **`protocol.file.allow=always` is test-only.** Production submodule URLs
  resolve to `https://` — relative ones included, since they resolve
  against the parent's authenticated HTTPS remote — so nothing in the
  clone path needs it. Setting it there would reopen CVE-2022-39253.
- **The empty-token trap in task 1.1** is the one place this slice can
  introduce a bug that every existing test would miss: the suite calls
  `Clone` with an empty token almost everywhere, and an unguarded
  `ReplaceAll` on an empty needle corrupts every string it touches.
- **Observation, not a task:** the orchestrator's constructor already
  takes ten positional parameters and this slice adds an eleventh. An
  options struct would be an improvement, but it is unrelated churn in a
  slice that is already threading a value through six files — worth
  raising separately rather than bundling here.
- **Ignore `.claude/worktrees/fix-version-validation-clean/`** when
  grepping: it is a stale duplicate tree that doubles every call-site
  count. Go tooling skips dot-directories, so it does not affect builds.
- **Coverage target**: 80% for touched packages, consistent with prior
  slices.

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1.1"] },
    { "id": 1, "tasks": ["1.2", "3.1"] },
    { "id": 2, "tasks": ["3.2", "3.3"] },
    { "id": 3, "tasks": ["3.4"] },
    { "id": 4, "tasks": ["3.5", "5.1"] },
    { "id": 5, "tasks": ["5.2", "6.1", "6.2"] },
    { "id": 6, "tasks": ["6.3", "7.1"] },
    { "id": 7, "tasks": ["7.2", "7.3", "7.4"] },
    { "id": 8, "tasks": ["9.1", "9.2", "9.3"] },
    { "id": 9, "tasks": ["11.1", "11.2", "11.3", "11.4", "11.5"] },
    { "id": 10, "tasks": ["12.1", "12.2", "12.3"] }
  ]
}
```
