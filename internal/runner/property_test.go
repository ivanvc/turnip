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

		require.NoError(rt, Clone(context.Background(), dest, repoDir, shas[i], "", ""))

		content, err := os.ReadFile(filepath.Join(dest, "marker.txt"))
		require.NoError(rt, err)
		require.Equal(rt, "commit-"+strconv.Itoa(i), string(content))
	})
}

// divergingFixture creates a local git repository with a "main" branch
// (baseCount sequential commits, each writing distinct content to
// base-marker.txt) and a "feature" branch forked from main's first commit
// (headCount sequential commits, each writing distinct content to
// head-marker.txt, disjoint from base-marker.txt so every combination
// merges cleanly). It returns the repo path, feature's commit SHAs in
// order, and main's tip content.
func divergingFixture(tb testing.TB, baseCount, headCount int) (repoDir string, headSHAs []string, baseTipContent string) {
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
	require.NoError(tb, os.WriteFile(filepath.Join(repoDir, "root.txt"), []byte("root"), 0o644))
	run("add", "root.txt")
	run("commit", "-m", "root")

	run("checkout", "-b", "feature")
	headSHAs = make([]string, headCount)
	for i := range headCount {
		content := "head-" + strconv.Itoa(i)
		require.NoError(tb, os.WriteFile(filepath.Join(repoDir, "head-marker.txt"), []byte(content), 0o644))
		run("add", "head-marker.txt")
		run("commit", "-m", "head "+strconv.Itoa(i))
		sha := run("rev-parse", "HEAD")
		headSHAs[i] = sha[:len(sha)-1] // trim trailing newline
	}

	run("checkout", "main")
	for i := range baseCount {
		baseTipContent = "base-" + strconv.Itoa(i)
		require.NoError(tb, os.WriteFile(filepath.Join(repoDir, "base-marker.txt"), []byte(baseTipContent), 0o644))
		run("add", "base-marker.txt")
		run("commit", "-m", "base "+strconv.Itoa(i))
	}

	return repoDir, headSHAs, baseTipContent
}

// Feature: multi-iac-automation-platform, Property 24: Runner Clones
// Correct Commit — reinterpreted per requirements.md's Introduction note:
// Clone now also merges baseRef in, so the property this asserts is that
// the resulting tree contains both the exact head commit's unique content
// and the base branch's current tip content, for any head commit merged
// against the same base.
func TestProperty_RunnerClonesCorrectCommit_WithBaseMerge(t *testing.T) {
	const headCount = 5
	repoDir, headSHAs, baseTipContent := divergingFixture(t, 3, headCount)

	rapid.Check(t, func(rt *rapid.T) {
		i := rapid.IntRange(0, headCount-1).Draw(rt, "headCommitIndex")
		dest := filepath.Join(t.TempDir(), "checkout")

		require.NoError(rt, Clone(context.Background(), dest, repoDir, headSHAs[i], "main", ""))

		headContent, err := os.ReadFile(filepath.Join(dest, "head-marker.txt"))
		require.NoError(rt, err)
		require.Equal(rt, "head-"+strconv.Itoa(i), string(headContent))

		baseContent, err := os.ReadFile(filepath.Join(dest, "base-marker.txt"))
		require.NoError(rt, err)
		require.Equal(rt, baseTipContent, string(baseContent))
	})
}
