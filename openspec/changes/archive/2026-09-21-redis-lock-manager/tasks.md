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
  - Verified: `go build ./...` OK; `go vet ./internal/lock/...` clean; `gofmt -l internal/lock/` empty; `go mod tidy` stable (go.mod unchanged on a second run); `go test -race ./internal/lock/...` passes all 17 tests (13 unit, 4 property, ≥100 iterations each). The asdf-pinned `golangci-lint` (v1.64.8) can't parse this repo's v2 config, but the real v2 binary runs fine locally via `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest run ./...` (network access permitting) — this later caught two real `errcheck` findings here (`client.Close()`'s error return unchecked in two test files, fixed in the 2026-08 amendment task below) that `go vet`/`gofmt` alone had missed; re-verified 0 issues repo-wide after the fix.

- [x] 14. Migrate property tests from `gopter` to `pgregory.net/rapid` (2026-08 amendment)
  - [x] 14.1 Rewrite `internal/lock/property_test.go` against `rapid`
    - `gopter`'s last release (`v0.2.11`) is from April 2024 with no newer tag; `pgregory.net/rapid` is actively maintained (release history checked against the module proxy at amendment time) and became this repo's property-testing convention for all slices, not just this one — see the global design doc's Testing Strategy and `CLAUDE.md`
    - Replace the `testParameters()`/`gopter.NewProperties`/`prop.ForAll` pattern with `rapid.Check(t, func(t *rapid.T) {...})` per property function; `rapid.Check`'s default `checks` count (100) already satisfies the ≥100-iterations convention, so no parameters helper is needed
    - Replace `gen.Identifier()` with `rapid.StringMatching(identifierPattern)` (a package-level `const identifierPattern = "[a-zA-Z][a-zA-Z0-9]{0,15}"`), `gen.IntRange` with `rapid.IntRange`, `gen.SliceOf(gen.UInt8())`/`gen.SliceOfN(16, gen.UInt8())` with `rapid.SliceOf(rapid.Byte())`/`rapid.SliceOfN(rapid.Byte(), 16, 16)` (no `[]uint8`-to-`[]byte` `.Map()` conversion needed — `rapid.Byte()` already yields `[]byte` through `SliceOf`), and the `gen.Struct`-based `changeSummaryGen` with a plain `genChangeSummary(t *rapid.T) plugin.ChangeSummary` helper function
    - Property assertions moved from returning `bool` to calling `t.Fatalf` with a descriptive message on failure — more debuggable than gopter's boolean-only convention, and consistent with this codebase's existing unit-test assertion style
    - `newPropertyClient`'s signature changed from an ad hoc duck-typed interface (needed because gopter's property functions only close over the outer `*testing.T`) to `*rapid.T` directly, since `*rapid.T` is passed into each property closure and already satisfies `miniredis.Tester`'s `Fatalf`/`Cleanup`/`Logf` shape — this also means `miniredis`/`redis.Client` cleanup now runs after each of the 100 iterations rather than accumulating until the whole test function returns, per `rapid`'s per-check `Cleanup` semantics (verified against its `engine.go` source)
    - Verify `go test -race ./internal/lock/... -run TestProperty` passes all 4 properties
    - _Requirements: (maintenance amendment, no behavioral change — see the "Testing Strategy" section of design.md, which now names `rapid`)_

  - [x] 14.2 Update dependencies
    - Add `pgregory.net/rapid` as a direct dependency; `go mod tidy` removes `github.com/leanovate/gopter` once no package in the module imports it anymore (verified repo-wide, not just `internal/lock`, since `internal/config` and `internal/plugin` were migrated in the same amendment)
    - _Requirements: (dependency infrastructure, no direct requirement)_

  - [x] 14.3 Checkpoint - Full re-verification
    - Ensure `go build ./...`, `go vet ./internal/lock/...`, `gofmt -l internal/lock/`, and `go test -race ./internal/lock/...` (17 tests: 13 unit, 4 property) all pass, and `go mod tidy` is stable

- [x] 15. Fix real golangci-lint v2 findings (2026-08 amendment)
  - [x] 15.1 Run the actual v2 linter and fix what it found
    - The asdf-pinned `golangci-lint` (v1.64.8) can't parse this repo's `.golangci.yml` (`version: "2"`); running the real v2 binary via `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest run ./...` (network access permitting) is what CI actually runs, and had never been exercised locally against this slice before this amendment — `go vet`/`gofmt` alone are an approximation, not a substitute
    - Found two `errcheck` findings: `client.Close()`'s error return unchecked in `t.Cleanup(func() { client.Close() })`, in both `redis_test.go` and `property_test.go` (the latter introduced by task 14's `rapid` migration, which replaced the cleanup helper but kept the same unchecked call)
    - Fixed both by discarding the error explicitly: `t.Cleanup(func() { _ = client.Close() })`
    - Verify `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest run ./...` reports 0 issues repo-wide (not just `internal/lock` — this run also covers whatever other slices exist at amendment time)
    - _Requirements: (maintenance amendment, no behavioral change)_

