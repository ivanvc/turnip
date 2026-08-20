package lock

import "errors"

var (
	// ErrLockedByOtherPR is returned when an operation targets a Lock held
	// by a PR other than the caller's.
	ErrLockedByOtherPR = errors.New("lock: held by a different PR")

	// ErrNoLock is returned when an operation requires an existing Lock but
	// none exists for the given project key.
	ErrNoLock = errors.New("lock: no lock exists for this project")

	// ErrNoPlanData is returned by GetPlanData when the Lock exists and is
	// held by the caller, but no plan data has been stored yet.
	ErrNoPlanData = errors.New("lock: no plan data stored for this lock")
)
