package orchestrator

import (
	"fmt"
	"slices"
	"strings"
)

// overrideServiceAccount is the one Project field an operator may permit a
// repository to set for itself, via TURNIP_ALLOWED_OVERRIDES.
//
// A list rather than a flag per field: the configuration file is read from
// the pull request's own head commit, so every setting that reaches out of
// the repository — an identity today, pod labels or resources tomorrow —
// needs the same gate, and a boolean per setting does not generalize.
// Atlantis reached the same shape with `allowed_overrides`.
//
// Only settings the Server itself also provides belong here, because an
// override is by definition a repository replacing a Server-supplied
// value. A Project's `uses` is not one: turnip has no Server-side tool to
// fall back to, so the tool can only come from the repository, and
// "gating" the one field every Project must set has no coherent meaning.
// Its version is repository-owned for the same reason Atlantis leaves
// `terraform_version` out of `allowed_overrides` entirely.
const overrideServiceAccount = "runner.serviceAccount"

// overrideCloneSubmodules lets a repository choose how its own submodules
// are cloned. Unlike the path above it is repository-scoped rather than
// per-Project: one clone serves every Project a pull request matches, so
// two Projects disagreeing would have no coherent resolution.
//
// It is gated for cost rather than for safety. Fetching a submodule the
// App can already read grants no capability the repository does not
// already have, so by the principle that gates belong on what *grants*
// capability this would not need gating at all. It is listed because an
// operator may still have reason to refuse the fetch — an expensive
// submodule the IaC never reads — and reusing this list costs nothing.
const overrideCloneSubmodules = "clone.submodules"

// knownOverridePaths is sorted so error messages list them stably.
var knownOverridePaths = []string{overrideCloneSubmodules, overrideServiceAccount}

// defaultAllowedOverrides permits nothing — exactly what turnip did before
// this setting existed, where a repository could never choose its own
// ServiceAccount.
func defaultAllowedOverrides() map[string]bool {
	return map[string]bool{}
}

// parseAllowedOverrides reads the comma-separated list. Unset or blank
// permits nothing, which is both the default and the safe end of the
// range, so no special case is needed for it.
//
// An unrecognized path is an error rather than a no-op. Accepting it would
// gate nothing while looking like it gated something, which is the
// operator-side version of the silently-ignored key this schema version
// removes from the configuration file.
func parseAllowedOverrides(raw string) (map[string]bool, error) {
	allowed := make(map[string]bool, len(knownOverridePaths))
	for _, field := range strings.Split(raw, ",") {
		path := strings.TrimSpace(field)
		if path == "" {
			continue
		}
		if !slices.Contains(knownOverridePaths, path) {
			return nil, fmt.Errorf(
				"unknown override path %q; known paths are %s",
				path, strings.Join(knownOverridePaths, ", "),
			)
		}
		allowed[path] = true
	}
	return allowed, nil
}
