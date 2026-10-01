# Requirements Document: Structured Logging (Slice 8)

## Introduction

This slice replaces every unstructured `log.Printf`/`fmt.Fprintf` call
site in the Server and Runner with `log/slog`-based structured, leveled
logging.

This is one of four slices that together replace what was originally
scoped as a single, too-broad "HA, Observability & Deployment" roadmap
entry: **8. Structured Logging** (this slice) → **9. Metrics & Health
Endpoints** (`metrics`) → **10. Deployment: Kustomize & Release Images**
(`deployment-kustomize`) → **11. HA Validation & Documentation**
(`ha-validation`). This slice has no dependency on the other three —
logging is orthogonal to what it logs about — and per `roadmap.md`'s
dependency column depends only on Slice 6 (Server Orchestration). It
implements no specific numbered global requirement (the global spec has
no EARS acceptance criteria for logging format); it's a non-functional,
production-readiness concern the global `design.md`'s Testing Strategy
and Error Handling sections assume exists without specifying its shape.

## Glossary

(Inherited from the global spec glossary. No new terms.)

## Requirements

### Requirement 1: Structured Logging

**User Story:** As a platform operator, I want structured, leveled logs
from the Server and Runner, so that log aggregation and troubleshooting
in production don't depend on parsing free-form sentences.

#### Acceptance Criteria

1. THE Server and Runner SHALL emit structured (JSON) log records for
   every log line they currently produce via `log.Printf`/`fmt.Fprintf`
   (`internal/orchestrator`'s many `log.Printf` call sites across
   `comment.go`, `comments.go`, `execute.go`, `pullrequest.go`,
   `result.go`, `sweep.go`; `cmd/server/main.go`'s and
   `cmd/runner/main.go`'s startup-error `fmt.Fprintf`s), using Go's
   standard `log/slog` package — no new third-party logging dependency
2. Every log record SHALL include a level (`debug`/`info`/`warn`/
   `error`), a timestamp, a message, and the contextual values already
   available at that call site (operation ID, project key, PR number,
   owner/repo, etc.) as structured fields, not string-interpolated into
   the message
3. THE Server and Runner SHALL support a `TURNIP_LOG_LEVEL` environment
   variable selecting the minimum emitted level, defaulting to `info`
   when unset or unrecognized
4. WHERE a log call currently embeds an `error` value in a formatted
   string, THE replacement `slog` call SHALL pass it as a structured
   field (e.g. `slog.Any("error", err)`), not interpolate its `Error()`
   text into the message
5. This Requirement SHALL NOT change which events are logged or at what
   point in the flow they're logged — it is a format/structure change to
   existing log sites, not new observability call sites (the `metrics`
   slice covers new signals, as metrics rather than logs)

### Requirement 2: A Healthy Server Is Not a Silent One (amendment)

**User Story:** As a platform operator, I want the Server to log what it
is doing at `info`, and every failure it answers with, so that its pod's
log is enough to follow a pull request through turnip without first
lowering the level or reading metrics.

Requirement 1.5 kept this slice to the call sites that already existed,
and every one of those was an error path. The consequence, found in
production debugging: at the default `info` level a working Server
logged nothing at all, and several failures were answered without a log
line either (a webhook handler error answered 500, a signature failure
401, an infrastructure failure turned into a PR-comment-only rejection).
This requirement supersedes 1.5 for the events listed below.

#### Acceptance Criteria

1. WHEN the Server starts serving, THE Server SHALL log at `info` the
   HTTP and gRPC addresses it listens on and the effective log level;
   WHEN it has shut down cleanly, it SHALL log that at `info`
2. WHEN the webhook handler answers a delivery with a non-2xx status,
   THE Server SHALL log why — `warn` for a failed signature or an
   unparseable payload, `error` for a handler error — with the
   delivery's `X-GitHub-Delivery` ID
3. WHEN a delivery is dispatched to the orchestrator, THE Server SHALL
   log at `info` its event type, action, repository, pull request
   number and delivery ID; a delivery skipped without dispatch SHALL be
   logged at `debug`
4. THE Server SHALL log at `info` when a Runner Job is created for an
   Operation (with the Operation ID and Job name), when an Operation's
   result is received (with its success and the Lock transition it
   caused), and when an Operation finishes (with its outcome)
5. WHEN an Operation is rejected before or instead of running, THE
   Server SHALL log the rejection reason at `warn` — the same reason the
   pull request comment shows
6. WHEN the sweep times out an Operation, THE Server SHALL log it at
   `warn`
7. WHEN a GitHub permission check fails with an error (as opposed to
   answering "no"), THE Server SHALL log that error at `error`; a
   trigger refused for lack of permission SHALL be logged at `info`
8. WHEN a Lock is released by an unlock command or a pull request
   closing, THE Server SHALL log it at `info`
9. Log records SHALL NOT contain tool output (plan text, diffs) — only
   identifiers, outcomes and error values. Tool output can carry
   secrets and belongs to the pull request comment

## Out of Scope

- Metrics, health/readiness endpoints, dashboards — `metrics` (Slice 9)
- Kustomize manifests, release images — `deployment-kustomize` (Slice 10)
- Multi-instance/load testing, documentation — `ha-validation` (Slice 11)
- A request-ID/trace-ID propagated through `context.Context` — every
  `slog` call site added here uses the `*Context` variant so this can be
  layered in later for free, but generating/propagating such an ID is
  not this slice's concern
