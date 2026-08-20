package lock

import (
	"context"
	"time"

	"github.com/ivanvc/turnip/internal/plugin"
)

// LockData is the JSON-serialized value stored in Redis/Valkey per Lock.
type LockData struct {
	PRNumber       int                  `json:"pr_number"`
	PullRequestURL string               `json:"pull_request_url"`
	LockedAt       time.Time            `json:"locked_at"`
	LockedBy       string               `json:"locked_by"`
	PlanData       []byte               `json:"plan_data,omitempty"`
	PlanSummary    plugin.ChangeSummary `json:"plan_summary,omitzero"`
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

// LockManager prevents concurrent operations on the same Project and
// carries plan data from a plan operation through to its apply.
type LockManager interface {
	// AcquireLock attempts to acquire a lock for projectKey on behalf of
	// prNumber. Succeeds (true) if no lock exists, or if the existing lock
	// is already held by prNumber (idempotent re-acquire, e.g. after a
	// failed plan — existing plan data, if any, is left untouched).
	// Returns false only when a different PR holds the lock.
	AcquireLock(ctx context.Context, projectKey string, prNumber int, pullRequestURL, lockedBy string) (bool, error)

	// StorePlanData attaches plan output to a lock already held by prNumber.
	// Returns an error if the lock isn't held by prNumber (including "no
	// lock at all").
	StorePlanData(ctx context.Context, projectKey string, prNumber int, planData []byte, summary plugin.ChangeSummary) error

	// GetPlanData retrieves plan data from a lock held by prNumber.
	// Returns an error if the lock isn't held by prNumber, or if the lock
	// has no plan data stored yet.
	GetPlanData(ctx context.Context, projectKey string, prNumber int) ([]byte, plugin.ChangeSummary, error)

	// ReleaseLock releases projectKey's lock. No-op (nil error) if no lock
	// exists. Returns an error if the lock is held by a different PR.
	ReleaseLock(ctx context.Context, projectKey string, prNumber int) error

	// GetLockStatus reports whether projectKey is locked and by whom.
	GetLockStatus(ctx context.Context, projectKey string) (*LockStatus, error)

	// IsLockedByPR reports whether projectKey's lock (if any) is held by
	// exactly prNumber.
	IsLockedByPR(ctx context.Context, projectKey string, prNumber int) (bool, error)
}
