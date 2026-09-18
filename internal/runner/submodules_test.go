package runner

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivanvc/turnip/internal/config"
)

// allowFileTransport lets git clone a submodule from a local path.
//
// git refuses the `file` transport for submodules as CVE-2022-39253
// hardening, and the refusal covers `submodule update --init`, not merely
// `submodule add`. Real submodule URLs are https:// — relative ones
// included, since they resolve against the parent's authenticated remote —
// so turnip's own clone path must never set this. It belongs in the test
// only, which is why it is granted through a throwaway global config file
// rather than anywhere near initSubmodules.
func allowFileTransport(t *testing.T) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "gitconfig")
	require.NoError(t, os.WriteFile(path, []byte("[protocol \"file\"]\n\tallow = always\n"), 0o644))
	t.Setenv("GIT_CONFIG_GLOBAL", path)
}

func gitInDir(t *testing.T, dir string, args ...string) string {
	t.Helper()

	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=turnip-test", "GIT_AUTHOR_EMAIL=turnip-test@example.com",
		"GIT_COMMITTER_NAME=turnip-test", "GIT_COMMITTER_EMAIL=turnip-test@example.com",
	)
	out, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "git %s: %s", strings.Join(args, " "), out)
	return strings.TrimSpace(string(out))
}

// newSubmoduleFixture builds a parent repository carrying one submodule
// that points at a second local repository, and returns the parent's path
// and the commit that records the submodule.
func newSubmoduleFixture(t *testing.T) (parentDir, headSHA string) {
	t.Helper()
	allowFileTransport(t)

	childDir := t.TempDir()
	gitInDir(t, childDir, "init", "-q", "-b", "main")
	require.NoError(t, os.WriteFile(filepath.Join(childDir, "chart.yaml"), []byte("from-submodule"), 0o644))
	gitInDir(t, childDir, "add", "chart.yaml")
	gitInDir(t, childDir, "commit", "-qm", "child")

	parentDir = t.TempDir()
	gitInDir(t, parentDir, "init", "-q", "-b", "main")
	require.NoError(t, os.WriteFile(filepath.Join(parentDir, "root.txt"), []byte("root"), 0o644))
	gitInDir(t, parentDir, "add", "root.txt")
	gitInDir(t, parentDir, "commit", "-qm", "root")

	gitInDir(t, parentDir, "-c", "protocol.file.allow=always", "submodule", "add", "-q", childDir, "sub")
	gitInDir(t, parentDir, "commit", "-qm", "add submodule")
	headSHA = gitInDir(t, parentDir, "rev-parse", "HEAD")

	return parentDir, headSHA
}

// Requirement 1.1: the submodule's content is actually present afterwards —
// the whole point of the slice, and the failure that reached the pilot as a
// tool reading through an empty directory.
func TestClone_InitialisesSubmodules(t *testing.T) {
	parentDir, headSHA := newSubmoduleFixture(t)

	dest := filepath.Join(t.TempDir(), "checkout")
	require.NoError(t, Clone(context.Background(), dest, parentDir, headSHA, "", "", config.SubmodulesTopLevel))

	content, err := os.ReadFile(filepath.Join(dest, "sub", "chart.yaml"))
	require.NoError(t, err, "submodule was not checked out")
	assert.Equal(t, "from-submodule", string(content))
}

// Requirement 2.2 / Decision 5: "configured off" must be distinguishable
// from "failed to fetch" — none leaves the directory empty and succeeds.
func TestClone_ModeNoneLeavesSubmoduleEmptyWithoutFailing(t *testing.T) {
	parentDir, headSHA := newSubmoduleFixture(t)

	dest := filepath.Join(t.TempDir(), "checkout")
	require.NoError(t, Clone(context.Background(), dest, parentDir, headSHA, "", "", config.SubmodulesNone))

	assert.NoFileExists(t, filepath.Join(dest, "sub", "chart.yaml"),
		"mode none must not fetch the submodule")
	assert.DirExists(t, dest, "the parent checkout itself still succeeded")
}

// An empty mode means top-level, not off: a Job built by an older Server
// carries no mode, and defaulting it off would silently reintroduce the
// empty-directory failure.
func TestClone_EmptyModeDefaultsToTopLevel(t *testing.T) {
	parentDir, headSHA := newSubmoduleFixture(t)

	dest := filepath.Join(t.TempDir(), "checkout")
	require.NoError(t, Clone(context.Background(), dest, parentDir, headSHA, "", "", ""))

	assert.FileExists(t, filepath.Join(dest, "sub", "chart.yaml"))
}

