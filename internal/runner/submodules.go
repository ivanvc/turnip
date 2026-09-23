package runner

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/ivanvc/turnip/internal/config"
)

// submoduleURL is one `submodule.<name>.url` entry read out of .gitmodules.
type submoduleURL struct {
	name string
	url  string
}

// initSubmodules checks out the repository's submodules according to mode,
// after the base-branch merge has run (Requirement 1.2) so the submodule
// commits resolved are the ones the merged tree records.
//
// An empty mode means top-level: a Job built by an older Server carries no
// mode at all, and defaulting it off would silently reintroduce the empty
// submodule directory this slice exists to remove.
func initSubmodules(ctx context.Context, run gitRunner, dir, repoURL string, gitConfig []gitConfigEntry, mode string) error {
	if mode == config.SubmodulesNone {
		return nil
	}

	// A repository without submodules is a no-op, not a failure
	// (Requirement 1.3). Testing for the file is cheaper and less
	// ambiguous than reading git's exit status, which does not
	// distinguish "no such file" from "no matching keys".
	if _, err := os.Stat(filepath.Join(dir, ".gitmodules")); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("runner: clone: submodules: reading .gitmodules: %w", err)
	}

	urls, err := readSubmoduleURLs(ctx, run, dir)
	if err != nil {
		return err
	}

	parentHost := gitURLHost(repoURL)

	// Reported before anything is fetched (Requirement 4.4), naming the
	// submodule and its host rather than surfacing as a generic
	// authentication failure several layers down. The test is the host,
	// not the scheme: every form of the repository's own host is
	// authenticated below, but a GitHub installation token authenticates
	// nothing on another forge whatever the URL looks like.
	for _, s := range urls {
		host := gitURLHost(s.url)
		if host == "" || strings.EqualFold(host, parentHost) {
			continue
		}
		return fmt.Errorf(
			"runner: clone: submodule %q is hosted on %s, which this installation token cannot authenticate (turnip authenticates only %s)",
			s.name, host, parentHost,
		)
	}

	args := []string{"-C", dir, "submodule", "update", "--init"}
	if mode == config.SubmodulesRecursive {
		args = append(args, "--recursive")
	}
	// No --depth: the parent's bounded fetch does not apply to submodules,
	// and a shallow submodule fetch can fail to reach the exact commit the
	// parent pins (Requirement 3.3).

	env := gitConfigEnv(append(gitConfig, submoduleConfigEntries(urls, parentHost)...))
	if out, err := run(ctx, "", "git", args, env); err != nil {
		return fmt.Errorf(
			"runner: clone: git %s: %w: %s%s",
			strings.Join(args, " "), err, string(out), accessHint(string(out)),
		)
	}
	return nil
}

// accessHint explains the one thing git's own message cannot.
//
// GitHub answers "Repository not found" both for a repository that does
// not exist and for one the credential cannot see — it deliberately does
// not distinguish them, so as not to leak which private repositories
// exist. Read literally, the message sends the reader off to check a
// spelling that is usually correct.
//
// By this point turnip's credential helper has already supplied the
// installation token, so the fetch was authenticated. The remaining
// explanation is almost always that the GitHub App is not installed on the
// submodule's repository: an installation token reaches only the
// repositories its installation was granted, and an installation set to
// "only select repositories" commonly covers the parent but not a shared
// chart or module repository beside it.
func accessHint(out string) string {
	if !strings.Contains(out, "Repository not found") {
		return ""
	}
	return "\nhint: turnip authenticated this fetch with its installation token, so " +
		"\"Repository not found\" most likely means the GitHub App is not installed on " +
		"that repository rather than that it does not exist — check the App's repository access"
}

// readSubmoduleURLs asks git for the submodule URLs rather than parsing
// .gitmodules by hand — it is a git config file, with git's own escaping
// and merge rules.
func readSubmoduleURLs(ctx context.Context, run gitRunner, dir string) ([]submoduleURL, error) {
	args := []string{"-C", dir, "config", "-f", ".gitmodules", "--get-regexp", `^submodule\..*\.url$`}

	out, err := run(ctx, "", "git", args, nil)
	if err != nil {
		// `git config --get-regexp` exits non-zero when nothing matches,
		// which is not a failure: a .gitmodules may legitimately carry no
		// url entries. Real errors print something.
		if strings.TrimSpace(string(out)) == "" {
			return nil, nil
		}
		return nil, fmt.Errorf("runner: clone: submodules: reading .gitmodules: %w: %s", err, strings.TrimSpace(string(out)))
	}

	var urls []submoduleURL
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), " ")
		if !ok {
			continue
		}
		urls = append(urls, submoduleURL{
			name: strings.TrimSuffix(strings.TrimPrefix(key, "submodule."), ".url"),
			url:  value,
		})
	}
	return urls, nil
}

