package orchestrator

import (
	"github.com/ivanvc/turnip/internal/config"
	"github.com/ivanvc/turnip/internal/plugin"
)

// PluginRegistry maps a tool name to its Plugin implementation, used to
// validate a TriggerCommand's tool/operation and to know each tool's
// plan/apply operation names.
type PluginRegistry map[string]plugin.Plugin

// NewPluginRegistry constructs a PluginRegistry from every Plugin this
// codebase currently provides — only Helmfile until Slice 7 adds
// Terraform and Pulumi.
func NewPluginRegistry() PluginRegistry {
	return PluginRegistry{
		config.ToolHelmfile: plugin.NewHelmfilePlugin(),
	}
}
