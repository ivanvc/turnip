package lock

import (
	"context"
	"time"

	"github.com/ivanvc/turnip/internal/plugin"
)

// LockState is the lifecycle position of a *held* Lock. The absence of a
// Lock has no LockState: it is the absence of the Redis key, which is what
// the acquire and compare-and-mutate scripts key their atomicity off.
type LockState string

const (
	// StatePlanning means the Lock is held and no successful plan has been
	// recorded against it. Nothing has been established, so releasing it
	// takes nothing from anyone.
	StatePlanning LockState = "planning"

	// StatePlanReady means the recorded plan is believed to describe the
	// delta between current infrastructure and the desired state at the
	// current head commit. It is the only state that admits a mutating
	// Operation.
	StatePlanReady LockState = "plan_ready"

	// StatePlanStale means a plan was recorded and something has since
	// invalidated it — a new commit, a failed re-plan, or a mutating
	// Operation that failed or timed out. The Lock is still held; the plan
	// cannot be applied.
	StatePlanStale LockState = "plan_stale"
)

// LockData is the JSON-serialized value stored in Redis/Valkey per Lock.
type LockData struct {
	PRNumber       int       `json:"pr_number"`
	PullRequestURL string    `json:"pull_request_url"`
	LockedAt       time.Time `json:"locked_at"`
	LockedBy       string    `json:"locked_by"`

	// State is the Lock's lifecycle position. Empty means the value was
	// written before states existed; DecodedState is what interprets that,
	// and no reader should consult this field directly.
	//
	// It replaced a has_plan boolean, which could say that a plan had been
	// recorded but never whether it was still true — the distinction a
	// push, a failed apply and a timed-out apply all turn on.
	State LockState `json:"state,omitempty"`

	// PlanArgs is the scope the plan ran with. Where Terraform stores a
	// plan file, Helmfile has only the arguments that selected what the
	// diff examined, so those arguments are the artifact.
	PlanArgs []string `json:"plan_args,omitempty"`

	PlanData    []byte               `json:"plan_data,omitempty"`
	PlanSummary plugin.ChangeSummary `json:"plan_summary,omitzero"`
}

// DecodedState reports the Lock's state, tolerating a value this build
// does not recognize — including one written before the field existed.
//
// Unrecognized decodes to StatePlanStale rather than StatePlanning, which
// is the conservative answer on both axes that matter. It is not
// StatePlanReady, so nothing unreviewed is applied on the strength of a
// plan whose validity this build cannot vouch for. And it is not
// StatePlanning, which would license releasing the Lock when a plan fails
// — evicting a pull request that may well have established work this
// build simply cannot see.
//
// Deliberately not derived from HasPlan: that field records that a plan
// ran, never that it is still true, so reading it here would promote
// exactly the Locks this slice exists to withdraw.
func (d LockData) DecodedState() LockState {
	switch d.State {
	case StatePlanning, StatePlanReady, StatePlanStale:
		return d.State
	default:
		return StatePlanStale
	}
}

// PlanRecord is everything a plan leaves behind for a mutating Operation
// to replay. Data is optional — a Plugin whose plan produces no artifact
// (Helmfile) stores an empty slice, and the Lock's StatePlanReady rather
// than len(Data) is what reports that a usable plan exists.
type PlanRecord struct {
	Data    []byte
	Args    []string
	Summary plugin.ChangeSummary
}

// LockStatus is GetLockStatus's return value.
//
// State has two readers, and they must agree: admission decides whether a
// mutating Operation may run, and selection decides which Projects a bare
// one targets. Splitting those conditions is how a bare apply comes to
// gather Projects it then refuses one at a time.
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
	PlanSummary    plugin.ChangeSummary

	// State is the Lock's lifecycle position, already interpreted through
	// DecodedState, so a caller never has to handle an unrecognized value.
	State LockState
}

// LockManager prevents concurrent operations on the same Project and
// carries plan data from a plan operation through to its apply.
type LockManager interface {
	// AcquireForPlan acquires the Lock for a plan, or confirms prNumber
	// already holds it, applying EventPlanDispatched in the same step.
	//
	// Returns acquired=false only when a different pull request holds the
	// Lock, in which case no state changes. The Transition reports what
	// the dispatch did — notably StatePlanReady to StatePlanStale, which
	// is how a stored plan is superseded at the push rather than minutes
	// later when the Runner reports.
	AcquireForPlan(ctx context.Context, projectKey string, prNumber int, pullRequestURL, lockedBy string) (bool, Transition, error)

	// Apply moves the Lock according to the transition table and reports
	// what it did, so the caller can say so on the pull request.
	//
	// plan is recorded when the edge keeps the Lock and a plan is given;
	// it is ignored otherwise. An event for a Lock that no longer exists
	// is a no-op with a nil error, not a failure: closing a pull request
	// mid-apply deletes the Lock, and the Operation's result still has to
	// reach the reader.
	Apply(ctx context.Context, projectKey string, prNumber int, ev Event, plan *PlanRecord) (Transition, error)

	// GetPlan retrieves the plan record from a lock held by prNumber.
	// Returns an error if the lock isn't held by prNumber, or if no plan
	// has been recorded on it yet — which is a different condition from
	// the record's Data being empty, since a Plugin may legitimately
	// record a plan with no artifact.
	GetPlan(ctx context.Context, projectKey string, prNumber int) (PlanRecord, error)

	// GetLockStatus reports whether projectKey is locked and by whom.
	GetLockStatus(ctx context.Context, projectKey string) (*LockStatus, error)

	// IsLockedByPR reports whether projectKey's lock (if any) is held by
	// exactly prNumber.
	IsLockedByPR(ctx context.Context, projectKey string, prNumber int) (bool, error)
}
