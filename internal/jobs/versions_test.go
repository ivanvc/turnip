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

func TestResolveVersion_MalformedVersionIsRejected(t *testing.T) {
	for _, malformed := range []string{"not-a-version", "latest", "v1.9.5", "1.x", "1.9"} {
		t.Run(malformed, func(t *testing.T) {
			_, err := resolveVersion("terraform", malformed)
			require.Error(t, err)
			var unrecognized *UnrecognizedVersionError
			require.ErrorAs(t, err, &unrecognized)
			assert.Equal(t, "terraform", unrecognized.Tool)
		})
	}
}

// TestResolveVersion_WellFormedVersionNotInExampleListIsAccepted is the
// crux of the fix: turnip must never require its own code/release change
// just to adopt a tool version its vendor already published (Requirement
// 8's user story) — resolveVersion must not gate on membership in
// toolImage.versions, only on looking like a real version.
func TestResolveVersion_WellFormedVersionNotInExampleListIsAccepted(t *testing.T) {
	for tool, ti := range toolImages {
		t.Run(tool, func(t *testing.T) {
			const notInList = "9.9.9"
			require.NotContains(t, ti.versions, notInList)

			version, err := resolveVersion(tool, notInList)
			require.NoError(t, err)
			assert.Equal(t, notInList, version)
		})
	}
}

func TestResolveVersion_UnrecognizedTool(t *testing.T) {
	_, err := resolveVersion("ansible", "")
	require.Error(t, err)
	var unrecognized *UnrecognizedToolError
	require.ErrorAs(t, err, &unrecognized)
	assert.Equal(t, "ansible", unrecognized.Tool)
}
