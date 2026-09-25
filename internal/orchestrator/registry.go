package orchestrator

import (
	"slices"

	"github.com/ivanvc/turnip/internal/plugin"
)

// PluginRegistry maps a tool name to its Plugin implementation, used to
// validate a TriggerCommand's tool/operation and to know each tool's
// plan/apply operation names. The Orchestrator takes it as a parameter,
// rather than calling plugin.Registry() itself, so tests can inject fakes.
type PluginRegistry map[string]plugin.Plugin

// NewPluginRegistry returns every Plugin turnip ships, from the one
// registry the Runner also selects from, so the two cannot disagree about
// which tools exist.
func NewPluginRegistry() PluginRegistry {
	return plugin.Registry()
}

// Names returns the registered tool names, sorted so that the errors which
// list them (an unknown uses:, a missing one) read the same on every call.
// They are the vocabulary config.Parse and github.ParseTriggers accept.
func (r PluginRegistry) Names() []string {
	names := make([]string, 0, len(r))
	for name := range r {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}
