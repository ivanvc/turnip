package github

import (
	"fmt"
	"html"
	"strings"
)

const maxCommentLength = 65536 // GitHub's documented PR/issue comment cap

// minReserve is a floor on how much of maxCommentLength each detail-section
// piece assumes is unavailable to it for the verdict line and footer
// (group 0) or a continuation header (later groups) — see reserveFor.
const minReserve = 128

// BuildConsolidatedComment renders results into one or more comment
// bodies, splitting across bodies only when the combined content would
// exceed maxCommentLength. A single Project's Output that alone is too
// large for one body is split across multiple <details> pieces — each
// closing its own code fence before the body ends, and the next piece
// reopening one — rather than losing content to truncation.
//
// There is no summary table. Each Project is one collapsible section
// whose <summary> carries its status and change counts, and GitHub
// renders that summary while the section is collapsed — so a reader scans
// a run without expanding anything, which is the job the table used to do
// (global Requirements 10.2 and 17.3, amended for this).
// notices are command-level caveats — a selector that matched nothing —
// rendered as a GitHub warning alert above the verdict, because they
// change how the verdict should be read. They are variadic so that the
// many callers with nothing to say stay unchanged.
func BuildConsolidatedComment(results []ProjectResult, notices ...string) []string {
	if len(results) == 0 {
		return nil
	}

	// head is what opens the first body: the notice alert, when there is
	// one, followed by the verdict line it qualifies.
	head := buildNoticeBlock(notices) + buildVerdictLine(results)
	footer := buildFooter(results)
	reserve := reserveFor(head, footer)

	var pieces []section
	for _, r := range results {
		pieces = append(pieces, splitDetailSection(r, reserve)...)
	}

	groups := packSections(head, pieces)

	bodies := make([]string, len(groups))
	lastGroup := len(groups) - 1
	for i, group := range groups {
		header := head + "\n\n"
		if i > 0 {
			header = fmt.Sprintf("_(continued %d/%d)_\n\n", i+1, len(groups))
		}

		// The footer describes the run as a whole, so it belongs once, at
		// the end of the last body a reader reaches.
		var groupFooter string
		if i == lastGroup {
			groupFooter = footer
		}

		bodies[i] = clampBody(header, group, groupFooter)
	}

	return bodies
}

// section is one <details> piece together with everything that produced
// it.
//
// The rendered string alone would be enough for assembly, but not for
// clamping: when a body must be cut *into* a section, the cut has to
// rebuild that section from the same builder that emitted it rather than
// parse its own output. Carrying the inputs is what makes that possible
// (Decision 6, tier 2).
type section struct {
	result ProjectResult
	chunk  string
	part   int
	total  int
}

func (s section) render() string {
	return buildDetailSectionPart(s.result, s.chunk, s.part, s.total)
}

// reserveFor returns how much of maxCommentLength a single detail-section
// piece must assume is unavailable to it, regardless of which body it
// lands in.
//
// Both the head (which opens the first body — the notice alert, if any,
// and the verdict line) and the footer (which closes the last) are
// counted, because a piece does not know which body it ends up in and the
// two can land on the same one. A continuation header is a short
// "_(continued N/M)_" line, always smaller than that pair, so reserving
// against them is the conservative choice everywhere.
//
// Counting the notice here is what stops a warning from costing an output
// section: without it the head grows, body 0 overshoots, and clampBody
// drops a section to make room.
func reserveFor(head, footer string) int {
	return max(len(head)+len(footer), minReserve)
}

// splitDetailSection renders r as one or more self-contained <details>
// pieces, each guaranteed to fit within maxCommentLength-reserve on its
// own. A piece never depends on another piece within the same comment
// body to be valid markdown: every piece opens and closes its own code
// fence and <details> tag.
func splitDetailSection(r ProjectResult, reserve int) []section {
	budget := maxCommentLength - reserve

	full := buildDetailSectionPart(r, r.Output, 1, 1)
	if len(full) <= budget {
		return []section{{result: r, chunk: r.Output, part: 1, total: 1}}
	}

	// Estimate one piece's non-Output scaffold size using a pessimistic
	// (4-digit) part/total placeholder, so the real "part N/M" suffix -
	// whatever N and M turn out to be - never ends up making a piece a
	// few bytes larger than budgeted. Passing equal part/total also means
	// the estimate includes the next-steps block, which only the final
	// piece carries.
	scaffold := len(buildDetailSectionPart(r, "", 9999, 9999))
	chunkCap := max(budget-scaffold, 1)

	total := (len(r.Output) + chunkCap - 1) / chunkCap // ceil division

	pieces := make([]section, 0, total)
	output := r.Output
	for part := 1; part <= total; part++ {
		n := min(chunkCap, len(output))
		pieces = append(pieces, section{result: r, chunk: output[:n], part: part, total: total})
		output = output[n:]
	}
	return pieces
}

