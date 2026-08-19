package config

import (
	"fmt"

	"github.com/bmatcuk/doublestar/v4"
)

// validate checks a parsed Config against Requirement 2's rules,
// accumulating every violation found rather than stopping at the first one.
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
