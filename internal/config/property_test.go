package config

import (
	"reflect"
	"testing"

	yaml "go.yaml.in/yaml/v3"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

func testParameters() *gopter.TestParameters {
	params := gopter.DefaultTestParameters()
	params.MinSuccessfulTests = 100
	return params
}

var projectGen = gen.Struct(reflect.TypeOf(Project{}), map[string]gopter.Gen{
	"Name":         gen.Identifier(),
	"Directory":    gen.Identifier(),
	"Tool":         gen.OneConstOf(ToolTerraform, ToolPulumi, ToolHelmfile),
	"WhenModified": gen.SliceOfN(2, gen.Identifier()),
	"Config":       gen.MapOf(gen.Identifier(), gen.Identifier()),
})

var projectsGen = gen.IntRange(1, 4).FlatMap(func(v interface{}) gopter.Gen {
	return gen.SliceOfN(v.(int), projectGen)
}, reflect.TypeOf([]Project{}))

// uniqueNames returns a copy of projects with names disambiguated by index,
// so generated inputs never trip the duplicate-name validation rule.
func uniqueNames(projects []Project) []Project {
	out := make([]Project, len(projects))
	for i, p := range projects {
		p.Name = p.Name + "-" + string(rune('a'+i))
		out[i] = p
	}
	return out
}

// Feature: multi-iac-automation-platform, Property 1: Configuration Round-Trip
func TestProperty_ConfigurationRoundTrip(t *testing.T) {
	properties := gopter.NewProperties(testParameters())

	properties.Property("parsing a marshaled valid Config returns an equal Config", prop.ForAll(
		func(version int, projects []Project) bool {
			original := &Config{Version: version, Projects: uniqueNames(projects)}

			data, err := yaml.Marshal(original)
			if err != nil {
				return false
			}

			got, err := Parse(data)
			if err != nil {
				return false
			}

			return reflect.DeepEqual(original, got)
		},
		gen.IntRange(1, 100),
		projectsGen,
	))

	properties.TestingRun(t)
}

// Feature: multi-iac-automation-platform, Property 2: Tool Validation Rejects Invalid Tools
func TestProperty_ToolValidationRejectsInvalidTools(t *testing.T) {
	properties := gopter.NewProperties(testParameters())

	properties.Property("a project is accepted iff its tool is one of the three supported values", prop.ForAll(
		func(tool string) bool {
			data := []byte("version: 1\nprojects:\n  - name: p\n    directory: d\n    tool: " + tool + "\n")

			_, err := Parse(data)

			valid := tool == ToolTerraform || tool == ToolPulumi || tool == ToolHelmfile
			return valid == (err == nil)
		},
		gen.Identifier(),
	))

	properties.TestingRun(t)
}

// Feature: multi-iac-automation-platform, Property 3: WhenModified Pattern Matching
func TestProperty_WhenModifiedPatternMatching(t *testing.T) {
	properties := gopter.NewProperties(testParameters())

	properties.Property("a project is matched when the modified files include one of its patterns", prop.ForAll(
		func(pattern string, extraFiles []string) bool {
			project := Project{Name: "p", WhenModified: []string{pattern}}
			modifiedFiles := append(append([]string{}, extraFiles...), pattern)

			got := MatchProjects([]Project{project}, modifiedFiles)

			return len(got) == 1 && got[0].Name == "p"
		},
		gen.Identifier(),
		gen.SliceOfN(3, gen.Identifier()),
	))

	properties.TestingRun(t)
}
