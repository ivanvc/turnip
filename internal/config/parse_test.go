package config

import (
	"errors"
	"testing"
)

func TestParse_ValidRoundTrip(t *testing.T) {
	data := []byte(`
version: 1
projects:
  - name: vpc
    directory: infra/vpc
    tool: terraform
    whenModified:
      - "infra/vpc/**/*.tf"
    config:
      workspace: prod
`)

	c, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}
	if c.Version != 1 {
		t.Errorf("Version = %d, want 1", c.Version)
	}
	if len(c.Projects) != 1 {
		t.Fatalf("len(Projects) = %d, want 1", len(c.Projects))
	}
	p := c.Projects[0]
	if p.Name != "vpc" || p.Directory != "infra/vpc" || p.Tool != ToolTerraform {
		t.Errorf("unexpected project: %+v", p)
	}
	if len(p.WhenModified) != 1 || p.WhenModified[0] != "infra/vpc/**/*.tf" {
		t.Errorf("unexpected WhenModified: %v", p.WhenModified)
	}
	if p.Config["workspace"] != "prod" {
		t.Errorf("unexpected Config: %v", p.Config)
	}
}

func TestParse_MalformedYAML(t *testing.T) {
	data := []byte("version: [1\n")

	c, err := Parse(data)
	if c != nil {
		t.Fatalf("Parse returned non-nil Config on malformed YAML: %+v", c)
	}
	var parseErr *ParseError
	if !errors.As(err, &parseErr) {
		t.Fatalf("Parse error = %v (%T), want *ParseError", err, err)
	}
	if parseErr.Line == 0 {
		t.Errorf("ParseError.Line = 0, want a non-zero line number")
	}
}

func TestParse_NameDefaultsToDirectory(t *testing.T) {
	data := []byte("version: 1\nprojects:\n  - directory: infra/vpc\n    tool: terraform\n")

	c, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}
	if got, want := c.Projects[0].Name, "infra/vpc"; got != want {
		t.Errorf("Name = %q, want %q", got, want)
	}
}

func TestParse_ExplicitNameNotOverridden(t *testing.T) {
	data := []byte("version: 1\nprojects:\n  - name: vpc\n    directory: infra/vpc\n    tool: terraform\n")

	c, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}
	if got, want := c.Projects[0].Name, "vpc"; got != want {
		t.Errorf("Name = %q, want %q", got, want)
	}
}

func TestParse_DefaultedNamesStillDetectDuplicates(t *testing.T) {
	data := []byte(`
version: 1
projects:
  - directory: infra/vpc
    tool: terraform
  - directory: infra/vpc
    tool: pulumi
`)

	_, err := Parse(data)
	if err == nil {
		t.Fatal("Parse returned nil error, want a validation error for duplicate defaulted names")
	}
	var verrs ValidationErrors
	if !errors.As(err, &verrs) {
		t.Fatalf("err = %v (%T), want ValidationErrors", err, err)
	}
	found := false
	for _, v := range verrs {
		if v.Field == "name" {
			found = true
		}
	}
	if !found {
		t.Errorf("no duplicate-name ValidationError found in %v", verrs)
	}
}

func TestParse_TypeMismatch(t *testing.T) {
	data := []byte("version: notanumber\n")

	c, err := Parse(data)
	if c != nil {
		t.Fatalf("Parse returned non-nil Config on type mismatch: %+v", c)
	}
	var parseErr *ParseError
	if !errors.As(err, &parseErr) {
		t.Fatalf("Parse error = %v (%T), want *ParseError", err, err)
	}
	if parseErr.Line == 0 {
		t.Errorf("ParseError.Line = 0, want a non-zero line number")
	}
}

func TestParse_EmptyProjects(t *testing.T) {
	data := []byte("version: 1\n")

	c, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}
	if c.Projects != nil {
		t.Errorf("Projects = %v, want nil", c.Projects)
	}
}

func TestParse_VersionNotRejected(t *testing.T) {
	data := []byte("version: 2\nprojects: []\n")

	c, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse returned unexpected error for non-1 version: %v", err)
	}
	if c.Version != 2 {
		t.Errorf("Version = %d, want 2", c.Version)
	}
}
