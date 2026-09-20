package lock

import "errors"

var (
	// ErrLockedByOtherPR is returned when an operation targets a Lock held
	// by a PR other than the caller's.
	ErrLockedByOtherPR = errors.New("lock: held by a different PR")

	// ErrNoLock is returned when an operation requires an existing Lock but
	// none exists for the given project key.
	ErrNoLock = errors.New("lock: no lock exists for this project")

	// ErrNoPlan is returned by GetPlan when the Lock exists and is held by
	// the caller, but no plan has been recorded on it yet.
	//
	// Named for the plan rather than for its data because the condition
	// changed: it once meant "the stored bytes are empty", which wrongly
	// described a Plugin whose plan produces no artifact as having no
	// plan at all.
	ErrNoPlan = errors.New("lock: no plan recorded for this lock")
)
