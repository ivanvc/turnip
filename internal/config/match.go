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
