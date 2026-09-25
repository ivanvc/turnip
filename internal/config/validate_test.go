package config

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func asValidationErrors(t *testing.T, err error) ValidationErrors {
	t.Helper()
	var verrs ValidationErrors
	require.ErrorAs(t, err, &verrs)
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
			yaml:  "schemaVersion: v1alpha3\nprojects:\n  - name: vpc\n    uses: terraform@1.9.5\n",
			field: "directory",
		},
		{
			name:  "missing uses",
			yaml:  "schemaVersion: v1alpha3\nprojects:\n  - name: vpc\n    directory: infra/vpc\n",
			field: "uses",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.yaml), testTools)
			require.Error(t, err)
			verrs := asValidationErrors(t, err)

			found := false
			for _, v := range verrs {
				if v.Field == tt.field {
					found = true
				}
			}
			assert.True(t, found, "no ValidationError for field %q in %v", tt.field, verrs)
		})
	}
}

func TestParse_MissingNameAndDirectoryRefersToProjectByIndex(t *testing.T) {
	data := []byte("schemaVersion: v1alpha3\nprojects:\n  - uses: terraform@1.9.5\n")

	_, err := Parse(data, testTools)
	require.Error(t, err)
	verrs := asValidationErrors(t, err)

	found := false
	for _, v := range verrs {
		if v.ProjectRef == "projects[0]" && v.Field == "directory" {
			found = true
		}
	}
	assert.True(t, found, "no directory ValidationError referencing projects[0] found in %v", verrs)
}

// Requirement 3.3: the accepted tools are exactly the names Parse is
// given, so a tool nobody registered is refused here, listing the names
// that would have been accepted, in the order given.
func TestParse_UnsupportedToolListsRegisteredNames(t *testing.T) {
	data := []byte("schemaVersion: v1alpha3\nprojects:\n  - name: vpc\n    directory: infra/vpc\n    uses: cloudformation@1.0.0\n")

	_, err := Parse(data, testTools)
	require.Error(t, err)
	verrs := asValidationErrors(t, err)
	require.Len(t, verrs, 1)

	assert.Contains(t, verrs[0].Message, `"cloudformation"`)
	assert.Contains(t, verrs[0].Message, `one of "helmfile", "pulumi", "terraform"`)
}

// The names are listed alphabetically whatever order they were registered
// in, so no Plugin is presented first because of where it sits in the list.
func TestParse_RegisteredToolsListedAlphabetically(t *testing.T) {
	data := []byte("schemaVersion: v1alpha3\nprojects:\n  - name: vpc\n    directory: infra/vpc\n    uses: cloudformation@1.0.0\n")

	_, err := Parse(data, []string{"terraform", "helmfile", "pulumi"})
	require.Error(t, err)
	verrs := asValidationErrors(t, err)
	require.Len(t, verrs, 1)
	assert.Contains(t, verrs[0].Message, `one of "helmfile", "pulumi", "terraform"`)
}

// A name is accepted only because it was passed in: the same file fails
// once that name is no longer registered, which is how terraform and
// pulumi are refused until their Plugins exist.
func TestParse_ToolAcceptedOnlyWhenRegistered(t *testing.T) {
	data := []byte("schemaVersion: v1alpha3\nprojects:\n  - name: vpc\n    directory: infra/vpc\n    uses: terraform@1.9.5\n")

	_, err := Parse(data, testTools)
	require.NoError(t, err)

	_, err = Parse(data, []string{"helmfile"})
	require.Error(t, err)
	verrs := asValidationErrors(t, err)
	require.Len(t, verrs, 1)
	assert.Contains(t, verrs[0].Message, `unsupported tool "terraform", must be one of "helmfile"`)
}

// Requirement 3.4: the missing-uses: error lists the registered names too,
// not a fixed list.
func TestParse_MissingUsesListsRegisteredNames(t *testing.T) {
	data := []byte("schemaVersion: v1alpha3\nprojects:\n  - name: vpc\n    directory: infra/vpc\n")

	_, err := Parse(data, []string{"alpha", "beta"})
	require.Error(t, err)
	verrs := asValidationErrors(t, err)
	require.Len(t, verrs, 1)
	assert.Equal(t, "uses", verrs[0].Field)
	assert.Contains(t, verrs[0].Message, `one of "alpha", "beta"`)
	assert.Contains(t, verrs[0].Message, `"helmfile@v1.7.4"`)
}

