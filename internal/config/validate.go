package config

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// configFileRef is the ProjectRef used for problems that belong to the
// file as a whole rather than to any one project.
const configFileRef = "turnip.yaml"

// validate checks a parsed Config against Requirement 2's rules,
// accumulating every violation found rather than stopping at the first one.
func validate(c *Config) error {
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
		if p.Tool == "" {
			errs = append(errs, &ValidationError{
				ProjectRef: ref,
				Field:      "tool",
				Message:    "tool is required",
			})
		} else if !isSupportedTool(p.Tool) {
			errs = append(errs, &ValidationError{
				ProjectRef: ref,
				Field:      "tool",
				Message: fmt.Sprintf(
					"unsupported tool %q, must be one of %q, %q, %q",
					p.Tool, ToolTerraform, ToolPulumi, ToolHelmfile,
				),
			})
		}

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
		for _, name := range slices.Sorted(maps.Keys(p.Env)) {
			switch {
			case strings.HasPrefix(name, reservedEnvPrefix):
				errs = append(errs, &ValidationError{
					ProjectRef: ref,
					Field:      fmt.Sprintf("env[%q]", name),
					Message:    fmt.Sprintf("names beginning with %q are reserved", reservedEnvPrefix),
				})
			case name == "PATH":
				errs = append(errs, &ValidationError{
					ProjectRef: ref,
					Field:      fmt.Sprintf("env[%q]", name),
					Message:    "PATH is reserved; the tools directory is prepended to it at startup",
				})
			}
		}
	}

	if len(errs) == 0 {
		return nil
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
