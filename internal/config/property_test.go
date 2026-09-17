package config

import (
	"testing"

	yaml "go.yaml.in/yaml/v3"

	"github.com/stretchr/testify/require"
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
		// Env names carry a fixed prefix so a draw can never land on a
		// reserved name ("PATH", or anything under TURNIP_) and fail
		// validation for a reason this property isn't about. Drawn
		// non-empty because Env is omitempty: an empty map marshals to
		// nothing and parses back as nil, which would be a flaw in the
		// fixture rather than in the round-trip under test.
		Env: rapid.MapOfN(
			rapid.StringMatching("E"+identifierPattern),
			rapid.StringMatching(identifierPattern),
			1, 3,
		).Draw(t, "env"),
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
		// No version is drawn: exactly one schema version is legal, so
		// there is nothing to vary. What this property exercises is the
		// projects round-tripping through YAML.
		original := &Config{SchemaVersion: SupportedSchemaVersion, Projects: genProjects(t)}

		data, err := yaml.Marshal(original)
		require.NoError(t, err)

		got, err := Parse(data)
		require.NoError(t, err)

		require.Equal(t, original, got)
	})
}

// Feature: multi-iac-automation-platform, Property 2: Tool Validation Rejects Invalid Tools
func TestProperty_ToolValidationRejectsInvalidTools(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		tool := genIdentifier(t, "tool")
		data := []byte("schemaVersion: v1alpha1\nprojects:\n  - name: p\n    directory: d\n    tool: " + tool + "\n")

		_, err := Parse(data)

		valid := tool == ToolTerraform || tool == ToolPulumi || tool == ToolHelmfile
		if valid {
			require.NoErrorf(t, err, "Parse() with valid tool %q", tool)
		} else {
			require.Errorf(t, err, "Parse() with invalid tool %q", tool)
		}
	})
}

// Feature: runner-workspace-environment, Requirement 2.3: every name under
// the reserved prefix is rejected, whatever follows it — the prefix is
// what's reserved, not an enumerated list of the variables turnip
// happens to set today.
func TestProperty_ReservedEnvPrefixRejectedRegardlessOfSuffix(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		name := reservedEnvPrefix + genIdentifier(t, "suffix")
		data := []byte("schemaVersion: v1alpha1\nprojects:\n  - name: p\n    directory: d\n    tool: helmfile\n    env:\n      " + name + ": value\n")

		_, err := Parse(data)

		require.Errorf(t, err, "Parse() with reserved env name %q", name)
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

		require.Len(t, got, 1)
		require.Equal(t, "p", got[0].Name)
	})
}
