# Implementation Plan: Metrics & Health Endpoints (Slice 9)

## Overview

This plan implements two new leaf packages (`internal/health`,
`internal/metrics`) per `design.md`, extends `internal/orchestrator/record.go`'s
`MarkStarted` CAS to report the started transition (Decision 2), instruments
`internal/github/webhook.go` and `internal/orchestrator/execute.go`/`result.go`
at the points design.md's instrumentation map names, rewires
`cmd/server/main.go` onto a real `*http.ServeMux` (Decision 1), and adds
`dashboards/turnip.json` (Requirement 3). `cmd/runner/main.go` is untouched.

`internal/health` and `internal/metrics` have no dependency on each other and
can be built in parallel; both are needed before the `main.go` rewiring
(task 5). The `record.go` signature change (task 3) must land before
`result.go`'s `HandleLog` (task 4.2) since that call site consumes the new
return values. `execute.go` (task 4.1) and `webhook.go` (task 4.3) only need
`internal/metrics` to exist (task 2) and are otherwise independent of each
other and of tasks 3/4.2.

## Tasks

- [x] 1. Add the `prometheus/client_golang` dependency
  - [x] 1.1 `go get github.com/prometheus/client_golang@latest` and `go mod tidy`
    - Verify with `go build ./...` (no source changes yet — this just
      updates `go.mod`/`go.sum`)
    - _Requirements: 2.1_

- [x] 2. Implement `internal/health` and `internal/metrics`
  - [x] 2.1 Create `internal/health/health.go`
    - `func Healthz() http.HandlerFunc`: always writes 200
    - `func Readyz(ping func(context.Context) error) http.HandlerFunc`:
      calls `ping(r.Context())`, writes 200 on nil error, 503 otherwise
    - _Requirements: 1.1, 1.2_
  - [x] 2.2 Create `internal/health/doc.go`
    - One-paragraph package doc (liveness/readiness HTTP handlers),
      matching `internal/lock/doc.go`'s style
  - [x] 2.3 Create `internal/health/health_test.go`
    - `Healthz`: `httptest.NewRecorder`, assert 200
    - `Readyz`: table over a fake `ping` func succeeding (200) and
      returning an error (503)
    - Use `assert`/`require` per repo convention
    - _Requirements: 1.1, 1.2_
  - [x] 2.4 Create `internal/metrics/metrics.go`
    - `promauto`-registered collectors against the default registry:
      `turnip_webhook_events_total` (Counter, labels `event_type`,
      `outcome`), `turnip_operations_dispatched_total` (Counter, labels
      `tool`, `operation`, `outcome`), `turnip_lock_attempts_total`
      (Counter, label `outcome`), `turnip_operation_duration_seconds`
      (Histogram, labels `tool`, `operation`),
      `turnip_runner_job_start_latency_seconds` (Histogram, no labels)
    - Façade funcs: `WebhookEvent(eventType, outcome string)`,
      `OperationDispatched(tool, operation, outcome string)`,
      `LockAttempt(outcome string)`,
      `ObserveOperationDuration(tool, operation string, d time.Duration)`,
      `ObserveJobStartLatency(d time.Duration)`
    - `func Handler() http.Handler` returning `promhttp.Handler()`
    - _Requirements: 2.1, 2.2, 2.3, 2.5_
  - [x] 2.5 Create `internal/metrics/doc.go`
    - One-paragraph package doc (Prometheus collectors and the `/metrics`
      handler), matching `internal/lock/doc.go`'s style
  - [x] 2.6 Create `internal/metrics/metrics_test.go`
    - Use `prometheus/client_golang/prometheus/testutil`'s
      `CollectAndCompare`/`ToFloat64` to assert each façade func moves its
      named collector by the right label combination
    - Assert `Handler()` serves Prometheus text exposition
      (`httptest.NewRecorder`, check `Content-Type` and that a known metric
      name appears in the body)
    - _Requirements: 2.1, 2.2, 2.3, 2.5_

- [x] 3. Checkpoint - Verify `internal/health` and `internal/metrics` compile and pass
  - `go build ./internal/health/... ./internal/metrics/...` and
    `go test ./internal/health/... ./internal/metrics/...`. Ask the user if
    questions arise.

