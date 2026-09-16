package orchestrator

import (
	"context"
	"errors"
	"fmt"

	"github.com/ivanvc/turnip/internal/config"
	"github.com/ivanvc/turnip/internal/github"
)

// fetchConfig fetches and parses turnip.yaml, trying the repository root
// first and .github/turnip.yaml only on a root-level ErrFileNotFound
// (Requirement 1.1). Returns ErrConfigMissing (wrapped) if neither
// location has the file (Requirement 1.2); any other GetFile error is
// returned as-is (Requirement 1.3); a config.Parse failure
// (*config.ParseError or config.ValidationErrors) is returned as-is too
// (Requirement 1.4).
func fetchConfig(ctx context.Context, client github.GitHubClient, owner, repo, headSHA string) (*config.Config, error) {
	data, err := client.GetFile(ctx, owner, repo, "turnip.yaml", headSHA)
	if errors.Is(err, github.ErrFileNotFound) {
		data, err = client.GetFile(ctx, owner, repo, ".github/turnip.yaml", headSHA)
		if errors.Is(err, github.ErrFileNotFound) {
			return nil, fmt.Errorf("%w: %s/%s@%s", ErrConfigMissing, owner, repo, headSHA)
		}
	}
	if err != nil {
		return nil, err
	}

	return config.Parse(data)
}

// configErrorComment renders a fetchConfig failure into a PR comment body
// (Requirement 1.2-1.4).
func configErrorComment(err error) string {
	if errors.Is(err, ErrConfigMissing) {
		// GitHub alert syntax: renders as a highlighted "Warning" callout
		// rather than plain text. The "> " prefix is required on every
		// line of the block, including the marker line.
		return "> [!WARNING]\n" +
			"> `turnip.yaml` was not found in this repository (checked the repository root and `.github/turnip.yaml`)."
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
			"> `turnip.yaml` is invalid, so no operations ran.\n\n" +
			fmt.Sprintf("```\n%s\n```", err.Error())
	}

	// CAUTION: not the PR author's to fix. A failed fetch is an auth,
	// permissions, or rate-limit problem on turnip's own installation.
	return "> [!CAUTION]\n" +
		"> Fetching `turnip.yaml` failed, so no operations ran. This is usually an authentication, permission, or rate-limit problem with turnip's GitHub App rather than something wrong with this repository.\n\n" +
		fmt.Sprintf("<details>\n<summary>Error</summary>\n\n```\n%s\n```\n\n</details>", err.Error())
}
