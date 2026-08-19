package plugin

import (
	"fmt"
	"strings"
)

// UnsupportedOperationError is returned when Execute is called with an
// operation the Plugin does not support.
type UnsupportedOperationError struct {
	Plugin    string   // e.g. "helmfile"
	Operation string   // the invalid operation requested
	Supported []string // GetOperations()'s result, for the error message
}

func (e *UnsupportedOperationError) Error() string {
	return fmt.Sprintf(
		"plugin %q: unsupported operation %q, must be one of [%s]",
		e.Plugin, e.Operation, strings.Join(e.Supported, ", "),
	)
}
