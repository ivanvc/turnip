package github

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
	CommentsURL string `json:"comments_url"`
	PullRequest `json:"pull_request"`
}

// Comment holds the comment from the IssueComment.
type Comment struct {
	Body   string `json:"body"`
	NodeID string `json:"node_id"`
	ID     uint64 `json:"id"`
}

// Repository holds the repository from the IssueComment.
type Repository struct {
	CloneURL string `json:"clone_url"`
}

// Converts the IssueComment into a JSON.
func (ic IssueComment) ToJSON() ([]byte, error) {
	return json.Marshal(ic)
}
