package runner

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// gitRunner abstracts subprocess execution so tests can substitute a fake
// without invoking a real git binary. It's a small, deliberately
// unshared duplicate of internal/plugin/command.go's commandRunner seam —
// two call sites don't justify a premature cross-package abstraction.
// env carries additional environment entries ("KEY=VALUE") for this one
// subprocess. It exists so submodule authentication can pass the token
// through GIT_CONFIG_* without the token ever appearing in argv, on disk,
// or in the Job spec (Requirement 3.2). A nil env leaves the subprocess
// inheriting the parent's environment exactly as before.
type gitRunner func(ctx context.Context, dir, name string, args, env []string) (output []byte, err error)

func execGit(ctx context.Context, dir, name string, args, env []string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	if len(env) > 0 {
		// Extend the inherited environment rather than replacing it: git
		// still needs PATH to find itself and HOME to resolve config.
		cmd.Env = append(os.Environ(), env...)
	}

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	if err := cmd.Run(); err != nil {
		return out.Bytes(), err
	}
	return out.Bytes(), nil
}

// mergeFetchDepth bounds the fetch's history depth to the common case of a
// base and PR that haven't diverged by an unusual number of commits,
// preserving Clone's original shallow-fetch rationale (IaC repos can be
// large, and a full clone of one for every Operation is wasteful).
// runMerge falls back to a full, unshallow fetch only when this depth
// isn't enough to find a common ancestor.
const mergeFetchDepth = 50

// headRef/baseLocalRef are the local ref names Clone fetches commitSHA and
// baseRef into. Named local refs are used instead of FETCH_HEAD because
// two refs are ever in flight at once here, and FETCH_HEAD only
// unambiguously identifies one.
const (
	headRef      = "refs/turnip/head"
	baseLocalRef = "refs/turnip/base"
)

// mergeIdentity sets a synthetic committer identity for the merge commit
// Clone creates. Nothing downstream reads this history or its authorship —
// only the resulting working tree — so any stable identity is fine.
var mergeIdentity = []string{"-c", "user.name=turnip", "-c", "user.email=turnip@localhost"}

// Clone checks out commitSHA of repoURL into dir: a shallow fetch rather
// than a full clone, since IaC repos can be large and only one commit's
// tree is ever needed (Requirement 5.1). When baseRef is non-empty, Clone
// also merges baseRef's current tip into the checked-out commit —
// Atlantis' "atlantis-merge" strategy — so a Plugin operates on the tree
// that would result from merging the PR, not the PR branch alone
// (Requirement 5.3). An empty baseRef skips the merge entirely, mirroring
// embedToken's "empty means no-op" convention. IF that merge can't
// complete cleanly because of a genuine content conflict, Clone returns a
// *MergeConflictError, distinguishable from any other clone/fetch failure
// (Requirement 5.4). token authenticates against a private repository
// (Requirement 5.2) and is never allowed to reach an error message or log
// line this function produces.
func Clone(ctx context.Context, dir, repoURL, commitSHA, baseRef, submodules string) error {
	return cloneWith(ctx, execGit, dir, repoURL, commitSHA, baseRef, submodules)
}

func cloneWith(ctx context.Context, run gitRunner, dir, repoURL, commitSHA, baseRef, submodules string) error {
	// No credential is assembled here and none is held. git asks turnip's
	// own binary for one when it needs it, so nothing this function
	// writes — a remote URL, an argument, an error message — can carry a
	// token, and no cleanup step has to remember to remove one.
	helper, err := credentialHelperEntry()
	if err != nil {
		return err
	}
	gitConfig := []gitConfigEntry{helper}
	gitEnv := gitConfigEnv(gitConfig)

	fetchRefspecs := []string{commitSHA + ":" + headRef}
	if baseRef != "" {
		fetchRefspecs = append(fetchRefspecs, baseRef+":"+baseLocalRef)
	}
	fetchArgs := append([]string{"-C", dir, "fetch", "--depth", strconv.Itoa(mergeFetchDepth), "origin"}, fetchRefspecs...)

	steps := [][]string{
		{"init", dir},
		{"-C", dir, "remote", "add", "origin", repoURL},
		fetchArgs,
		{"-C", dir, "checkout", headRef},
	}

	for _, args := range steps {
		// Every step names its target directory explicitly (the "init"
		// argument or "-C dir"), so none of them need — or should assume
		// — a working directory that already exists.
		if out, err := run(ctx, "", "git", args, gitEnv); err != nil {
			return fmt.Errorf("runner: clone: git %s: %w: %s", strings.Join(args, " "), err, string(out))
		}
	}

	// Submodules are initialised after the merge, never after the
	// checkout: it is the merged tree's gitlinks that record which
	// submodule commits belong to it (Decision 2).
	if baseRef != "" {
		if err := runMerge(ctx, run, dir, gitEnv); err != nil {
			return err
		}
	}
	return initSubmodules(ctx, run, dir, repoURL, gitConfig, submodules)
}

// runMerge merges baseLocalRef into the already-checked-out headRef. If
// the bounded-depth fetch above didn't reach a common ancestor (git
// reports "refusing to merge unrelated histories" — the two refs' shallow
// grafts have no recorded parents in common, indistinguishable to git
// from actually unrelated history), runMerge falls back to a single full,
// unshallow re-fetch and retries the merge exactly once (Requirement 5.6)
// rather than misreporting that as a content conflict. A genuine content
// conflict, whether on the first attempt or after the fallback, is
// reported as a *MergeConflictError after aborting the merge to leave a
// clean working tree.
func runMerge(ctx context.Context, run gitRunner, dir string, gitEnv []string) error {
	mergeArgs := append(append([]string{"-C", dir}, mergeIdentity...), "merge", "--no-ff", "-m", "turnip-merge", baseLocalRef)

	out, err := run(ctx, "", "git", mergeArgs, nil)
	if err != nil && strings.Contains(string(out), "refusing to merge unrelated histories") {
		unshallowArgs := []string{"-C", dir, "fetch", "--unshallow", "origin"}
		if unshallowOut, unshallowErr := run(ctx, "", "git", unshallowArgs, nil); unshallowErr != nil {
			return fmt.Errorf("runner: clone: git %s: %w: %s", strings.Join(unshallowArgs, " "), unshallowErr, string(unshallowOut))
		}
		out, err = run(ctx, "", "git", mergeArgs, nil)
	}
	if err == nil {
		return nil
	}

	if strings.Contains(string(out), "CONFLICT") || strings.Contains(string(out), "Automatic merge failed") {
		_, _ = run(ctx, "", "git", []string{"-C", dir, "merge", "--abort"}, nil)
		return &MergeConflictError{Output: string(out)}
	}

	return fmt.Errorf("runner: clone: git %s: %w: %s", strings.Join(mergeArgs, " "), err, string(out))
}

// MergeConflictError reports that a PR's base branch could not be merged
// cleanly into its head — a conflict the developer needs to resolve by
// editing their PR, distinguishable (Requirement 5.4) from a generic
// clone/fetch failure (invalid token, network error, missing ref).
type MergeConflictError struct {
	Output string
}

func (e *MergeConflictError) Error() string {
	return fmt.Sprintf("runner: clone: merge conflict: %s", e.Output)
}
