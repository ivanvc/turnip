package objects

// APIRequest is the payload for an API call.
type APIRequest struct {
	Repo        string `json:"repo"`
	Ref         string `json:"ref"`
	Dir         string `json:"dir"`
	Workspace   string `json:"workspace"`
	Environment string `json:"environment"`
	Stack       string `json:"stack"`
	ExtraArgs   string `json:"extra_args"`
}

// APIResponse is the response for an API call.
type APIResponse struct {
	CheckURL string `json:"check_url"`
	Context  string `json:"context"`
}
