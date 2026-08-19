package config

import (
	"fmt"
	"strings"
)

// ParseError wraps a YAML syntax error, preserving line/column information
// from the underlying decoder when available.
type ParseError struct {
	Line    int // 0 if unknown
	Column  int // 0 if unknown
	Message string
}

func (e *ParseError) Error() string {
	if e.Line == 0 && e.Column == 0 {
		return fmt.Sprintf("config: parse error: %s", e.Message)
	}
	return fmt.Sprintf("config: parse error at line %d, column %d: %s", e.Line, e.Column, e.Message)
}

// ValidationError describes one problem with one project.
type ValidationError struct {
	ProjectRef string // project name if known, else "projects[<index>]"
	Field      string // e.g. "tool", "name", "whenModified[1]"
	Message    string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("config: %s: %s: %s", e.ProjectRef, e.Field, e.Message)
}

// ValidationErrors aggregates every ValidationError found in a single Parse
// call (Requirement 2.5: don't stop at the first problem).
type ValidationErrors []*ValidationError

func (e ValidationErrors) Error() string {
	msgs := make([]string, len(e))
	for i, err := range e {
		msgs[i] = err.Error()
	}
	return strings.Join(msgs, "\n")
}
