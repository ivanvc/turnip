package runner

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

// commitSequenceFixture creates a local git repository with n commits,
// each writing a distinct, content-addressable marker file, and returns
// the repo's path plus each commit's SHA in order.
func commitSequenceFixture(tb testing.TB, n int) (repoDir string, shas []string) {
	repoDir = tb.TempDir()

	run := func(args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=turnip-test", "GIT_AUTHOR_EMAIL=turnip-test@example.com",
			"GIT_COMMITTER_NAME=turnip-test", "GIT_COMMITTER_EMAIL=turnip-test@example.com",
		)
		out, err := cmd.CombinedOutput()
		require.NoErrorf(tb, err, "git %v: %s", args, out)
		return string(out)
	}

	run("init", "-b", "main")
	shas = make([]string, n)
	for i := range n {
		require.NoError(tb, os.WriteFile(filepath.Join(repoDir, "marker.txt"), []byte("commit-"+strconv.Itoa(i)), 0o644))
		run("add", "marker.txt")
		run("commit", "-m", "commit "+strconv.Itoa(i))
		sha := run("rev-parse", "HEAD")
		shas[i] = sha[:len(sha)-1] // trim trailing newline
	}
	return repoDir, shas
}

// Feature: multi-iac-automation-platform, Property 24: Runner Clones Correct Commit
func TestProperty_RunnerClonesCorrectCommit(t *testing.T) {
	const commitCount = 5
	repoDir, shas := commitSequenceFixture(t, commitCount)

	rapid.Check(t, func(rt *rapid.T) {
		i := rapid.IntRange(0, commitCount-1).Draw(rt, "commitIndex")
		dest := filepath.Join(t.TempDir(), "checkout")

		require.NoError(rt, Clone(context.Background(), dest, repoDir, shas[i], ""))

		content, err := os.ReadFile(filepath.Join(dest, "marker.txt"))
		require.NoError(rt, err)
		require.Equal(rt, "commit-"+strconv.Itoa(i), string(content))
	})
}