- [x] 4. Extend `MarkStarted`'s CAS to report the started transition (Decision 2)
  - [x] 4.1 Amend `internal/orchestrator/record.go`
    - Change `markStartedScript` to the two-element-array-returning form in
      design.md's Decision 2 (`{-1, ''}` gone/finalized, `{1, created_at}`
      this call performed the flip, `{0, ''}` already started)
    - Change `MarkStarted`'s signature to
      `func (s *recordStore) MarkStarted(ctx context.Context, operationID string) (justStarted bool, createdAt time.Time, err error)`,
      parsing the Lua array reply and `data.created_at` (RFC3339, matching
      `OperationRecord.CreatedAt`'s `json:"created_at"` `time.Time`
      encoding) into `createdAt`
    - _Requirements: 2.3_
  - [x] 4.2 Update `internal/orchestrator/record_test.go` for the new signature
    - `TestRecordStore_MarkStarted_Idempotent`: first call asserts
      `justStarted == true` and `createdAt` equals the record's
      `CreatedAt` (within a small delta for JSON round-trip precision);
      second call asserts `justStarted == false`
    - `TestRecordStore_MarkStarted_MissingRecordIsNotAnError`: asserts
      `justStarted == false`, `err == nil`
    - Update the `TestRecordStore_ClaimForTimeout_NotClaimedWhenStarted`
      call site (line ~130) for the new return arity
    - _Requirements: 2.3_

- [x] 5. Instrument `HandleLog`, `executeOne`, and `ServeHTTP`
  - [x] 5.1 Amend `internal/orchestrator/result.go`'s `HandleLog`
    - Consume `MarkStarted`'s new return values; call
      `metrics.ObserveJobStartLatency(time.Since(createdAt))` only when
      `justStarted` is true, per design.md's `result.go` snippet
    - _Requirements: 2.3_
  - [x] 5.2 Update `internal/orchestrator/result_test.go`'s `TestHandleLog_MarksStarted`
    - Assert `turnip_runner_job_start_latency_seconds`'s sample count
      increments by exactly 1 across two `HandleLog` calls for the same
      operation ID (idempotency — Decision 2's "observed exactly once per
      Operation")
    - _Requirements: 2.3_
  - [x] 5.3 Amend `internal/orchestrator/execute.go`'s `executeOne`
    - Wrap the function body timing with `time.Now()` at entry and
      `metrics.ObserveOperationDuration(t.Project.Tool, t.Operation,
      time.Since(start))` via `defer` at every return (a named
      defer covering all paths, matching this codebase's existing
      single-`defer`-at-entry style elsewhere)
    - Call `metrics.LockAttempt("acquired")` / `metrics.LockAttempt("rejected")`
      at the `AcquireLock`/`IsLockedByPR` outcomes per design.md's
      instrumentation map
    - Call `metrics.OperationDispatched(t.Project.Tool, t.Operation, outcome)`
      at every return path (`"rejected"` for every `rejectedResult` path,
      `"success"`/`"failure"` from `waitResult.Success` on the final
      return)
    - _Requirements: 2.2_
  - [x] 5.4 Update `internal/orchestrator/execute_test.go`
    - Add assertions (via `testutil.ToFloat64`/`CollectAndCompare`,
      resetting or reading pre/post deltas around each test) that a
      representative rejected path (e.g. lock already held) increments
      `turnip_operations_dispatched_total{outcome="rejected"}` and
      `turnip_lock_attempts_total{outcome="rejected"}`, and that a
      successful path increments the `"acquired"`/`"success"` series and
      observes `turnip_operation_duration_seconds`
    - _Requirements: 2.2_
  - [x] 5.5 Amend `internal/github/webhook.go`'s `ServeHTTP`
    - Call `metrics.WebhookEvent(eventType, outcome)` at every return path:
      `outcome="rejected"` for signature-verification failure (`eventType`
      unknown at that point — use `"unknown"`) and unparseable payload,
      `outcome="skipped"` for an unsupported event type or a
      `dispatch=false` issue-comment, `outcome="dispatched"` paired with
      `"success"`/`"failure"` sub-cases collapsed into the existing
      `outcome` label per design.md's table (`dispatched` covers both;
      handler error still returns 500 as today) — match design.md's
      literal label set (`dispatched`/`skipped`/`rejected`) rather than
      inventing new outcome strings
    - _Requirements: 2.2_
  - [x] 5.6 Update `internal/github/webhook_test.go`
    - Assert representative paths increment
      `turnip_webhook_events_total` with the right `event_type`/`outcome`
      pair: bad signature, unsupported event type, non-PR issue comment,
      and a successful dispatch
    - _Requirements: 2.2_

