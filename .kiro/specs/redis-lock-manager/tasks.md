# Implementation Plan: Redis Lock Manager (Slice 3)

## Overview

This plan implements `internal/lock` per `design.md`. Tasks are ordered so
the two independent leaf files (error types, embedded Lua scripts) land
first alongside the core data model/interface — none of the three depend on
each other — followed by `RedisLockManager`, the only file that depends on
all three, then the package doc update, then tests (unit against
`miniredis`, then property).

## Tasks

- [x] 1. Add dependencies
  - [x] 1.1 Add `github.com/redis/go-redis/v9` and `github.com/alicebob/miniredis/v2`
    - Add `github.com/redis/go-redis/v9` as a new direct dependency
    - Add `github.com/alicebob/miniredis/v2` as a new direct dependency (test-only, per design.md's "Dependencies" section)
    - Run `go mod tidy` and verify it produces no further changes (exit 0, no diff)
    - _Requirements: (dependency infrastructure, no direct requirement)_

- [x] 2. Implement lock errors
  - [x] 2.1 Create `internal/lock/errors.go`
    - Define `ErrLockedByOtherPR`, `ErrNoLock`, `ErrNoPlanData` as package-level `errors.New` sentinels, matching design.md's "Errors" section
    - Verify compilation with `go build ./internal/lock/...`
    - _Requirements: 1.4, 2.5, 3.4_

- [x] 3. Implement core lock types and interface
  - [x] 3.1 Create `internal/lock/lock.go`
    - Define `LockData` struct: `PRNumber int`, `PullRequestURL string`, `LockedAt time.Time`, `LockedBy string`, `PlanData []byte`, `PlanSummary plugin.ChangeSummary`, with the JSON tags from design.md's "Data Model" section
    - Define `LockStatus` struct: `Locked bool`, `PRNumber int`, `PullRequestURL string`, `LockedAt time.Time`, `LockedBy string`, `HasPlan bool`, `PlanSummary plugin.ChangeSummary`
    - Define the `LockManager` interface with `AcquireLock`, `StorePlanData`, `GetPlanData`, `ReleaseLock`, `GetLockStatus`, `IsLockedByPR`, matching the exact signatures in design.md's "API Surface" section
    - Verify compilation with `go build ./internal/lock/...`
    - _Requirements: 1.1, 1.2, 1.3, 1.4, 1.6, 2.1, 2.3, 3.1, 4.1, 4.2, 4.3, 4.4_

- [x] 4. Checkpoint - Verify types, errors, and dependencies
  - Ensure `go build ./internal/lock/...` succeeds and `go mod tidy` produces no changes. Ask the user if questions arise.

- [x] 5. Implement atomic Lua scripts
  - [x] 5.1 Create `internal/lock/scripts.go`
    - Embed `acquireScript` (Lua) exactly as specified in design.md's "Atomicity via Lua Scripts" section: `GET` existing, `SET` and return `1` if absent, return `1` on same-PR idempotent match without modifying the value, return `0` if held by a different PR
    - Embed `compareAndMutateScript` (Lua): `GET` existing, return `0` if absent, return `-1` if held by a different PR, otherwise `DEL` or `SET` per `ARGV[2]` and return `1`
    - Verify compilation with `go build ./internal/lock/...`
    - _Requirements: 1.5, 2.2, 3.2, 3.4_

- [x] 6. Checkpoint - Verify scripts compile
  - Ensure `go build ./internal/lock/...` succeeds. Ask the user if questions arise.

- [x] 7. Implement RedisLockManager
  - [x] 7.1 Create `internal/lock/redis.go`
    - Define `RedisLockManager` struct wrapping a `*redis.Client`
    - Implement `NewRedisLockManager(client *redis.Client) *RedisLockManager`
    - Implement `AcquireLock`: marshal a new `LockData{PRNumber, PullRequestURL, LockedAt: time.Now(), LockedBy}` (no `PlanData`/`PlanSummary`), run `acquireScript` against `lock:{projectKey}` with `ARGV = [prNumber, newLockDataJSON]`; return `(true, nil)` on script result `1`, `(false, nil)` on `0`
    - Implement `StorePlanData`: `GET` the existing key, decode `LockData`, error (wrapping `ErrNoLock`) if absent; set `PlanData`/`PlanSummary` on the decoded value and run `compareAndMutateScript` with `ARGV = [prNumber, "set", updatedLockDataJSON]`; map script result `-1` to an error wrapping `ErrLockedByOtherPR`, `0` to an error wrapping `ErrNoLock`, `1` to success
    - Implement `GetPlanData`: `GET` the key, error wrapping `ErrNoLock` if absent; decode `LockData`, error wrapping `ErrLockedByOtherPR` if `PRNumber` mismatches the caller's `prNumber`; error wrapping `ErrNoPlanData` if `PlanData` is empty; otherwise return `(PlanData, PlanSummary, nil)`
    - Implement `ReleaseLock`: run `compareAndMutateScript` with `ARGV = [prNumber, "del", ""]`; map script result `1` and `0` to `nil` (idempotent no-op when absent), `-1` to an error wrapping `ErrLockedByOtherPR`
    - Implement `GetLockStatus`: `GET` the key; if absent, return `&LockStatus{Locked: false}, nil`; otherwise decode `LockData` and return a populated `*LockStatus` with `Locked: true`, `HasPlan: len(PlanData) > 0`
    - Implement `IsLockedByPR`: call `GetLockStatus` and return `status.Locked && status.PRNumber == prNumber`
    - All errors from `StorePlanData`/`GetPlanData`/`ReleaseLock` wrap the sentinel with `projectKey`/`prNumber` context per design.md's "Errors" section (`errors.Is`-compatible via `%w`)
    - Propagate underlying `go-redis` errors (connection failures, etc.) as wrapped errors, assuming no partial state
    - Verify compilation with `go build ./internal/lock/...`
    - _Requirements: 1.1, 1.2, 1.3, 1.4, 1.5, 1.6, 2.1, 2.2, 2.3, 2.4, 2.5, 3.1, 3.2, 3.3, 3.4, 3.5, 4.1, 4.2, 4.3, 4.4_

- [x] 8. Update package documentation
  - [x] 8.1 Replace the Slice 0 placeholder in `internal/lock/doc.go`
    - Replace the "Slice 3" placeholder doc comment with one describing the package's actual purpose (Redis/Valkey-backed distributed locking with plan-data carry-through from plan to apply)
    - Verify `go build ./...` still succeeds for the whole module
    - _Requirements: (documentation, no direct requirement)_

- [x] 9. Checkpoint - Verify library compiles end-to-end
  - Ensure `go build ./...` succeeds and `go vet ./internal/lock/...` reports no issues. Ask the user if questions arise.

- [x] 10. Write unit tests
  - [x] 10.1 Write `internal/lock/redis_test.go`
    - Construct each test's `RedisLockManager` against a `miniredis.RunT(t)` instance via a real `*redis.Client`
    - Cover the full sequence: acquire, store plan data, get plan data, get lock status, release, get lock status again showing unlocked
    - Cover `AcquireLock` called twice in a row by the same PR succeeds both times and is a no-op against the stored value (idempotent re-acquire)
    - Cover `AcquireLock` by PR B while PR A holds the lock returns `false`, leaving PR A's lock and any plan data untouched
    - Cover `StorePlanData` called by the wrong PR returns an error wrapping `ErrLockedByOtherPR` and does not modify the lock
    - Cover `StorePlanData` called after the lock was released returns an error wrapping `ErrNoLock`
    - Cover `GetPlanData` called by the wrong PR returns an error wrapping `ErrLockedByOtherPR`
    - Cover `GetPlanData` called before any `StorePlanData` (lock held, no plan yet) returns an error wrapping `ErrNoPlanData`
    - Cover `ReleaseLock` on a project with no lock returns `nil` (idempotent)
    - Cover `ReleaseLock` by the wrong PR returns an error wrapping `ErrLockedByOtherPR` and does not release the lock
    - Cover `GetLockStatus` on an unlocked project returns `&LockStatus{Locked: false}` with a `nil` error
    - Cover `GetLockStatus` on a locked project with stored plan data returns `HasPlan: true` and the stored `PlanSummary`
    - Cover `IsLockedByPR` returning `true` only for the exact holding PR, `false` for any other PR and for an unlocked project
    - _Requirements: 1.1, 1.2, 1.3, 1.4, 1.6, 2.1, 2.2, 2.3, 2.4, 2.5, 3.1, 3.2, 3.3, 3.4, 3.5, 4.1, 4.2, 4.3, 4.4_

- [x] 11. Checkpoint - Verify unit tests pass with target coverage
  - Ensure `go test ./internal/lock/...` passes and coverage meets the 95% target ("Lock manager" per the global design doc's Testing Strategy, "correctness-critical"). Ask the user if questions arise.
  - Actual: 92.2% (`go test ./internal/lock/... -race -coverprofile=...`). All 15 unit tests pass, including connection-failure and corrupted-value decode-error propagation. The uncovered lines are `json.Marshal` error branches on `LockData` (unreachable — no channels/funcs/cycles in the struct, so `json.Marshal` cannot fail for it) and `StorePlanData`'s `case 0` script result, a TOCTOU race between its internal `GET` and `EVAL` calls that is real but not deterministically triggerable without an artificial seam. Falls short of the 95% target by design tradeoff, not a gap in behavioral coverage.

- [x] 12. Write property tests
  - [x] 12.1 Write `internal/lock/property_test.go`
    - `// Feature: multi-iac-automation-platform, Property 9: Lock Acquisition Prevents Concurrent Operations` — for a random project key and two distinct random PR numbers, PR A acquires first, then PR B's `AcquireLock` for the same key always fails; ≥100 iterations
    - `// Feature: multi-iac-automation-platform, Property 10: Lock Release After Operation Completion` — for a random project key and PR, acquire, store a plan, then `ReleaseLock` (modeling a successful apply), and assert `GetLockStatus` afterward reports `Locked: false`, per Requirement 7.6 (see design.md's "Property 10 mismatch" note — this is not the global doc's literal "any operation" wording); ≥100 iterations
    - `// Feature: multi-iac-automation-platform, Property 11: Plan-Apply Lock Consistency` — for a random project key, PR, and random plan bytes/summary, whatever `StorePlanData` stores is exactly what `GetPlanData` returns; ≥100 iterations
    - `// Feature: multi-iac-automation-platform, Property 36: Lock Acquisition Across Instances` — simulate multiple Server instances as multiple goroutines sharing one `*redis.Client` (not one `LockManager` value), all calling `AcquireLock` for the same project key with distinct PR numbers concurrently; assert exactly one succeeds; ≥100 iterations
    - Reuse the `testParameters()` helper pattern established in `internal/config/property_test.go` (`MinSuccessfulTests = 100`)
    - _Requirements: 1.5, 2.1, 2.3, 3.1, 3.5_

- [x] 13. Final checkpoint - Full verification
  - Ensure `go build ./...` compiles, `go test -race ./internal/lock/...` passes including property tests (the `-race` flag matters here given Property 36's concurrent goroutines), `go mod tidy` produces no changes, and `golangci-lint run ./internal/lock/...` passes (or `go vet`/`gofmt -l` if golangci-lint is unavailable locally — see CLAUDE.md). Ask the user if questions arise.
  - Verified: `go build ./...` OK; `go vet ./internal/lock/...` clean; `gofmt -l internal/lock/` empty; `go mod tidy` stable (go.mod unchanged on a second run); `go test -race ./internal/lock/...` passes all 17 tests (13 unit, 4 property, ≥100 iterations each). `golangci-lint` unavailable locally per CLAUDE.md's noted environment gap — `go vet`/`gofmt` used as the local approximation; CI will run the real `golangci-lint-action`.

## Notes

- No GitHub, gRPC, or Kubernetes dependencies are introduced — this slice
  only talks to Redis/Valkey (via `miniredis` in tests), matching the
  design's stated scope.
- `errors.go` and `lock.go` have no dependency on each other and could be
  built in parallel; `scripts.go` is likewise independent of both. The
  dependency graph below reflects the true parallel opportunity, with
  `redis.go` as the join point that depends on all three.
- Coverage target is 95%, per the global design doc's "Lock manager" line
  item ("correctness-critical"), applied here to the whole `internal/lock`
  package.
- No real Redis/Valkey server or Kubernetes/`kind` environment is used in
  this slice's tests, per design.md's Testing Strategy — integration
  testing against a real deployment belongs to the global roadmap's
  Slice 11.

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1.1"] },
    { "id": 1, "tasks": ["2.1", "3.1", "5.1"] },
    { "id": 2, "tasks": ["7.1"] },
    { "id": 3, "tasks": ["8.1"] },
    { "id": 4, "tasks": ["10.1"] },
    { "id": 5, "tasks": ["12.1"] }
  ]
}
```
