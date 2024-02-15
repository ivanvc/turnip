package objects

type Commit struct {
	CommentsURL string `json:"comments_url,omitempty"`
	SHA         string `json:"sha,omitempty"`
}
