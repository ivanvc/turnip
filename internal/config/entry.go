package config

import (
	"fmt"
	"maps"
	"path"
	"slices"
	"strings"
)

// Entry allows one image, for one tool, with the Tag_Specs its glob matches.
type Entry struct {
	Tool  string
	Image string // fully qualified repository, no tag
	Glob  string // over the Tag_Spec's text
}

// String renders the Entry in the form it is written in, tool:image@glob.
func (e Entry) String() string {
	return e.Tool + ":" + e.Image + "@" + e.Glob
}

// Catalog is what this Server will run: each Alias's Entries and the
// operator's Access_List.
type Catalog struct {
	Aliases map[string][]Entry // keyed by Tool_Name
	Allowed []Entry            // the Access_List
}

// EntryError reports one tool:image@glob line that cannot be an Entry.
type EntryError struct {
	Line   string
	Reason string
}

func (e *EntryError) Error() string {
	return fmt.Sprintf("config: allowed image %q: %s", e.Line, e.Reason)
}

// ParseEntry parses one tool:image@glob line. tools is the set of
// registered Tool_Names: the text before the first ":" is read as the
// tool only when it is one of them, which is also what keeps a registry
// port (registry.local:5000/...) from being read as a tool.
//
// The image must be fully qualified and carry no tag or digest of its
// own; the glob is what follows the last "@".
func ParseEntry(line string, tools []string) (Entry, error) {
	line = strings.TrimSpace(line)
	fail := func(format string, args ...any) (Entry, error) {
		return Entry{}, &EntryError{Line: line, Reason: fmt.Sprintf(format, args...)}
	}

	tool, rest, found := strings.Cut(line, ":")
	if !found || tool == "" {
		return fail("must be tool:image@glob")
	}
	if !slices.Contains(tools, tool) {
		return fail("%q is not a tool turnip runs (one of: %s)", tool, strings.Join(slices.Sorted(slices.Values(tools)), ", "))
	}

	at := strings.LastIndex(rest, "@")
	if at < 0 {
		return fail("must be tool:image@glob; name the Tag_Specs it allows after an @, e.g. %s:%s@*", tool, rest)
	}
	image, glob := rest[:at], rest[at+1:]
	if image == "" {
		return fail("must be tool:image@glob; the image is empty")
	}
	if glob == "" {
		return fail("must be tool:image@glob; the glob is empty (@* allows any Tag_Spec)")
	}
	if _, err := path.Match(glob, ""); err != nil {
		return fail("glob %q is malformed", glob)
	}

	if strings.Contains(image, "@") {
		return fail("image %q carries a digest of its own; the glob after the last @ says which Tag_Specs it allows", image)
	}
	if strings.Contains(image[strings.LastIndex(image, "/")+1:], ":") {
		return fail("image %q carries a tag of its own; the glob after the @ says which Tag_Specs it allows", image)
	}
	if !isQualifiedImage(image) {
		return fail("image %q is not fully qualified; write its registry host (e.g. docker.io/%s)", image, image)
	}

	return Entry{Tool: tool, Image: image, Glob: glob}, nil
}

// isQualifiedImage reports whether image names its registry host: its
// first path element contains a "." or a ":", or is localhost. Anything
// else is resolved by the node's container runtime against whatever
// default registry it has, so the same name could mean different images
// on different clusters.
func isQualifiedImage(image string) bool {
	host, _, found := strings.Cut(image, "/")
	if !found {
		return false
	}
	return strings.ContainsAny(host, ".:") || host == "localhost"
}

// ImageConflict is one image listed for more than one tool.
type ImageConflict struct {
	Image string
	Tools []string // sorted
}

func (c ImageConflict) Error() string {
	return fmt.Sprintf("config: image %q is listed for more than one tool (%s)", c.Image, strings.Join(c.Tools, ", "))
}

// ImageConflicts reports every image the Catalog lists, by an Alias or
// the Access_List, for two or more different tools, sorted by image. A
// Project's tool is the one its matching Entry declares, so an image
// listed for two tools leaves turnip unable to tell which Plugin to drive
// it with. Entries for the same tool may repeat an image freely.
func ImageConflicts(c Catalog) []ImageConflict {
	toolsByImage := map[string]map[string]struct{}{}
	add := func(e Entry) {
		if toolsByImage[e.Image] == nil {
			toolsByImage[e.Image] = map[string]struct{}{}
		}
		toolsByImage[e.Image][e.Tool] = struct{}{}
	}
	for _, entries := range c.Aliases {
		for _, e := range entries {
			add(e)
		}
	}
	for _, e := range c.Allowed {
		add(e)
	}

	var conflicts []ImageConflict
	for image, tools := range toolsByImage {
		if len(tools) < 2 {
			continue
		}
		conflicts = append(conflicts, ImageConflict{Image: image, Tools: slices.Sorted(maps.Keys(tools))})
	}
	slices.SortFunc(conflicts, func(a, b ImageConflict) int { return strings.Compare(a.Image, b.Image) })
	return conflicts
}
