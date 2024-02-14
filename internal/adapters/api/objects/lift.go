package objects

// LiftRequest is the payload for the lift API call.
type LiftRequest struct {
	Repo        string `json:"repo"`
	Ref         string `json:"ref"`
	Dir         string `json:"dir"`
	Workspace   string `json:"workspace"`
	Environment string `json:"environment"`
	Stack       string `json:"stack"`
}

// LiftResponse is the response for the lift API call.
type LiftResponse struct {
	CheckURL string `json:"check_url"`
}
