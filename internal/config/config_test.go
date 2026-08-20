package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse_ConfigMapRoundTrips(t *testing.T) {
	data := []byte(`
version: 1
projects:
  - name: vpc
    directory: infra/vpc
    tool: terraform
    config:
      workspace: prod
      region: us-east-1
`)

	c, err := Parse(data)
	require.NoError(t, err)

	got := c.Projects[0].Config
	want := map[string]string{"workspace": "prod", "region": "us-east-1"}
	require.Len(t, got, len(want))
	for k, v := range want {
		assert.Equal(t, v, got[k], "Config[%q]", k)
	}
}

func TestParse_ConfigMapAbsentIsNil(t *testing.T) {
	data := []byte(`
version: 1
projects:
  - name: vpc
    directory: infra/vpc
    tool: terraform
`)

	c, err := Parse(data)
	require.NoError(t, err)
	assert.Nil(t, c.Projects[0].Config)
}
