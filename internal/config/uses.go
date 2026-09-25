package config

import (
	"fmt"
	"maps"
	"path"
	"regexp"
	"slices"
	"strings"
)

// ToolNames returns the registered Tool_Names, the keys of Aliases,
// sorted.
func (c Catalog) ToolNames() []string {
	return slices.Sorted(maps.Keys(c.Aliases))
}

// resolveUses resolves a Project's uses: line against the Catalog and, when
// an Entry allows it, sets the Project's Tool, Image and ToolVersion. The
// checks run in the order the design's resolution table lists them, and
// the first that fails is the one reported: each later check presumes the
// earlier ones passed, so a second message would only restate the first.
//
// The line splits at its last "@". What precedes it is an Alias when it is
// a registered Tool_Name, and otherwise an Image_Reference; what follows
// is the Tag_Spec, kept verbatim. turnip keeps no version-shape check of
// its own: which Tag_Specs a line may name is decided only by the Entries.
func resolveUses(p *Project, ref string, catalog Catalog) ValidationErrors {
	fail := func(format string, args ...any) ValidationErrors {
		return ValidationErrors{{ProjectRef: ref, Field: "uses", Message: fmt.Sprintf(format, args...)}}
	}
	tools := catalog.ToolNames()

	if p.Uses == "" {
		return fail(
			"required; set uses: <tool>@<tag>, e.g. %q, where <tool> is %s, or a fully qualified <image>@<tag>",
			usesExample, oneOf(tools),
		)
	}

	// turnip keeps no default version: one would choose what a Project runs
	// from turnip's release rather than from the repository, and change it
	// with no diff to review.
	at := strings.LastIndex(p.Uses, "@")
	if at < 0 {
		if repo, tag, ok := splitRegistryTag(p.Uses); ok {
			return pastedTag(fail, p.Uses, repo+"@"+tag)
		}
		return fail("%q names no version; name one as <tool>@<tag>, e.g. %q", p.Uses, usesExample)
	}
	name, tagSpec := p.Uses[:at], p.Uses[at+1:]
	if name == "" {
		return fail("%q names no tool or image before %q", p.Uses, "@")
	}
	if tagSpec == "" {
		return fail(`version is empty after "@"; name one, e.g. %q`, usesExample)
	}

	// Registry form, image:tag, pasted into the @ form. Accepting ":" as a
	// synonym would leave two spellings of one thing, so the fix is spelled
	// out instead.
	if repo, _, ok := splitRegistryTag(name); ok {
		return pastedTag(fail, p.Uses, repo+"@"+tagSpec)
	}

	aliasEntries, isAlias := catalog.Aliases[name]
	if !isAlias && !isQualifiedImage(name) {
		return fail(
			"unsupported tool %q, must be %s; any other image must be fully qualified, registry host included (e.g. %q)",
			name, oneOf(tools), "ghcr.io/org/"+name+"@"+tagSpec,
		)
	}

	if msg := digestProblem(tagSpec); msg != "" {
		return fail("%q: %s", p.Uses, msg)
	}
	if !strings.Contains(tagSpec, ":") && !ociTag.MatchString(tagSpec) {
		return fail(
			"%q is not an exact tag; uses: takes one exact tag or sha256 digest, not a pattern or a range (a tag is letters, digits, \".\", \"_\" and \"-\", e.g. %q)",
			tagSpec, usesExample,
		)
	}

	// An Alias tries only its own Entries, so the vendor's image under any
	// other tag is reachable only written out in full, where a reviewer
	// sees it. A full reference tries every Entry for its image, the
	// Aliases' included, so the vendor's image in full is allowed exactly
	// as its Alias allows it.
	candidates := aliasEntries
	if !isAlias {
		candidates = catalog.entriesFor(name)
	}
	for _, e := range candidates {
		if matched, _ := path.Match(e.Glob, tagSpec); matched {
			p.Tool, p.Image, p.ToolVersion = e.Tool, e.Image, tagSpec
			return nil
		}
	}

	if isAlias {
		return fail("%s", aliasRefusal(name, tagSpec, aliasEntries))
	}
	return fail(
		"this Server does not allow %q; to run it, the operator must add it to %s (e.g. <tool>:%s@%s)",
		name+"@"+tagSpec, allowedImagesSetting, name, tagSpec,
	)
}

// aliasRefusal explains a Tag_Spec an Alias's Entries do not match: which
// ones they do, and that any other tag of the vendor's image runs only
// written out in full and allowed by the operator.
func aliasRefusal(alias, tagSpec string, entries []Entry) string {
	globs := make([]string, 0, len(entries))
	image := ""
	for _, e := range entries {
		globs = append(globs, fmt.Sprintf("%q", e.Glob))
		image = e.Image
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%q is not a tag %s allows", tagSpec, alias)
	if len(globs) > 0 {
		fmt.Fprintf(&b, "; it allows only tags matching %s", strings.Join(globs, " or "))
	}
	if exampleAlias, _, _ := strings.Cut(usesExample, "@"); exampleAlias == alias {
		fmt.Fprintf(&b, ", e.g. %q", usesExample)
	}
	if image != "" {
		fmt.Fprintf(&b, ". To run %q, write the image in full and the operator must add it to %s", image+"@"+tagSpec, allowedImagesSetting)
	}
	return b.String()
}

// pastedTag reports a reference written in registry form, image:tag,
// showing the line as it should read.
func pastedTag(fail func(string, ...any) ValidationErrors, uses, corrected string) ValidationErrors {
	return fail("%q names its tag with \":\"; write it after an \"@\": %q", uses, corrected)
}

// splitRegistryTag reports whether ref carries a ":tag" after its last
// "/", as an image in registry form does, and returns the repository and
// the tag apart. A ":" before the last "/" is a registry port and is left
// alone.
func splitRegistryTag(ref string) (repo, tag string, ok bool) {
	lastSlash := strings.LastIndex(ref, "/")
	colon := strings.LastIndex(ref, ":")
	if colon <= lastSlash {
		return "", "", false
	}
	return ref[:colon], ref[colon+1:], true
}

// ociTag is the OCI distribution grammar for a tag. It is a syntax check,
// not a version-shape one: it only keeps a pattern, a range or stray
// whitespace from passing as a tag, where a glob's metacharacters would
// otherwise match themselves.
var ociTag = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]{0,127}$`)

// digestProblem returns why a Tag_Spec in digest form, <algorithm>:<hex>,
// is not one turnip accepts, or "" when it is a Tag or a well-formed
// sha256 Digest. A Tag never contains ":", so any ":" marks a digest.
func digestProblem(tagSpec string) string {
	algorithm, hex, found := strings.Cut(tagSpec, ":")
	if !found {
		return ""
	}
	if algorithm != "sha256" {
		return fmt.Sprintf("digest algorithm %q is not accepted; only sha256 digests are (sha256:<64 hex characters>)", algorithm)
	}
	if len(hex) != 64 || strings.Trim(hex, "0123456789abcdef") != "" {
		return "a sha256 digest is \"sha256:\" followed by exactly 64 lowercase hex characters"
	}
	return ""
}

// entriesFor returns every Entry, the Aliases' first in Tool_Name order and
// then the Access_List's, whose image is image.
func (c Catalog) entriesFor(image string) []Entry {
	var out []Entry
	for _, tool := range c.ToolNames() {
		for _, e := range c.Aliases[tool] {
			if e.Image == image {
				out = append(out, e)
			}
		}
	}
	for _, e := range c.Allowed {
		if e.Image == image {
			out = append(out, e)
		}
	}
	return out
}
