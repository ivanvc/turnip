package plugin

import (
	"strings"
	"testing"
)

func TestUnsupportedOperationError_Error(t *testing.T) {
	err := &UnsupportedOperationError{
		Plugin:    "helmfile",
		Operation: "plan",
		Supported: []string{"diff", "apply", "sync", "destroy"},
	}

	msg := err.Error()

	for _, want := range []string{"helmfile", "plan", "diff", "apply", "sync", "destroy"} {
		if !strings.Contains(msg, want) {
			t.Errorf("Error() = %q, missing %q", msg, want)
		}
	}
}
