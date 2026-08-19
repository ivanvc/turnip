package config

import "testing"

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
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestValidationError_Error(t *testing.T) {
	err := &ValidationError{ProjectRef: "vpc", Field: "tool", Message: "unsupported tool"}
	want := "config: vpc: tool: unsupported tool"
	if got := err.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestValidationErrors_Error(t *testing.T) {
	errs := ValidationErrors{
		&ValidationError{ProjectRef: "vpc", Field: "name", Message: "name is required"},
		&ValidationError{ProjectRef: "rds", Field: "tool", Message: "tool is required"},
	}
	want := "config: vpc: name: name is required\nconfig: rds: tool: tool is required"
	if got := errs.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}