// chunkOmissionMarker opens a section whose output was cut, inside its
// fence. It carries no "-" or "+" prefix, so a diff-fenced block renders
// it as ordinary context rather than coloring it.
const chunkOmissionMarker = "…(earlier output omitted)\n"

// clampBody assembles one comment body and brings it under
// maxCommentLength, preferring to drop whole sections over cutting into
// one.
//
// Dropping from the front is safe by construction: every section opens
// and closes its own fence and <details>, so removing entire sections
// leaves valid markdown with nothing to repair. Cutting bytes out of a
// packed body could not promise that — a fragment can begin inside a
// fence, inside a <details>, or between them.
//
// What survives is the *end*, because that is where a reader finds the
// verdict's supporting detail and any error: Atlantis truncates the same
// direction for the same reason.
func clampBody(header string, sections []section, footer string) string {
	dropped := 0
	for {
		body := assembleBody(header, sections, footer, dropped)
		if len(body) <= maxCommentLength {
			return body
		}
		if len(sections) > 1 {
			sections = sections[1:]
			dropped++
			continue
		}
		return cutWithinSection(header, sections[0], footer, dropped)
	}
}

// assembleBody renders a body from its parts. The omission note is
// prepended after the drop rather than before it, so the verdict line
// above still describes the whole run rather than the surviving fragment.
func assembleBody(header string, sections []section, footer string, dropped int) string {
	var b strings.Builder
	b.WriteString(header)

	if dropped > 0 {
		b.WriteString(omissionNote(dropped))
		b.WriteString("\n\n")
	}

	rendered := make([]string, len(sections))
	for i, s := range sections {
		rendered[i] = s.render()
	}
	b.WriteString(strings.Join(rendered, "\n\n"))

	if footer != "" {
		b.WriteString("\n\n")
		b.WriteString(footer)
	}

	return b.String()
}

// buildNoticeBlock renders command-level caveats as a GitHub warning
// alert. Every line carries the "> " prefix, including the marker line,
// and nothing is nested inside the block — a code fence or <details>
// indented into the blockquote degrades the whole alert into literal
// "[!WARNING]" text (see configErrorComment, which learned this first).
//
// Inline backticks are safe: they are a span, not a nested element. So is
// the caller's text generally, because selector tokens come from
// strings.Fields and therefore contain no newline that could escape the
// blockquote.
//
// WARNING rather than CAUTION: a selector that matched nothing is the
// author's to fix and leaves nothing in a bad state.
func buildNoticeBlock(notices []string) string {
	if len(notices) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("> [!WARNING]\n")
	for _, n := range notices {
		b.WriteString("> ")
		b.WriteString(n)
		b.WriteString("\n")
	}
	b.WriteString("\n")
	return b.String()
}

func omissionNote(dropped int) string {
	if dropped == 1 {
		return "_(1 earlier section omitted — this comment reached GitHub's size limit)_"
	}
	return fmt.Sprintf("_(%d earlier sections omitted — this comment reached GitHub's size limit)_", dropped)
}

// cutWithinSection is the floor beneath dropping: one section remains and
// it still does not fit, which splitDetailSection normally prevents but
// cannot guarantee — it floors its own chunk arithmetic at one byte, so a
// section whose scaffold alone exceeds the budget produces oversized
// pieces however it is split.
//
// Cutting is acceptable here for the reason it was not acceptable above:
// exactly one section remains, turnip generated its scaffold, and the
// section is rebuilt through buildDetailSectionPart rather than patched
// as text. Nothing is guessed about which elements to reopen.
func cutWithinSection(header string, s section, footer string, dropped int) string {
	empty := s
	empty.chunk = ""
	overhead := len(assembleBody(header, []section{empty}, footer, dropped))

	room := max(maxCommentLength-overhead-len(chunkOmissionMarker), 0)

	chunk := s.chunk
	if len(chunk) > room {
		chunk = chunk[len(chunk)-room:] // keep the end
	}
	s.chunk = chunkOmissionMarker + chunk

	return assembleBody(header, []section{s}, footer, dropped)
}

