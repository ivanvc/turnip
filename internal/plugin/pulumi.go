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

// ApplyName conforms to the Plugin interface.
func (p Pulumi) ApplyName() string {
	return "up"
}

// Workspace conforms to the Plugin interface.
func (p Pulumi) Workspace(project *yaml.Project) string {
	return project.Stack
}

// AutoPlan conforms to the Plugin interface.
func (p Pulumi) AutoPlan(project *yaml.Project) *yaml.AutoPlan {
	return project.AutoPreview
}

// FormatDiff conforms to the Plugin interface.
func (p Pulumi) FormatDiff(diff string) (string, error) {
	f := pulumi.NewFormatter([]byte(diff))
	return f.Format()
}
