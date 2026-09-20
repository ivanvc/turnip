package config

import (
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// MatchProjects returns the subset of projects whose WhenModified patterns
// match at least one of modifiedFiles. It never returns an error: pattern
// syntax is validated eagerly in Parse, so by the time a *Config exists,
// its patterns are known-valid.
func MatchProjects(projects []Project, modifiedFiles []string) []Project {
	var matched []Project

	for _, p := range projects {
		if projectMatches(p, modifiedFiles) {
			matched = append(matched, p)
		}
	}

	return matched
}

// MatchProjectsByName returns the projects whose Name matches at least one
// pattern, in configuration order and without duplicates, together with
// the patterns that matched nothing.
//
// Patterns use the same dialect WhenModified does — doublestar.Match — so
// "gcp/*" reaches "gcp/project" but not "gcp/team/project", and "gcp/**"
// reaches both. One glob dialect per configuration file is the point: a
// reader who has written a whenModified pattern has already learned this.
//
// A caller wanting *every* project must not pass "*" as a pattern. "*"
// stops at a separator, so it would silently skip every project named for
// its path — which is why the trigger grammar treats a bare "*" as a
// reserved word rather than routing it here.
func MatchProjectsByName(projects []Project, patterns []string) (matched []Project, unmatched []string) {
	hit := make(map[int]bool, len(projects))

	for _, pattern := range patterns {
		found := false
		for i, p := range projects {
			if ok, _ := doublestar.Match(pattern, p.Name); ok {
				hit[i] = true
				found = true
			}
		}
		if !found {
			unmatched = append(unmatched, pattern)
		}
	}

	for i, p := range projects {
		if hit[i] {
			matched = append(matched, p)
		}
	}
	return matched, unmatched
}

func projectMatches(p Project, modifiedFiles []string) bool {
	for _, pattern := range p.WhenModified {
		for _, f := range modifiedFiles {
			if ok, _ := doublestar.Match(pattern, normalize(f)); ok {
				return true
			}
		}
	}
	return false
}

// normalize strips a leading "./" and converts "\"-style separators to
// "/", since patterns are always written and compared as repo-root-relative,
// forward-slash paths.
func normalize(path string) string {
	path = strings.TrimPrefix(path, "./")
	return strings.ReplaceAll(path, "\\", "/")
}