// packSections greedily groups pieces into comment bodies, keeping each
// body's total content under maxCommentLength. The first group's budget
// accounts for the head (notice alert, if any, plus the verdict line);
// later groups start fresh (their continuation header is small enough not
// to need accounting for here).
func packSections(head string, pieces []section) [][]section {
	var groups [][]section
	var current []section
	currentLen := len(head)

	for _, piece := range pieces {
		size := len(piece.render())
		if len(current) > 0 && currentLen+size > maxCommentLength {
			groups = append(groups, current)
			current = nil
			currentLen = 0
		}
		current = append(current, piece)
		currentLen += size
	}
	if len(current) > 0 {
		groups = append(groups, current)
	}

	return groups
}

// buildVerdictLine opens the comment with the one sentence a reader needs
// to decide whether to read further.
//
// It states the total change and names only the exceptions — Projects
// that reported no changes, and failures. It deliberately does not also
// report how many Projects had changes: the total implies it, and the
// rows below show it.
func buildVerdictLine(results []ProjectResult) string {
	if len(results) == 1 {
		return singleProjectVerdict(results[0])
	}

	var total, changed, noChanges, failed int
	for _, r := range results {
		switch {
		case !r.Success:
			failed++
		case r.Changes.Any():
			changed++
			total += r.Changes.Total()
		default:
			noChanges++
		}
	}

	var headline string
	switch {
	case failed == len(results):
		headline = fmt.Sprintf("All %d projects failed", len(results))
	case changed == 0:
		headline = fmt.Sprintf("No changes across %d projects", len(results))
	default:
		headline = fmt.Sprintf("%s across %d projects", changeCount(total), len(results))
	}

	var notes []string
	if changed > 0 && noChanges > 0 {
		notes = append(notes, fmt.Sprintf("%d with no changes", noChanges))
	}
	if failed > 0 && failed != len(results) {
		notes = append(notes, fmt.Sprintf("%d failed", failed))
	}

	// The bold runs to the end of the headline's own sentence. With notes
	// the headline is a clause and the sentence continues past the bold;
	// without them it is the whole sentence, so its full stop belongs
	// inside — matching the single-Project forms above, which build a
	// complete sentence either way.
	if len(notes) == 0 {
		return "**" + headline + ".**"
	}
	return "**" + headline + "** — " + strings.Join(notes, ", ") + "."
}

// singleProjectVerdict reads as a sentence about one Project rather than
// as arithmetic over a set of one — "4 changes in `infra`", not "4 changes
// across 1 project, 0 with no changes, 0 failed". A repository with one
// Project is the common case, not an edge case.
func singleProjectVerdict(r ProjectResult) string {
	switch {
	case !r.Success:
		return fmt.Sprintf("**`%s` failed.**", r.ProjectName)
	case r.Changes.Any():
		return fmt.Sprintf("**%s in `%s`.**", changeCount(r.Changes.Total()), r.ProjectName)
	default:
		return fmt.Sprintf("**No changes in `%s`.**", r.ProjectName)
	}
}

func changeCount(n int) string {
	if n == 1 {
		return "1 change"
	}
	return fmt.Sprintf("%d changes", n)
}

// buildFooter closes the comment with what applies to the whole run: the
// Locks this pull request now holds, and the commands that act on all
// Projects at once. Empty when no Lock is held, so a run that locked
// nothing says nothing about locks.
func buildFooter(results []ProjectResult) string {
	var locked []string
	for _, r := range results {
		if r.Locked {
			locked = append(locked, "`"+r.ProjectName+"`")
		}
	}
	if len(locked) == 0 {
		return ""
	}

	body := fmt.Sprintf("This pull request holds locks on %s until applied or released.",
		strings.Join(locked, ", "))

	// Where a locked Project's plan recorded arguments, applying replays
	// that scope rather than covering the whole Project — which the counts
	// above do not say on their own.
	//
	// Said in prose, never as a command carrying the arguments: a mutating
	// Operation that supplies its own is refused, so such a suggestion
	// would be offering something turnip rejects.
	for _, r := range results {
		if r.Locked && len(r.ScopeArgs) > 0 {
			body += "\nApplying replays the scope each plan recorded, which may be narrower than the whole Project."
			break
		}
	}

	return body + "\n`/turnip apply` or `/turnip unlock`"
}

