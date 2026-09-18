package orchestrator

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Unset must mean exactly what turnip did before this setting existed: a
// repository could never choose its own ServiceAccount.
func TestParseAllowedOverrides_UnsetPermitsNothing(t *testing.T) {
	for _, raw := range []string{"", "   ", ",,"} {
		allowed, err := parseAllowedOverrides(raw)
		require.NoError(t, err)
		assert.Empty(t, allowed, "raw=%q", raw)
	}
}

func TestParseAllowedOverrides_ExplicitPath(t *testing.T) {
	allowed, err := parseAllowedOverrides("runner.serviceAccount")
	require.NoError(t, err)
	assert.True(t, allowed[overrideServiceAccount])
}

func TestParseAllowedOverrides_TrimsWhitespaceAndSkipsBlanks(t *testing.T) {
	allowed, err := parseAllowedOverrides("  runner.serviceAccount ,, ")
	require.NoError(t, err)
	assert.True(t, allowed[overrideServiceAccount])
	assert.Len(t, allowed, 1)
}

// A path turnip doesn't know would gate nothing while looking like it
// gated something — the operator-side twin of the silently-ignored key
// this schema version removes from the configuration file.
func TestParseAllowedOverrides_UnknownPathIsAnError(t *testing.T) {
	_, err := parseAllowedOverrides("runner.serviceaccount")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "runner.serviceaccount", "names what was written")
	assert.Contains(t, err.Error(), overrideServiceAccount, "and what was probably meant")
}

// `uses` is deliberately not gateable: an override is a repository
// replacing a Server-supplied value, and turnip has no Server-side tool to
// fall back to. Naming it is an error like any other unknown path, rather
// than a silently accepted no-op.
func TestParseAllowedOverrides_UsesIsNotAnOverridePath(t *testing.T) {
	_, err := parseAllowedOverrides("uses")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "uses")
}
