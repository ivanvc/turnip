# Implementation Plan: Show What Ran and With What Scope (Slice 33)

## Overview

Two halves that barely touch: the Runner writes a transcript into the
Operation's output, and the Server renders a scope marker and a
scope-aware footer. They share only `ProjectResult`.

**The fence hardening goes first**, before anything new is written into a
fenced block. The exposure it closes already exists for the tool's own
output, so it is worth doing on its own; doing it first also means the
transcript is never briefly unprotected content built from
trigger-supplied tokens.

**The checkpoint that matters is task 6.** Requirement 4.2 was written
believing `OnOutput` feeds the comment. It does not — it feeds the live
gRPC path the Server currently drops, while the comment is built from
`execCommand`'s captured buffers. A transcript that reaches only
`onOutput` would look correct in every unit test that asserts the callback
fired, and be absent from every comment. So the checkpoint asserts it in
`ExecuteResult.Output`, and the design records why.

The version has to travel before the provenance line can name it, which
is the only ordering constraint inside the Runner half.

## Tasks

- [x] 1. Harden the fence, for content that already exists
  - [x] 1.1 Neutralise fence terminators in rendered content
    - Applied in the renderer to whatever goes inside a fence, not at the
      seam — the seam does not know it is writing markdown
    - Covers the tool's own `Output`, which is interpolated with no
      escaping today (`comment.go:407`)
    - _Requirements: 6.2_
  - [x] 1.2 One fence for both outcomes — **reversed, deviation recorded**
    - **Not done, deliberately.** `fenceFor`'s existing doc gave a reason
      the design under-weighted: a failure's body is an error message, not
      a diff, so `diff` highlighting would colour any line starting with
      `-` as a deletion and redden an unrelated message.
    - The design argued the same hazard exists on success, where YAML can
      carry a column-0 `-`. True, but not symmetric: on success the
      content really *is* a diff, so the highlighting is right and a stray
      `-` is the cost of a mostly-correct choice. On failure it is wrong
      in general.
    - Requirement 6.3 asks that the annotation render *consistently*. It
      does: the `#` lines are present, legible and identifiable by their
      text in either fence. Only their tint differs, and buying that tint
      costs correctness on the failure body.
    - Pinned by `TestBuildConsolidatedComment_FailureKeepsAPlainFence`, so
      the split is now a decision with a test rather than an accident.
    - _Requirements: 6.3 (satisfied differently — see above)_

- [x] 2. Checkpoint - hardening changes nothing visible
  - `go build ./...`, `go test -race ./...` and golangci-lint pass.
  - A tool output containing a fence terminator now renders inside the
    block instead of escaping it. Nothing else about the comment moves.

- [x] 3. The tool's version reaches the Runner
  - [x] 3.1 Pass it from `BuildJob`
    - `build.go:101` already resolves it to build the image tag; the same
      string goes to the Runner as an environment variable
    - `internal/runner/config.go` reads it beside `Tool`
    - _Requirements: 3.3_
  - [x] 3.2 Carry it into the Plugin call
    - `ExecuteOptions` gains the tool name and version, so the seam can
      name them without asking the Plugin
    - _Requirements: 3.3_

- [x] 4. The transcript, written where the commands run
  - [x] 4.1 Emit at the seam
    - In `execCommand` (`command.go:68`), after `cmd.Start()` returns nil
      and before the scanners run
    - **Write to the captured stdout buffer and mirror to `onOutput`** —
      the buffer is what reaches the comment, the callback is what a
      future live view reads. One emission, both destinations
    - After `Start` succeeds, not before: a command that failed to start
      must not report a transcript for something that never ran
    - _Requirements: 3.1, 4.1, 4.2, 4.3_
  - [x] 4.2 The lines themselves
    - `# turnip · <project-dir> · <tool> <version>`, then
      `# <resolved argv>`, then a `# exit <code> · <duration>` trailer
    - `#` because a `diff` fence renders it as a muted comment and because
      `$` would imply a shell line that argv does not round-trip to
    - **Deviation**: every line carries one prefix, `# turnip · `, rather
      than the design's `# <tool> <args>` for the command line.
      `parseChangedReleases` counts a release as changed when any
      non-empty line follows its "Comparing release=" line, so the trailer
      alone would have made the **last** release of every diff look
      changed — silently inflating the count a reviewer reads. A shared
      prefix lets the parser skip turnip's lines; a bare `#` would not,
      because helm renders `# Source: ...` into the manifests a diff
      prints. Pinned by
      `TestParseChangedReleases_IgnoresTurnipsOwnAnnotations`.
    - Never `+` or `-`: those are the payload's, and an annotation so
      prefixed is counted by eye as part of the change
    - The Project directory is repository-relative, which the Runner's
      existing `stripWorkspacePath` already produces for anything headed
      to the Server
    - _Requirements: 3.2, 3.4, 3.5, 5.3, 6.1_

