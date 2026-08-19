package plugin

import (
	"regexp"
	"strings"
)

var comparingReleaseRe = regexp.MustCompile(`^Comparing release=(\S+),`)

// parseChangedReleases counts releases in `helmfile diff` output whose
// "Comparing release=<name>, chart=<chart>" header is followed by at least
// one non-blank line of diff body before the next such header (or EOF). A
// header with no body before the next header means that release had no
// changes.
func parseChangedReleases(output string) int {
	lines := strings.Split(output, "\n")

	changed := 0
	inBody := false
	sawBodyLine := false

	for _, line := range lines {
		if comparingReleaseRe.MatchString(line) {
			if inBody && sawBodyLine {
				changed++
			}
			inBody = true
			sawBodyLine = false
			continue
		}
		if inBody && strings.TrimSpace(line) != "" {
			sawBodyLine = true
		}
	}

	if inBody && sawBodyLine {
		changed++
	}

	return changed
}
