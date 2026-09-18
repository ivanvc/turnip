package config

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// configFileRef is the ProjectRef used for problems that belong to the
// file as a whole rather than to any one project.
const configFileRef = "turnip.yaml"

// versionPattern is a permissive semver-shaped check ("1.9.5", "0.170.1",
// "1.7.4-rc1") — not a vendor-specific grammar. It rejects obviously-wrong
// input (typos, a floating tag like "latest", stray whitespace) rather
// than enumerating what a vendor has published: turnip must never lag
// behind a tool's latest release, so any well-formed version is accepted
// whether or not turnip has heard of it.
//
// It lives here rather than in internal/jobs because internal/config is a
// leaf package that jobs imports, so the dependency only runs one way —
// and because rejecting a malformed version at parse time puts the error
// on the pull request instead of at Job-build time.
var versionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$`)

// IsWellFormedVersion reports whether v has the shape of a tool version.
// Exported for internal/jobs, which resolves the vendor image tag and
// would otherwise need its own copy of the same rule.
func IsWellFormedVersion(v string) bool {
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
func validate(c *Config) error {
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

		errs = append(errs, validateUses(p, ref)...)

		if p.Name != "" {
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
// distinguishes "no version given" (which means the default) from a
// trailing "@" with nothing after it (which is a mistake).
func validateUses(p Project, ref string) ValidationErrors {
	var errs ValidationErrors

	if p.Uses == "" {
		return ValidationErrors{{
			ProjectRef: ref,
			Field:      "uses",
			Message: fmt.Sprintf(
				"required; set uses: <tool> or <tool>@<version>, where <tool> is one of %q, %q, %q",
				ToolTerraform, ToolPulumi, ToolHelmfile,
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
	case !isSupportedTool(p.Tool):
		errs = append(errs, &ValidationError{
			ProjectRef: ref,
			Field:      "uses",
			Message: fmt.Sprintf(
				"unsupported tool %q, must be one of %q, %q, %q",
				p.Tool, ToolTerraform, ToolPulumi, ToolHelmfile,
			),
		})
	}

	if _, rawVersion, found := strings.Cut(p.Uses, "@"); found {
		switch {
		case rawVersion == "":
			errs = append(errs, &ValidationError{
				ProjectRef: ref,
				Field:      "uses",
				Message:    `version is empty after "@"; omit "@" to use the default version`,
			})
		case !IsWellFormedVersion(p.ToolVersion):
			errs = append(errs, &ValidationError{
				ProjectRef: ref,
				Field:      "uses",
				Message: fmt.Sprintf(
					"%q is not a well-formed version (expected roughly semver, e.g. %q); floating tags are not accepted",
					rawVersion, "1.9.5",
				),
			})
		}
	}

	return errs
}

func isSupportedTool(tool string) bool {
	switch tool {
	case ToolTerraform, ToolPulumi, ToolHelmfile:
		return true
	default:
		return false
	}
}

func projectRef(p Project, index int) string {
	if p.Name != "" {
		return p.Name
	}
	return fmt.Sprintf("projects[%d]", index)
}
