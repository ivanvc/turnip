# Requirements Document: Metrics & Health Endpoints (Slice 9)

## Introduction

This slice gives the Server an HTTP observability surface: liveness/
readiness endpoints, Prometheus metrics, and a Grafana dashboard for
those metrics.

This is one of four slices splitting what was originally scoped as a
single, too-broad "HA, Observability & Deployment" roadmap entry: **8.
Structured Logging** (`structured-logging`) → **9. Metrics & Health
Endpoints** (this slice) → **10. Deployment: Kustomize & Release Images**
(`deployment-kustomize`) → **11. HA Validation & Documentation**
(`ha-validation`). Per `roadmap.md`'s dependency column, this slice
depends only on Slice 6 (Server Orchestration) — it does not depend on
`structured-logging`, though both touch `cmd/server/main.go`'s startup
sequence independently. **Slice 10 and Slice 11 depend on this one**:
Slice 10's Kustomize `readinessProbe`/`livenessProbe` wire to the
endpoints Requirement 1 below adds, and Slice 11's multi-instance test
harness polls `/readyz`.

This slice implements no specific numbered global requirement — it's a
non-functional, production-readiness concern the global `design.md`'s
"Testing Strategy" section assumes exists without specifying its shape.
(Global Requirement 19, High Availability Server Deployment, is
implemented by `ha-validation`, Slice 11 — this slice provides the
`/readyz` signal that validation exercises, but doesn't itself assert
the HA properties.)

## Glossary

(Inherited from the global spec glossary.)

- **Runner Job Start Latency**: The window between a Server creating a
  Runner Job and that Job's Runner successfully connecting over gRPC —
  the same window grpc-runner's Requirement 14.5 already times out at 5
  minutes if it's never reached.

## Requirements

### Requirement 1: Health and Readiness Endpoints

**User Story:** As a platform operator, I want the Server to expose
liveness and readiness over HTTP, so that Kubernetes (and a human) can
tell a starting, healthy, or degraded instance apart without reading logs.

#### Acceptance Criteria

1. THE Server SHALL expose an HTTP `GET /healthz` endpoint returning 200
   once its Redis and Kubernetes clients have been constructed at
   startup, independent of whether it can currently reach either — this
   is "the process is up," not "the process can serve a webhook"
2. THE Server SHALL expose an HTTP `GET /readyz` endpoint returning 200
   only when a live Redis `PING` currently succeeds, and a non-200 status
   otherwise
3. THE Server's HTTP handler SHALL route `/healthz`, `/readyz`, the
   metrics endpoint (Requirement 2), and the webhook path through an
   `http.ServeMux` (or equivalent), since `cmd/server/main.go` today
   wires `github.NewWebhookHandler` as the entire `http.Server.Handler`
   with no routing at all — every path currently reaches the webhook
   handler and is rejected by signature verification
4. Adding these endpoints SHALL NOT change the webhook path's own
   behavior, route, or signature-verification requirement

### Requirement 2: Prometheus Metrics

**User Story:** As a platform operator, I want numeric signals — rate,
error rate, latency, lock contention, Runner Job start latency — so an
on-call engineer can diagnose a production incident without reading logs.

#### Acceptance Criteria

1. THE Server SHALL expose a `GET /metrics` endpoint in Prometheus
   text-exposition format via `github.com/prometheus/client_golang`,
   routed through the `http.ServeMux` Requirement 1.3 introduces
2. THE Server SHALL emit counters for: webhook events received (labeled
   by event type and outcome), Operations dispatched (labeled by
   Project's tool and Operation), and lock acquisition attempts (labeled
   by outcome: acquired/rejected)
3. THE Server SHALL emit histograms for: webhook-to-Operation-result
   latency, and Runner Job Start Latency (the window Requirement 14.5 of
   grpc-runner already times out at 5 minutes if never reached)
4. THE Runner SHALL NOT expose its own `/metrics` endpoint — it is an
   ephemeral, one-shot process torn down after each Operation, so a
   scrape target that may not outlive a single Prometheus scrape interval
   adds no value; its outcome is already captured by the Server-side
   "Operations dispatched" counter's outcome label
5. Metric names and labels SHALL follow Prometheus naming conventions (a
   `turnip_` prefix, a `_total` suffix on counters, base units on
   histograms), so the exposition is usable without a custom relabeling
   config

### Requirement 3: Grafana Dashboard

**User Story:** As a platform operator, I want a ready-made dashboard for
the metrics Requirement 2 exposes, so getting observability running
doesn't require hand-building panels from scratch.

#### Acceptance Criteria

1. THE repository SHALL provide a Grafana dashboard JSON definition
   covering: webhook throughput and error rate, Operation dispatch rate
   by tool and outcome, lock acquisition rejection rate, Runner Job Start
   Latency, and Operation latency
2. THE dashboard SHALL be checked into the repository as versioned JSON —
   not hand-exported and pasted once — so it evolves alongside the
   metrics it visualizes
3. THE dashboard SHALL be provisionable via a Kubernetes ConfigMap
   labeled per Grafana's sidecar-based dashboard-discovery convention
   (`grafana_dashboard: "1"`); packaging that ConfigMap as an opt-in
   Kustomize component is `deployment-kustomize`'s concern (Slice 10) —
   this Requirement only obligates the dashboard JSON and its label
   convention to exist

## Out of Scope

- Structured logging — `structured-logging` (Slice 8)
- Kustomize manifests, the opt-in dashboard-provisioning component,
  release images — `deployment-kustomize` (Slice 10)
- Multi-instance/load testing that exercises these endpoints under real
  concurrency, and documentation — `ha-validation` (Slice 11)
- Runner-side metrics or tracing — Requirement 2.4 excludes this and
  explains why
