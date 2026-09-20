package lock

import (
	"context"
	"time"

	"github.com/ivanvc/turnip/internal/plugin"
)

// LockData is the JSON-serialized value stored in Redis/Valkey per Lock.
type LockData struct {
	PRNumber       int       `json:"pr_number"`
	PullRequestURL string    `json:"pull_request_url"`
	LockedAt       time.Time `json:"locked_at"`
	LockedBy       string    `json:"locked_by"`

	// HasPlan records that a plan ran and its result was stored, which is
	// not the same question as whether PlanData holds bytes: Helmfile
	// produces none, so for it the byte slice is always empty even after a
	// successful plan. It deliberately carries no omitempty — it is the
	// only field here that survives the round trip when a plan ran with no
	// arguments, which is what distinguishes that from no plan at all.
	//
	// A Lock written before this field existed decodes as false, so its
	// pull request is asked to re-plan rather than having an unrecorded
	// scope replayed on its behalf.
	HasPlan bool `json:"has_plan"`

	// PlanArgs is the scope the plan ran with. Where Terraform stores a
	// plan file, Helmfile has only the arguments that selected what the
	// diff examined, so those arguments are the artifact.
	PlanArgs []string `json:"plan_args,omitempty"`

	PlanData    []byte               `json:"plan_data,omitempty"`
	PlanSummary plugin.ChangeSummary `json:"plan_summary,omitzero"`
}

// PlanRecord is everything a plan leaves behind for a mutating Operation
// to replay. Data is optional — a Plugin whose plan produces no artifact
// (Helmfile) stores an empty slice, and HasPlan on the Lock rather than
// len(Data) is what reports that a plan happened.
type PlanRecord struct {
	Data    []byte
	Args    []string
	Summary plugin.ChangeSummary
}

// LockStatus is GetLockStatus's return value.
//
// HasPlan has a reader: selection uses it to decide which Projects a bare
// mutating Operation targets — those whose Lock this pull request holds
// with a plan recorded (project-selection, Requirement 4.1). Locked and
// PRNumber are read alongside it.
//
// PlanSummary still has none. It is populated anyway because Requirement
// 4.3 specifies that a Lock's status reports the holding PR, its URL, the
// lock time, and — when present — the stored plan's ChangeSummary, for the
// lock-status surface Requirement 4's user story describes.
//
// Recorded here because a dead-code sweep will flag it again and the
// obvious move is deletion, which would quietly drop a specified
// contract. It is ahead of its consumer, not left behind by one.
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

	// StorePlan attaches a plan record to a lock already held by prNumber,
	// marking the lock as carrying a plan. Returns an error if the lock
	// isn't held by prNumber (including "no lock at all").
	StorePlan(ctx context.Context, projectKey string, prNumber int, plan PlanRecord) error

	// GetPlan retrieves the plan record from a lock held by prNumber.
	// Returns an error if the lock isn't held by prNumber, or if no plan
	// has been recorded on it yet — which is a different condition from
	// the record's Data being empty, since a Plugin may legitimately
	// record a plan with no artifact.
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
