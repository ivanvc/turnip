package github

// WebhookEvent is this package's VCS-agnostic representation of a parsed,
// signature-verified GitHub webhook delivery.
type WebhookEvent struct {
	Type         string // "pull_request", "issue_comment"
	Action       string // "opened", "synchronize", "created", etc.
	Repository   Repository
	PullRequest  *PullRequest // nil for non-PR issue_comment events
	Comment      *Comment     // nil for pull_request events
	Installation Installation
}

type Repository struct {
	Owner string
	Name  string
	URL   string
}

// PullRequest carries what's available from the triggering payload. For a
// pull_request event, every field is populated. For an issue_comment
// event, only Number is populated — GetPullRequest fetches the rest when
// needed.
type PullRequest struct {
	Number  int
	HeadSHA string
	BaseRef string
	HeadRef string
}

type Comment struct {
	ID     int64
	Body   string
	Author string
}

type Installation struct {
	ID int64
}

// CheckRunOptions mirrors the global design's sketch field-for-field.
type CheckRunOptions struct {
	Name       string
	HeadSHA    string
	Status     string // "queued", "in_progress", "completed"
	Conclusion string // "success", "failure", "neutral", "cancelled", ...
	Title      string
	Summary    string
	Text       string
}

// TriggerCommand is one element of ParseTriggers' result — one per matched
// line in a comment body.
type TriggerCommand struct {
	Tool      string   // "turnip", "terraform", "pulumi", "helmfile"
	Operation string   // tool-native operation, unvalidated by this package
	Projects  []string // empty means "all projects"
	ExtraArgs []string // tokens after "--", verbatim
}

// ProjectResult is BuildConsolidatedComment's per-Project input.
type ProjectResult struct {
	ProjectName string
	// Tool is the Project's IaC tool (e.g. "helmfile"). BuildConsolidatedComment
	// doesn't read it — it exists so a caller (server-orchestration) can
	// resolve which Plugin's operation names Operation should be compared
	// against, without having to carry a second, parallel data structure
	// alongside a []ProjectResult.
	Tool      string
	Operation string
	Success   bool
	Output    string
}
