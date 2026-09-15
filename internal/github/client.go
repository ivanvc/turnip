package github

import (
	"context"
	"fmt"
	"net/http"

	"github.com/bradleyfalzon/ghinstallation/v2"
	gh "github.com/google/go-github/v90/github"
)

// GitHubClient is the set of GitHub-facing primitives this package
// provides. Slice 6 depends on this interface, not the concrete Client, so
// it can substitute a fake in its own tests.
type GitHubClient interface {
	GenerateInstallationToken(ctx context.Context) (string, error)
	GetFile(ctx context.Context, owner, repo, path, ref string) ([]byte, error)
	GetModifiedFiles(ctx context.Context, owner, repo string, prNumber int) ([]string, error)
	GetPullRequest(ctx context.Context, owner, repo string, prNumber int) (*PullRequest, error)
	CreateCheckRun(ctx context.Context, owner, repo string, opts CheckRunOptions) (int64, error)
	UpdateCheckRun(ctx context.Context, owner, repo string, checkRunID int64, opts CheckRunOptions) error
	PostComment(ctx context.Context, owner, repo string, prNumber int, body string) (*PostedComment, error)
	UpdateComment(ctx context.Context, owner, repo string, commentID int64, body string) error
	IsCollaborator(ctx context.Context, owner, repo, username string) (bool, error)
	GetCollaboratorPermission(ctx context.Context, owner, repo, username string) (string, error)
	MinimizeComment(ctx context.Context, nodeID string) error
}

// Client implements GitHubClient by wrapping a *gh.Client whose transport
// is an installation-scoped *ghinstallation.Transport.
type Client struct {
	gh  *gh.Client
	itr *ghinstallation.Transport

	// graphQLURL overrides graphQLEndpoint when set; used by tests only.
	graphQLURL string
}

var _ GitHubClient = (*Client)(nil)

func (c *Client) GenerateInstallationToken(ctx context.Context) (string, error) {
	token, err := c.itr.Token(ctx)
	if err != nil {
		return "", fmt.Errorf("github: generating installation token: %w", err)
	}
	return token, nil
}

func (c *Client) GetFile(ctx context.Context, owner, repo, path, ref string) ([]byte, error) {
	fileContent, dirContent, resp, err := c.gh.Repositories.GetContents(ctx, owner, repo, path, &gh.RepositoryContentGetOptions{Ref: ref})
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			return nil, fmt.Errorf("github: getting %s/%s:%s@%s: %w", owner, repo, path, ref, ErrFileNotFound)
		}
		return nil, fmt.Errorf("github: getting %s/%s:%s@%s: %w", owner, repo, path, ref, err)
	}
	if dirContent != nil || fileContent == nil {
		return nil, fmt.Errorf("github: %s/%s:%s@%s is a directory, not a file", owner, repo, path, ref)
	}

	content, err := fileContent.GetContent()
	if err != nil {
		return nil, fmt.Errorf("github: decoding %s/%s:%s@%s: %w", owner, repo, path, ref, err)
	}
	return []byte(content), nil
}

