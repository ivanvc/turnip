package plugin

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUnsupportedOperationError_Error(t *testing.T) {
	err := &UnsupportedOperationError{
		Plugin:    "helmfile",
		Operation: "plan",
		Supported: []string{"diff", "apply", "sync", "destroy"},
	}

	msg := err.Error()

	for _, want := range []string{"helmfile", "plan", "diff", "apply", "sync", "destroy"} {
		assert.Contains(t, msg, want)
	}
}
