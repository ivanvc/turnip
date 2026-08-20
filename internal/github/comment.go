package github

import (
	"fmt"
	"strings"
)

const maxCommentLength = 65536 // GitHub's documented PR/issue comment cap

// minReserve is a floor on how much of maxCommentLength each detail-section
// piece assumes is unavailable to it for the summary table (group 0) or
// continuation header (later groups) — see reserveFor.
const minReserve = 128

// BuildConsolidatedComment renders results into one or more comment
// bodies, splitting across bodies only when the combined content would
// exceed maxCommentLength. A single Project's Output that alone is too
// large for one body is split across multiple <details> pieces — each
// closing its own code fence before the body ends, and the next piece
// reopening one — rather than losing content to truncation.
func BuildConsolidatedComment(results []ProjectResult) []string {
	if len(results) == 0 {
		return nil
	}

	table := buildSummaryTable(results)
	reserve := reserveFor(table)

	var pieces []string
	for _, r := range results {
		pieces = append(pieces, splitDetailSection(r, reserve)...)
	}

	groups := packSections(table, pieces)

	bodies := make([]string, len(groups))
	for i, group := range groups {
		var b strings.Builder
		if i == 0 {
			b.WriteString(table)
			b.WriteString("\n\n")
		} else {
			fmt.Fprintf(&b, "_(continued %d/%d)_\n\n", i+1, len(groups))
		}
		b.WriteString(strings.Join(group, "\n\n"))
		bodies[i] = truncateBody(b.String())
	}

	return bodies
}

// reserveFor returns how much of maxCommentLength a single detail-section
// piece must assume is unavailable to it, regardless of which body it
// eventually lands in. The summary table (group 0's overhead) is
// virtually always larger than a continuation header (later groups'
// overhead — a short "_(continued N/M)_" line), so reserving against the
// table is the conservative choice for every piece, wherever it lands.
func reserveFor(table string) int {
	return max(len(table), minReserve)
}

// splitDetailSection renders r as one or more self-contained <details>
// pieces, each guaranteed to fit within maxCommentLength-reserve on its
// own. A piece never depends on another piece within the same comment
// body to be valid markdown: every piece opens and closes its own code
// fence and <details> tag.
func splitDetailSection(r ProjectResult, reserve int) []string {
	budget := maxCommentLength - reserve

	full := buildDetailSectionPart(r, r.Output, 1, 1)
	if len(full) <= budget {
		return []string{full}
	}

	// Estimate one piece's non-Output scaffold size using a pessimistic
	// (4-digit) part/total placeholder, so the real "part N/M" suffix -
	// whatever N and M turn out to be - never ends up making a piece a
	// few bytes larger than budgeted.
	scaffold := len(buildDetailSectionPart(r, "", 9999, 9999))
	chunkCap := max(budget-scaffold, 1)

	total := (len(r.Output) + chunkCap - 1) / chunkCap // ceil division

	pieces := make([]string, 0, total)
	output := r.Output
	for part := 1; part <= total; part++ {
		n := min(chunkCap, len(output))
		pieces = append(pieces, buildDetailSectionPart(r, output[:n], part, total))
		output = output[n:]
	}
	return pieces
}

// truncateBody bounds one fully-assembled comment body to maxCommentLength
// as a last-resort safety net — splitDetailSection already sizes every
// piece to fit, so this should not normally fire, but a few bytes of
// per-piece join/header overhead aren't accounted for during packing. The
// marker closes the code fence and <details> tag it may be cutting
// through, so the clamped body stays valid markdown, and is appended
// last so it's never itself clipped by the length check.
func truncateBody(body string) string {
	if len(body) <= maxCommentLength {
		return body
	}
	const marker = "\n```\n\n_(truncated)_\n</details>"
	keep := max(maxCommentLength-len(marker), 0)
	return body[:keep] + marker
}

// packSections greedily groups pieces into comment bodies, keeping each
// body's total content under maxCommentLength. The first group's budget
// accounts for the summary table; later groups start fresh (their
// continuation header is small enough not to need accounting for here).
func packSections(table string, pieces []string) [][]string {
	var groups [][]string
	var current []string
	currentLen := len(table)

	for _, piece := range pieces {
		if len(current) > 0 && currentLen+len(piece) > maxCommentLength {
			groups = append(groups, current)
			current = nil
			currentLen = 0
		}
		current = append(current, piece)
		currentLen += len(piece)
	}
	if len(current) > 0 {
		groups = append(groups, current)
	}

	return groups
}

func buildSummaryTable(results []ProjectResult) string {
	var b strings.Builder
	b.WriteString("| Project | Operation | Status |\n")
	b.WriteString("|---|---|---|\n")
	for _, r := range results {
		fmt.Fprintf(&b, "| %s | %s | %s |\n", r.ProjectName, r.Operation, statusEmoji(r.Success))
	}
	return b.String()
}

// buildDetailSectionPart renders one self-contained <details> piece
// carrying chunk (a whole Output, or one slice of a split Output).
// total == 1 omits the "part N/M" suffix entirely, for the common case
// where a Project's Output fits in a single piece.
func buildDetailSectionPart(r ProjectResult, chunk string, part, total int) string {
	summary := fmt.Sprintf("%s (%s) — %s", r.ProjectName, r.Operation, statusWord(r.Success))
	if total > 1 {
		summary += fmt.Sprintf(" (output part %d/%d)", part, total)
	}
	return fmt.Sprintf("<details>\n<summary>%s</summary>\n\n```\n%s\n```\n\n</details>", summary, chunk)
}

func statusEmoji(success bool) string {
	if success {
		return "✅ success"
	}
	return "❌ failure"
}

func statusWord(success bool) string {
	if success {
		return "success"
	}
	return "failure"
}
