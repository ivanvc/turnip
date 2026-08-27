package orchestrator

import (
	"errors"
	"fmt"
)

// ErrUnmatchedProject is returned (wrapped, via UnmatchedProjectError)
// when a TriggerCommand names a Project that doesn't exist among its tool
// candidates (Requirement 4.3) — a whole-command error, reported as a
// standalone reply comment rather than folded into a per-Project
// rejection, since there is no Project row to attach it to.
var ErrUnmatchedProject = errors.New("orchestrator: named project not found")

// UnmatchedProjectError names the specific Project that didn't match.
type UnmatchedProjectError struct {
	Name string
}

func (e *UnmatchedProjectError) Error() string {
	return fmt.Sprintf("orchestrator: project %q not found in turnip.yaml", e.Name)
}

func (e *UnmatchedProjectError) Unwrap() error {
	return ErrUnmatchedProject
}

// ErrConfigMissing is returned by fetchConfig when turnip.yaml exists at
// neither the repository root nor .github/turnip.yaml (Requirement 1.2)
// — distinguishable from any other fetch failure (Requirement 1.3), which
// callers report with the underlying error's message included.
var ErrConfigMissing = errors.New("orchestrator: turnip.yaml not found at repository root or .github/turnip.yaml")
