package runner

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Requirement 4.3: neither the authenticated remote URL nor the raw
// installation token may reach a reported message. The token is a needle
// in its own right because submodule authentication carries it inside a
// larger string, which a whole-argument comparison against the remote URL
// would not catch.

func TestRedact_StripsTokenInsideLargerString(t *testing.T) {
	const token = "ghs_exampletokenvalue"
	authed := "https://x-access-token:" + token + "@github.com/owner/repo"

	// A submodule rewrite value: the token is present, but the string is
	// not the parent's remote URL, so the authedURL needle alone misses it.
	rewrite := "url." + authed + ".insteadOf=ssh://git@github.com:22/owner/repo"

	got := redact(rewrite, authed, token)

	assert.NotContains(t, got, token, "token survived redaction inside a larger string")
	assert.Contains(t, got, redactedRemote)
}

func TestRedact_StripsWholeAuthenticatedURL(t *testing.T) {
	const token = "ghs_exampletokenvalue"
	authed := "https://x-access-token:" + token + "@github.com/owner/repo"

	got := redact("fatal: could not read from "+authed, authed, token)

	assert.NotContains(t, got, token)
	assert.Contains(t, got, redactedRemote)
}

// An empty secret must be skipped, not replaced: strings.ReplaceAll with an
// empty old value inserts the placeholder between every character. Both
// secrets are legitimately empty whenever the remote needs no credential,
// which is the case across most of this package's own tests.
func TestRedact_EmptySecretsLeaveTheStringUntouched(t *testing.T) {
	const s = "runner: clone: git init /turnip/src"

	assert.Equal(t, s, redact(s, "", ""))
	assert.Equal(t, s, redact(s, "https://github.com/owner/repo", ""))
	assert.Empty(t, redact("", "", ""))
}

func TestRedactArgs_RedactsWholeArgumentsAndSubstrings(t *testing.T) {
	const token = "ghs_exampletokenvalue"
	authed := "https://x-access-token:" + token + "@github.com/owner/repo"

	args := []string{
		"remote", "add", "origin", authed,
		"url." + authed + ".insteadOf",
	}

	got := redactArgs(args, authed, token)

	assert.Equal(t, []string{
		"remote", "add", "origin", redactedRemote,
		"url." + redactedRemote + ".insteadOf",
	}, got)
	for _, a := range got {
		assert.NotContains(t, a, token)
	}
}

func TestRedactArgs_EmptySecretsLeaveArgsUnchanged(t *testing.T) {
	args := []string{"init", "/turnip/src"}

	assert.Equal(t, args, redactArgs(args, "", ""))
}

// redactArgs must not mutate its input: the caller still holds the real
// arguments, and a git step that is retried (the unshallow fallback) would
// otherwise be retried with redacted placeholders instead of a usable URL.
func TestRedactArgs_DoesNotMutateInput(t *testing.T) {
	const token = "ghs_exampletokenvalue"
	authed := "https://x-access-token:" + token + "@github.com/owner/repo"

	args := []string{"remote", "add", "origin", authed}
	_ = redactArgs(args, authed, token)

	assert.Equal(t, authed, args[3], "redactArgs mutated the caller's slice")
}