// Requirement 1.3: a repository with no .gitmodules is a no-op, not a
// failure. The existing fixtures have no submodules, so this pins the
// behaviour explicitly rather than relying on them incidentally.
func TestClone_NoSubmodulesIsANoOp(t *testing.T) {
	repoDir, firstSHA, _ := newGitFixture(t)

	dest := filepath.Join(t.TempDir(), "checkout")
	require.NoError(t, Clone(context.Background(), dest, repoDir, firstSHA, "", "", config.SubmodulesRecursive))

	assert.FileExists(t, filepath.Join(dest, "marker.txt"))
}

// recordingGit is a gitRunner that captures every invocation and replies
// from a canned table, so a test can assert what was *not* run.
func recordingGit(reply func(args []string) ([]byte, error)) (gitRunner, *[][]string) {
	var calls [][]string
	run := func(_ context.Context, _, _ string, args, _ []string) ([]byte, error) {
		calls = append(calls, args)
		return reply(args)
	}
	return run, &calls
}

func writeGitmodules(t *testing.T, dir string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".gitmodules"), []byte("placeholder"), 0o644))
}

// Requirement 4.4: a submodule on a host the token cannot authenticate is
// reported by name, before anything is fetched.
func TestInitSubmodules_ForeignHostIsReportedBeforeAnyFetch(t *testing.T) {
	dir := t.TempDir()
	writeGitmodules(t, dir)

	run, calls := recordingGit(func(args []string) ([]byte, error) {
		if slices.Contains(args, "config") {
			return []byte("submodule.charts.url https://gitlab.com/other/charts\n"), nil
		}
		return nil, errors.New("should not have been called")
	})

	err := initSubmodules(context.Background(), run, dir, "https://github.com/owner/repo", "tok", config.SubmodulesTopLevel)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "charts", "the failure names the submodule")
	assert.Contains(t, err.Error(), "gitlab.com", "the failure names the host")
	for _, args := range *calls {
		assert.NotContains(t, args, "update", "no fetch may be attempted for an unauthenticatable submodule")
	}
}

// Requirement 4.3, in the submodule path specifically: git's own output can
// echo a rewritten URL carrying the token, and it must not reach the error.
func TestInitSubmodules_TokenNeverAppearsInAFailure(t *testing.T) {
	const token = "ghs_exampletokenvalue"
	dir := t.TempDir()
	writeGitmodules(t, dir)

	run, _ := recordingGit(func(args []string) ([]byte, error) {
		if slices.Contains(args, "config") {
			return []byte("submodule.sub.url https://github.com/owner/charts\n"), nil
		}
		// git echoing an authenticated URL back in its failure output.
		return []byte("fatal: could not read https://x-access-token:" + token + "@github.com/owner/charts"),
			errors.New("exit status 128")
	})

	err := initSubmodules(context.Background(), run, dir, "https://github.com/owner/repo", token, config.SubmodulesTopLevel)

	require.Error(t, err)
	assert.NotContains(t, err.Error(), token, "the installation token leaked into a reported error")
}

func TestInitSubmodules_RecursiveAddsTheFlag(t *testing.T) {
	dir := t.TempDir()
	writeGitmodules(t, dir)

	run, calls := recordingGit(func(args []string) ([]byte, error) {
		if slices.Contains(args, "config") {
			return []byte("submodule.sub.url https://github.com/owner/charts\n"), nil
		}
		return nil, nil
	})

	require.NoError(t, initSubmodules(context.Background(), run, dir, "https://github.com/owner/repo", "tok", config.SubmodulesRecursive))

	var update []string
	for _, args := range *calls {
		if slices.Contains(args, "update") {
			update = args
		}
	}
	require.NotNil(t, update, "submodule update was never run")
	assert.Contains(t, update, "--recursive")
	assert.NotContains(t, update, "--depth", "submodules are fetched at full depth (Requirement 3.3)")
}

