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
		return "turnip.yaml was not found in this repository (checked the repository root and `.github/turnip.yaml`)."
	}

	var parseErr *config.ParseError
	var validationErrs config.ValidationErrors
	if errors.As(err, &parseErr) || errors.As(err, &validationErrs) {
		return fmt.Sprintf("turnip.yaml is invalid:\n\n```\n%s\n```", err.Error())
	}

	return fmt.Sprintf("Fetching turnip.yaml failed.\n\n<details>\n<summary>Error</summary>\n\n```\n%s\n```\n\n</details>", err.Error())
}
