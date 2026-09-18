package orchestrator

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivanvc/turnip/internal/config"
)

// Requirement 2.1: unset means top-level, diverging from actions/checkout,
// because turnip clones specifically to run IaC that may reference
// submodule paths.
func TestParseSubmodules_UnsetDefaultsToTopLevel(t *testing.T) {
	mode, err := parseSubmodules("")

	require.NoError(t, err)
	assert.Equal(t, config.SubmodulesTopLevel, mode)
}

func TestParseSubmodules_AcceptsEveryKnownMode(t *testing.T) {
	for _, known := range []string{config.SubmodulesNone, config.SubmodulesTopLevel, config.SubmodulesRecursive} {
		mode, err := parseSubmodules(known)

		require.NoErrorf(t, err, "mode %q should be accepted", known)
		assert.Equal(t, known, mode)
	}
}

// Requirement 2.2: an unrecognised value is a startup error rather than a
// silently ignored setting. "shallow" specifically, because it is the name
// the requirements originally used and the one an operator is most likely
// to reach for out of habit.
func TestParseSubmodules_RejectsAnUnknownMode(t *testing.T) {
	_, err := parseSubmodules("shallow")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "shallow")
	assert.Contains(t, err.Error(), config.SubmodulesRecursive,
		"the error lists the modes that would have worked")
}

func TestResolveSubmodules_ServerDefaultAppliesWhenTheRepositoryIsSilent(t *testing.T) {
	mode, err := resolveSubmodules(config.CloneSpec{}, config.SubmodulesNone, defaultAllowedOverrides())

	require.NoError(t, err)
	assert.Equal(t, config.SubmodulesNone, mode)
}

// Requirement 2.4: the override goes through the existing gate.
func TestResolveSubmodules_OverrideIsRefusedWhenNotPermitted(t *testing.T) {
	_, err := resolveSubmodules(
		config.CloneSpec{Submodules: config.SubmodulesRecursive},
		config.SubmodulesTopLevel,
		defaultAllowedOverrides(),
	)

	require.Error(t, err)
	var notPermitted *SubmodulesNotPermittedError
	require.ErrorAs(t, err, &notPermitted)
	assert.Contains(t, err.Error(), overrideCloneSubmodules)
	assert.Contains(t, err.Error(), "TURNIP_ALLOWED_OVERRIDES",
		"the refusal tells the operator how to permit it")
}

func TestResolveSubmodules_OverrideIsHonouredWhenPermitted(t *testing.T) {
	allowed, err := parseAllowedOverrides(overrideCloneSubmodules)
	require.NoError(t, err)

	mode, err := resolveSubmodules(
		config.CloneSpec{Submodules: config.SubmodulesRecursive},
		config.SubmodulesTopLevel,
		allowed,
	)

	require.NoError(t, err)
	assert.Equal(t, config.SubmodulesRecursive, mode)
}

// Decision 6: the repository override is opt-in. Registering a new path
// must not quietly enable it for deployments that never asked.
func TestDefaultAllowedOverrides_DoesNotPermitCloneSubmodules(t *testing.T) {
	assert.False(t, defaultAllowedOverrides()[overrideCloneSubmodules],
		"the default allowed set must stay empty")
}

func TestParseAllowedOverrides_AcceptsTheNewPathAndStillRejectsUnknownOnes(t *testing.T) {
	allowed, err := parseAllowedOverrides("clone.submodules,runner.serviceAccount")
	require.NoError(t, err)
	assert.True(t, allowed[overrideCloneSubmodules])
	assert.True(t, allowed[overrideServiceAccount])

	_, err = parseAllowedOverrides("clone.submodule")
	require.Error(t, err, "a near-miss path must not be accepted as a no-op")
}

// Requirement 2.2, through the real startup path rather than the parser
// alone: an unrecognised value must stop the Server rather than be
// silently ignored.
func TestConfigFromEnv_UnknownCloneSubmodulesIsAStartupError(t *testing.T) {
	_, err := ConfigFromEnv(envMap(map[string]string{"TURNIP_CLONE_SUBMODULES": "shallow"}))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "TURNIP_CLONE_SUBMODULES")
	assert.Contains(t, err.Error(), "shallow")
}

func TestConfigFromEnv_CloneSubmodulesDefaultsToTopLevel(t *testing.T) {
	cfg, err := ConfigFromEnv(envMap(nil))

	require.NoError(t, err)
	assert.Equal(t, config.SubmodulesTopLevel, cfg.CloneSubmodules)
}

func TestConfigFromEnv_CloneSubmodulesFromTheEnvironment(t *testing.T) {
	cfg, err := ConfigFromEnv(envMap(map[string]string{"TURNIP_CLONE_SUBMODULES": config.SubmodulesNone}))

	require.NoError(t, err)
	assert.Equal(t, config.SubmodulesNone, cfg.CloneSubmodules)
}
