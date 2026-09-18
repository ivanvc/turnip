package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse_ValidRoundTrip(t *testing.T) {
	data := []byte(`
schemaVersion: v1alpha2
projects:
  - name: vpc
    directory: infra/vpc
    uses: terraform@1.9.5
    whenModified:
      - "infra/vpc/**"
    with:
      workspace: prod
`)

	c, err := Parse(data)
	require.NoError(t, err)
	assert.Equal(t, SupportedSchemaVersion, c.SchemaVersion)
	require.Len(t, c.Projects, 1)

	p := c.Projects[0]
	assert.Equal(t, "vpc", p.Name)
	assert.Equal(t, "infra/vpc", p.Directory)
	assert.Equal(t, ToolTerraform, p.Tool)
	assert.Equal(t, "1.9.5", p.ToolVersion)
	assert.Equal(t, []string{"infra/vpc/**"}, p.WhenModified)
	assert.Equal(t, "prod", p.With["workspace"])
}

func TestParse_MalformedYAML(t *testing.T) {
	data := []byte("schemaVersion: v1alpha2\nprojects:\n  - name: [unclosed\n")

	c, err := Parse(data)
	require.Nil(t, c)

	var parseErr *ParseError
	require.ErrorAs(t, err, &parseErr)
}

func TestParse_NameDefaultsToDirectory(t *testing.T) {
	data := []byte("schemaVersion: v1alpha2\nprojects:\n  - directory: infra/vpc\n    uses: terraform\n")

	c, err := Parse(data)
	require.NoError(t, err)
	assert.Equal(t, "infra/vpc", c.Projects[0].Name)
}

func TestParse_ExplicitNameNotOverridden(t *testing.T) {
	data := []byte("schemaVersion: v1alpha2\nprojects:\n  - name: vpc\n    directory: infra/vpc\n    uses: terraform\n")

	c, err := Parse(data)
	require.NoError(t, err)
	assert.Equal(t, "vpc", c.Projects[0].Name)
}

func TestParse_DefaultedNamesStillDetectDuplicates(t *testing.T) {
	data := []byte(`
schemaVersion: v1alpha2
projects:
  - directory: infra/vpc
    uses: terraform
  - directory: infra/vpc
    uses: pulumi
`)

	_, err := Parse(data)
	require.Error(t, err)

	var verrs ValidationErrors
	require.ErrorAs(t, err, &verrs)

	found := false
	for _, v := range verrs {
		if v.Field == "name" {
			found = true
		}
	}
	assert.True(t, found, "no duplicate-name ValidationError found in %v", verrs)
}

// `uses` fuses two things that used to live at different levels, so the
// forms it accepts are worth pinning individually.
func TestParse_UsesForms(t *testing.T) {
	tests := []struct {
		name        string
		uses        string
		wantTool    string
		wantVersion string
	}{
		{name: "tool only", uses: "helmfile", wantTool: ToolHelmfile, wantVersion: ""},
		{name: "tool and version", uses: "helmfile@1.7.4", wantTool: ToolHelmfile, wantVersion: "1.7.4"},
		{name: "leading v normalized", uses: "terraform@v1.9.5", wantTool: ToolTerraform, wantVersion: "1.9.5"},
		{name: "prerelease", uses: "pulumi@3.130.0-rc1", wantTool: ToolPulumi, wantVersion: "3.130.0-rc1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := []byte("schemaVersion: v1alpha2\nprojects:\n  - directory: d\n    uses: " + tt.uses + "\n")

			c, err := Parse(data)
			require.NoError(t, err)
			assert.Equal(t, tt.wantTool, c.Projects[0].Tool)
			assert.Equal(t, tt.wantVersion, c.Projects[0].ToolVersion)
		})
	}
}

