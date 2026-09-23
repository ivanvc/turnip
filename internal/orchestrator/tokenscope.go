package orchestrator

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/ivanvc/turnip/internal/config"
	"github.com/ivanvc/turnip/internal/github"
)

// gitmodulesPath is read from the Operation's own commit, through the same
// file-at-a-ref call that fetches turnip.yaml.
const gitmodulesPath = ".gitmodules"

// submoduleURLPattern pulls the url of each submodule out of .gitmodules.
// The file is git-config format, and only this one key matters here — the
// paths and branches say nothing about which repositories are fetched.
var submoduleURLPattern = regexp.MustCompile(`(?m)^\s*url\s*=\s*(\S+)\s*$`)

// tokenScopeFor computes the repositories the Clone_Step will fetch, so
// the Installation_Token can be minted for those and no others
// (Requirement 1.1).
//
// It never returns an error. Every failure here — an unreadable
// .gitmodules, a URL that does not parse — resolves to a wider scope
// rather than a refusal, because Requirement 1.4 forbids narrowing in a
// way that would fail a fetch turnip would otherwise have performed. What
// does refuse is the mint itself: if GitHub rejects the scope, the
// Operation fails rather than falling back (Requirement 1.3), and that
// distinction is the whole difference between a deliberate width and a
// silent fallback.
func (o *Orchestrator) tokenScopeFor(
	ctx context.Context,
	client github.GitHubClient,
	repo github.Repository,
	headSHA string,
	mode string,
) github.TokenScope {
	if mode == config.SubmodulesRecursive {
		// A submodule's own submodules are declared in a .gitmodules that
		// does not exist until that submodule is cloned, so the set
		// cannot be completed before minting. An empty Repositories is
		// the installation's full breadth; permissions stay narrowed.
		slog.DebugContext(ctx, "recursive submodules: repository scope left wide",
			"repo", repo.Name, "reason", "nested submodules are not enumerable before the clone")
		return github.TokenScope{}
	}

	scope := github.TokenScope{Repositories: []string{repo.Name}}
	if mode == config.SubmodulesNone {
		return scope
	}

	raw, err := client.GetFile(ctx, repo.Owner, repo.Name, gitmodulesPath, headSHA)
	if err != nil {
		if errors.Is(err, github.ErrFileNotFound) {
			// The ordinary case: no submodules, so the Operation's own
			// repository is the whole set.
			return scope
		}
		// Reading it failed for some other reason. Widening beats
		// guessing: a clone that would have worked must not stop working
		// because turnip could not read a file.
		slog.WarnContext(ctx, "could not read .gitmodules; leaving repository scope wide",
			"repo", repo.Name, "ref", headSHA, "error", err)
		return github.TokenScope{}
	}

	for _, name := range submoduleRepoNames(raw, repo) {
		if !slices.Contains(scope.Repositories, name) {
			scope.Repositories = append(scope.Repositories, name)
		}
	}
	return scope
}

// submoduleRepoNames returns the repositories declared in a .gitmodules
// that the parent's installation token could plausibly reach.
//
// Two kinds are left out, and neither costs a fetch that works today:
// another host is refused by internal/runner/submodules.go before it is
// tried, and another account is outside the installation entirely, so a
// token could not cover it however it was minted. Naming either in the
// scope would make GitHub reject the mint and, by Requirement 1.3, fail
// the Operation — turning "this submodule was never reachable" into "the
// Operation will not run at all".
func submoduleRepoNames(gitmodules []byte, parent github.Repository) []string {
	parentHost := hostOf(parent.URL)

	var names []string
	for _, m := range submoduleURLPattern.FindAllStringSubmatch(string(gitmodules), -1) {
		raw := strings.TrimSpace(m[1])

		// A relative url resolves against the parent's own remote, so it
		// is the same host and the same account by construction. This is
		// the form most repositories actually use.
		if strings.HasPrefix(raw, "./") || strings.HasPrefix(raw, "../") {
			names = append(names, repoName(path.Base(raw)))
			continue
		}

		host, owner, name := parseRemote(raw)
		if host == "" || name == "" {
			continue
		}
		if parentHost != "" && !strings.EqualFold(host, parentHost) {
			continue
		}
		if !strings.EqualFold(owner, parent.Owner) {
			continue
		}
		names = append(names, name)
	}
	return names
}

// parseRemote handles the three remote forms git accepts for a GitHub
// submodule: https, ssh:// and the scp-like git@host:owner/name.
func parseRemote(raw string) (host, owner, name string) {
	if strings.HasPrefix(raw, "git@") || (!strings.Contains(raw, "://") && strings.Contains(raw, ":")) {
		rest := strings.TrimPrefix(raw, "git@")
		host, path, ok := strings.Cut(rest, ":")
		if !ok {
			return "", "", ""
		}
		owner, name, ok := strings.Cut(strings.TrimPrefix(path, "/"), "/")
		if !ok {
			return "", "", ""
		}
		return host, owner, repoName(name)
	}

	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", "", ""
	}
	owner, name, ok := strings.Cut(strings.Trim(u.Path, "/"), "/")
	if !ok {
		return "", "", ""
	}
	return u.Hostname(), owner, repoName(name)
}

func hostOf(raw string) string {
	if u, err := url.Parse(raw); err == nil {
		return u.Hostname()
	}
	return ""
}

// repoName strips the optional .git suffix and any trailing path, so
// "name.git", "name" and "sub/name.git" all yield the repository name
// GitHub's InstallationTokenOptions expects.
func repoName(s string) string {
	s = path.Base(strings.TrimSuffix(strings.TrimSuffix(s, "/"), ".git"))
	if s == "." || s == "/" {
		return ""
	}
	return s
}