// buildDetailSectionPart renders one self-contained <details> piece
// carrying chunk (a whole Output, or one slice of a split Output).
// total == 1 omits the "part N/M" suffix entirely, for the common case
// where a Project's Output fits in a single piece.
//
// The next-steps block is attached to the final piece only: repeating it
// on every slice of a split output would be noise, and the reader reaches
// the last piece before deciding what to do.
func buildDetailSectionPart(r ProjectResult, chunk string, part, total int) string {
	summary := summaryLine(r)
	if total > 1 {
		summary += fmt.Sprintf(" (output part %d/%d)", part, total)
	}

	var trailer string
	if part == total {
		trailer = blockedNote(r) + lockNote(r) + nextSteps(r)
	}

	open, close := fencesFor(chunk, r.Success)

	return fmt.Sprintf(
		"<details>\n<summary>%s</summary>\n\n%s\n%s\n%s%s\n\n</details>",
		summary, open, chunk, close, trailer,
	)
}

// summaryLine is what a reader sees without expanding anything, so it
// carries the whole per-Project verdict: status, name, Operation, and
// what changed.
//
// It is interpolated into <summary>, an HTML element GitHub does not parse
// markdown inside: code is <code>, not backticks, and every value is
// HTML-escaped — the Project name comes from turnip.yaml, and one carrying
// "</summary>" would otherwise rewrite the comment's structure. Plain
// punctuation joins the parts; the name is in code, as the headline above
// the sections renders it.
//
// Counts are rendered only for a successful Operation. A failure reports
// no usable counts — presenting its zeroes would make a run that never
// completed look like one that planned nothing.
func summaryLine(r ProjectResult) string {
	detail := "failed"
	if r.Success {
		detail = ChangeText(r.Changes)
	}

	line := fmt.Sprintf("%s <code>%s</code>: %s, %s",
		statusMark(r.Success), html.EscapeString(r.ProjectName), html.EscapeString(r.Operation), detail)
	if marker := scopeMarker(r.ScopeArgs); marker != "" {
		line += ", " + marker
	}
	return line
}

// scopeMarkerWidth caps the marker on a line that is already dense. The
// full arguments stay visible in the Project's section, inside the
// execution transcript.
const scopeMarkerWidth = 40

// scopeMarker renders the arguments an Operation ran with, for the text a
// reader sees without expanding anything.
//
// Verbatim and uninterpreted. Slice 20 put "teaching turnip which flags
// affect scope" out of scope because such a list needs updating whenever a
// tool gains a flag; this reports what was passed, and the reader, who
// knows their own tool, decides what it meant.
//
// The arguments come from the trigger comment and land inside <summary>,
// so they are HTML-escaped — which is also what keeps an argument carrying
// "</code>" or a backtick from breaking out. Truncated before escaping, so
// the cut can never land inside an entity such as "&amp;".
func scopeMarker(args []string) string {
	text := ScopeText(args)
	if text == "" {
		return ""
	}
	return "<code>" + html.EscapeString(text) + "</code>"
}

// ChangeText renders an Operation's counts as the comment's summary line
// does: "+1 ~4 -2", or "no changes" when all are zero. Plain text, shared
// with check run titles so the same outcome reads the same in the comment
// and in the checks list.
func ChangeText(c ChangeCounts) string {
	if !c.Any() {
		return "no changes"
	}
	return fmt.Sprintf("+%d ~%d -%d", c.Add, c.Change, c.Destroy)
}

// ScopeText renders the arguments an Operation ran with, truncated to the
// width the comment's scope marker uses. Plain text: the comment escapes
// and wraps it for <summary>, and a check run title uses it as-is, since a
// title is not HTML.
func ScopeText(args []string) string {
	if len(args) == 0 {
		return ""
	}
	joined := strings.Join(args, " ")
	if runes := []rune(joined); len(runes) > scopeMarkerWidth {
		joined = string(runes[:scopeMarkerWidth-1]) + "…"
	}
	return joined
}

