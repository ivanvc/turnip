package github

// PullRequest holds the pull request GitHub resource.
type PullRequest struct {
	URL string `json:"url"`

	State string `json:"state",omitempty`
	Head  `json:"head",omitempty`
	Base  Head `json:"base",omitempty`
}

// Head holds the reference to a branch
type Head struct {
	Ref string `json:"ref"`
	SHA string `json:"sha"`
}
