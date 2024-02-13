package plugin

import (
	"github.com/ivanvc/turnip/internal/plugin/pulumi"
	"github.com/ivanvc/turnip/internal/yaml"
)

// Pulumi is a plugin.
type Pulumi struct{}

// Name conforms to the Plugin interface.
func (p Pulumi) Name() string {
	return "pulumi"
}

// PlanName conforms to the Plugin interface.
func (p Pulumi) PlanName() string {
	return "preview"
}

// LiftName conforms to the Plugin interface.
func (p Pulumi) LiftName() string {
	return "up"
}

// Workspace conforms to the Plugin interface.
func (p Pulumi) Workspace(project *yaml.Project) string {
	return project.Stack
}

// AutoPlan conforms to the Plugin interface.
func (p Pulumi) AutoPlan(project *yaml.Project) bool {
	return project.AutoPreview
}

// FormatDiff conforms to the Plugin interface.
func (p Pulumi) FormatDiff(diff []byte) (string, error) {
	f := pulumi.NewFormatter(diff)
	return f.Format()
}
