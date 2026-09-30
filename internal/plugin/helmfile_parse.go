package plugin

import (
	"regexp"
	"strings"
)

var comparingReleaseRe = regexp.MustCompile(`^Comparing release=(\S+),`)

// ansiSGRRe matches an ANSI SGR (color/style) escape sequence, e.g.
// "\x1b[1;34m" or "\x1b[0m". Helmfile's framed banner is bold blue unless
// color is off, and the raw escape bytes would otherwise break the
// frame-detection below.
var ansiSGRRe = regexp.MustCompile("\x1b\\[[0-9;]*m")

// stripANSI removes every ANSI SGR escape sequence from s.
func stripANSI(s string) string {
	return ansiSGRRe.ReplaceAllString(s, "")
}

// releaseTableKind says how a Release_Table's rows are counted.
type releaseTableKind int

const (
	tableChanged releaseTableKind = iota
	tableDestroyed
)

// releaseTableTitles maps a counted framed banner's title, exactly as
// Helmfile v1.6.0 on prints it, to how its rows are counted. The Failed
// and Failed to Delete tables are not listed: an unknown title's rows are
// skipped, and every table's rows end at the next banner of any title, so
// a failed table's rows are never read as the body of the table before it
// even without the blank line between them (Requirement 1.3, 3).
var releaseTableTitles = map[string]releaseTableKind{
	"Updated Releases":     tableChanged,
	"Reinstalled Releases": tableChanged,
	"Deleted Releases":     tableDestroyed,
}

// bannerTitle reports the title framed by a trimmed line shaped
// "=…title…=", such as "========== Updated Releases ==========" or
// "==== Updated Releases ====". ok is false for any other line, including
// the older "UPDATED RELEASES:" banner, which carries no "=" frame.
func bannerTitle(line string) (title string, ok bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "=") || !strings.HasSuffix(trimmed, "=") {
		return "", false
	}
	inner := strings.TrimSpace(strings.Trim(trimmed, "="))
	if inner == "" {
		return "", false
	}
	return inner, true
}

// parseReleaseTables counts the releases in every Release_Table of a
// Mutating_Operation's stderr, summed over every table Helmfile printed
// (one set per sub-helmfile that changed something). Only the
// "="-framed banner Helmfile prints from v1.6.0 on is recognized; see
// MutationChangesReported for the older, upper-case banner.
func parseReleaseTables(output string) ChangeSummary {
	lines := strings.Split(stripANSI(output), "\n")

	var summary ChangeSummary
	i := 0
	for i < len(lines) {
		title, ok := bannerTitle(lines[i])
		if !ok {
			i++
			continue
		}
		kind, known := releaseTableTitles[title]
		i++
		if !known {
			continue
		}

		// The header row: the first non-blank line after the banner, skipped.
		for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
			i++
		}
		if i < len(lines) {
			i++
		}

		count := 0
		for i < len(lines) {
			if strings.TrimSpace(lines[i]) == "" {
				i++
				break
			}
			if _, isBanner := bannerTitle(lines[i]); isBanner {
				break
			}
			count++
			i++
		}

		switch kind {
		case tableChanged:
			summary.Change += count
		case tableDestroyed:
			summary.Destroy += count
		}
	}

	return summary
}

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
		// turnip's own annotation lines are not the tool's output and
		// must not count as a release's body. Without this the trailer,
		// which follows the last release, makes that release look changed
		// on every diff that ends with an unchanged one.
		if strings.HasPrefix(line, transcriptPrefix) {
			continue
		}
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
