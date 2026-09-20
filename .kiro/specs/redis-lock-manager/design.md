# Design Document: Redis Lock Manager (Slice 3)

## Overview

This slice implements `internal/lock`: a `LockManager` backed by
Redis/Valkey that prevents concurrent operations on the same Project and
carries plan data from a successful plan through to its apply. It has no
dependency on GitHub, gRPC, or Kubernetes — Slice 6 owns deciding *when*
to call it; Slice 4 owns turning its results into PR comments.

The package replaces the placeholder `internal/lock/doc.go` created in
Slice 0 with the real interface, types, and a Redis-backed implementation.

## Package Layout

```
internal/lock/
  lock.go        // LockManager interface, LockData, LockStatus
  redis.go       // RedisLockManager implementing LockManager
  scripts.go     // embedded Lua scripts for atomic acquire/mutate
  errors.go      // ErrLockedByOtherPR, ErrNoLock, ErrNoPlan
  doc.go         // updated package doc
```

## Dependencies

Two new dependencies:

- **`github.com/redis/go-redis/v9`** — the actively-maintained, official Go
  Redis client (confirmed current as of this session). Protocol-compatible
  with Valkey, so no separate Valkey client is needed.
- **`github.com/alicebob/miniredis/v2`** (test-only) — an in-process,
  Redis-protocol-compatible server, per the global design's explicit
  testing-strategy line ("Use miniredis for Redis testing"). Unit tests run
  a real `redis.Client` against a `miniredis` instance rather than a
  hand-rolled fake, so the Lua scripts below are exercised for real, not
  mocked around.

## Reconciling the interface sketch with the data model

The global design's `LockManager` sketch
(`multi-iac-automation-platform/design.md:369-390`) declares
`AcquireLock(ctx, projectKey, prNumber, pullRequestURL) (bool, error)` —
but the same doc's `LockData`/`LockStatus` structs
(lines 392-400, 596-604) both carry a `LockedBy string // Username who
triggered the plan` field that nothing in that signature can populate, and
no acceptance criterion in Requirement 7 mentions a username at all.
*Alternative considered*: drop `LockedBy` from this slice's types entirely,
since nothing requires it. Rejected because a lock-status comment/UI
showing "locked by PR #42" without saying who *triggered* it is a
regression from what the global data model clearly intends — this slice
adds `lockedBy string` as a fourth parameter to `AcquireLock`, extending
the sketch rather than truncating the data model.

