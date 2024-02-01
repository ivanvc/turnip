package objects

// PullRequestWebhook holds the pull request webhook GitHub resource.
type PullRequestWebhook struct {
	Action      string `json:"action"`
	PullRequest `json:"pull_request"`
}

// PullRequest holds the pull request GitHub resource.
type PullRequest struct {
	URL     string `json:"url"`
	DiffURL string `json:"diff_url"`

	State string    `json:"state",omitempty`
	Head  BranchRef `json:"head",omitempty`
	Base  BranchRef `json:"base",omitempty`
}

// BranchRef holds the reference to a branch
type BranchRef struct {
	Ref string `json:"ref"`
	SHA string `json:"sha"`

	Repository `json:"repo",omitempty`
}