// nextSteps prints the commands that act on this Project alone.
//
// Each is offered by state rather than from a fixed list: apply only
// where there is something to apply, unlock only where a Lock is actually
// held. Nothing here encodes *which* outcomes leave a Lock held — that is
// the lock lifecycle's business, and reading r.Locked rather than
// inferring from r.Success keeps this correct when that lifecycle
// changes.
//
// An unresolved operation name omits its command rather than guessing at
// one, since the names are per-tool and this package has no Plugin
// registry to ask.
func nextSteps(r ProjectResult) string {
	var lines []string

	if r.Success && r.Changes.Any() && r.ApplyOperation != "" {
		lines = append(lines, fmt.Sprintf("- Apply just this project: `/turnip %s %s`", r.ApplyOperation, r.ProjectName))
	}
	if r.PlanOperation != "" {
		lines = append(lines, fmt.Sprintf("- Re-plan it: `/turnip %s %s`", r.PlanOperation, r.ProjectName))
	}
	if r.Locked {
		lines = append(lines, fmt.Sprintf("- Release its lock: `/turnip unlock %s`", r.ProjectName))
	}

	if len(lines) == 0 {
		return ""
	}
	return "\n\n" + strings.Join(lines, "\n")
}

// blockedNote names the pull request whose Lock refused this Operation,
// linking to it so the reader can go and look rather than search
// (Requirement 7.2). A holder we could not identify is reported without
// inventing a reference.
// lockNote renders what happened to this Project's Lock, and why.
//
// It sits in the trailer rather than in Output because Output is
// interpolated *inside* the fenced block — turnip's own voice would be
// styled as tool output — and because Output is what gets split across
// "part N/M", which would bury a Lock state change at the end of the last
// piece, behind a fold.
//
// It precedes nextSteps deliberately: the commands offered there are
// decided by the Lock state this sentence has just explained.
func lockNote(r ProjectResult) string {
	if r.LockNote == "" {
		return ""
	}
	return "\n\n" + r.LockNote
}

func blockedNote(r ProjectResult) string {
	if r.BlockedBy == nil {
		return ""
	}
	if r.BlockedBy.URL == "" {
		return fmt.Sprintf("\n\nLocked by pull request #%d.", r.BlockedBy.Number)
	}
	return fmt.Sprintf("\n\nLocked by [#%d](%s).", r.BlockedBy.Number, r.BlockedBy.URL)
}

// fencesFor picks the opening and closing code fences for a Project's
// output, wide enough that nothing inside can terminate them.
//
// A fence of N backticks is closed by a line of N or more, so content
// carrying its own fence escapes the block and everything after it renders
// as markdown — inside a comment authored by turnip, which a reader trusts
// differently from one authored by a contributor. Widening the fence past
// the longest run in the content closes that without touching the
// content: the tool's output reaches the reader exactly as the tool wrote
// it, which is the whole point of quoting it.
//
// IaC tools emit something close to a unified diff, so tagging the block
// `diff` makes GitHub color added and removed lines — which is most of
// the value of reading a plan at all (Requirement 10.5's "syntax
// highlighting"). Atlantis fences plan output the same way.
//
// A *failure* still gets a plain fence. Its body is an error message
// rather than a diff, and diff highlighting would color any line starting
// with "-" as a deletion, turning an unrelated message red. Slice 33
// considered making both fences `diff` so its annotation lines render as
// muted comments in either case, and reversed that: on success the content
// really is a diff, so the highlighting is right and a stray column-0 "-"
// is the cost of a mostly-correct choice; on failure it is wrong in
// general. The annotation stays identifiable by its text in both, which is
// the part that matters.
func fencesFor(content string, success bool) (open, close string) {
	longest := 0
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		run := 0
		for run < len(trimmed) && trimmed[run] == '`' {
			run++
		}
		longest = max(longest, run)
	}

	bar := strings.Repeat("`", max(3, longest+1))
	if success {
		return bar + "diff", bar
	}
	return bar, bar
}

func statusMark(success bool) string {
	if success {
		return "✅"
	}
	return "❌"
}
