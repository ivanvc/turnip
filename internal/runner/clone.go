package runner

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
)

// gitRunner abstracts subprocess execution so tests can substitute a fake
// without invoking a real git binary. It's a small, deliberately
// unshared duplicate of internal/plugin/command.go's commandRunner seam —
// two call sites don't justify a premature cross-package abstraction.
type gitRunner func(ctx context.Context, dir, name string, args []string) (output []byte, err error)

func execGit(ctx context.Context, dir, name string, args []string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir

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
func Clone(ctx context.Context, dir, repoURL, commitSHA, baseRef, token string) error {
	return cloneWith(ctx, execGit, dir, repoURL, commitSHA, baseRef, token)
}

func cloneWith(ctx context.Context, run gitRunner, dir, repoURL, commitSHA, baseRef, token string) error {
	authedURL, err := embedToken(repoURL, token)
	if err != nil {
		return fmt.Errorf("runner: clone: build authenticated remote URL: %w", err)
	}

	fetchRefspecs := []string{commitSHA + ":" + headRef}
	if baseRef != "" {
		fetchRefspecs = append(fetchRefspecs, baseRef+":"+baseLocalRef)
	}
	fetchArgs := append([]string{"-C", dir, "fetch", "--depth", strconv.Itoa(mergeFetchDepth), "origin"}, fetchRefspecs...)

	steps := [][]string{
		{"init", dir},
		{"-C", dir, "remote", "add", "origin", authedURL},
		fetchArgs,
		{"-C", dir, "checkout", headRef},
	}

	for _, args := range steps {
		// Every step names its target directory explicitly (the "init"
		// argument or "-C dir"), so none of them need — or should assume
		// — a working directory that already exists.
		if out, err := run(ctx, "", "git", args); err != nil {
			return fmt.Errorf("runner: clone: git %s: %w: %s", strings.Join(redactArgs(args, authedURL, redactedRemote), " "), err, redact(string(out), authedURL))
		}
	}

	if baseRef == "" {
		return nil
	}
	return runMerge(ctx, run, dir, authedURL)
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
func runMerge(ctx context.Context, run gitRunner, dir, authedURL string) error {
	mergeArgs := append(append([]string{"-C", dir}, mergeIdentity...), "merge", "--no-ff", "-m", "turnip-merge", baseLocalRef)

	out, err := run(ctx, "", "git", mergeArgs)
	if err != nil && strings.Contains(string(out), "refusing to merge unrelated histories") {
		unshallowArgs := []string{"-C", dir, "fetch", "--unshallow", "origin"}
		if unshallowOut, unshallowErr := run(ctx, "", "git", unshallowArgs); unshallowErr != nil {
			return fmt.Errorf("runner: clone: git %s: %w: %s", strings.Join(unshallowArgs, " "), unshallowErr, redact(string(unshallowOut), authedURL))
		}
		out, err = run(ctx, "", "git", mergeArgs)
	}
	if err == nil {
		return nil
	}

	if strings.Contains(string(out), "CONFLICT") || strings.Contains(string(out), "Automatic merge failed") {
		_, _ = run(ctx, "", "git", []string{"-C", dir, "merge", "--abort"})
		return &MergeConflictError{Output: redact(string(out), authedURL)}
	}

	return fmt.Errorf("runner: clone: git %s: %w: %s", strings.Join(redactArgs(mergeArgs, authedURL, redactedRemote), " "), err, redact(string(out), authedURL))
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

const redactedRemote = "<redacted>"

// embedToken returns repoURL with an "x-access-token:<token>@" userinfo
// segment inserted, the standard way to authenticate an HTTPS git remote
// with a GitHub App installation token. An empty token leaves repoURL
// untouched, since there's no credential to embed — a plain local path or
// an already-public remote (as used by this package's own tests) would
// otherwise be corrupted by an empty userinfo segment.
func embedToken(repoURL, token string) (string, error) {
	if token == "" {
		return repoURL, nil
	}
	u, err := url.Parse(repoURL)
	if err != nil {
		return "", err
	}
	u.User = url.UserPassword("x-access-token", token)
	return u.String(), nil
}

// redactArgs replaces any argument equal to authedURL with a redacted
// placeholder, so the authenticated remote URL (which embeds the
// installation token) never reaches an error message.
func redactArgs(args []string, authedURL, placeholder string) []string {
	redacted := make([]string, len(args))
	for i, a := range args {
		if a == authedURL {
			redacted[i] = placeholder
			continue
		}
		redacted[i] = a
	}
	return redacted
}

// redact strips any occurrence of authedURL out of s, so a git subprocess's
// own error/output text (which might otherwise echo the remote URL) never
// leaks the token it embeds.
func redact(s, authedURL string) string {
	return strings.ReplaceAll(s, authedURL, redactedRemote)
}