// Requirement 3.5 and Decision 3: host extraction must agree with git about
// which URL forms name a host, including the scp-like form and the local
// paths it must not be confused with.
func TestGitURLHost(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want string
	}{
		{"https://github.com/o/r", "github.com"},
		{"http://github.com/o/r", "github.com"},
		{"git://github.com/o/r", "github.com"},
		{"ssh://git@github.com/o/r", "github.com"},
		{"ssh://git@github.com:22/o/r", "github.com"},
		{"git@github.com:o/r", "github.com"},
		{"github.com:o/r", "github.com"},
		{"https://gitlab.com/o/r", "gitlab.com"},
		// No host: git resolves these against the parent's remote, which
		// already carries the token.
		{"../helm-charts", ""},
		{"./foo:bar", ""},
		{"/absolute/path", ""},
	} {
		assert.Equalf(t, tc.want, gitURLHost(tc.raw), "gitURLHost(%q)", tc.raw)
	}
}

// The assertion that pins the *derivation*: a fixed prefix list passes the
// plain SSH case and fails this one, so without an explicit port the
// central claim of Decision 3 would go untested.
func TestAuthenticatedHTTPS_RewritesEveryFormIncludingAnExplicitPort(t *testing.T) {
	const token = "ghs_exampletokenvalue"
	want := "https://x-access-token:" + token + "@github.com/owner/charts"

	for _, raw := range []string{
		"https://github.com/owner/charts",
		"http://github.com/owner/charts",
		"git://github.com/owner/charts",
		"ssh://git@github.com/owner/charts",
		"ssh://git@github.com:22/owner/charts",
		"git@github.com:owner/charts",
		"github.com:owner/charts",
	} {
		assert.Equalf(t, want, authenticatedHTTPS(raw, token), "authenticatedHTTPS(%q)", raw)
	}

	assert.Empty(t, authenticatedHTTPS("../helm-charts", token),
		"a relative URL names no host and needs no rewrite")
}

func TestSubmoduleConfigEnv_DerivesAnExactRewritePerURL(t *testing.T) {
	const token = "ghs_exampletokenvalue"
	urls := []submoduleURL{{name: "charts", url: "ssh://git@github.com:22/owner/charts"}}

	env := submoduleConfigEnv(urls, "github.com", token)

	require.NotEmpty(t, env)
	// One derived entry plus the five nested-submodule prefixes.
	assert.Equal(t, "GIT_CONFIG_COUNT=6", env[0])
	assert.Len(t, env, 1+2*6, "each entry contributes a KEY and a VALUE")

	joined := strings.Join(env, "\n")
	assert.Contains(t, joined,
		"GIT_CONFIG_KEY_0=url.https://x-access-token:"+token+"@github.com/owner/charts.insteadOf")
	assert.Contains(t, joined, "GIT_CONFIG_VALUE_0=ssh://git@github.com:22/owner/charts",
		"the rewrite is keyed on the exact URL found, which a prefix rule would miss")
}

func TestSubmoduleConfigEnv_NoTokenMeansNoRewrites(t *testing.T) {
	urls := []submoduleURL{{name: "charts", url: "https://github.com/owner/charts"}}

	assert.Nil(t, submoduleConfigEnv(urls, "github.com", ""))
	assert.Nil(t, submoduleConfigEnv(urls, "", "tok"))
}

// newSubmoduleMergeFixture is newSubmoduleFixture plus a diverging base
// branch, so the clone actually performs a merge before reaching the
// submodule step.
func newSubmoduleMergeFixture(t *testing.T) (parentDir, headSHA, baseRef string) {
	t.Helper()
	allowFileTransport(t)

	childDir := t.TempDir()
	gitInDir(t, childDir, "init", "-q", "-b", "main")
	require.NoError(t, os.WriteFile(filepath.Join(childDir, "chart.yaml"), []byte("from-submodule"), 0o644))
	gitInDir(t, childDir, "add", "chart.yaml")
	gitInDir(t, childDir, "commit", "-qm", "child")

	parentDir = t.TempDir()
	gitInDir(t, parentDir, "init", "-q", "-b", "main")
	require.NoError(t, os.WriteFile(filepath.Join(parentDir, "root.txt"), []byte("root"), 0o644))
	gitInDir(t, parentDir, "add", "root.txt")
	gitInDir(t, parentDir, "commit", "-qm", "root")
	gitInDir(t, parentDir, "-c", "protocol.file.allow=always", "submodule", "add", "-q", childDir, "sub")
	gitInDir(t, parentDir, "commit", "-qm", "add submodule")

	gitInDir(t, parentDir, "checkout", "-q", "-b", "feature")
	require.NoError(t, os.WriteFile(filepath.Join(parentDir, "feature.txt"), []byte("from-feature"), 0o644))
	gitInDir(t, parentDir, "add", "feature.txt")
	gitInDir(t, parentDir, "commit", "-qm", "feature change")
	headSHA = gitInDir(t, parentDir, "rev-parse", "HEAD")

	gitInDir(t, parentDir, "checkout", "-q", "main")
	require.NoError(t, os.WriteFile(filepath.Join(parentDir, "base.txt"), []byte("from-main"), 0o644))
	gitInDir(t, parentDir, "add", "base.txt")
	gitInDir(t, parentDir, "commit", "-qm", "main change")

	return parentDir, headSHA, "main"
}