func TestParse_DuplicateProjectNames(t *testing.T) {
	data := []byte(`
schemaVersion: v1alpha3
projects:
  - name: vpc
    directory: infra/vpc-a
    uses: terraform@1.9.5
  - name: vpc
    directory: infra/vpc-b
    uses: terraform@1.9.5
`)

	_, err := Parse(data, testTools)
	require.Error(t, err)
	verrs := asValidationErrors(t, err)

	found := false
	for _, v := range verrs {
		if strings.Contains(v.Message, "duplicate") {
			found = true
		}
	}
	assert.True(t, found, "no duplicate-name ValidationError found in %v", verrs)
}

func TestParse_UnaddressableProjectNamesRejected(t *testing.T) {
	tests := []struct {
		name        string
		projectName string
		wantIn      string
	}{
		{name: "bare star", projectName: `"*"`, wantIn: `cannot contain "*"`},
		{name: "star within a name", projectName: `"gcp/*"`, wantIn: `cannot contain "*"`},
		{name: "leading dash", projectName: `"-infra"`, wantIn: `cannot begin with "-"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := []byte("schemaVersion: v1alpha3\nprojects:\n  - name: " + tt.projectName +
				"\n    directory: infra/vpc\n    uses: terraform@1.9.5\n")

			_, err := Parse(data, testTools)
			require.Error(t, err)
			verrs := asValidationErrors(t, err)

			found := false
			for _, v := range verrs {
				if v.Field == "name" && strings.Contains(v.Message, tt.wantIn) {
					found = true
				}
			}
			assert.True(t, found, "no name ValidationError containing %q in %v", tt.wantIn, verrs)
		})
	}
}

// A Project with no name takes its directory (applyDefaults), so an
// unaddressable directory produces an unaddressable name. This test is
// what fails if applyDefaults and validate are ever reordered, which is
// the assumption the checks in validate.go are written against.
func TestParse_DefaultedNameFromDirectoryStillRejected(t *testing.T) {
	data := []byte("schemaVersion: v1alpha3\nprojects:\n  - directory: \"-infra\"\n    uses: terraform@1.9.5\n")

	_, err := Parse(data, testTools)
	require.Error(t, err)
	verrs := asValidationErrors(t, err)

	found := false
	for _, v := range verrs {
		if v.Field == "name" && strings.Contains(v.Message, `cannot begin with "-"`) {
			found = true
		}
	}
	assert.True(t, found, "a name defaulted from an unaddressable directory was accepted: %v", verrs)
}

// Path-shaped names are the convention this reservation must not break:
// "gcp/project" contains a separator but no "*", and a repository that
// names Projects for their directories depends on it being accepted.
func TestParse_PathShapedProjectNamesAccepted(t *testing.T) {
	data := []byte(`
schemaVersion: v1alpha3
projects:
  - name: gcp/project
    directory: env/gcp/project
    uses: terraform@1.9.5
  - name: aws/project
    directory: env/aws/project
    uses: terraform@1.9.5
`)

	cfg, err := Parse(data, testTools)
	require.NoError(t, err)
	require.Len(t, cfg.Projects, 2)
	assert.Equal(t, "gcp/project", cfg.Projects[0].Name)
	assert.Equal(t, "aws/project", cfg.Projects[1].Name)
}

func TestParse_MultipleSimultaneousViolations(t *testing.T) {
	data := []byte(`
schemaVersion: v1alpha3
projects:
  - directory: infra/a
  - name: b
    directory: infra/b
    uses: terraform@1.9.5
  - name: b
    directory: infra/c
    uses: nope
`)

	_, err := Parse(data, testTools)
	require.Error(t, err)
	verrs := asValidationErrors(t, err)
	assert.GreaterOrEqual(t, len(verrs), 3, "want at least 3 violations (missing uses, duplicate name, bad tool): %v", verrs)
}

// The Runner reads its own configuration out of TURNIP_* and finds its
// tool binary through PATH, so a Project setting either would be
// reconfiguring the Runner rather than the tool it runs.
func TestParse_ReservedEnvNamesRejectedTogether(t *testing.T) {
	data := []byte(`
schemaVersion: v1alpha3
projects:
  - name: web
    directory: infra/web
    uses: helmfile@v1.7.4
    runner:
      env:
        TURNIP_SERVER_ADDR: elsewhere
        PATH: /nowhere
        AWS_PROFILE: untouched
`)

	_, err := Parse(data, testTools)
	require.Error(t, err)
	verrs := asValidationErrors(t, err)
	require.Len(t, verrs, 2, "both reserved names reported, and the legal one left alone: %v", verrs)

	assert.ElementsMatch(t,
		[]string{`runner.env["PATH"]`, `runner.env["TURNIP_SERVER_ADDR"]`},
		[]string{verrs[0].Field, verrs[1].Field})
}

// The prefix is what's reserved, not a fixed list of known variables —
// a name turnip doesn't use today is still rejected.
func TestParse_UnknownTurnipPrefixedEnvNameStillRejected(t *testing.T) {
	data := []byte("schemaVersion: v1alpha3\nprojects:\n  - directory: d\n    uses: helmfile@v1.7.4\n    runner:\n      env:\n        TURNIP_NOT_A_REAL_VARIABLE: x\n")

	_, err := Parse(data, testTools)
	require.Error(t, err)
	verrs := asValidationErrors(t, err)
	require.Len(t, verrs, 1)
	assert.Contains(t, verrs[0].Message, "reserved")
}

func TestParse_OrdinaryEnvNamesAccepted(t *testing.T) {
	data := []byte("schemaVersion: v1alpha3\nprojects:\n  - directory: d\n    uses: helmfile@v1.7.4\n    runner:\n      env:\n        AWS_PROFILE: prod\n        PATHOLOGICAL: not-PATH\n")

	_, err := Parse(data, testTools)
	require.NoError(t, err, "only PATH exactly is reserved, not names that merely start with it")
}

func TestParse_InvalidGlobPattern(t *testing.T) {
	data := []byte(`
schemaVersion: v1alpha3
projects:
  - name: vpc
    directory: infra/vpc
    uses: terraform@1.9.5
    whenModified:
      - "infra/vpc/["
`)

	_, err := Parse(data, testTools)
	require.Error(t, err)
	verrs := asValidationErrors(t, err)

	found := false
	for _, v := range verrs {
		if strings.HasPrefix(v.Field, "whenModified") {
			found = true
		}
	}
	assert.True(t, found, "no whenModified ValidationError found in %v", verrs)
}

// Unrecognized keys turnip *does* define the shape of are errors, and
// several are reported together rather than one per attempt.
func TestParse_UnknownKeysRejectedTogether(t *testing.T) {
	data := []byte(`
schemaVersion: v1alpha3
bogusTop: 1
projects:
  - directory: d
    uses: helmfile@v1.7.4
    nope: x
`)

	_, err := Parse(data, testTools)
	require.Error(t, err)
	verrs := asValidationErrors(t, err)
	require.Len(t, verrs, 2, "both unknown keys reported: %v", verrs)

	assert.ElementsMatch(t, []string{"bogusTop", "nope"}, []string{verrs[0].Field, verrs[1].Field})
	for _, v := range verrs {
		assert.Contains(t, v.Message, "unrecognized field")
		assert.NotContains(t, v.Message, "config.", "the decoder's Go type names must never reach a PR comment")
	}
}

// `runner` is a struct turnip defines, so a typo inside it is caught —
// unlike `with`, which is a free map by design.
func TestParse_UnknownKeyInsideRunnerRejected(t *testing.T) {
	data := []byte("schemaVersion: v1alpha3\nprojects:\n  - directory: d\n    uses: helmfile@v1.7.4\n    runner:\n      serviceAcount: typo\n")

	_, err := Parse(data, testTools)
	require.Error(t, err)
	verrs := asValidationErrors(t, err)
	require.Len(t, verrs, 1)
	assert.Equal(t, "serviceAcount", verrs[0].Field)
}

// Anchors and merge keys are how real configurations avoid the repetition
// a per-project schema forces. yaml.v3 expands "<<" before matching
// fields, so strict decoding must not see it as an unknown key — pinned
// here because the instinct on reading "reject unknown keys" is to add a
// special case for it.
func TestParse_MergeKeysSurviveStrictDecoding(t *testing.T) {
	data := []byte(`
schemaVersion: v1alpha3
projects:
  - &base
    name: a
    directory: infra/a
    uses: terraform@1.9.5
    whenModified: ["infra/**"]
  - <<: *base
    name: b
`)

	c, err := Parse(data, testTools)
	require.NoError(t, err)
	require.Len(t, c.Projects, 2)

	assert.Equal(t, "b", c.Projects[1].Name)
	assert.Equal(t, "infra/a", c.Projects[1].Directory, "the merged key is inherited")
	assert.Equal(t, "1.9.5", c.Projects[1].ToolVersion, "derivation runs on the merged result")
}