- [x] 16. Adopt `testify` for unit test assertions (2026-08 amendment)
  - [x] 16.1 Rewrite `internal/lock`'s unit test files against `github.com/stretchr/testify`
    - Adopted repo-wide (see `CLAUDE.md`) to replace hand-rolled `if ... { t.Fatalf(...) }` checks with `require`/`assert`
    - `redis_test.go`: `require` where the original check was fatal, `assert` where it was non-fatal
    - `property_test.go`: `*rapid.T` satisfies testify's `TestingT` interface directly, so `rapid.Check` bodies use `require` the same way; the concurrent-goroutines property (36) keeps its single post-`wg.Wait()` assertion, now `require.EqualValues`
    - Verify `go test -race ./internal/lock/...` passes with unchanged behavior (17 tests: 13 unit, 4 property) and coverage
    - _Requirements: (maintenance amendment, no behavioral change)_

  - [x] 16.2 Update dependencies
    - Add `github.com/stretchr/testify` as a direct dependency
    - _Requirements: (dependency infrastructure, no direct requirement)_

  - [x] 16.3 Checkpoint - Full re-verification
    - Ensure `go build ./...`, `go vet ./internal/lock/...`, `gofmt -l internal/lock/`, `go test -race ./internal/lock/...`, and the real `golangci-lint` v2 (`go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest run ./...`) all pass

- [x] 17. Enable `testifylint` in `.golangci.yml` (2026-08 amendment)
  - [x] 17.1 Fix findings in `internal/lock`
    - `.golangci.yml` gained `testifylint` in `linters.enable` (repo-wide, not slice-specific — see `CLAUDE.md`)
    - `require-error`: every bare `assert.Error`/`assert.ErrorIs` in `redis_test.go` (where nothing meaningful follows that specific check) changed to `require` — this ended up being every remaining one in `TestConnectionFailurePropagates` and two in earlier tests, since the linter's rule fires per-statement, not just on the last one in a function
    - `go-require`: none found in this package (no `httptest` handlers here)
    - Verify `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest run ./...` reports 0 issues repo-wide
    - _Requirements: (maintenance amendment, no behavioral change)_

- [x] 18. Record a plan, not a byte slice (2026-09 `plan-scoped-apply` amendment)
  - `LockData` gains `HasPlan bool` — deliberately without `omitempty`,
    since it is the only field that survives the round trip when a plan
    ran with no arguments — and `PlanArgs []string`, the scope it ran with
  - `StorePlanData`/`GetPlanData` become `StorePlan`/`GetPlan`, taking and
    returning a `PlanRecord{Data, Args, Summary}`. A record absorbs what
    Slice 7's Terraform and Pulumi plans will want to add; a widening
    parameter list would pay the same churn every time
  - `ErrNoPlanData` becomes `ErrNoPlan`. The condition changed from "the
    stored bytes are empty" to "no plan was recorded", and the old name
    described a Plugin whose plan produces no artifact as having no plan
    at all
  - **The rule this replaces was wrong in three places, not two.** "Empty
    `PlanData` means no plan data" lived in `GetPlanData`, in the storage
    condition upstream, and — found only during implementation — in
    `GetLockStatus`, which derives `LockStatus.HasPlan`. All three now
    test the recorded fact; the Data Model and Edge Cases sections are
    amended to match
  - **The third one changes nothing observable today.** An earlier version
    of this entry claimed it decided whether a pull request comment offers
    an apply; that was wrong. `LockStatus.HasPlan` has no production
    reader — both `GetLockStatus` callers use only `Locked` and
    `PRNumber`. It is corrected so the field stops reporting something
    false, not because a reader was misled
  - A Lock written before this amendment decodes with `HasPlan` false, so
    its pull request is asked to re-plan rather than having an unrecorded
    scope replayed on its behalf. No backfill and no version field — the
    condition clears itself on the next plan
  - _Requirements: amends 2.5 and the "empty plan data" rule in the Data Model_

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

- [x] 16. The Lock's lifecycle became a state machine (Slice 18 amendment)
  - This slice's documented lifecycle — acquire on plan, release on a
    successful mutating Operation, manual unlock, or pull request close —
    is no longer the whole of it. A held Lock now records one of
    `planning`, `plan_ready` or `plan_stale`, and every lifecycle change
    goes through `AcquireForPlan` or `Apply` rather than through
    `AcquireLock`/`StorePlan`/`ReleaseLock`, which Slice 18 removed along
    with the `acquire` and `compare-and-mutate` scripts they used.
  - What changed behaviorally, from this slice's point of view: a plan
    that fails with nothing recorded releases its Lock; a plan that finds
    nothing to apply releases it when the tool is inert without changes;
    dispatching a plan invalidates a stored one; and a mutating Operation
    that fails or times out keeps the Lock but invalidates the plan.
  - `has_plan` on the stored value is gone. A Lock written before the
    state field decodes as `plan_stale` — not appliable, and not
    releasable on a failed plan either, which is the conservative answer
    on both axes.
  - _Requirements: (amendment — see `lock-release-rules` for the rules
    themselves; this entry exists so a reader of this slice is not left
    with a lifecycle that no longer matches the code)_
