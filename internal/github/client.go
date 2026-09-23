package github

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/bradleyfalzon/ghinstallation/v2"
	gh "github.com/google/go-github/v90/github"
)

// GitHubClient is the set of GitHub-facing primitives this package
// provides. Slice 6 depends on this interface, not the concrete Client, so
// it can substitute a fake in its own tests.
type GitHubClient interface {
	GenerateInstallationToken(ctx context.Context, scope TokenScope) (InstallationToken, error)
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
	// apps mints tokens on a throwaway transport, leaving itr — which
	// backs gh — unscoped. See InstallationClient.
	apps *ghinstallation.AppsTransport

	// graphQLURL overrides graphQLEndpoint when set; used by tests only.
	graphQLURL string
}

var _ GitHubClient = (*Client)(nil)

// InstallationToken is a minted credential and the moment it stops
// working. The expiry is carried because the Runner fetches this at the
// point of use: a credential that expired in transit should fail saying
// so, not as an unexplained 401 from GitHub.
type InstallationToken struct {
	Token     string
	ExpiresAt time.Time
}

// TokenScope narrows what a minted installation token can reach.
//
// Permissions are not a field: the only consumer is the Runner's clone,
// which needs `contents: read` and nothing else, and a caller able to ask
// for more is a caller that eventually will.
type TokenScope struct {
	// Repositories are names within the installation's account. Empty
	// means every repository the installation covers — deliberately
	// reachable, because a recursive submodule clone cannot enumerate
	// what it will fetch before it fetches it (Slice 24, Requirement
	// 1.4). Permissions are narrowed either way.
	Repositories []string
}

// GenerateInstallationToken mints an installation token limited to scope.
//
// It mints on a transport of its own rather than on c.itr. c.itr backs
// c.gh, so setting InstallationTokenOptions there would narrow every
// subsequent API call this Client makes — posting a comment needs
// issues:write and a check run needs checks:write, neither of which
// survives a `contents: read` scoping. The bug that would produce is a
// Server that dispatches fine and then cannot report, which is
// indistinguishable from the network failing.
func (c *Client) GenerateInstallationToken(ctx context.Context, scope TokenScope) (InstallationToken, error) {
	// The Apps endpoint is called directly rather than through
	// ghinstallation's own scoping field, because ghinstallation/v2 is
	// built against go-github v88 while turnip is on v90 — its
	// InstallationTokenOptions is a different type from the one the rest
	// of this package speaks. Calling the API turnip already has a client
	// for keeps one version of go-github in play.
	//
	// c.apps signs as the App (a JWT), which is what this endpoint wants;
	// an installation token cannot mint another.
	// c.itr.BaseURL is where this installation already mints, so the
	// app-authenticated client follows it. Without this a GitHub
	// Enterprise install — or a test's stub server — would be bypassed by
	// a second client defaulting to github.com.
	base := c.itr.BaseURL
	appClient, err := gh.NewClient(gh.WithTransport(c.apps), gh.WithURLs(&base, nil))
	if err != nil {
		return InstallationToken{}, fmt.Errorf("github: constructing app client: %w", err)
	}

	token, _, err := appClient.Apps.CreateInstallationToken(ctx, c.itr.InstallationID(), &gh.InstallationTokenOptions{
		Repositories: scope.Repositories,
		Permissions:  &gh.InstallationPermissions{Contents: gh.Ptr("read")},
	})
	if err != nil {
		// No fallback to an unscoped token: a silent widening would make
		// the narrowing invisible on the day it stops working
		// (Requirement 1.3).
		return InstallationToken{}, fmt.Errorf("github: generating installation token scoped to %v: %w", scope.Repositories, err)
	}
	return InstallationToken{
		Token:     token.GetToken(),
		ExpiresAt: token.GetExpiresAt().Time,
	}, nil
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
		Author:  pr.GetUser().GetLogin(),
		// The only source of head-repository identity on the
		// issue_comment path: that payload carries a pull request number
		// and nothing else.
		HeadRepo: repositoryFrom(pr.GetHead().GetRepo()),
		// The only source of pull-request state on the issue_comment
		// path, for the same reason. GetMerged is not read: a merged pull
		// request already reports "closed".
		Open: pr.GetState() == "open",
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
