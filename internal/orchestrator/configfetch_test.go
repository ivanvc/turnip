package orchestrator

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivanvc/turnip/internal/config"
)

// The missing-config reply uses GitHub's alert syntax so it renders as a
// highlighted "Warning" callout. That formatting breaks silently — into
// ordinary quoted text — if the "> " prefix is dropped from any line,
// including the marker line, so this asserts the exact body.
func TestConfigErrorComment_MissingIsAGitHubWarningAlert(t *testing.T) {
	body := configErrorComment(fmt.Errorf("%w: owner/repo@abc123", ErrConfigMissing))

	assert.Equal(t,
		"> [!WARNING]\n"+
			"> `turnip.yaml` was not found in this repository (checked the repository root and `.github/turnip.yaml`).",
		body)

	for _, line := range strings.Split(body, "\n") {
		assert.Truef(t, strings.HasPrefix(line, "> "), "every line must stay inside the blockquote, got %q", line)
	}
}

// An invalid config is the author's to fix and leaves nothing behind, so
// it ranks WARNING — CAUTION stays reserved for failures needing an
// operator, which is what makes it worth noticing.
func TestConfigErrorComment_InvalidConfigIsAWarningWithDetailsOutsideTheAlert(t *testing.T) {
	_, err := config.Parse([]byte("schemaVersion: v1alpha1\nprojects:\n  - name: broken\n"))
	require.Error(t, err, "a project with no directory or tool is invalid")

	body := configErrorComment(err)
	assert.Contains(t, body, "> [!WARNING]")
	assert.Contains(t, body, "is invalid")
	assert.Contains(t, body, "directory", "the underlying validation errors are included")
	assertNothingNestedInsideAlert(t, body)
}

// A failed fetch is an auth/permission/rate-limit problem with turnip's
// own installation — not something the PR author can fix — so it ranks
// CAUTION.
func TestConfigErrorComment_FetchFailureIsACaution(t *testing.T) {
	body := configErrorComment(errors.New("rate limit exceeded"))

	assert.Contains(t, body, "> [!CAUTION]")
	assert.Contains(t, body, "Fetching `turnip.yaml` failed")
	assert.Contains(t, body, "rate limit exceeded", "the cause must be readable without pod/log access")
	assertNothingNestedInsideAlert(t, body)
}

// GitHub alerts don't render when another element is nested inside them:
// a code fence or <details> indented into the blockquote degrades the
// whole block into literal "[!WARNING]" text. Detail blocks must follow
// the alert, never sit within it.
func assertNothingNestedInsideAlert(t *testing.T, body string) {
	t.Helper()
	for _, line := range strings.Split(body, "\n") {
		if !strings.HasPrefix(line, ">") {
			continue
		}
		assert.NotContainsf(t, line, "```", "code fence nested inside the alert: %q", line)
		assert.NotContainsf(t, line, "<details", "details block nested inside the alert: %q", line)
	}
}
