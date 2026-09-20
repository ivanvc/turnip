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

// ErrMixedSelector is returned (wrapped, via MixedSelectorError) when a
// trigger combines "*" with Project names or patterns (Requirement 2.3).
// "*" already means every Project, so pairing it with a narrower selector
// asks two incompatible questions at once — refused rather than resolved
// to whichever reading turnip happens to implement.
var ErrMixedSelector = errors.New("orchestrator: \"*\" cannot be combined with other selectors")

// MixedSelectorError names the selectors that accompanied "*".
type MixedSelectorError struct {
	Others []string
}

func (e *MixedSelectorError) Error() string {
	return fmt.Sprintf("orchestrator: %q cannot be combined with %v", "*", e.Others)
}

func (e *MixedSelectorError) Unwrap() error {
	return ErrMixedSelector
}

// ErrNoModifiedProjects is returned (wrapped, via
// NoModifiedProjectsError) when a bare plan's Modified_Set is empty
// (Requirement 1.3). Reported once for the command rather than once per
// configured Project: the reader typed four words and is owed one answer.
var ErrNoModifiedProjects = errors.New("orchestrator: no project matched the pull request's changed files")

// NoModifiedProjectsError carries the trigger it answers, so the reply can
// quote the exact command that would target everything instead.
type NoModifiedProjectsError struct {
	Tool      string
	Operation string
}

func (e *NoModifiedProjectsError) Error() string {
	return fmt.Sprintf("orchestrator: no project matched the changed files for %q", e.Operation)
}

func (e *NoModifiedProjectsError) Unwrap() error {
	return ErrNoModifiedProjects
}

// ErrNoPlannedProjects is returned (wrapped, via NoPlannedProjectsError)
// when a bare mutating Operation finds no Project whose Lock this pull
// request holds with a plan recorded (Requirement 4.2).
//
// This is what replaces one refusal per configured Project: before it,
// applying the single planned Project among eight produced one apply and
// seven "no lock is held" rejections.
var ErrNoPlannedProjects = errors.New("orchestrator: this pull request holds no plan to apply")

// NoPlannedProjectsError carries the trigger it answers.
type NoPlannedProjectsError struct {
	Tool      string
	Operation string
}

func (e *NoPlannedProjectsError) Error() string {
	return fmt.Sprintf("orchestrator: no project has a plan from this pull request for %q", e.Operation)
}

func (e *NoPlannedProjectsError) Unwrap() error {
	return ErrNoPlannedProjects
}

// ErrConfigMissing is returned by fetchConfig when no turnip
// configuration exists at any accepted location (Requirement 1.2) —
// distinguishable from any other fetch failure (Requirement 1.3), which
// callers report with the underlying error's message included.
//
// The locations come from configFilePaths rather than being written out
// again here; see configFilePathList.
var ErrConfigMissing = fmt.Errorf("orchestrator: no turnip configuration found at %s", configFilePathList(""))
