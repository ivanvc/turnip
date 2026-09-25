package plugin

// Registry returns every Plugin turnip ships, keyed by Name(). Adding a
// tool means writing its Plugin and adding it here, nowhere else.
//
// It is an explicit list rather than Plugins registering themselves from
// init(), so that which tools exist is answered by reading one place, and
// the Server and Runner binaries cannot link different sets. The map is
// built fresh on each call, so a caller that keeps one may be handed a
// different map in its tests without affecting anyone else's.
func Registry() map[string]Plugin {
	plugins := []Plugin{
		NewHelmfilePlugin(),
	}

	registry := make(map[string]Plugin, len(plugins))
	for _, p := range plugins {
		registry[p.Name()] = p
	}
	return registry
}
