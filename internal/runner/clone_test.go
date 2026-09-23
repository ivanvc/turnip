package runner

import (
	"context"
	"fmt"
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
	require.NoError(t, Clone(context.Background(), dest, repoDir, firstSHA, "", ""))

	content, err := os.ReadFile(filepath.Join(dest, "marker.txt"))
	require.NoError(t, err)
	assert.Equal(t, "first", string(content))

	dest2 := filepath.Join(t.TempDir(), "checkout2")
	require.NoError(t, Clone(context.Background(), dest2, repoDir, secondSHA, "", ""))

	content2, err := os.ReadFile(filepath.Join(dest2, "marker.txt"))
	require.NoError(t, err)
	assert.Equal(t, "second", string(content2))
}

func TestClone_NonexistentCommitFails(t *testing.T) {
	repoDir, _, _ := newGitFixture(t)

	dest := filepath.Join(t.TempDir(), "checkout")
	err := Clone(context.Background(), dest, repoDir, "0000000000000000000000000000000000000000", "", "")
	require.Error(t, err)
}

func TestClone_TokenNeverAppearsInErrors(t *testing.T) {
	const token = "super-secret-token"

	// A file:// URL to a nonexistent path is a well-formed remote (so
	// embedToken's userinfo insertion is well-defined) that fails purely
	// locally, with no network access required.
	badRemote := "file://" + filepath.Join(t.TempDir(), "does-not-exist.git")

	dest := filepath.Join(t.TempDir(), "checkout")
	err := Clone(context.Background(), dest, badRemote, "abc123", "", "")
	require.Error(t, err)
	assert.NotContains(t, err.Error(), token)
}

// newDivergingFixture creates a local git repository with a "main" branch
// and a "feature" branch forked from a single root commit. feature gets
// one commit touching feature.txt (or shared.txt, when conflicting is
// true); main advances baseCommits further commits past the fork point,
// the last of which touches base.txt (or shared.txt, when conflicting is
// true, with different content than feature's). It returns the repo path,
// feature's HEAD commit SHA, and "main" — the baseRef Clone should merge
// in.
func newDivergingFixture(t *testing.T, baseCommits int, conflicting bool) (repoDir, headSHA, baseRef string) {
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
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "root.txt"), []byte("root"), 0o644))
	run("add", "root.txt")
	run("commit", "-m", "root")

	run("checkout", "-b", "feature")
	featureFile := "feature.txt"
	if conflicting {
		featureFile = "shared.txt"
	}
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, featureFile), []byte("from-feature"), 0o644))
	run("add", featureFile)
	run("commit", "-m", "feature change")
	headSHA = run("rev-parse", "HEAD")

	run("checkout", "main")
	for i := 0; i < baseCommits-1; i++ {
		run("commit", "--allow-empty", "-m", fmt.Sprintf("filler %d", i))
	}
	if conflicting {
		require.NoError(t, os.WriteFile(filepath.Join(repoDir, "shared.txt"), []byte("from-main"), 0o644))
		run("add", "shared.txt")
	} else {
		require.NoError(t, os.WriteFile(filepath.Join(repoDir, "base.txt"), []byte("from-main"), 0o644))
		run("add", "base.txt")
	}
	run("commit", "-m", "main change")

	return repoDir, headSHA, "main"
}

func TestClone_MergesBaseIntoHead(t *testing.T) {
	repoDir, headSHA, baseRef := newDivergingFixture(t, 1, false)

	dest := filepath.Join(t.TempDir(), "checkout")
	require.NoError(t, Clone(context.Background(), dest, repoDir, headSHA, baseRef, ""))

	featureContent, err := os.ReadFile(filepath.Join(dest, "feature.txt"))
	require.NoError(t, err)
	assert.Equal(t, "from-feature", string(featureContent))

	baseContent, err := os.ReadFile(filepath.Join(dest, "base.txt"))
	require.NoError(t, err)
	assert.Equal(t, "from-main", string(baseContent))
}

func TestClone_EmptyBaseRefSkipsMerge(t *testing.T) {
	repoDir, headSHA, _ := newDivergingFixture(t, 1, false)

	dest := filepath.Join(t.TempDir(), "checkout")
	require.NoError(t, Clone(context.Background(), dest, repoDir, headSHA, "", ""))

	assert.FileExists(t, filepath.Join(dest, "feature.txt"))
	assert.NoFileExists(t, filepath.Join(dest, "base.txt"))
}

func TestClone_MergeConflictReturnsDistinguishableError(t *testing.T) {
	repoDir, headSHA, baseRef := newDivergingFixture(t, 1, true)

	dest := filepath.Join(t.TempDir(), "checkout")
	err := Clone(context.Background(), dest, repoDir, headSHA, baseRef, "")

	require.Error(t, err)
	var conflictErr *MergeConflictError
	require.ErrorAs(t, err, &conflictErr)
	assert.Contains(t, conflictErr.Output, "CONFLICT")
}

func TestClone_FallsBackToUnshallowFetchWhenHistoryDivergesBeyondDepth(t *testing.T) {
	// main advances well past mergeFetchDepth commits beyond the fork
	// point, so the bounded fetch's shallow boundary never reaches it —
	// only the unshallow fallback (Requirement 5.6) can find the shared
	// ancestor.
	repoDir, headSHA, baseRef := newDivergingFixture(t, mergeFetchDepth+10, false)

	dest := filepath.Join(t.TempDir(), "checkout")
	require.NoError(t, Clone(context.Background(), dest, repoDir, headSHA, baseRef, ""))

	featureContent, err := os.ReadFile(filepath.Join(dest, "feature.txt"))
	require.NoError(t, err)
	assert.Equal(t, "from-feature", string(featureContent))

	baseContent, err := os.ReadFile(filepath.Join(dest, "base.txt"))
	require.NoError(t, err)
	assert.Equal(t, "from-main", string(baseContent))
}
