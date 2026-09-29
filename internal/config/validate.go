package config

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// configFileRef is the ProjectRef used for problems that belong to the
// file as a whole rather than to any one project.
const configFileRef = "turnip.yaml"

// usesExample is the fixed example the uses: errors show. It names a real
// published tag so that copying it works, whichever tools are registered.
const usesExample = "helmfile@v1.7.4"

// allowedImagesSetting is the Server setting an operator adds an Entry to,
// named in the error for a uses: line no Entry allows.
const allowedImagesSetting = "TURNIP_ALLOWED_IMAGES"

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
func validate(c *Config, catalog Catalog) error {
	var errs ValidationErrors

	seenNames := make(map[string]bool, len(c.Projects))

	for i := range c.Projects {
		p := &c.Projects[i]
		ref := projectRef(*p, i)

		if p.Directory == "" {
			errs = append(errs, &ValidationError{
				ProjectRef: ref,
				Field:      "directory",
				Message:    "directory is required",
			})
		}

		errs = append(errs, resolveUses(p, ref, catalog)...)

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

		errs = append(errs, validateRunnerEnv(p.Runner.Env, ref)...)
	}

	// The top-level runner: block is checked by the same rules, once,
	// against the file rather than against every Project that inherits it.
	errs = append(errs, validateRunnerEnv(c.Runner.Env, configFileRef)...)

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

// validateRunnerEnv checks the variable names of one runner: block's env,
// reporting each violation against ref, the block's owner: a Project, or
// the file for the top-level block.
func validateRunnerEnv(env map[string]string, ref string) ValidationErrors {
	var errs ValidationErrors
	// Sorted so that a block with several offending names reports them in
	// a stable order rather than Go's randomized map order.
	for _, name := range slices.Sorted(maps.Keys(env)) {
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
