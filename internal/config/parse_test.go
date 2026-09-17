package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse_ValidRoundTrip(t *testing.T) {
	data := []byte(`
schemaVersion: v1alpha1
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
	require.NoError(t, err)
	assert.Equal(t, SupportedSchemaVersion, c.SchemaVersion)
	require.Len(t, c.Projects, 1)

	p := c.Projects[0]
	assert.Equal(t, "vpc", p.Name)
	assert.Equal(t, "infra/vpc", p.Directory)
	assert.Equal(t, ToolTerraform, p.Tool)
	assert.Equal(t, []string{"infra/vpc/**/*.tf"}, p.WhenModified)
	assert.Equal(t, "prod", p.Config["workspace"])
}

func TestParse_MalformedYAML(t *testing.T) {
	data := []byte("version: [1\n")

	c, err := Parse(data)
	require.Nil(t, c)

	var parseErr *ParseError
	require.ErrorAs(t, err, &parseErr)
	assert.NotZero(t, parseErr.Line)
}

func TestParse_NameDefaultsToDirectory(t *testing.T) {
	data := []byte("schemaVersion: v1alpha1\nprojects:\n  - directory: infra/vpc\n    tool: terraform\n")

	c, err := Parse(data)
	require.NoError(t, err)
	assert.Equal(t, "infra/vpc", c.Projects[0].Name)
}

func TestParse_ExplicitNameNotOverridden(t *testing.T) {
	data := []byte("schemaVersion: v1alpha1\nprojects:\n  - name: vpc\n    directory: infra/vpc\n    tool: terraform\n")

	c, err := Parse(data)
	require.NoError(t, err)
	assert.Equal(t, "vpc", c.Projects[0].Name)
}

func TestParse_DefaultedNamesStillDetectDuplicates(t *testing.T) {
	data := []byte(`
schemaVersion: v1alpha1
projects:
  - directory: infra/vpc
    tool: terraform
  - directory: infra/vpc
    tool: pulumi
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

func TestParse_TypeMismatch(t *testing.T) {
	// A scalar where a sequence belongs: the mismatch must come from a
	// typed field, and every scalar is a valid schemaVersion.
	data := []byte("schemaVersion: v1alpha1\nprojects: notalist\n")

	c, err := Parse(data)
	require.Nil(t, c)

	var parseErr *ParseError
	require.ErrorAs(t, err, &parseErr)
	assert.NotZero(t, parseErr.Line)
}

func TestParse_EmptyProjects(t *testing.T) {
	data := []byte("schemaVersion: v1alpha1\n")

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

// The previous schema is not detected by name: turnip carries no
// compatibility machinery for it (design.md, Decision 4), so the old key
// is ignored like any other unknown field and the file fails on the
// absent schemaVersion.
func TestParse_LegacyVersionFieldFailsOnMissingSchemaVersion(t *testing.T) {
	data := []byte("version: 1\nprojects: []\n")

	_, err := Parse(data)
	require.Error(t, err)

	var errs ValidationErrors
	require.ErrorAs(t, err, &errs)
	require.Len(t, errs, 1)
	assert.Equal(t, "schemaVersion", errs[0].Field)
	assert.Contains(t, errs[0].Message, "required")
}
