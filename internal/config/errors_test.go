package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseError_Error(t *testing.T) {
	tests := []struct {
		name string
		err  *ParseError
		want string
	}{
		{
			name: "with line and column",
			err:  &ParseError{Line: 4, Column: 2, Message: "bad indent"},
			want: "config: parse error at line 4, column 2: bad indent",
		},
		{
			name: "without line or column",
			err:  &ParseError{Message: "unknown error"},
			want: "config: parse error: unknown error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.err.Error())
		})
	}
}

func TestValidationError_Error(t *testing.T) {
	err := &ValidationError{ProjectRef: "vpc", Field: "uses", Message: "unsupported tool"}
	assert.Equal(t, "config: vpc: uses: unsupported tool", err.Error())
}

func TestValidationErrors_Error(t *testing.T) {
	errs := ValidationErrors{
		&ValidationError{ProjectRef: "vpc", Field: "name", Message: "name is required"},
		&ValidationError{ProjectRef: "rds", Field: "uses", Message: "uses is required"},
	}
	want := "config: vpc: name: name is required\nconfig: rds: uses: uses is required"
	assert.Equal(t, want, errs.Error())
}