func (c *Client) GetModifiedFiles(ctx context.Context, owner, repo string, prNumber int) ([]string, error) {
	var files []string
	opts := &gh.ListOptions{PerPage: 100}
	for {
		page, resp, err := c.gh.PullRequests.ListFiles(ctx, owner, repo, prNumber, opts)
		if err != nil {
			return nil, fmt.Errorf("github: listing modified files for %s/%s#%d: %w", owner, repo, prNumber, err)
		}
		for _, f := range page {
			files = append(files, f.GetFilename())
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return files, nil
}

func (c *Client) GetPullRequest(ctx context.Context, owner, repo string, prNumber int) (*PullRequest, error) {
	pr, _, err := c.gh.PullRequests.Get(ctx, owner, repo, prNumber)
	if err != nil {
		return nil, fmt.Errorf("github: getting %s/%s#%d: %w", owner, repo, prNumber, err)
	}
	return &PullRequest{
		Number:  pr.GetNumber(),
		HeadSHA: pr.GetHead().GetSHA(),
		BaseRef: pr.GetBase().GetRef(),
		HeadRef: pr.GetHead().GetRef(),
	}, nil
}

func (c *Client) CreateCheckRun(ctx context.Context, owner, repo string, opts CheckRunOptions) (int64, error) {
	run, _, err := c.gh.Checks.CreateCheckRun(ctx, owner, repo, toCreateCheckRunOptions(opts))
	if err != nil {
		return 0, fmt.Errorf("github: creating check run for %s/%s@%s: %w", owner, repo, opts.HeadSHA, err)
	}
	return run.GetID(), nil
}

func (c *Client) UpdateCheckRun(ctx context.Context, owner, repo string, checkRunID int64, opts CheckRunOptions) error {
	_, _, err := c.gh.Checks.UpdateCheckRun(ctx, owner, repo, checkRunID, toUpdateCheckRunOptions(opts))
	if err != nil {
		return fmt.Errorf("github: updating check run %d for %s/%s: %w", checkRunID, owner, repo, err)
	}
	return nil
}

// PostedComment identifies a newly-created comment by both its numeric
// REST id and its GraphQL node id — the latter is what MinimizeComment
// needs, and go-github's create-comment response already carries it, so
// capturing it here costs no extra API call.
type PostedComment struct {
	ID     int64
	NodeID string
}

func (c *Client) PostComment(ctx context.Context, owner, repo string, prNumber int, body string) (*PostedComment, error) {
	comment, _, err := c.gh.Issues.CreateComment(ctx, owner, repo, prNumber, &gh.IssueComment{Body: &body})
	if err != nil {
		return nil, fmt.Errorf("github: posting comment on %s/%s#%d: %w", owner, repo, prNumber, err)
	}
	return &PostedComment{ID: comment.GetID(), NodeID: comment.GetNodeID()}, nil
}

func (c *Client) UpdateComment(ctx context.Context, owner, repo string, commentID int64, body string) error {
	_, _, err := c.gh.Issues.EditComment(ctx, owner, repo, commentID, &gh.IssueComment{Body: &body})
	if err != nil {
		return fmt.Errorf("github: updating comment %d on %s/%s: %w", commentID, owner, repo, err)
	}
	return nil
}

func (c *Client) IsCollaborator(ctx context.Context, owner, repo, username string) (bool, error) {
	ok, _, err := c.gh.Repositories.IsCollaborator(ctx, owner, repo, username)
	if err != nil {
		return false, fmt.Errorf("github: checking collaborator status of %s on %s/%s: %w", username, owner, repo, err)
	}
	return ok, nil
}

func (c *Client) GetCollaboratorPermission(ctx context.Context, owner, repo, username string) (string, error) {
	level, _, err := c.gh.Repositories.GetPermissionLevel(ctx, owner, repo, username)
	if err != nil {
		return "", fmt.Errorf("github: getting permission level of %s on %s/%s: %w", username, owner, repo, err)
	}
	return level.GetPermission(), nil
}

func toCreateCheckRunOptions(opts CheckRunOptions) gh.CreateCheckRunOptions {
	return gh.CreateCheckRunOptions{
		Name:       opts.Name,
		HeadSHA:    opts.HeadSHA,
		Status:     strPtr(opts.Status),
		Conclusion: strPtr(opts.Conclusion),
		Output:     toCheckRunOutput(opts),
	}
}

func toUpdateCheckRunOptions(opts CheckRunOptions) gh.UpdateCheckRunOptions {
	return gh.UpdateCheckRunOptions{
		Name:       opts.Name,
		Status:     strPtr(opts.Status),
		Conclusion: strPtr(opts.Conclusion),
		Output:     toCheckRunOutput(opts),
	}
}

// toCheckRunOutput builds the check run's optional `output` object.
//
// GitHub's contract is all-or-nothing: `output` may be omitted entirely,
// but when it is present both `title` and `summary` are required. Sending
// an output object built only from the fields a caller happened to set —
// an empty one, or one carrying just `text` — is rejected outright with
// `422 Invalid request: "summary", "title" weren't supplied`, which took
// down every check run turnip tried to create. So: omit `output` when
// there is nothing to report, and otherwise always supply both required
// fields, falling back to the check run's own name rather than emitting
// a null.
func toCheckRunOutput(opts CheckRunOptions) *gh.CheckRunOutput {
	if opts.Title == "" && opts.Summary == "" && opts.Text == "" {
		return nil
	}

	title := firstNonEmpty(opts.Title, opts.Name, "turnip")
	summary := firstNonEmpty(opts.Summary, title)

	return &gh.CheckRunOutput{
		Title:   &title,
		Summary: &summary,
		Text:    strPtr(opts.Text),
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
