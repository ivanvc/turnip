package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ivanvc/turnip/internal/config"
	"github.com/ivanvc/turnip/internal/github"
)

// configFilePaths are the locations a repository's turnip configuration
// may live, in the order they are tried.
//
// The dedicated directory comes first so a repository migrating to it
// takes effect by adding the new file, with no second step to remove the
// old one. `.turnip/` also gives a repository somewhere to keep files the
// tool itself reads — an AWS config, say — which a Project can reference
// through the fixed workspace path; turnip reads nothing in there but
// this one file.
//
// Only `.yaml` is accepted. `.github/turnip.yaml`, read by versions
// before v1alpha2, is not.
var configFilePaths = []string{".turnip/config.yaml", "turnip.yaml"}

// configFilePathList renders the accepted locations for a human. Both the
// error and the PR comment call it rather than writing the list out
// again: a reader who finds two lists disagreeing has no way to know
// which one the code actually uses.
func configFilePathList(quote string) string {
	parts := make([]string, len(configFilePaths))
	for i, path := range configFilePaths {
		parts[i] = quote + path + quote
	}
	return strings.Join(parts, " or ")
}

// fetchConfig fetches and parses the repository's turnip configuration,
// trying each accepted location in order and stopping at the first that
// exists (Requirement 1.1). Returns ErrConfigMissing (wrapped) if none
// does (Requirement 1.2); any other GetFile error is returned as-is
// (Requirement 1.3); a config.Parse failure (*config.ParseError or
// config.ValidationErrors) is returned as-is too (Requirement 1.4).
// tools is the registered tool names, the only ones uses: may name.
//
// Exactly one request per location, and no more: this runs on every pull
// request open and synchronize in every installed repository, including
// those that never onboard, so the not-found path is the one paid most
// often.
func fetchConfig(ctx context.Context, client github.GitHubClient, owner, repo, headSHA string, tools []string) (*config.Config, error) {
	for _, path := range configFilePaths {
		data, err := client.GetFile(ctx, owner, repo, path, headSHA)
		switch {
		case err == nil:
			return config.Parse(data, tools)
		case errors.Is(err, github.ErrFileNotFound):
			continue
		default:
			return nil, err
		}
	}

	return nil, fmt.Errorf("%w: %s/%s@%s", ErrConfigMissing, owner, repo, headSHA)
}

// configErrorComment renders a fetchConfig failure into a PR comment body
// (Requirement 1.2-1.4).
func configErrorComment(err error) string {
	if errors.Is(err, ErrConfigMissing) {
		// GitHub alert syntax: renders as a highlighted "Warning" callout
		// rather than plain text. The "> " prefix is required on every
		// line of the block, including the marker line.
		return "> [!WARNING]\n" +
			"> No turnip configuration was found in this repository (checked " +
			configFilePathList("`") + ")."
	}

	// WARNING, not CAUTION: the repository opted into turnip and its
	// config is broken, but nothing is in a bad state and the author can
	// fix it themselves. CAUTION is reserved for failures that need an
	// operator or leave something behind.
	//
	// Everything after the alert block is deliberately outside it: GitHub
	// alerts don't render when another element (a code fence, a
	// <details>) is nested inside them — the block degrades into literal
	// "[!WARNING]" text.
	var parseErr *config.ParseError
	var validationErrs config.ValidationErrors
	if errors.As(err, &parseErr) || errors.As(err, &validationErrs) {
		return "> [!WARNING]\n" +
			"> The turnip configuration is invalid, so no operations ran.\n\n" +
			fmt.Sprintf("```\n%s\n```", err.Error())
	}

	// CAUTION: not the PR author's to fix. A failed fetch is an auth,
	// permissions, or rate-limit problem on turnip's own installation.
	return "> [!CAUTION]\n" +
		"> Fetching the turnip configuration failed, so no operations ran. This is usually an authentication, permission, or rate-limit problem with turnip's GitHub App rather than something wrong with this repository.\n\n" +
		fmt.Sprintf("<details>\n<summary>Error</summary>\n\n```\n%s\n```\n\n</details>", err.Error())
}

// closedPullRequestComment is the reply to a Trigger Command on a pull
// request that is no longer open.
//
// It names the reason rather than staying silent, unlike the fork
// refusal: there the requester may be an attacker and feedback is worth
// withholding, here they are almost certainly a colleague who commented
// on the wrong tab, and silence would read as turnip being broken.
//
// It does not distinguish merged from closed-without-merging, because
// turnip does not: a Lock taken after either has no lifecycle event left
// to release it.
func closedPullRequestComment() string {
	return "This pull request is closed, so turnip will not run anything on it.\n\n" +
		"Reopen it to plan again, or open a new pull request with these changes."
}