func TestParse_UsesRejected(t *testing.T) {
	tests := []struct {
		name string
		uses string
	}{
		{name: "unknown tool", uses: "cloudformation"},
		{name: "floating tag", uses: "terraform@latest"},
		{name: "two-part version", uses: "terraform@1.9"},
		{name: "empty version", uses: "terraform@"},
		// Quoted because "@" is a reserved indicator in YAML: unquoted,
		// this would fail as malformed YAML before validation ever saw it.
		{name: "no tool", uses: `"@1.9.5"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := []byte("schemaVersion: v1alpha2\nprojects:\n  - directory: d\n    uses: " + tt.uses + "\n")

			_, err := Parse(data)
			require.Error(t, err)

			var verrs ValidationErrors
			require.ErrorAs(t, err, &verrs)
			require.NotEmpty(t, verrs)
			assert.Equal(t, "uses", verrs[0].Field)
		})
	}
}

func TestParse_UsesIsRequired(t *testing.T) {
	data := []byte("schemaVersion: v1alpha2\nprojects:\n  - directory: d\n")

	_, err := Parse(data)
	require.Error(t, err)

	var verrs ValidationErrors
	require.ErrorAs(t, err, &verrs)
	require.Len(t, verrs, 1)
	assert.Equal(t, "uses", verrs[0].Field)
}

func TestParse_RunnerEnvSurvivesRoundTrip(t *testing.T) {
	data := []byte(`
schemaVersion: v1alpha2
projects:
  - name: web
    directory: infra/web
    uses: helmfile
    runner:
      serviceAccount: turnip-runner
      env:
        AWS_PROFILE: web-deployer
        HELM_DIFF_COLOR: "true"
`)

	c, err := Parse(data)
	require.NoError(t, err)
	require.Len(t, c.Projects, 1)
	assert.Equal(t, "turnip-runner", c.Projects[0].Runner.ServiceAccount)
	assert.Equal(t, map[string]string{
		"AWS_PROFILE":     "web-deployer",
		"HELM_DIFF_COLOR": "true",
	}, c.Projects[0].Runner.Env)
}

func TestParse_RunnerIsOptional(t *testing.T) {
	data := []byte("schemaVersion: v1alpha2\nprojects:\n  - directory: infra/web\n    uses: helmfile\n")

	c, err := Parse(data)
	require.NoError(t, err)
	require.Len(t, c.Projects, 1)
	assert.Empty(t, c.Projects[0].Runner.ServiceAccount)
	assert.Nil(t, c.Projects[0].Runner.Env, "an absent env stays nil rather than becoming an empty map")
}

func TestParse_TypeMismatch(t *testing.T) {
	// A scalar where a sequence belongs: the mismatch must come from a
	// typed field, and every scalar is a valid schemaVersion.
	data := []byte("schemaVersion: v1alpha2\nprojects: notalist\n")

	c, err := Parse(data)
	require.Nil(t, c)

	var parseErr *ParseError
	require.ErrorAs(t, err, &parseErr)
	assert.NotZero(t, parseErr.Line)
}

func TestParse_EmptyProjects(t *testing.T) {
	data := []byte("schemaVersion: v1alpha2\n")

	c, err := Parse(data)
	require.NoError(t, err)
	assert.Nil(t, c.Projects)
}

func TestParse_UnsupportedSchemaVersionRejected(t *testing.T) {
	data := []byte("schemaVersion: v0\nprojects: []\n")

	_, err := Parse(data)
	require.Error(t, err)

	var errs ValidationErrors
	require.ErrorAs(t, err, &errs)
	require.Len(t, errs, 1)
	assert.Equal(t, "schemaVersion", errs[0].Field)
	assert.Contains(t, errs[0].Message, SupportedSchemaVersion, "the error names the version that is supported")
}

func TestParse_MissingSchemaVersionRejected(t *testing.T) {
	data := []byte("projects: []\n")

	_, err := Parse(data)
	require.Error(t, err)

	var errs ValidationErrors
	require.ErrorAs(t, err, &errs)
	require.Len(t, errs, 1)
	assert.Equal(t, "schemaVersion", errs[0].Field)
}

// A file on the previous schema carries several keys this one doesn't
// define, so strict decoding alone would bury the one fact that explains
// them all. The version is reported by itself.
func TestParse_PreviousSchemaReportsVersionAlone(t *testing.T) {
	data := []byte(`
schemaVersion: v1alpha1
projects:
  - name: vpc
    directory: infra/vpc
    tool: terraform
    config:
      version: "1.9.5"
    env:
      AWS_PROFILE: prod
`)

	_, err := Parse(data)
	require.Error(t, err)

	var errs ValidationErrors
	require.ErrorAs(t, err, &errs)
	require.Len(t, errs, 1, "only the schemaVersion is reported, not one error per unknown key: %v", errs)
	assert.Equal(t, "schemaVersion", errs[0].Field)
	assert.Contains(t, errs[0].Message, "v1alpha1")
	assert.Contains(t, errs[0].Message, SupportedSchemaVersion)
}
