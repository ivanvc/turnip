package orchestrator

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivanvc/turnip/internal/reftable"
)

// serverSettingsReferencePath is the Reference table checked against what
// ConfigFromEnv actually reads. Relative to this package's directory.
const serverSettingsReferencePath = "../../docs/operating/server-settings.md"

// recordingGetenv wraps base, returning a getenv that also records every
// name it is asked for (including one asked for more than once, and one
// whose value comes back empty). Passing base's own env values through
// keeps ConfigFromEnv's parsing on the same path base would have produced,
// so it reads every optional setting too rather than stopping at the
// first missing required one.
func recordingGetenv(base func(string) string) (getenv func(string) string, seen func() map[string]bool) {
	names := map[string]bool{}
	return func(key string) string {
			names[key] = true
			return base(key)
		}, func() map[string]bool {
			return names
		}
}

// Feature: docs-onboarding, Requirement 4.2/4.3: the Server settings
// Reference lists every TURNIP_* variable ConfigFromEnv reads, and gaining
// one without a row (or a row for a name it never reads) fails the build.
//
// envMap(nil) sets every required variable, so ConfigFromEnv gets past its
// "missing required variable(s)" return and reaches the optional reads
// after it. What is recorded is what that one run asks for: a setting
// read only on a branch envMap(nil) does not take would not be recorded
// (TURNIP_GITHUB_PRIVATE_KEY, for one, is read because envMap leaves
// TURNIP_GITHUB_PRIVATE_KEY_PATH empty).
func TestServerSettingsReference_MatchesConfigFromEnv(t *testing.T) {
	getenv, seen := recordingGetenv(envMap(nil))

	_, err := ConfigFromEnv(getenv, NewPluginRegistry())
	require.NoError(t, err)

	docs, err := reftable.FirstColumnNames(serverSettingsReferencePath)
	require.NoError(t, err)

	var read []string
	for name := range seen() {
		read = append(read, name)
	}

	missing, extra := reftable.Diff(docs, read)
	assert.Empty(t, missing, "%s is missing a row for: %v", serverSettingsReferencePath, missing)
	assert.Empty(t, extra, "%s has a row naming a variable ConfigFromEnv never reads: %v", serverSettingsReferencePath, extra)
}
