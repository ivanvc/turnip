package objects

import "encoding/json"

// IssueComment holds the comment received.
type IssueComment struct {
	Action     string `json:"action"`
	Issue      `json:"issue"`
	Comment    `json:"comment"`
	Repository `json:"repository"`
}

// Issue holds the issue from the issue comment.
type Issue struct {
	CommentsURL string       `json:"comments_url"`
	PullRequest *PullRequest `json:"pull_request"`
}

// Comment holds the comment from the IssueComment.
type Comment struct {
	Body      string    `json:"body"`
	NodeID    string    `json:"node_id"`
	ID        uint64    `json:"id"`
	Reactions Reactions `json:"reactions"`
}

// Reactions holds the reactions from the Comment of the IssueComment.
type Reactions struct {
	URL string `json:"url"`
}

// Repository holds the repository from the IssueComment.
type Repository struct {
	CloneURL      string `json:"clone_url"`
	ContentsURL   string `json:"contents_url"`
	URL           string `json:"url"`
	FullName      string `json:"full_name"`
	StatusesURL   string `json:"statuses_url"`
	DefaultBranch string `json:"default_branch"`
}

func (r Repository) DefaultBranchRef() BranchRef {
	return BranchRef{Ref: "refs/heads/" + r.DefaultBranch}
}

// Converts the IssueComment into a JSON.
func (ic IssueComment) ToJSON() ([]byte, error) {
	return json.Marshal(ic)
}
