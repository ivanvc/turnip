# Requirements Document: Redis Lock Manager (Slice 3)

## Introduction

This slice delivers the `internal/lock` package: a Redis/Valkey-backed
`LockManager` that prevents concurrent operations on the same Project and
carries plan data from a plan operation through to its apply. It is
consumed by Slice 6 (Server Orchestration), which decides *when* to call
it (on plan start, on plan/apply success, on PR merge/close/manual-unlock
webhook), and by Slice 4 (GitHub Client), which formats lock-conflict and
unlock-confirmation messages into PR comments. This slice implements the
lock mechanism itself — acquisition, plan-data storage, release, and status
queries — against a real (or in-memory-fake) Redis/Valkey instance. It does
not talk to GitHub, gRPC, or Kubernetes.

This slice implements Requirements 7 and 20 from the global spec
(`.kiro/specs/multi-iac-automation-platform/requirements.md`), scoped to
the lock storage/retrieval library only.

## Glossary

(Inherited from the global spec glossary.)

- **Lock**: The Redis/Valkey-backed record that prevents concurrent
  operations on a Project and carries plan data from plan to apply.
- **Project Key**: An opaque, caller-supplied string identifying the
  Project a Lock applies to (e.g. `{repo_owner}/{repo_name}/{project_name}`).
  The `LockManager` does not construct or interpret this string — it is
  the caller's (Slice 6's) responsibility to make it unique per Project.
- **Lock Owner**: The PR number currently holding a Lock.

## Requirements

### Requirement 1: Lock acquisition

**User Story:** As a platform operator, I want a Project locked the moment
a plan operation starts, so that two PRs can never run concurrent
operations against the same Project.

#### Acceptance Criteria

1. THE LockManager SHALL provide `AcquireLock(ctx, projectKey, prNumber, pullRequestURL) (bool, error)`
2. IF no Lock exists for `projectKey`, THEN `AcquireLock` SHALL create one recording `prNumber`, `pullRequestURL`, and the current time, and SHALL return `true`
3. IF a Lock already exists for `projectKey` and is held by `prNumber` (the same PR retrying — e.g. after a failed plan), THEN `AcquireLock` SHALL succeed and return `true` without discarding any plan data already stored in the Lock
4. IF a Lock already exists for `projectKey` and is held by a different PR number, THEN `AcquireLock` SHALL return `false` and SHALL NOT modify the existing Lock
5. THE LockManager SHALL use an atomic Redis/Valkey operation for acquisition such that, of any number of simultaneous `AcquireLock` calls for the same `projectKey` from different PRs, exactly one succeeds — this SHALL hold even when calls originate from different Server processes (Requirement 19.6)
6. THE Lock SHALL be created without a TTL (Requirement 7.4) — it is released only via `ReleaseLock`, never by expiry

### Requirement 2: Plan data storage and retrieval

**User Story:** As a developer, I want an apply operation to use exactly
what was planned, so that there is no drift between what I reviewed and
what gets applied.

#### Acceptance Criteria

1. THE LockManager SHALL provide `StorePlanData(ctx, projectKey, prNumber, planData []byte, summary ChangeSummary) error`
2. `StorePlanData` SHALL succeed only when `projectKey`'s Lock is currently held by `prNumber`; otherwise it SHALL return an error without modifying the Lock
3. THE LockManager SHALL provide `GetPlanData(ctx, projectKey, prNumber) ([]byte, ChangeSummary, error)`
4. `GetPlanData` SHALL succeed only when `projectKey`'s Lock is currently held by `prNumber`; otherwise it SHALL return an error and no plan data
5. IF `projectKey` has no stored plan data (e.g. the Lock was acquired but the plan has not yet completed successfully), THEN `GetPlanData` SHALL return an error distinguishable from "wrong PR" (Requirement 7.5)

### Requirement 3: Lock release

**User Story:** As a developer, I want locks released automatically when
my PR is merged, closed, or an apply succeeds, and to be able to release
one manually, so that I don't have to fight stale locks.

#### Acceptance Criteria

1. THE LockManager SHALL provide `ReleaseLock(ctx, projectKey, prNumber) error`
2. `ReleaseLock` SHALL succeed when `projectKey`'s Lock is held by `prNumber`, removing the Lock entirely (including any stored plan data)
3. `ReleaseLock` SHALL succeed as a no-op when `projectKey` has no Lock at all (idempotent release)
4. IF `projectKey`'s Lock is held by a different PR than `prNumber`, THEN `ReleaseLock` SHALL return an error and SHALL NOT release the Lock
5. THE LockManager SHALL NOT itself decide *when* to release a Lock (on merge, close, successful apply, or manual unlock) — that orchestration belongs to the caller (Slice 6); this slice only implements the release primitive

### Requirement 4: Lock status queries

**User Story:** As a developer or platform operator, I want to check
whether a Project is locked and by whom, so that a "who holds this lock"
UI or comment can be built on top of it.

#### Acceptance Criteria

1. THE LockManager SHALL provide `GetLockStatus(ctx, projectKey) (*LockStatus, error)`
2. IF `projectKey` has no Lock, THEN `GetLockStatus` SHALL return a `LockStatus` with `Locked: false` and no error
3. IF `projectKey` has a Lock, THEN `GetLockStatus` SHALL return `Locked: true` along with the holding PR number, PR URL, lock time, and — when present — the stored plan's `ChangeSummary`
4. THE LockManager SHALL provide `IsLockedByPR(ctx, projectKey, prNumber) (bool, error)`, returning whether `projectKey`'s Lock (if any) is held by exactly `prNumber`

## Out of Scope

- Deciding *when* to call `AcquireLock`/`StorePlanData`/`ReleaseLock` during the plan/apply/webhook lifecycle — Slice 6 (Server Orchestration)
- Posting PR comments about lock conflicts, unlock confirmations, or lock status — Slice 4 (GitHub Client)
- Parsing the `/turnip unlock` PR comment trigger and authorizing who may issue it — Slice 4 (Comment Parser), Requirement 16
- Handling PR merged/closed webhook events that should trigger release — Slice 4/6 (webhook handling); this slice only exposes `ReleaseLock` for that code to call
- Redis/Valkey deployment, connection pooling configuration, and credentials management beyond constructing a client from a provided connection string/options — deployment concerns belong to Slice 8 (HA, Observability & Deployment)
- A lock UI or admin force-unlock endpoint — mentioned in the original monolithic global tasks.md as a nice-to-have but not in any numbered requirement; deferred indefinitely unless a future slice picks it up
