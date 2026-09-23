package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse_WithMapRoundTrips(t *testing.T) {
	data := []byte(`
schemaVersion: v1alpha2
projects:
  - name: vpc
    directory: infra/vpc
    uses: terraform
    with:
      workspace: prod
      region: us-east-1
`)

	c, err := Parse(data)
	require.NoError(t, err)

	got := c.Projects[0].With
	want := map[string]string{"workspace": "prod", "region": "us-east-1"}
	require.Len(t, got, len(want))
	for k, v := range want {
		assert.Equal(t, v, got[k], "With[%q]", k)
	}
}

func TestParse_WithMapAbsentIsNil(t *testing.T) {
	data := []byte(`
schemaVersion: v1alpha2
projects:
  - name: vpc
    directory: infra/vpc
    uses: terraform
`)

	c, err := Parse(data)
	require.NoError(t, err)
	assert.Nil(t, c.Projects[0].With)
}

// An unrecognized key inside `with` is a Plugin's business, not turnip's:
// the whole point of the map is to carry keys turnip does not define, so
// strict decoding must stop at its boundary.
func TestParse_UnknownKeyInsideWithIsAccepted(t *testing.T) {
	data := []byte("schemaVersion: v1alpha2\nprojects:\n  - directory: d\n    uses: helmfile\n    with:\n      somethingTurnipNeverHeardOf: yes\n")

	c, err := Parse(data)
	require.NoError(t, err)
	assert.Equal(t, "yes", c.Projects[0].With["somethingTurnipNeverHeardOf"])
}
