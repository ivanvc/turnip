package runner

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func credentialCfg() Config {
	return Config{
		ServerAddr:  "server:9090",
		OperationID: "op-1",
		RepoURL:     "https://github.com/acme/infra.git",
		TokenFile:   "/nonexistent/token",
	}
}

// git asks every configured helper about every host it talks to. This
// credential is GitHub's and belongs to this Operation, so a submodule
// pointing somewhere else must get silence rather than a token — and
// silence without the Server ever being contacted, since reaching it is
// what would mint one.
func TestRunCredential_AnswersOnlyForTheOperationsHost(t *testing.T) {
	for name, host := range map[string]string{
		"another host entirely": "evil.example.com",
		"a lookalike":           "github.com.evil.example.com",
		"no host at all":        "",
	} {
		t.Run(name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			in := strings.NewReader("protocol=https\nhost=" + host + "\n\n")

			code := RunCredential(context.Background(), credentialCfg(), "get", in, &stdout, &stderr)

			assert.Equal(t, 0, code)
			assert.Empty(t, stdout.String(), "no credential for a host this Operation does not clone from")
			assert.Empty(t, stderr.String(), "and no error either — git moves on to its next helper")
		})
	}
}

// Anything but "get" is answered with silence. turnip has nothing to
// store and nothing to erase, and a helper that failed on them would make
// git treat a successful clone as broken.
func TestRunCredential_IgnoresStoreAndErase(t *testing.T) {
	for _, op := range []string{"store", "erase"} {
		var stdout, stderr bytes.Buffer
		in := strings.NewReader("protocol=https\nhost=github.com\n\n")

		assert.Equal(t, 0, RunCredential(context.Background(), credentialCfg(), op, in, &stdout, &stderr))
		assert.Empty(t, stdout.String())
	}
}

// A matching host with an unreachable Server fails loudly rather than
// answering with an empty password, which git would present to GitHub as
// a credential and have rejected with an unexplained 401.
func TestRunCredential_MatchingHostWithNoServerFails(t *testing.T) {
	var stdout, stderr bytes.Buffer
	in := strings.NewReader("protocol=https\nhost=github.com\n\n")

	code := RunCredential(context.Background(), credentialCfg(), "get", in, &stdout, &stderr)

	assert.Equal(t, 1, code)
	assert.Empty(t, stdout.String(), "no partial credential is emitted")
	assert.Contains(t, stderr.String(), "runner: credential:")
}

func TestParseCredentialRequest(t *testing.T) {
	got := parseCredentialRequest(strings.NewReader(
		"protocol=https\nhost=github.com\npath=acme/infra.git\n\nignored=after-blank\n"))

	assert.Equal(t, map[string]string{
		"protocol": "https",
		"host":     "github.com",
		"path":     "acme/infra.git",
	}, got, "parsing stops at the blank line, as git's protocol specifies")
}

// The helper is pointed at this same binary, and the path is quoted
// because git hands the value to a shell.
func TestCredentialHelperEntry(t *testing.T) {
	e, err := credentialHelperEntry()
	require.NoError(t, err)

	assert.Equal(t, "credential.helper", e.key)
	assert.True(t, strings.HasPrefix(e.value, "!'"), "the shell form, with the path quoted: %q", e.value)
	assert.True(t, strings.HasSuffix(e.value, "' credential"), "invoked as the credential subcommand: %q", e.value)
}

// The quoted path is handed to a shell by git, so it round-trips through
// a real one rather than through an expectation I might have written to
// match the implementation.
func TestShellSingleQuote_RoundTripsThroughARealShell(t *testing.T) {
	for _, path := range []string{
		"/usr/local/bin/runner",
		"/od'd/path",
		"/path with spaces/runner",
		"/$(touch pwned)/runner",
		"/back\\slash/runner",
		"/semi;colon/runner",
	} {
		out, err := exec.Command("sh", "-c", "printf %s "+shellSingleQuote(path)).Output()
		require.NoError(t, err, "path %q", path)
		assert.Equal(t, path, string(out), "the shell must see exactly the path, not an expansion of it")
	}
}
