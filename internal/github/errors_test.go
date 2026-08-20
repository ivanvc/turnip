package github

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMalformedTriggerError_Error(t *testing.T) {
	err := &MalformedTriggerError{Line: 3, Content: "/turnip"}
	assert.NotEmpty(t, err.Error())
	assert.ErrorIs(t, err, ErrMalformedTrigger)
}

func TestMalformedTriggerErrors_Error(t *testing.T) {
	errs := MalformedTriggerErrors{
		{Line: 1, Content: "/turnip"},
		{Line: 3, Content: "/terraform"},
	}
	assert.NotEmpty(t, errs.Error())
	assert.ErrorIs(t, errs, ErrMalformedTrigger)
}
