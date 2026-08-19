package plugin

import "context"

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

	// Execute runs one of the operations returned by GetOperations.
	Execute(ctx context.Context, operation string, opts ExecuteOptions) (*ExecuteResult, error)
}

// ExecuteOptions carries the standardized inputs to a Plugin's Execute call.
type ExecuteOptions struct {
	WorkingDir string
	Config     map[string]string
	ExtraArgs  []string
	PlanData   []byte
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