// Requirement 1.2 / Decision 2: initialisation runs after the base-branch
// merge, because it is the merged tree's gitlinks that record which
// submodule commits belong to it. Every other submodule test here skips the
// merge entirely, so without this the ordering claim is untested.
func TestClone_InitialisesSubmodulesAfterTheMerge(t *testing.T) {
	parentDir, headSHA, baseRef := newSubmoduleMergeFixture(t)

	dest := filepath.Join(t.TempDir(), "checkout")
	require.NoError(t, Clone(context.Background(), dest, parentDir, headSHA, baseRef, "", config.SubmodulesTopLevel))

	// The merge really happened...
	assert.FileExists(t, filepath.Join(dest, "feature.txt"))
	assert.FileExists(t, filepath.Join(dest, "base.txt"))

	// ...and the submodule was still checked out afterwards.
	content, err := os.ReadFile(filepath.Join(dest, "sub", "chart.yaml"))
	require.NoError(t, err, "submodule was not checked out after the merge")
	assert.Equal(t, "from-submodule", string(content))
}

// Requirements 4.1 and 4.2: a submodule that cannot be fetched fails the
// clone, and git's own message — which names the submodule — survives to
// the caller. The alternative is the empty directory that reached the pilot
// as a tool complaining about a path it knew nothing about.
func TestInitSubmodules_FetchFailureFailsTheCloneNamingTheSubmodule(t *testing.T) {
	dir := t.TempDir()
	writeGitmodules(t, dir)

	run, _ := recordingGit(func(args []string) ([]byte, error) {
		if slices.Contains(args, "config") {
			return []byte("submodule.charts.url https://github.com/owner/charts\n"), nil
		}
		return []byte("fatal: clone of 'https://github.com/owner/charts' into submodule path 'charts' failed\n" +
			"Failed to clone 'charts' a second time, aborting"), errors.New("exit status 1")
	})

	err := initSubmodules(context.Background(), run, dir, "https://github.com/owner/repo", "tok", config.SubmodulesTopLevel)

	require.Error(t, err, "an unfetchable submodule must fail the clone, not be skipped")
	assert.Contains(t, err.Error(), "charts", "git's message names the submodule and must reach the caller")
}

// Found in the pilot: GitHub answers "Repository not found" both for a
// repository that does not exist and for one the credential cannot see, so
// the bare message sends the reader to check a spelling that is correct.
// The fetch was already authenticated by this point, so turnip names the
// likely cause instead.
func TestInitSubmodules_NotFoundSuggestsTheAppIsNotInstalled(t *testing.T) {
	dir := t.TempDir()
	writeGitmodules(t, dir)

	run, _ := recordingGit(func(args []string) ([]byte, error) {
		if slices.Contains(args, "config") {
			return []byte("submodule.charts.url https://github.com/owner/charts\n"), nil
		}
		return []byte("remote: Repository not found.\n" +
			"fatal: repository 'https://github.com/owner/charts/' not found"), errors.New("exit status 1")
	})

	err := initSubmodules(context.Background(), run, dir, "https://github.com/owner/repo", "tok", config.SubmodulesTopLevel)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not installed",
		"a 404 on an authenticated fetch should point at the App's repository access")
}

// The hint must not attach itself to unrelated failures.
func TestInitSubmodules_OtherFailuresCarryNoAccessHint(t *testing.T) {
	dir := t.TempDir()
	writeGitmodules(t, dir)

	run, _ := recordingGit(func(args []string) ([]byte, error) {
		if slices.Contains(args, "config") {
			return []byte("submodule.charts.url https://github.com/owner/charts\n"), nil
		}
		return []byte("fatal: unable to access: server certificate verification failed"), errors.New("exit status 1")
	})

	err := initSubmodules(context.Background(), run, dir, "https://github.com/owner/repo", "tok", config.SubmodulesTopLevel)

	require.Error(t, err)
	assert.NotContains(t, err.Error(), "not installed")
}