- [x] 6. Checkpoint - Verify `internal/orchestrator` and `internal/github` compile and pass
  - `go build ./... ` and `go test ./internal/orchestrator/... ./internal/github/...`.
    Ask the user if questions arise.

- [x] 7. Rewire `cmd/server/main.go` onto `*http.ServeMux` (Decision 1)
  - [x] 7.1 Amend `run(cfg)` in `cmd/server/main.go`
    - Build `mux := http.NewServeMux()`, register `GET /healthz` →
      `health.Healthz()`, `GET /readyz` → `health.Readyz(pingRedis)` where
      `pingRedis` is the inline closure from design.md
      (`func(ctx context.Context) error { return redisClient.Ping(ctx).Err() }`),
      `GET /metrics` → `metrics.Handler()`, and `"/"` →
      `github.NewWebhookHandler(...)` (unchanged); set
      `httpServer.Handler = mux`
    - _Requirements: 1.1, 1.2, 1.3, 1.4, 2.1_

- [x] 8. Checkpoint - Verify the server builds and starts
  - `go build ./cmd/...`; if feasible in this environment, `go run
    ./cmd/server` briefly against a local Redis/kubeconfig to confirm
    `/healthz`, `/readyz`, and `/metrics` respond (skip live-run
    verification if no local Redis/kubeconfig is available in this
    session — compilation plus the unit tests from tasks 2-6 stand in).
    Ask the user if questions arise.

- [x] 9. Add the Grafana dashboard
  - [x] 9.1 Create `dashboards/turnip.json`
    - Five panels per design.md's Requirement 3 section: webhook
      throughput/error rate, Operation dispatch rate by tool/outcome, lock
      rejection rate, Runner Job Start Latency (`histogram_quantile(0.99,
      ...)`), Operation duration
    - Hand-authored JSON in standard Grafana dashboard-export shape (a
      real Grafana instance isn't available in this session to export
      from — write the JSON directly in the same shape a UI export
      produces: `panels[]` with `targets[].expr` PromQL, `title`,
      `type`), plus the top-level dashboard fields Grafana needs to import
      it (`title`, `uid`, `schemaVersion`, `panels`)
    - _Requirements: 3.1, 3.2_
  - [x] 9.2 Create the ConfigMap label convention reference
    - No separate file needed beyond the dashboard JSON itself — confirm
      `dashboards/turnip.json`'s existence and Requirement 3.3's note that
      packaging as a `grafana_dashboard: "1"`-labeled ConfigMap is
      `deployment-kustomize`'s (Slice 10's) concern, not this slice's
    - _Requirements: 3.3_

- [x] 10. Final checkpoint - Full verification
  - Ensure `go build ./...` succeeds, `go test -race ./...` passes
    repo-wide, `gofmt -l .` is empty, `go mod tidy` produces no further
    changes, and the real golangci-lint v2 passes (`go run
    github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest run
    ./...`, or `go vet ./...` + `gofmt -l .` as a fallback per
    `CLAUDE.md`). Confirm `internal/health` and `internal/metrics` meet the
    80% coverage target from design.md's Testing Strategy (`go test
    ./internal/health/... ./internal/metrics/... -coverprofile=/tmp/cover.out
    && go tool cover -func=/tmp/cover.out`). Ask the user if questions
    arise.

## Notes

- No Runner-side changes — Requirement 2.4 needs no code, only the absence
  of `internal/health`/`internal/metrics` wiring in `cmd/runner/main.go`.
- `MarkStarted`'s signature change (task 4) has exactly one caller
  (`result.go`'s `HandleLog`, task 5.1) — both land together so the tree
  never sits in a non-compiling state between them.
- Tests reading Prometheus collector state must account for
  `promauto`'s default-registry collectors being package-level singletons
  shared across every test in a `go test` binary run — assert deltas
  (before/after) rather than absolute values where a metric could
  plausibly be touched by more than one test in the package.

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1.1"] },
    { "id": 1, "tasks": ["2.1", "2.2", "2.3", "2.4", "2.5", "2.6"] },
    { "id": 2, "tasks": ["4.1", "4.2", "5.3", "5.4", "5.5", "5.6", "7.1"] },
    { "id": 3, "tasks": ["5.1", "5.2"] },
    { "id": 4, "tasks": ["9.1", "9.2"] }
  ]
}
```
