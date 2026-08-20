package github

import (
	"errors"
	"fmt"
	"strings"
)

var (
	// ErrFileNotFound is returned by GetFile when no file exists at the
	// requested ref.
	ErrFileNotFound = errors.New("github: file not found at ref")

	// ErrNoTrigger is returned by ParseTriggers when no line in the
	// comment body matches the trigger grammar at all.
	ErrNoTrigger = errors.New("github: no trigger command found in comment")

	// ErrMalformedTrigger is returned (wrapped, via MalformedTriggerErrors)
	// by ParseTriggers when a line starts with "/<token>" but has no
	// operation token following it.
	ErrMalformedTrigger = errors.New("github: trigger command missing an operation")
)

// MalformedTriggerError describes one malformed trigger line found by
// ParseTriggers.
type MalformedTriggerError struct {
	Line    int    // 1-indexed line number within the comment body
	Content string // the offending line, trimmed
}

func (e *MalformedTriggerError) Error() string {
	return fmt.Sprintf("github: line %d: trigger command missing an operation: %q", e.Line, e.Content)
}

func (e *MalformedTriggerError) Unwrap() error {
	return ErrMalformedTrigger
}

// MalformedTriggerErrors aggregates every malformed trigger line found in
// one ParseTriggers call, mirroring internal/config's ValidationErrors
// (accumulate every problem in one call, not just the first).
type MalformedTriggerErrors []*MalformedTriggerError

func (e MalformedTriggerErrors) Error() string {
	msgs := make([]string, len(e))
	for i, err := range e {
		msgs[i] = err.Error()
	}
	return strings.Join(msgs, "\n")
}

func (e MalformedTriggerErrors) Is(target error) bool {
	return target == ErrMalformedTrigger
}
