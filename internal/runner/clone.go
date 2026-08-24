package runner

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"os/exec"
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

// Clone checks out commitSHA of repoURL into dir: a shallow, single-commit
// fetch rather than a full clone, since IaC repos can be large and only
// one commit's tree is ever needed (Requirement 5.1). token authenticates
// against a private repository (Requirement 5.2) and is never allowed to
// reach an error message or log line this function produces.
func Clone(ctx context.Context, dir, repoURL, commitSHA, token string) error {
	return cloneWith(ctx, execGit, dir, repoURL, commitSHA, token)
}

func cloneWith(ctx context.Context, run gitRunner, dir, repoURL, commitSHA, token string) error {
	authedURL, err := embedToken(repoURL, token)
	if err != nil {
		return fmt.Errorf("runner: clone: build authenticated remote URL: %w", err)
	}

	steps := [][]string{
		{"init", dir},
		{"-C", dir, "remote", "add", "origin", authedURL},
		{"-C", dir, "fetch", "--depth", "1", "origin", commitSHA},
		{"-C", dir, "checkout", "FETCH_HEAD"},
	}

	for _, args := range steps {
		// Every step names its target directory explicitly (the "init"
		// argument or "-C dir"), so none of them need — or should assume
		// — a working directory that already exists.
		if out, err := run(ctx, "", "git", args); err != nil {
			return fmt.Errorf("runner: clone: git %s: %w: %s", strings.Join(redactArgs(args, authedURL, redactedRemote), " "), err, redact(string(out), authedURL))
		}
	}

	return nil
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
