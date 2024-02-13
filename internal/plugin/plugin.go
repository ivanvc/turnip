package plugin

import "github.com/ivanvc/turnip/internal/yaml"

// Plugin is the interface that must be implemented by all plugins.
type Plugin interface {
	// Name returns the name of the plugin.
	Name() string
	// PlanName returns the name of the plan command.
	PlanName() string
	// LiftName returns the name of the lift command.
	LiftName() string
	// Workspace returns the workspace/sack/environment to use for the project.
	Workspace(project *yaml.Project) string
	// AutoPlan returns the auto plan configuration.
	AutoPlan(project *yaml.Project) bool
	// FormatDiff returns the formatted diff.
	FormatDiff(diff []byte) (string, error)
}
