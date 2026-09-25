package config

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// configFileRef is the ProjectRef used for problems that belong to the
// file as a whole rather than to any one project.
const configFileRef = "turnip.yaml"

// versionPattern is a permissive semver-shaped check ("v1.7.4", "1.9.5",
// "3.130.0-rc1"), not a vendor-specific grammar. It rejects obviously-wrong
// input (typos, a floating tag like "latest", stray whitespace) rather
// than enumerating what a vendor has published: turnip must never lag
// behind a tool's latest release, so any well-formed version is accepted
// whether or not turnip has heard of it.
//
// The leading "v" is optional because the version is the image tag as
// written, and vendors differ: helmfile's tags carry one, terraform's do
// not. Checking it here puts a malformed version on the pull request
// rather than failing when the Runner's image is pulled.
var versionPattern = regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$`)

// usesExample is the fixed example the uses: errors show. It names a real
// published tag so that copying it works, whichever tools are registered.
const usesExample = "helmfile@v1.7.4"

func isWellFormedVersion(v string) bool {
	return versionPattern.MatchString(v)
}

// validateSchemaVersion checks the one field that explains every other
// error in a file written for a different schema. Parse calls it before
// strict field checking so that such a file reports its version rather
// than one unknown-key error per field it carries.
func validateSchemaVersion(c *Config) error {
	var errs ValidationErrors

	switch {
	case c.SchemaVersion == "":
		errs = append(errs, &ValidationError{
			ProjectRef: configFileRef,
			Field:      "schemaVersion",
			Message:    fmt.Sprintf("required; set schemaVersion: %s", SupportedSchemaVersion),
		})
	case c.SchemaVersion != SupportedSchemaVersion:
		errs = append(errs, &ValidationError{
			ProjectRef: configFileRef,
			Field:      "schemaVersion",
			Message:    fmt.Sprintf("unsupported version %q; this turnip supports %q", c.SchemaVersion, SupportedSchemaVersion),
		})
	}

	if len(errs) == 0 {
		return nil
	}
	return errs
}

// validate checks a parsed Config's projects, accumulating every violation
// found rather than stopping at the first one. schemaVersion is not
// checked here; see validateSchemaVersion.
func validate(c *Config, tools []string) error {
	var errs ValidationErrors

	seenNames := make(map[string]bool, len(c.Projects))

	for i, p := range c.Projects {
		ref := projectRef(p, i)

		if p.Directory == "" {
			errs = append(errs, &ValidationError{
				ProjectRef: ref,
				Field:      "directory",
				Message:    "directory is required",
			})
		}

		errs = append(errs, validateUses(p, ref, tools)...)

		// Names a trigger line could never address are rejected here, where
		// the file is written, rather than when someone tries to select the
		// Project and gets silence. Both checks run against the effective
		// name: applyDefaults has already copied directory into an empty
		// name by this point, so a directory named "-infra" is caught too.
		if p.Name != "" {
			if strings.Contains(p.Name, "*") {
				errs = append(errs, &ValidationError{
					ProjectRef: ref,
					Field:      "name",
					Message:    fmt.Sprintf(`project name %q cannot contain "*": a trigger reads a token containing "*" as a pattern, so this project could never be named directly`, p.Name),
				})
			}
			if strings.HasPrefix(p.Name, "-") {
				errs = append(errs, &ValidationError{
					ProjectRef: ref,
					Field:      "name",
					Message:    fmt.Sprintf(`project name %q cannot begin with "-": a trigger reads the first "-" token as the start of tool arguments`, p.Name),
				})
			}
			if seenNames[p.Name] {
				errs = append(errs, &ValidationError{
					ProjectRef: ref,
					Field:      "name",
					Message:    fmt.Sprintf("duplicate project name %q", p.Name),
				})
			}
			seenNames[p.Name] = true
		}

		for j, pattern := range p.WhenModified {
			if !doublestar.ValidatePattern(pattern) {
				errs = append(errs, &ValidationError{
					ProjectRef: ref,
					Field:      fmt.Sprintf("whenModified[%d]", j),
					Message:    fmt.Sprintf("invalid glob pattern %q", pattern),
				})
			}
		}

		// Sorted so that a file with several offending names reports them
		// in a stable order rather than Go's randomized map order.
		for _, name := range slices.Sorted(maps.Keys(p.Runner.Env)) {
			switch {
			case strings.HasPrefix(name, reservedEnvPrefix):
				errs = append(errs, &ValidationError{
					ProjectRef: ref,
					Field:      fmt.Sprintf("runner.env[%q]", name),
					Message:    fmt.Sprintf("names beginning with %q are reserved", reservedEnvPrefix),
				})
			case name == "PATH":
				errs = append(errs, &ValidationError{
					ProjectRef: ref,
					Field:      fmt.Sprintf("runner.env[%q]", name),
					Message:    "PATH is reserved; the tools directory is prepended to it at startup",
				})
			}
		}
	}

	// clone: belongs to the file rather than to any one Project, so its
	// problems are reported against the file itself. An empty value is
	// valid and means "unset" — the Server's default applies.
	switch c.Clone.Submodules {
	case "", SubmodulesNone, SubmodulesTopLevel, SubmodulesRecursive:
	default:
		errs = append(errs, &ValidationError{
			ProjectRef: configFileRef,
			Field:      "clone.submodules",
			Message: fmt.Sprintf(
				"unrecognized value %q; expected %q, %q or %q",
				c.Clone.Submodules, SubmodulesNone, SubmodulesTopLevel, SubmodulesRecursive,
			),
		})
	}

	if len(errs) == 0 {
		return nil
	}
	return errs
}

// validateUses checks a Project's tool reference. It reads the raw Uses
// string as well as the decomposed parts, because only the raw form
// distinguishes "no version given" from a trailing "@" with nothing after
// it, and the two call for different advice.
func validateUses(p Project, ref string, tools []string) ValidationErrors {
	var errs ValidationErrors

	if p.Uses == "" {
		return ValidationErrors{{
			ProjectRef: ref,
			Field:      "uses",
			Message: fmt.Sprintf(
				"required; set uses: <tool>@<version>, e.g. %q, where <tool> is %s",
				usesExample, oneOf(tools),
			),
		}}
	}

	switch {
	case p.Tool == "":
		errs = append(errs, &ValidationError{
			ProjectRef: ref,
			Field:      "uses",
			Message:    fmt.Sprintf("%q names no tool before %q", p.Uses, "@"),
		})
	case !slices.Contains(tools, p.Tool):
		errs = append(errs, &ValidationError{
			ProjectRef: ref,
			Field:      "uses",
			Message:    fmt.Sprintf("unsupported tool %q, must be %s", p.Tool, oneOf(tools)),
		})
	}

	// turnip keeps no default version: one would choose what a Project runs
	// from turnip's release rather than from the repository, and change it
	// with no diff to review.
	_, rawVersion, found := strings.Cut(p.Uses, "@")
	switch {
	case !found:
		errs = append(errs, &ValidationError{
			ProjectRef: ref,
			Field:      "uses",
			Message:    fmt.Sprintf("%q names no version; name one as <tool>@<version>, e.g. %q", p.Uses, usesExample),
		})
	case rawVersion == "":
		errs = append(errs, &ValidationError{
			ProjectRef: ref,
			Field:      "uses",
			Message:    fmt.Sprintf(`version is empty after "@"; name one, e.g. %q`, usesExample),
		})
	case !isWellFormedVersion(p.ToolVersion):
		errs = append(errs, &ValidationError{
			ProjectRef: ref,
			Field:      "uses",
			Message: fmt.Sprintf(
				"%q is not a well-formed version (expected roughly semver, e.g. %q); floating tags are not accepted",
				rawVersion, "v1.7.4",
			),
		})
	}

	return errs
}

// oneOf renders the registered tool names for an error message, in
// alphabetical order whatever order the caller gave them in: no Plugin is
// listed first by virtue of where it was registered.
func oneOf(tools []string) string {
	if len(tools) == 0 {
		return "one of the registered tools, and none are registered"
	}
	quoted := make([]string, len(tools))
	for i, t := range slices.Sorted(slices.Values(tools)) {
		quoted[i] = strconv.Quote(t)
	}
	return "one of " + strings.Join(quoted, ", ")
}

func projectRef(p Project, index int) string {
	if p.Name != "" {
		return p.Name
	}
	return fmt.Sprintf("projects[%d]", index)
}
