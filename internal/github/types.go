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

	// Author is the account that opened the pull request. Read when a
	// refusal is logged, so an operator can tell who attempted to have
	// turnip execute code — the automatic path has no commenter to name.
	Author string

	// HeadRepo identifies the repository this pull request's head branch
	// lives in: equal to the event's own repository for an ordinary
	// branch pull request, different for one opened from a fork.
	//
	// Zero when GitHub reports no head repository — a fork deleted after
	// the pull request was opened. IsForeign treats that as foreign
	// rather than as a match, because a payload turnip cannot read is not
	// one it should execute.
	HeadRepo Repository

	// Open reports whether GitHub still considers this pull request open.
	// A merged pull request and one closed without merging are both
	// false: GitHub gives each the state "closed", and turnip draws no
	// distinction between them, because a Lock taken after either is
	// equally unreleasable.
	//
	// False is the safe default. A path that forgets to map it refuses
	// everything, loudly, rather than silently restoring the bug this
	// field exists to fix.
	//
	// Like Draft, it is false on every issue_comment event, where the
	// payload populates only Number — which is why the comment path reads
	// the pull request GetPullRequest returned and never the event's own.
	// Unlike Draft, reading the wrong one does not merely skip an
	// autoplan: it refuses every comment trigger in the repository.
	Open bool

	// Draft reports whether GitHub considers this pull request a draft.
	// It is read on exactly one path — the automatic plan, which skips
	// drafts — and it is false on every issue_comment event, where only
	// Number is populated. A comment trigger therefore cannot consult it
	// even by accident, which is what keeps "drafts change when turnip
	// acts on its own, never what it can be asked to do" structural
	// rather than a rule to remember.
	Draft bool
}

// IsForeign reports whether this pull request's code comes from a
// repository other than base — the case turnip must never execute,
// because the head commit and the configuration read from it are both
// chosen by whoever opened the pull request.
//
// The comparison is on owner and name, deliberately not on the
// repository's fork flag: turnip installed on a repository that is itself
// a fork is an ordinary case, and keying on the flag would refuse
// legitimate work while detecting nothing a comparison does not.
//
// A pull request with no head repository is foreign. That is tested
// explicitly rather than left to "" differing from base's owner, which
// only holds while base is non-empty and is therefore accidental.
func (p *PullRequest) IsForeign(base Repository) bool {
	if p.HeadRepo.Owner == "" || p.HeadRepo.Name == "" {
		return true
	}
	return p.HeadRepo.Owner != base.Owner || p.HeadRepo.Name != base.Name
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

	// Changes is what the Operation reported changing. It is read only
	// when Success is true: a Project that failed, or that never ran at
	// all, must not render counts that look like a completed plan.
	Changes ChangeCounts

	// PlanOperation/ApplyOperation are the tool-native operation names for
	// this Project's Plugin — "diff"/"apply" for Helmfile,
	// "preview"/"up" for Pulumi. They are resolved by the caller because
	// this package has no Plugin registry to ask, which is the same reason
	// Tool above is carried rather than interpreted.
	//
	// An empty value omits that command from the Project's section rather
	// than guessing at a name.
	PlanOperation  string
	ApplyOperation string

	// LockNote states what happened to this Project's Lock and why — a
	// release, or an invalidation that kept the Lock. Empty means neither
	// happened, which covers a Lock still held unchanged and an Operation
	// that never held one, since a rejected Operation touches no Lock.
	//
	// Set only from a confirmed transition, in the same place as Locked,
	// so the two cannot disagree. It is therefore not derivable from
	// !Locked: a rejected Operation has Locked false having released
	// nothing.
	LockNote string

	// Locked reports whether the pull request holds this Project's Lock
	// once the Operation has been handled. It is deliberately not derived
	// from Success: which outcomes leave a Lock held is the lock
	// lifecycle's business, and stating the fact rather than inferring it
	// keeps this package correct when that lifecycle changes.
	Locked bool

	// BlockedBy identifies the pull request whose Lock refused this
	// Operation. Nil unless the Operation was rejected for that reason.
	BlockedBy *BlockingPullRequest
}

// ChangeCounts reports how much an Operation changed, as the Plugin
// extracted it from the tool's own output.
//
// Declared here rather than imported from internal/plugin: this package
// talks to GitHub, and pulling in the plugin system for one struct of
// three ints would couple them for no benefit. The caller converts.
type ChangeCounts struct {
	Add     int
	Change  int
	Destroy int
}

// Any reports whether anything changed at all. A successful Operation
// whose counts are all zero is "no changes" — a distinct statement from
// having reported nothing, which is what a failure leaves behind.
func (c ChangeCounts) Any() bool {
	return c.Add != 0 || c.Change != 0 || c.Destroy != 0
}

// Total is the number a verdict line reports across Projects.
func (c ChangeCounts) Total() int {
	return c.Add + c.Change + c.Destroy
}

// BlockingPullRequest identifies the pull request holding a Lock that
// refused an Operation, so the refusal can link to it rather than merely
// naming a number.
type BlockingPullRequest struct {
	Number int
	URL    string
}
