package config

import (
	"slices"
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
	tool := rapid.SampledFrom(testTools).Draw(t, "tool")
	image := testAliasImages[tool]
	// Drawn from what the tool's test Alias allows (helmfile's tags carry
	// a "v", the others' do not, and a Digest is allowed for every tool),
	// and the round-trip only holds if Parse keeps whichever was written.
	versions := []string{"1.9.5", "0.170.1", "3.130.0-rc1", testDigest}
	if tool == "helmfile" {
		versions = []string{"v1.7.4", "v1.0.0-rc.1", testDigest}
	}
	version := rapid.SampledFrom(versions).Draw(t, "version")
	// The Alias and the vendor's image written in full resolve alike.
	name := rapid.SampledFrom([]string{tool, image}).Draw(t, "name before @")
	uses := name + "@" + version

	return Project{
		Name:         genIdentifier(t, "name"),
		Directory:    genIdentifier(t, "directory"),
		Uses:         uses,
		WhenModified: rapid.SliceOfN(rapid.StringMatching(identifierPattern), 2, 2).Draw(t, "whenModified"),
		// Drawn non-empty for the same reason as Env below: With is
		// omitempty, so an empty map marshals to nothing and parses back
		// as nil, which would fail the round-trip for a flaw in the
		// fixture rather than in the code under test.
		With: rapid.MapOfN(
			rapid.StringMatching(identifierPattern),
			rapid.StringMatching(identifierPattern),
			1, 3,
		).Draw(t, "with"),

		Runner: genRunner(t, "project runner"),

		// Tool, Image and ToolVersion are resolved by Parse, so a
		// generated fixture must carry what Parse would have produced for
		// its Uses; otherwise the round-trip compares a hand-built struct
		// against a parsed one and fails for a reason that isn't about YAML.
		Tool:        tool,
		Image:       image,
		ToolVersion: version,
	}
}

// sharedEnvNames are env names a top-level and a Project block may both
// draw, so that the round-trip sees a Project overriding an inherited
// variable and not only disjoint maps.
var sharedEnvNames = []string{"EA", "EB", "EC"}

// genRunner draws one runner: block, at either level. Each field is drawn
// absent or set, since the merge's behavior differs between the two.
//
// Env names carry a fixed prefix so a draw can never land on a reserved
// name ("PATH", or anything under TURNIP_) and fail validation for a
// reason this property isn't about. A drawn Env is nil or non-empty,
// never empty: Env is omitempty, so an empty map marshals to nothing and
// parses back as nil, which would be a flaw in the fixture rather than in
// the round-trip under test.
func genRunner(t *rapid.T, label string) RunnerSpec {
	var r RunnerSpec
	if rapid.Bool().Draw(t, label+" sets serviceAccount") {
		r.ServiceAccount = genIdentifier(t, label+" serviceAccount")
	}
	if rapid.Bool().Draw(t, label+" sets env") {
		r.Env = rapid.MapOfN(
			rapid.OneOf(
				rapid.SampledFrom(sharedEnvNames),
				rapid.StringMatching("E"+identifierPattern),
			),
			rapid.StringMatching(identifierPattern),
			1, 3,
		).Draw(t, label+" env")
	}
	return r
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
		original := &Config{
			SchemaVersion: SupportedSchemaVersion,
			Projects:      genProjects(t),
			Runner:        genRunner(t, "top-level runner"),
		}

		data, err := yaml.Marshal(original)
		require.NoError(t, err)

		got, err := Parse(data, testCatalog)
		require.NoError(t, err)

		// Parse hands back each Project's Effective_Runner, so the
		// expectation is the merge of what was generated rather than the
		// Project as generated. The top-level block stays as written.
		want := *original
		want.Projects = slices.Clone(original.Projects)
		for i := range want.Projects {
			p := &want.Projects[i]
			p.ServiceAccountSource = serviceAccountSource(original.Runner, p.Runner)
			p.Runner = mergeRunner(original.Runner, p.Runner)
		}

		require.Equal(t, &want, got)
	})
}

// Feature: multi-iac-automation-platform, Property 2: Tool Validation Rejects Invalid Tools
func TestProperty_ToolValidationRejectsInvalidTools(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		tool := genIdentifier(t, "tool")
		// A Digest, which every test Alias allows, so that a registered
		// tool is accepted whatever its vendor's tag convention.
		data := []byte("schemaVersion: v1alpha3\nprojects:\n  - name: p\n    directory: d\n    uses: " + tool + "@" + testDigest + "\n")

		_, err := Parse(data, testCatalog)

		if slices.Contains(testTools, tool) {
			require.NoErrorf(t, err, "Parse(, testCatalog) with valid tool %q", tool)
		} else {
			require.Errorf(t, err, "Parse(, testCatalog) with invalid tool %q", tool)
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
		data := []byte("schemaVersion: v1alpha3\nprojects:\n  - name: p\n    directory: d\n    uses: helmfile@v1.7.4\n    runner:\n      env:\n        " + name + ": value\n")

		_, err := Parse(data, testCatalog)

		require.Errorf(t, err, "Parse(, testCatalog) with reserved env name %q", name)
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
