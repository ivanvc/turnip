package config

import (
	"errors"
	"strings"
	"testing"
)

func asValidationErrors(t *testing.T, err error) ValidationErrors {
	t.Helper()
	var verrs ValidationErrors
	if !errors.As(err, &verrs) {
		t.Fatalf("err = %v (%T), want ValidationErrors", err, err)
	}
	return verrs
}

func TestParse_MissingRequiredFields(t *testing.T) {
	tests := []struct {
		name  string
		yaml  string
		field string
	}{
		{
			name:  "missing directory",
			yaml:  "version: 1\nprojects:\n  - name: vpc\n    tool: terraform\n",
			field: "directory",
		},
		{
			name:  "missing tool",
			yaml:  "version: 1\nprojects:\n  - name: vpc\n    directory: infra/vpc\n",
			field: "tool",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.yaml))
			if err == nil {
				t.Fatal("Parse returned nil error, want a validation error")
			}
			verrs := asValidationErrors(t, err)
			found := false
			for _, v := range verrs {
				if v.Field == tt.field {
					found = true
				}
			}
			if !found {
				t.Errorf("no ValidationError for field %q in %v", tt.field, verrs)
			}
		})
	}
}

func TestParse_MissingNameAndDirectoryRefersToProjectByIndex(t *testing.T) {
	data := []byte("version: 1\nprojects:\n  - tool: terraform\n")

	_, err := Parse(data)
	if err == nil {
		t.Fatal("Parse returned nil error, want a validation error")
	}
	verrs := asValidationErrors(t, err)
	found := false
	for _, v := range verrs {
		if v.ProjectRef == "projects[0]" && v.Field == "directory" {
			found = true
		}
	}
	if !found {
		t.Errorf("no directory ValidationError referencing projects[0] found in %v", verrs)
	}
}

func TestParse_UnsupportedTool(t *testing.T) {
	data := []byte("version: 1\nprojects:\n  - name: vpc\n    directory: infra/vpc\n    tool: cloudformation\n")

	_, err := Parse(data)
	if err == nil {
		t.Fatal("Parse returned nil error, want a validation error")
	}
	verrs := asValidationErrors(t, err)
	if len(verrs) != 1 {
		t.Fatalf("len(verrs) = %d, want 1: %v", len(verrs), verrs)
	}
	msg := verrs[0].Message
	for _, tool := range []string{ToolTerraform, ToolPulumi, ToolHelmfile} {
		if !strings.Contains(msg, tool) {
			t.Errorf("message %q does not mention valid tool %q", msg, tool)
		}
	}
}

func TestParse_DuplicateProjectNames(t *testing.T) {
	data := []byte(`
version: 1
projects:
  - name: vpc
    directory: infra/vpc-a
    tool: terraform
  - name: vpc
    directory: infra/vpc-b
    tool: terraform
`)

	_, err := Parse(data)
	if err == nil {
		t.Fatal("Parse returned nil error, want a validation error")
	}
	verrs := asValidationErrors(t, err)
	found := false
	for _, v := range verrs {
		if strings.Contains(v.Message, "duplicate") {
			found = true
		}
	}
	if !found {
		t.Errorf("no duplicate-name ValidationError found in %v", verrs)
	}
}

func TestParse_MultipleSimultaneousViolations(t *testing.T) {
	data := []byte(`
version: 1
projects:
  - directory: infra/a
  - name: b
    directory: infra/b
    tool: terraform
  - name: b
    directory: infra/c
    tool: nope
`)

	_, err := Parse(data)
	if err == nil {
		t.Fatal("Parse returned nil error, want validation errors")
	}
	verrs := asValidationErrors(t, err)
	if len(verrs) < 3 {
		t.Fatalf("len(verrs) = %d, want at least 3 (missing tool, missing name, duplicate name, bad tool): %v", len(verrs), verrs)
	}
}

func TestParse_InvalidGlobPattern(t *testing.T) {
	data := []byte(`
version: 1
projects:
  - name: vpc
    directory: infra/vpc
    tool: terraform
    whenModified:
      - "infra/vpc/["
`)

	_, err := Parse(data)
	if err == nil {
		t.Fatal("Parse returned nil error, want a validation error")
	}
	verrs := asValidationErrors(t, err)
	found := false
	for _, v := range verrs {
		if strings.HasPrefix(v.Field, "whenModified") {
			found = true
		}
	}
	if !found {
		t.Errorf("no whenModified ValidationError found in %v", verrs)
	}
}