// submoduleConfigEnv builds the url.<authenticated>.insteadOf rewrites that
// let the installation token authenticate submodule fetches, carried in
// GIT_CONFIG_* on the subprocess: never in argv, never on disk, never in
// the Job spec (Requirement 3.2).
//
// The rewrites are derived from the URLs actually present rather than from
// a list of prefixes turnip guesses at (Requirement 3.5). git accepts
// ssh://, git://, http(s)://, ftp(s)://, file:// and the scp-like
// [user@]host:path, every :// form admits an explicit port, and scp-like
// makes the user optional — so a fixed prefix list silently misses legal
// URLs such as ssh://git@host:22/o/r. An exact URL is a valid prefix of
// itself, which makes the derived entry both precise and sufficient.
//
// The fixed prefixes are still emitted, but only for *nested* submodules:
// under recursive their own .gitmodules does not exist until their parent
// is fetched, so their URLs cannot have been read here.
func submoduleConfigEntries(urls []submoduleURL, parentHost string) []gitConfigEntry {
	if parentHost == "" {
		return nil
	}

	var entries []gitConfigEntry
	for _, s := range urls {
		// An identity rewrite is skipped: before this slice every entry
		// changed the URL by adding a credential to it, so even an https
		// source needed one. Now the rewrite's only job is the scheme,
		// and an https URL already has the right one.
		if equivalent := httpsEquivalent(s.url); equivalent != "" && equivalent != s.url {
			entries = append(entries, gitConfigEntry{"url." + equivalent + ".insteadOf", s.url})
		}
	}

	base := "https://" + parentHost + "/"
	for _, prefix := range []string{
		"http://" + parentHost + "/",
		"git://" + parentHost + "/",
		"git@" + parentHost + ":",
		"ssh://git@" + parentHost + "/",
	} {
		entries = append(entries, gitConfigEntry{"url." + base + ".insteadOf", prefix})
	}

	return entries
}

// httpsEquivalent rewrites raw as a plain HTTPS URL.
//
// turnip holds no SSH key and never will, so HTTPS is the only scheme
// that can work. It carries no credential: git asks turnip's credential
// helper for one when it reaches the host. An empty result means raw
// names no host — a relative or local path, which git resolves against
// the parent's own remote.
func httpsEquivalent(raw string) string {
	host := gitURLHost(raw)
	if host == "" {
		return ""
	}

	var path string
	i := strings.IndexAny(raw, ":/")
	switch {
	case i < 0:
		return ""
	case strings.HasPrefix(raw[i:], "://"):
		u, err := url.Parse(raw)
		if err != nil {
			return ""
		}
		path = strings.TrimPrefix(u.Path, "/")
	default:
		path = raw[i+1:]
	}

	return "https://" + host + "/" + path
}

// gitURLHost extracts the host from any URL form git accepts, returning ""
// for one that names no host.
//
// net/url.Parse alone is not enough: the scp-like form carries no scheme.
// git's own rule is that scp-like is recognised only when no slash precedes
// the first colon, which is what separates host:org/repo from the local
// path ./foo:bar — applied here so turnip agrees with git about which is
// which.
func gitURLHost(raw string) string {
	i := strings.IndexAny(raw, ":/")
	if i < 0 || raw[i] == '/' {
		// A slash first (or neither) means a relative or absolute local
		// path, never a remote host.
		return ""
	}

	if strings.HasPrefix(raw[i:], "://") {
		u, err := url.Parse(raw)
		if err != nil {
			return ""
		}
		return u.Hostname()
	}

	host := raw[:i]
	if at := strings.LastIndex(host, "@"); at >= 0 {
		host = host[at+1:]
	}
	return host
}
