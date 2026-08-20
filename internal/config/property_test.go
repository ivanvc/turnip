package config

import (
	"reflect"
	"testing"

	yaml "go.yaml.in/yaml/v3"

	"pgregory.net/rapid"
)

// identifierPattern approximates a Go-style identifier: a letter followed
// by up to 15 letters or digits.
const identifierPattern = "[a-zA-Z][a-zA-Z0-9]{0,15}"

func genIdentifier(t *rapid.T, label string) string {
	return rapid.StringMatching(identifierPattern).Draw(t, label)
}

func genProject(t *rapid.T) Project {
	return Project{
		Name:         genIdentifier(t, "name"),
		Directory:    genIdentifier(t, "directory"),
		Tool:         rapid.SampledFrom([]string{ToolTerraform, ToolPulumi, ToolHelmfile}).Draw(t, "tool"),
		WhenModified: rapid.SliceOfN(rapid.StringMatching(identifierPattern), 2, 2).Draw(t, "whenModified"),
		Config:       rapid.MapOf(rapid.StringMatching(identifierPattern), rapid.StringMatching(identifierPattern)).Draw(t, "config"),
	}
}

// genProjects draws 1-4 Projects with disambiguated names, so generated
// inputs never trip the duplicate-name validation rule.
func genProjects(t *rapid.T) []Project {
	n := rapid.IntRange(1, 4).Draw(t, "numProjects")
	projects := make([]Project, n)
	for i := range projects {
		p := genProject(t)
		p.Name = p.Name + "-" + string(rune('a'+i))
		projects[i] = p
	}
	return projects
}

// Feature: multi-iac-automation-platform, Property 1: Configuration Round-Trip
func TestProperty_ConfigurationRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		version := rapid.IntRange(1, 100).Draw(t, "version")
		original := &Config{Version: version, Projects: genProjects(t)}

		data, err := yaml.Marshal(original)
		if err != nil {
			t.Fatalf("yaml.Marshal() error = %v", err)
		}

		got, err := Parse(data)
		if err != nil {
			t.Fatalf("Parse() error = %v", err)
		}

		if !reflect.DeepEqual(original, got) {
			t.Fatalf("Parse(yaml.Marshal(original)) = %+v, want %+v", got, original)
		}
	})
}

// Feature: multi-iac-automation-platform, Property 2: Tool Validation Rejects Invalid Tools
func TestProperty_ToolValidationRejectsInvalidTools(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		tool := genIdentifier(t, "tool")
		data := []byte("version: 1\nprojects:\n  - name: p\n    directory: d\n    tool: " + tool + "\n")

		_, err := Parse(data)

		valid := tool == ToolTerraform || tool == ToolPulumi || tool == ToolHelmfile
		if valid && err != nil {
			t.Fatalf("Parse() with valid tool %q returned error: %v", tool, err)
		}
		if !valid && err == nil {
			t.Fatalf("Parse() with invalid tool %q returned no error", tool)
		}
	})
}

// Feature: multi-iac-automation-platform, Property 3: WhenModified Pattern Matching
func TestProperty_WhenModifiedPatternMatching(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		pattern := genIdentifier(t, "pattern")
		extraFiles := rapid.SliceOfN(rapid.StringMatching(identifierPattern), 3, 3).Draw(t, "extraFiles")

		project := Project{Name: "p", WhenModified: []string{pattern}}
		modifiedFiles := append(append([]string{}, extraFiles...), pattern)

		got := MatchProjects([]Project{project}, modifiedFiles)

		if len(got) != 1 || got[0].Name != "p" {
			t.Fatalf("MatchProjects() = %+v, want project %q matched", got, "p")
		}
	})
}