**Property 10 mismatch**: the global design's plain-English Property 10
("*For any* operation... when the operation completes, the lock... should
be released", validating Requirement 7.3) directly contradicts Requirement
7.3/7.4, which say a **successful plan** leaves the Lock held with no TTL,
released only on successful apply, PR merge/close, or manual unlock.
Property 10 as literally written would mean a successful plan releases its
own lock immediately — defeating the entire plan-apply consistency
guarantee Requirement 7 exists for. This slice's testing strategy (below)
implements the property as Requirement 7.6 actually states it: release
follows a **successful apply**, not "any operation."

## Data Model

```go
package lock

import (
    "time"

    "github.com/ivanvc/turnip/internal/plugin"
)

// LockData is the JSON-serialized value stored in Redis/Valkey per Lock.
type LockData struct {
    PRNumber       int                  `json:"pr_number"`
    PullRequestURL string               `json:"pull_request_url"`
    LockedAt       time.Time            `json:"locked_at"`
    LockedBy       string               `json:"locked_by"`
    HasPlan        bool                 `json:"has_plan"`
    PlanData       []byte               `json:"plan_data,omitempty"`
    PlanArgs       []string             `json:"plan_args,omitempty"`
    PlanSummary    plugin.ChangeSummary `json:"plan_summary,omitzero"`
}

// PlanRecord is everything a plan leaves behind for a mutating Operation
// to replay. Data is optional — a Plugin whose plan produces no artifact
// stores an empty slice, and HasPlan on the Lock rather than len(Data) is
// what reports that a plan happened (Slice 20 amendment).
type PlanRecord struct {
    Data    []byte
    Args    []string
    Summary plugin.ChangeSummary
}

// LockStatus is GetLockStatus's return value.
type LockStatus struct {
    Locked         bool
    PRNumber       int
    PullRequestURL string
    LockedAt       time.Time
    LockedBy       string
    HasPlan        bool
    PlanSummary    plugin.ChangeSummary
}
```

`PlanSummary` reuses `plugin.ChangeSummary` (Slice 2) directly rather than
declaring a duplicate three-int struct. *Alternative considered*: a
lock-local `ChangeSummary` copy, keeping `internal/lock` fully independent
of `internal/plugin`. Rejected — `internal/plugin` has zero dependencies of
its own (a true leaf package, same tier as `internal/lock`), so importing
it costs nothing structurally, and it lets Slice 6 pass a
`plugin.ExecuteResult.ChangeSummary` straight into `StorePlan` with no
conversion step.

## API Surface

```go
package lock

type LockManager interface {
    // AcquireLock attempts to acquire a lock for projectKey on behalf of
    // prNumber. Succeeds (true) if no lock exists, or if the existing lock
    // is already held by prNumber (idempotent re-acquire, e.g. after a
    // failed plan — existing plan data, if any, is left untouched).
    // Returns false only when a different PR holds the lock.
    AcquireLock(ctx context.Context, projectKey string, prNumber int, pullRequestURL, lockedBy string) (bool, error)

    // StorePlan attaches a plan record to a lock already held by prNumber,
    // marking the lock as carrying a plan. Returns an error if the lock
    // isn't held by prNumber (including "no lock at all").
    StorePlan(ctx context.Context, projectKey string, prNumber int, plan PlanRecord) error

    // GetPlan retrieves the plan record from a lock held by prNumber.
    // Returns an error if the lock isn't held by prNumber, or if no plan
    // has been recorded on it yet — a different condition from the
    // record's Data being empty.
    GetPlan(ctx context.Context, projectKey string, prNumber int) (PlanRecord, error)

    // ReleaseLock releases projectKey's lock. No-op (nil error) if no lock
    // exists. Returns an error if the lock is held by a different PR.
    ReleaseLock(ctx context.Context, projectKey string, prNumber int) error

    // GetLockStatus reports whether projectKey is locked and by whom.
    GetLockStatus(ctx context.Context, projectKey string) (*LockStatus, error)

    // IsLockedByPR reports whether projectKey's lock (if any) is held by
    // exactly prNumber.
    IsLockedByPR(ctx context.Context, projectKey string, prNumber int) (bool, error)
}

func NewRedisLockManager(client *redis.Client) *RedisLockManager
```

## Redis Key Format

`lock:{projectKey}` — `projectKey` is treated as fully opaque (Requirement
"Project Key" glossary entry); `lock:` is a fixed namespace prefix this
package owns, not an interpretation of the caller's string. The value is
`LockData` marshaled as JSON (matching the global design's "Stored in Redis
as JSON without TTL").

## Atomicity via Lua Scripts

Property 36 (global design) requires that of any number of simultaneous
`AcquireLock` calls for the same key from *different* Server instances,
exactly one succeeds. A naive `GET`-then-check-then-`SET` from application
code is not atomic across multiple Server replicas (classic
check-then-act race). Both scripts below run atomically on the Redis/Valkey
server itself via `EVAL`, sidestepping the race entirely — this is the
standard pattern for compare-and-set-like logic against Redis.

**`acquireScript`** (used by `AcquireLock`):
```lua
-- KEYS[1] = "lock:{projectKey}"
-- ARGV[1] = requesting pr_number (string)
-- ARGV[2] = new LockData JSON (used only when creating)
local existing = redis.call('GET', KEYS[1])
if not existing then
    redis.call('SET', KEYS[1], ARGV[2])
    return 1
end
local data = cjson.decode(existing)
if tostring(data.pr_number) == ARGV[1] then
    return 1 -- idempotent re-acquire, existing value (incl. any plan data) untouched
end
return 0 -- held by a different PR
```

**`compareAndMutateScript`** (used by `StorePlan` and `ReleaseLock`):
```lua
-- KEYS[1] = "lock:{projectKey}"
-- ARGV[1] = expected pr_number (string)
-- ARGV[2] = "set" or "del"
-- ARGV[3] = new LockData JSON (only used when ARGV[2] == "set")
local existing = redis.call('GET', KEYS[1])
if not existing then
    return 0 -- not found
end
local data = cjson.decode(existing)
if tostring(data.pr_number) ~= ARGV[1] then
    return -1 -- held by a different PR
end
if ARGV[2] == 'del' then
    redis.call('DEL', KEYS[1])
else
    redis.call('SET', KEYS[1], ARGV[3])
end
return 1 -- ok
```

Return code handling:
- `StorePlan`/`GetPlan` (read via plain `GET`, no script needed for
  reads): `0` or `-1` from a mutate call both map to an error; a `GET` that
  decodes successfully but whose `HasPlan` is false maps to the
  "no plan recorded" error (Requirement 2.5). **Keyed off `HasPlan`, not
  `len(PlanData)`** — a Plugin whose plan produces no artifact still ran a
  plan, and inferring otherwise made its apply unreachable (Slice 20).
- `ReleaseLock`: `1` and `0` both map to success (Requirement 3.2/3.3,
  idempotent); `-1` maps to an error (Requirement 3.4).

`GetLockStatus`/`IsLockedByPR` are plain `GET`s — no mutation, no script
needed; a benign read racing a concurrent mutation is fine since callers
only use these for display/checks, never as the basis for a subsequent
mutation (that's what the atomic scripts are for).

## Errors

```go
var (
    ErrLockedByOtherPR = errors.New("lock: held by a different PR")
    ErrNoLock          = errors.New("lock: no lock exists for this project")
    ErrNoPlan          = errors.New("lock: no plan recorded for this lock")
)
```

`StorePlan`/`GetPlan`/`ReleaseLock` wrap `ErrLockedByOtherPR` (or
`ErrNoLock`, distinguishable via `errors.Is`) with the project key and PR
number for context; `GetPlan` returns `ErrNoPlan` specifically when the
lock exists, is held by the right PR, but no plan has been recorded on it
(Requirement 2.5's "distinguishable from wrong PR").

## Edge Cases

| Case | Behavior |
|---|---|
| `AcquireLock` called twice in a row by the same PR (e.g. duplicate webhook delivery) | Both succeed; second call is a no-op against the stored value |
| `AcquireLock` by PR B while PR A holds the lock | Returns `false`, PR A's lock (and any plan data) untouched |
| `StorePlan` called after the lock was released (e.g. by manual unlock racing a plan finishing) | Returns `ErrNoLock` |
| `GetPlan` called before any `StorePlan` (lock held, plan still running) | Returns `ErrNoPlan` |
| `GetPlan` on a Lock written before `has_plan` existed | Decodes `HasPlan` false, so returns `ErrNoPlan` — the pull request is asked to re-plan rather than replaying an unrecorded scope |
| `StorePlan` for a Plugin whose plan produced no artifact (Helmfile) | Succeeds, `HasPlan` true, `Data` empty — the fact of the plan is what is recorded |
| `ReleaseLock` called on a project with no lock | Returns `nil` (idempotent) |
| `GetLockStatus` on an unlocked project | `&LockStatus{Locked: false}`, `nil` error |
| Redis/Valkey connection failure mid-call | Propagated as a wrapped error from the underlying `go-redis` call; no partial state assumed |

## Testing Strategy

Per the global spec's dual testing approach and this package's coverage
target (95%, "Lock manager" per the global design doc's Testing Strategy,
"correctness-critical"):

- **Unit tests** run a real `*redis.Client` against a `miniredis.RunT(t)`
  instance (not a hand-rolled fake), covering every row of the edge-case
  table above plus: acquiring, storing, retrieving, releasing, and status
  checks in sequence; two-different-PR conflict; idempotent same-PR
  re-acquire preserving existing plan data.
- **Property tests** using `pgregory.net/rapid` (originally `gopter`;
  migrated 2026-08, see `tasks.md`), ≥100 iterations, tagged per the global
  convention:
  - `// Feature: multi-iac-automation-platform, Property 9: Lock Acquisition Prevents Concurrent Operations` — for a random project key and two distinct random PR numbers, if PR A acquires first, PR B's concurrent `AcquireLock` always fails.
  - `// Feature: multi-iac-automation-platform, Property 10: Lock Release After Operation Completion` — implemented as "lock release following a successful apply," per Requirement 7.6 (see "Property 10 mismatch" above, not the global doc's literal wording): for a random project key, PR that acquires, stores a plan, then calls `ReleaseLock` (modeling "apply succeeded"), `GetLockStatus` afterward reports `Locked: false`.
  - `// Feature: multi-iac-automation-platform, Property 11: Plan-Apply Lock Consistency` — for a random project key, PR, and random plan bytes, whatever `StorePlan` records is exactly what `GetPlan` returns. Since Slice 20 the property covers an **empty** artifact too: a plan that produced no bytes still round-trips as a recorded plan, which is the case that was previously indistinguishable from no plan at all.
  - `// Feature: multi-iac-automation-platform, Property 36: Lock Acquisition Across Instances` — simulate "multiple Server instances" as multiple goroutines sharing one `*redis.Client` (not one `LockManager` value, to avoid any accidental in-process serialization masking a real Redis-level race), all calling `AcquireLock` for the same project key with distinct PR numbers concurrently; assert exactly one succeeds.

No real Redis/Valkey server or Kubernetes/`kind` environment is used in
this slice's tests — `miniredis` is sufficient and matches the global
design's stated tool for this. Integration testing against a real
Redis/Valkey deployment belongs to the global roadmap's Slice 11.

## Backward Compatibility

N/A — new functionality with no prior consumers; `internal/lock` currently
contains only the Slice 0 placeholder `doc.go`, which this slice replaces
outright.
