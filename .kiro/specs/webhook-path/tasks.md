# Implementation Plan: Move the Webhook Off the Root Path (Slice 29)

## Overview

The behavioural change is one line. Almost all of the plan is about being
able to *prove* it, because `cmd/server` has no test files today and
Requirement 2 is entirely a claim about routing.

So the order is: build the seam first with no behaviour change, then write
the test while it still fails, then make it pass. The middle checkpoint is
the unusual one — it exists to confirm the test fails, and fails on the
rows that should fail. That is the same discipline Slice 21 reached by
mutating working code; here it is free, because the code does not exist
yet.

Two smaller ordering points. The `WebhookPath` constant arrives *with* the
test rather than before it, so it has a caller from the moment it exists —
Slice 21 added a field nothing assigned and `unused` reported it, which
cost a task and a correction. And the extraction in task 1 copies the
existing routes verbatim, including the catch-all, so its checkpoint means
exactly one thing: nothing moved.

Documentation lands after the code rather than alongside, because
Requirement 4.4's upgrade note describes a migration whose shape is only
settled once the route is real.

## Tasks

- [x] 1. Extract a mux builder, changing nothing
  - [x] 1.1 Move route construction out of `run` into `newMux`
    - `func newMux(webhook http.Handler, ready func(context.Context) error) *http.ServeMux`
      in `cmd/server/main.go`, per Decision 4. `run` calls it and keeps
      using the returned mux for `httpServer.Handler`
    - Copy all four route registrations **verbatim**, catch-all included.
      This task must not change behaviour; the next one exists to say so
    - Parameters stay narrow deliberately: `health.Healthz()`,
      `metrics.Handler()` and `health.Readyz(ready)` are constructed
      inside, so `newMux` needs no Config, Orchestrator or clients — which
      is what lets a test call it without Redis or Kubernetes
    - _Requirements: 1.4_

- [x] 2. Checkpoint - the seam exists and nothing moved
  - `go build ./...` and `go test -race ./...` pass. There is nothing to
    assert about routing yet; what this proves is that extraction did not
    disturb the server's construction. A failure here is a wiring mistake
    in `run`, not a routing problem.

- [x] 3. The constant and the routing test, while it still fails
  - [x] 3.1 Add the published path as a constant
    - `const WebhookPath = "/github/webhook"` in
      `internal/github/webhook.go`, with the doc comment from Decision 3
      recording that it is *published* — an operator types it into GitHub
      App settings — which is why it is defined once rather than spelled
      at a call site
    - Forge-namespaced rather than `/webhook`, so a second forge or
      Slice 28's OAuth callback needs no further migration of a published
      URL
    - _Requirements: 1.3_
  - [x] 3.2 Write `cmd/server/main_test.go` covering the routing table
    - All seven rows of design.md's table, driven through `newMux` with
      `httptest.NewRecorder`
    - The webhook handler is a **stub that records whether it was
      called**. "Handler not reached" must be asserted from that record,
      not inferred from a 404 — a handler can return 404 itself, so status
      alone cannot distinguish "never routed" from "routed and refused"
    - This is what makes Requirement 2.3 testable at all: it is a claim
      about what did *not* happen
    - _Requirements: 1.1, 1.2, 1.4, 2.1, 2.2, 2.3_

- [x] 4. Checkpoint - the test fails, and for the right reasons
  - `go test ./cmd/server/` **fails**. Confirm the failures are the
    expected rows: `POST /github/webhook` not reaching the handler, and
    the unmatched paths reaching it instead of 404ing.
  - A row that passes here is a row testing nothing — the catch-all makes
    every path reach the handler, so any assertion that "handler was
    reached" would pass for the wrong reason. Check those rows in
    particular.
  - `go build ./...` still passes: the constant has a caller (the test),
    so `unused` has nothing to report.

- [x] 5. Move the route
  - [x] 5.1 Register the webhook at its own path and drop the catch-all
    - `mux.Handle("POST "+github.WebhookPath, …)` replaces
      `mux.Handle("/", …)` in `newMux`
    - Method-scoped per Decision 2, and this does real work: the handler
      performs no method check of its own —
      `internal/github/webhook.go:33` goes straight to
      `gh.ValidatePayload` — so without the method in the pattern a `GET`
      would be answered 401 rather than 405
    - No trailing slash: exact-path match only, so `/github/webhook/` does
      not quietly become a smaller catch-all
    - **Nothing is added to produce the 404.** Removing the catch-all is
      what produces it, via `ServeMux`'s own `NotFoundHandler`. Do not
      register a `/` handler to make the intent visible — Decision 1
      rejects that, because it reintroduces a catch-all in order to
      document the absence of one
    - _Requirements: 1.1, 1.2, 2.1, 2.2, 2.3, 3.1_

- [x] 6. Checkpoint - the routing table holds
  - `go test ./cmd/server/` passes, all seven rows.
  - `go build ./...`, `go test -race ./...`, and
    `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest run ./...`
    all pass. `internal/github`'s eight webhook tests construct the
    handler directly and never route, so they should be untouched — if one
    fails, something changed that this slice did not intend.

- [x] 7. Documentation
  - [x] 7.1 Rewrite the Webhook URL in `docs/configuration.md`
    - The current text asserts the root path emphatically — "the root path
      (`/`), not `/webhook` or any other sub-path" — so it must be
      rewritten, not adjusted. A sentence that anticipates the mistake is
      exactly the sentence a reader trusts
    - New value: `https://<your-hostname>/github/webhook`
    - _Requirements: 4.1, 4.2_
  - [x] 7.2 Update `docs/deployment.md` and add the upgrade note
    - Both places that instruct setting or verifying the Webhook URL
      (`:39` and `:165`). `:163`'s `/healthz` curl example is unaffected
    - Add the migration: an existing installation must update its GitHub
      App's Webhook URL, deliveries stop until it does, and **no ordering
      avoids a gap** — settings-first 404s until the deploy, deploy-first
      404s until the settings change
    - State the recovery, since an operator should not have to discover
      it: GitHub retains recent deliveries and offers **Redeliver** per
      delivery in the App's Advanced tab. Deploy, update the URL,
      redeliver what failed in between
    - _Requirements: 3.2, 4.3, 4.4_

- [x] 8. Roadmap status
  - Slice 29 to Complete in the slice table. No global requirement is
    amended — the global `requirements.md` and `design.md` contain no HTTP
    path literal between them — so there is no amendment to record, which
    is worth noting in the details block so a later reader does not go
    looking for one.
  - _Requirements: 1.1_

- [x] 9. Checkpoint - the slice is done
  - `go build ./...`, `go test -race ./...` and golangci-lint all pass.
  - No document still tells an operator to use the root path, and the
    upgrade note exists for the one installation that has to perform it.
  - The operator action itself — changing the GitHub App's Webhook URL —
    is **not** part of this checkpoint. It is the repository owner's to
    perform at deploy time, and the slice is complete without it.
