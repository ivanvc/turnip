package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	require.NoError(t, err)
	assert.Equal(t, 1, c.Version)
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
	data := []byte("version: 1\nprojects:\n  - directory: infra/vpc\n    tool: terraform\n")

	c, err := Parse(data)
	require.NoError(t, err)
	assert.Equal(t, "infra/vpc", c.Projects[0].Name)
}

func TestParse_ExplicitNameNotOverridden(t *testing.T) {
	data := []byte("version: 1\nprojects:\n  - name: vpc\n    directory: infra/vpc\n    tool: terraform\n")

	c, err := Parse(data)
	require.NoError(t, err)
	assert.Equal(t, "vpc", c.Projects[0].Name)
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
	data := []byte("version: notanumber\n")

	c, err := Parse(data)
	require.Nil(t, c)

	var parseErr *ParseError
	require.ErrorAs(t, err, &parseErr)
	assert.NotZero(t, parseErr.Line)
}

func TestParse_EmptyProjects(t *testing.T) {
	data := []byte("version: 1\n")

	c, err := Parse(data)
	require.NoError(t, err)
	assert.Nil(t, c.Projects)
}

func TestParse_VersionNotRejected(t *testing.T) {
	data := []byte("version: 2\nprojects: []\n")

	c, err := Parse(data)
	require.NoError(t, err)
	assert.Equal(t, 2, c.Version)
}
