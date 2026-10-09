# Tasks

**The checkpoint that matters is task 1.1's request test.** Removing
the field makes it impossible for turnip's code to set a check's text,
but only a test of the request the GitHub client actually sends shows
that no `output.text` reaches GitHub, whatever the output's size. It
runs against an `httptest` server, so it needs no GitHub.

Wave 0 removes the field and fixes the docs, so the orchestrator does
not build until wave 1 stops setting it; wave 2 then changes the note,
which touches the same files.

## 1. No output on a check (`internal/github`, `internal/orchestrator`)

- [x] 1.1 Remove `CheckRunOptions.Text` and its handling in
  `toCheckRunOutput`, and correct the comment calling `CheckRunOptions` a
  field-for-field mirror of the global design; verify `client_test.go`
  (output omitted only when title and summary are both empty) and a test
  of `UpdateCheckRun` against an `httptest` server whose request body has
  no `output.text`
- [x] 1.2 The result and start-timeout paths stop setting the text, and
  a failure with no counts is summarized `The operation failed.`; verify
  `result_test.go`, `sweep_test.go` and `images_test.go` assert no text
  and that the output still reaches the results comment, plus a test for
  the spec's scenario: a plan output of 160,406 characters completes its
  check and reaches the comment
  - _Deltas: project-checks (Check name and lifecycle, Change-count
    text)_

## 2. The note says what happened (`internal/orchestrator`)

- [x] 2.1 The note helper takes the failed action (create or complete)
  and renders the matching note from the spec; dispatch passes create,
  the result and start-timeout paths pass complete, and `refusalCheck`
  returns which call it made; verify tests of each note's exact text,
  including the refusal path's create and complete cases
  - _Deltas: project-checks (Check failures are reported, not fatal)_

## 3. Docs

- [x] 3.1 `docs/plugins/helmfile.md`, `docs/using/troubleshooting.md`,
  `docs/operating/troubleshooting.md` and `docs/using/pull-requests.md`
  send readers to the results comment for the output instead of the
  check's details, and the end-user troubleshooting page explains the
  "stays in progress" note (run the operation again for a new check);
  verify `make docs-check`

## 4. Final checkpoint

- [x] 4.1 `go build ./...`, `gofmt -l ./internal ./cmd` clean,
  `go vet ./...`, `make test`, `make lint` pass
- [x] 4.2 Every scenario in the delta maps to a test or a doc change;
  `openspec validate fix-check-output-size --strict` passes
- [x] 4.3 An intermittent failure is recorded with full output, not
  rerun away

## Repository owner

- Review and commit
- Deploy; a plan whose output is larger than 65,535 characters completes
  its check; then the change is archived

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1.1", "3.1"] },
    { "id": 1, "tasks": ["1.2"] },
    { "id": 2, "tasks": ["2.1"] },
    { "id": 3, "tasks": ["4.1", "4.2", "4.3"] }
  ]
}
```

## Deviations recorded at implementation

- The review found the end-user troubleshooting page promised that
  running the operation again replaces a check stuck in progress. That
  holds for a plan, not for a successful apply, whose Lock is released so
  a second apply is refused; the page now says so and points to
  requiring the `turnip` check instead.
- The review also caught an em dash pair kept in a rewritten comment in
  `internal/github/client.go`, and a test whose name and comment
  promised a title-only case it did not test; the test is renamed and a
  title-only case added.
