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

## Out of Scope

- Metrics, health/readiness endpoints, dashboards — `metrics` (Slice 9)
- Kustomize manifests, release images — `deployment-kustomize` (Slice 10)
- Multi-instance/load testing, documentation — `ha-validation` (Slice 11)
- A request-ID/trace-ID propagated through `context.Context` — every
  `slog` call site added here uses the `*Context` variant so this can be
  layered in later for free, but generating/propagating such an ID is
  not this slice's concern