- [x] 5. Checkpoint - it reaches the comment, not just the live stream
  - `go build ./...`, `go test -race ./...` and golangci-lint pass.
  - **The assertion this slice turns on**: the transcript appears in
    `ExecuteResult.Output`, not merely in what `onOutput` was called with.
    A fake `commandRunner` cannot prove it — this needs the real
    `execCommand` against a real short-lived process, which
    `internal/plugin/command_test.go` already does.
  - The command line precedes the first line of tool output in the
    captured buffer. Emitting after the scanners start would usually look
    right and would race.

- [x] 6. Redact where the secrets are
  - [x] 6.1 Redact when composing the Runner's Output
    - `run.go` composes `Output: strip(result.Output)`; it gains redaction
      in the same expression, using `cfg.GitHubToken` and the `redact`
      helper already in `internal/runner/clone.go`
    - The transcript is inside `result.Output` by then, so one pass covers
      both it and the tool's own output
    - _Requirements: 5.2_
  - [x] 6.2 The environment is never recorded
    - The seam records argv and is given no access to the environment.
      Credentials reach the tool through the environment and mounted files
      — cloud credentials, kubeconfig, the ServiceAccount token — so this
      is the distinction that makes recording argv safe at all
    - Asserted by a test that the transcript contains no environment value
    - _Requirements: 5.1_

- [x] 7. Checkpoint - a token in tool output no longer escapes
  - `go build ./...`, `go test -race ./...` and golangci-lint pass.
  - Tested with a token appearing in the **tool's own output**, not only
    in argv. That is the gap this closes — `redact` has only ever been
    applied to clone errors — and a test that checks argv alone would pass
    while the real hazard remains.

- [x] 8. The scope marker
  - [x] 8.1 `ScopeArgs` on `ProjectResult`
    - Set from `rec.ExtraArgs` where `HandleResult` already builds the
      result, beside `Locked` and `LockNote`
    - A field rather than a parse: the summary line is built before the
      output is, and recovering turnip's annotation from a blob the tool
      also writes into would be a guess
    - _Requirements: 1.1_
  - [x] 8.2 Render it on the summary line
    - Per Project, because `ParseTriggers` stops collecting names at the
      first `-`-prefixed token (`parser.go:101`), so a trigger with
      arguments and no names applies them to every selected Project
    - Verbatim, never classified — Slice 20 put flag semantics out of
      scope and this slice does not reopen it
    - Truncated on the summary line when long, with the full text
      remaining inside the Project's section
    - Absent entirely when the Operation ran with no arguments
    - _Requirements: 1.2, 1.3, 1.4, 1.5_

- [x] 9. The footer describes a scoped apply
  - [x] 9.1 Scope-aware wording
    - Where any locked Project's recorded plan carries arguments, say that
      applying replays the recorded scope rather than implying
      whole-Project coverage
    - Unchanged where none do
    - _Requirements: 2.1, 2.3_
  - [x] 9.2 Prose, not a command
    - Never render a copy-pasteable apply carrying the arguments:
      `execute.go:99` refuses a mutating Operation that supplies its own,
      so such a footer would offer a command turnip rejects
    - _Requirements: 2.2_

- [x] 10. Checkpoint - both halves, in one comment
  - `go build ./...`, `go test -race ./...` and golangci-lint pass.
  - A scoped plan renders: the marker on the summary line, the transcript
    inside the fence, the lock note and next steps in the trailer, and a
    scope-aware footer — in that order, reading as one voice rather than
    four slices.

- [x] 11. Documentation
  - [x] 11.1 What the annotation lines mean, and what is recorded
    - That a plan's arguments are recorded and replayed by a later apply,
      and where the pull request reports that
    - That the transcript carries arguments but never the environment
    - _Requirements: 7.1, 7.2_

- [x] 12. Roadmap status
  - Slice 33 to Complete.

- [x] 13. Checkpoint - the slice is done
  - `go build ./...`, `go test -race ./...` and golangci-lint all pass.
  - Mutation checks: removing the transcript write, the redaction pass, or
    the fence neutralisation must each fail a specific test. Absence
    assertions come from fakes that record calls, never from output that
    happens to look unchanged.
  - The marker is per Project: a trigger with arguments and no names,
    resolving to two Projects, marks both.
