package runner

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newGitFixture creates a local git repository with two commits, each
// writing a distinct marker file, and returns the repo's path plus both
// commit SHAs in order.
func newGitFixture(t *testing.T) (repoDir string, firstSHA, secondSHA string) {
	t.Helper()
	repoDir = t.TempDir()

	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=turnip-test", "GIT_AUTHOR_EMAIL=turnip-test@example.com",
			"GIT_COMMITTER_NAME=turnip-test", "GIT_COMMITTER_EMAIL=turnip-test@example.com",
		)
		out, err := cmd.CombinedOutput()
		require.NoErrorf(t, err, "git %s: %s", strings.Join(args, " "), out)
		return strings.TrimSpace(string(out))
	}

	run("init", "-b", "main")
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "marker.txt"), []byte("first"), 0o644))
	run("add", "marker.txt")
	run("commit", "-m", "first")
	firstSHA = run("rev-parse", "HEAD")

	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "marker.txt"), []byte("second"), 0o644))
	run("add", "marker.txt")
	run("commit", "-m", "second")
	secondSHA = run("rev-parse", "HEAD")

	return repoDir, firstSHA, secondSHA
}

func TestClone_ChecksOutExactCommit(t *testing.T) {
	repoDir, firstSHA, secondSHA := newGitFixture(t)

	dest := filepath.Join(t.TempDir(), "checkout")
	require.NoError(t, Clone(context.Background(), dest, repoDir, firstSHA, ""))

	content, err := os.ReadFile(filepath.Join(dest, "marker.txt"))
	require.NoError(t, err)
	assert.Equal(t, "first", string(content))

	dest2 := filepath.Join(t.TempDir(), "checkout2")
	require.NoError(t, Clone(context.Background(), dest2, repoDir, secondSHA, ""))

	content2, err := os.ReadFile(filepath.Join(dest2, "marker.txt"))
	require.NoError(t, err)
	assert.Equal(t, "second", string(content2))
}

func TestClone_NonexistentCommitFails(t *testing.T) {
	repoDir, _, _ := newGitFixture(t)

	dest := filepath.Join(t.TempDir(), "checkout")
	err := Clone(context.Background(), dest, repoDir, "0000000000000000000000000000000000000000", "")
	require.Error(t, err)
}

func TestClone_TokenNeverAppearsInErrors(t *testing.T) {
	const token = "super-secret-token"

	// A file:// URL to a nonexistent path is a well-formed remote (so
	// embedToken's userinfo insertion is well-defined) that fails purely
	// locally, with no network access required.
	badRemote := "file://" + filepath.Join(t.TempDir(), "does-not-exist.git")

	dest := filepath.Join(t.TempDir(), "checkout")
	err := Clone(context.Background(), dest, badRemote, "abc123", token)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), token)
}
