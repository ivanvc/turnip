package plugin

import (
	"context"

	"github.com/ivanvc/turnip/internal/provisioning"
)

// Plugin defines the unified interface for all IaC tools.
type Plugin interface {
	// Name returns the tool's identifier ("terraform", "pulumi", "helmfile").
	Name() string

	// GetOperations returns the tool-native operation names this Plugin
	// supports.
	GetOperations() []string

	// GetPlanOperation returns the operation name used for planning.
	GetPlanOperation() string

	// GetApplyOperation returns the operation name used for applying.
	GetApplyOperation() string

	// ActsWithoutChanges reports whether any operation this Plugin exposes
	// other than its plan can still affect infrastructure when the plan
	// reported no changes.
	//
	// It answers for the Plugin's whole operation surface rather than for
	// its designated apply operation, because a Lock's fate is decided
	// once, when the plan's result arrives — before turnip knows which
	// mutating operation the author will later ask for.
	ActsWithoutChanges() bool

	// Provisioning says how this tool reaches a Runner Job: which
	// Strategy, which image, and where the binary lives in it when the
	// Strategy copies it out. It names no version, because the version is
	// the repository's to state in uses:, never a Plugin's default.
	Provisioning() provisioning.Spec

	// ImageTags returns the Tag_Spec globs this Plugin's Alias allows:
	// uses: naming the Alias runs the Provisioning image only with a Tag or
	// Digest one of these matches. The Alias's Entries are formed from the
	// Plugin's name, its Provisioning image and these globs, so the image is
	// stated once. Each glob is matched with path.Match over the Tag_Spec's
	// text.
	ImageTags() []string

	// Execute runs one of the operations returned by GetOperations.
	Execute(ctx context.Context, operation string, opts ExecuteOptions) (*ExecuteResult, error)

	// MutationChangesReported reports whether a successful
	// Mutating_Operation's counts are meaningful, given its transcript.
	// False renders "succeeded".
	MutationChangesReported(output string) bool
}

// ExecuteOptions carries the standardized inputs to a Plugin's Execute call.
type ExecuteOptions struct {
	WorkingDir string

	// Tool and ToolVersion name the binary for the execution transcript.
	// They are carried rather than asked of the Plugin, because the Server
	// already resolved the version to build the Job's image and a second
	// source could disagree with the first.
	Tool        string
	ToolVersion string

	Config    map[string]string
	ExtraArgs []string
	PlanData  []byte

	// Record, when non-nil, is the Record the command's output is
	// appended to as the subprocess produces it, and closed when it ends.
	// A caller that wants the output while the command runs (to mirror it,
	// to send it on) creates the Record, starts following it with Next,
	// and passes it here; following it can never slow the command or
	// change the order it is recorded in. Nil means no one follows, and
	// Execute uses a Record of its own.
	Record *Record
}

// ExecuteResult carries the standardized outputs of a Plugin's Execute call.
type ExecuteResult struct {
	Output        string
	ChangeSummary ChangeSummary
	PlanData      []byte
	ExitCode      int
	Error         error
}

// ChangeSummary reports the add/change/destroy counts extracted from a
// completed operation's output.
type ChangeSummary struct {
	Add     int
	Change  int
	Destroy int
}
