package orchestrator

import (
	"fmt"
	"slices"
	"strings"

	"github.com/ivanvc/turnip/internal/config"
)

// submoduleModes is sorted so error messages list them stably, matching
// knownOverridePaths' convention.
var submoduleModes = []string{
	config.SubmodulesNone,
	config.SubmodulesRecursive,
	config.SubmodulesTopLevel,
}

// parseSubmodules reads TURNIP_CLONE_SUBMODULES.
//
// Unset means top-level rather than off, diverging from actions/checkout's
// default: that action checks out repositories for arbitrary purposes,
// where turnip clones specifically to run IaC that may reference submodule
// paths.
//
// An unrecognized value is a startup error rather than a silently ignored
// setting, for the same reason parseAllowedOverrides rejects an unknown
// path: a value that looks like it configured something while configuring
// nothing is worse than a refusal to start.
func parseSubmodules(raw string) (string, error) {
	mode := strings.TrimSpace(raw)
	if mode == "" {
		return config.SubmodulesTopLevel, nil
	}
	if !slices.Contains(submoduleModes, mode) {
		return "", fmt.Errorf(
			"unknown submodule mode %q; known modes are %s",
			mode, strings.Join(submoduleModes, ", "),
		)
	}
	return mode, nil
}

// SubmodulesNotPermittedError reports a repository that set
// clone.submodules while the Server has that override disabled.
//
// It names no Project, unlike ServiceAccountNotPermittedError: the setting
// is repository-scoped, so the offending value belongs to the file rather
// than to any one Project matched by the pull request.
type SubmodulesNotPermittedError struct {
	Submodules string
}

func (e *SubmodulesNotPermittedError) Error() string {
	return fmt.Sprintf(
		"turnip.yaml requested clone.submodules %q, which this turnip deployment does not permit. "+
			"Add %q to TURNIP_ALLOWED_OVERRIDES on the Server to let turnip.yaml choose how submodules are cloned.",
		e.Submodules, overrideCloneSubmodules,
	)
}

// resolveSubmodules decides the Submodule_Mode a clone uses: the
// repository's own when the Server permits that override, otherwise the
// Server-wide default.
//
// Mirrors resolveServiceAccount deliberately, including being called
// before any lock is acquired, so a refusal costs nothing and reaches the
// pull request as a comment rather than as a failed Job.
func resolveSubmodules(clone config.CloneSpec, defaultMode string, allowed map[string]bool) (string, error) {
	requested := clone.Submodules
	if requested == "" {
		return defaultMode, nil
	}
	if !allowed[overrideCloneSubmodules] {
		return "", &SubmodulesNotPermittedError{Submodules: requested}
	}
	return requested, nil
}
