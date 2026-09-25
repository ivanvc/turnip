package orchestrator

import (
	"fmt"
	"strings"

	"github.com/ivanvc/turnip/internal/config"
	"github.com/ivanvc/turnip/internal/provisioning"
)

// NewCatalog forms the Catalog a Server resolves uses: against: each
// Plugin's Alias Entries, from its registered name, its Provisioning image
// and its ImageTags globs, and the operator's Access_List as given. The
// image is stated once, by the Plugin, so an Alias cannot allow one
// repository while its Job runs another.
func NewCatalog(plugins PluginRegistry, allowed []config.Entry) config.Catalog {
	aliases := make(map[string][]config.Entry, len(plugins))
	for name, p := range plugins {
		image := p.Provisioning().Image
		globs := p.ImageTags()
		entries := make([]config.Entry, 0, len(globs))
		for _, glob := range globs {
			entries = append(entries, config.Entry{Tool: name, Image: image, Glob: glob})
		}
		aliases[name] = entries
	}
	return config.Catalog{Aliases: aliases, Allowed: allowed}
}

// AllowedImagesError names every offending Entry in TURNIP_ALLOWED_IMAGES
// at once, as parseAllowedOverrides names every unknown path: an operator
// fixing them one restart at a time would learn of the next only after
// the last.
type AllowedImagesError struct {
	Problems []string
}

func (e *AllowedImagesError) Error() string {
	return strings.Join(e.Problems, "; ")
}

// parseAllowedImages reads TURNIP_ALLOWED_IMAGES, a comma-separated list
// of tool:image@glob, and builds the Catalog from it and plugins. Unset or
// blank allows nothing beyond the built-in Aliases.
//
// The Server refuses to start, rather than skip an Entry, when one names
// a tool with no Plugin, an unqualified image, or an image carrying a tag
// or digest of its own, and when an image is listed for two different
// tools across the Aliases and the Access_List: a skipped Entry would
// look like it allowed something while allowing nothing, and an image
// with two tools leaves turnip unable to tell which Plugin drives it.
func parseAllowedImages(raw string, plugins PluginRegistry) (config.Catalog, error) {
	tools := plugins.Names()

	var (
		allowed  []config.Entry
		problems []string
	)
	for _, field := range strings.Split(raw, ",") {
		line := strings.TrimSpace(field)
		if line == "" {
			continue
		}
		entry, err := config.ParseEntry(line, tools)
		if err != nil {
			problems = append(problems, err.Error())
			continue
		}
		allowed = append(allowed, entry)
	}

	catalog := NewCatalog(plugins, allowed)
	for _, conflict := range config.ImageConflicts(catalog) {
		problems = append(problems, fmt.Sprintf("%s: %s", conflict.Error(), strings.Join(conflictingEntries(catalog, conflict.Image), ", ")))
	}

	if len(problems) > 0 {
		return config.Catalog{}, &AllowedImagesError{Problems: problems}
	}
	return catalog, nil
}

// conflictingEntries lists every Entry naming image, marking an Alias's so
// the operator can tell the ones they wrote from the ones they cannot
// change.
func conflictingEntries(catalog config.Catalog, image string) []string {
	var out []string
	for _, tool := range catalog.ToolNames() {
		for _, e := range catalog.Aliases[tool] {
			if e.Image == image {
				out = append(out, e.String()+" (built-in alias)")
			}
		}
	}
	for _, e := range catalog.Allowed {
		if e.Image == image {
			out = append(out, e.String())
		}
	}
	return out
}

// jobImage is the tool image a Job runs and whether it pulls on every
// start. A plan runs the Project's Tag_Spec as written: image:tag, pulled
// every time so the plan sees what the tag means now, or image@sha256:...,
// which cannot change and uses the node's cache. A Mutating_Operation runs
// exactly the digest its plan recorded, whatever the Tag_Spec says now.
func jobImage(project config.Project, isPlan bool, recordedDigest string) (ref string, pullAlways bool) {
	if !isPlan {
		return project.Image + "@" + recordedDigest, false
	}
	if strings.HasPrefix(project.ToolVersion, "sha256:") {
		return project.Image + "@" + project.ToolVersion, false
	}
	return project.Image + ":" + project.ToolVersion, true
}

// jobProvisioning is how the Project's tool reaches its Job. A Project
// running an image other than its Alias's runs RunInImage whatever the
// Plugin's Strategy: a custom image is chosen for what it carries beside
// the tool, which only running inside it provides.
func jobProvisioning(project config.Project, spec provisioning.Spec) provisioning.Spec {
	if project.Image != spec.Image {
		spec.Strategy = provisioning.RunInImage
		spec.BinaryPath = ""
	}
	return spec
}

// imageDigest extracts the "sha256:<hex>" digest from a container's
// imageID, or "" when it holds none that can be pulled again. Only a
// repository digest (repo@sha256:...) qualifies: a bare "sha256:..." is
// the runtime's local image ID, which names no manifest in any registry,
// so an apply built from it could never pull.
func imageDigest(imageID string) string {
	_, digest, found := strings.Cut(imageID, "@")
	if !found {
		return ""
	}
	hex, ok := strings.CutPrefix(digest, "sha256:")
	if !ok || len(hex) != 64 || strings.Trim(hex, "0123456789abcdef") != "" {
		return ""
	}
	return digest
}
