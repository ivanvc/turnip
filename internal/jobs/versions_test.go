package jobs

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveVersion_RecognizedExplicitVersion(t *testing.T) {
	for tool, ti := range toolImages {
		t.Run(tool, func(t *testing.T) {
			version, err := resolveVersion(tool, ti.versions[len(ti.versions)-1])
			require.NoError(t, err)
			assert.Equal(t, ti.versions[len(ti.versions)-1], version)
		})
	}
}

func TestResolveVersion_EmptyFallsBackToDefault(t *testing.T) {
	for tool, ti := range toolImages {
		t.Run(tool, func(t *testing.T) {
			version, err := resolveVersion(tool, "")
			require.NoError(t, err)
			assert.Equal(t, ti.versions[0], version)
		})
	}
}

func TestResolveVersion_UnrecognizedVersion(t *testing.T) {
	_, err := resolveVersion("terraform", "0.0.1-does-not-exist")
	require.Error(t, err)
	var unrecognized *UnrecognizedVersionError
	require.ErrorAs(t, err, &unrecognized)
	assert.Equal(t, "terraform", unrecognized.Tool)
}

func TestResolveVersion_UnrecognizedTool(t *testing.T) {
	_, err := resolveVersion("ansible", "")
	require.Error(t, err)
	var unrecognized *UnrecognizedToolError
	require.ErrorAs(t, err, &unrecognized)
	assert.Equal(t, "ansible", unrecognized.Tool)
}
