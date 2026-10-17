package github

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

func TestBuildConsolidatedComment_SingleBody(t *testing.T) {
	results := []ProjectResult{
		{ProjectName: "vpc", Operation: "plan", Success: true, Output: "no changes"},
		{ProjectName: "k8s-apps", Operation: "diff", Success: false, Output: "1 changed release"},
	}

	bodies := BuildConsolidatedComment(results, testMaxParts)
	require.Len(t, bodies, 1)

	body := bodies[0]
	assert.NotContains(t, body, "| Project | Operation | Status |",
		"the summary table was replaced by collapsed summary rows")
	for _, r := range results {
		assert.Contains(t, body, r.ProjectName)
		assert.Contains(t, body, r.Output)
	}
	assert.Contains(t, body, "<details>")
	assert.Contains(t, body, "</details>")

	// The verdict line opens the comment, before any section.
	assert.True(t, strings.HasPrefix(body, "**"),
		"the comment must open with the verdict line, got %q", body[:min(40, len(body))])
}

func TestBuildConsolidatedComment_SplitsAcrossBodiesWhenOverLimit(t *testing.T) {
	var results []ProjectResult
	bigOutput := strings.Repeat("x", 40000)
	for range 5 {
		results = append(results, ProjectResult{
			ProjectName: "project", Operation: "plan", Success: true, Output: bigOutput,
		})
	}

	bodies := BuildConsolidatedComment(results, testMaxParts)
	require.Greater(t, len(bodies), 1, "want more than 1 body for oversized content")

	for i, body := range bodies {
		assert.LessOrEqualf(t, len(body), maxCommentLength, "body %d length", i)
	}
	assert.True(t, strings.HasPrefix(bodies[0], "**"),
		"the first body opens with the verdict line")
	for i, body := range bodies[1:] {
		assert.NotContainsf(t, body, "across 5 projects", "continuation body %d repeats the verdict", i+1)
		assert.Containsf(t, body, "(continued", "continuation body %d", i+1)
	}
}

// numberedLines builds a large, distinguishable Output ("line-0\nline-1\n...")
// so tests can verify every line survives a split, in order, with nothing
// lost or duplicated.
func numberedLines(n int) string {
	var b strings.Builder
	for i := range n {
		fmt.Fprintf(&b, "line-%d\n", i)
	}
	return b.String()
}

// fencedContent returns the text between a piece's opening fence line and
// its closing fence.
//
// It deliberately does not assume what the opening fence carries: a
// successful result is fenced ```diff and a failure ```, so a test that
// searched for a bare "```\n" would skip the opening fence entirely and
// match the closing one instead — yielding a start past the end.
func fencedContent(t *testing.T, s string) string {
	t.Helper()

	open := strings.Index(s, "```")
	require.GreaterOrEqual(t, open, 0, "no opening code fence")
	newline := strings.Index(s[open:], "\n")
	require.GreaterOrEqual(t, newline, 0, "opening code fence is not terminated")

	start := open + newline + 1
	end := strings.LastIndex(s, "\n```")
	require.Greater(t, end, start, "missing a well-formed code fence")

	return s[start:end]
}

func TestBuildConsolidatedComment_SplitsOversizedSingleOutputAcrossBodies(t *testing.T) {
	output := numberedLines(20000) // several times maxCommentLength
	results := []ProjectResult{
		{ProjectName: "huge", Operation: "plan", Success: true, Output: output},
	}

	bodies := BuildConsolidatedComment(results, testMaxParts)
	require.Greater(t, len(bodies), 1, "want the oversized output split across multiple bodies")

	var reassembled strings.Builder
	for i, body := range bodies {
		assert.LessOrEqualf(t, len(body), maxCommentLength, "body %d length", i)
		assert.NotContains(t, body, "[output truncated]", "content should be split, not discarded")

		reassembled.WriteString(fencedContent(t, body))

		if i > 0 {
			assert.Containsf(t, body, "output part", "continuation body %d should mark itself as a continued part", i)
		}
	}

	assert.Equal(t, output, reassembled.String(), "every line must survive the split, in order, with nothing lost or duplicated")
}

func TestSplitDetailSection_ReassemblesExactly(t *testing.T) {
	output := numberedLines(20000)
	r := ProjectResult{ProjectName: "huge", Operation: "plan", Success: true, Output: output}

	pieces := splitDetailSection(r, minReserve, testMaxParts)
	require.Greater(t, len(pieces), 1, "want the output split into multiple pieces")

	var reassembled strings.Builder
	for i, piece := range pieces {
		rendered := piece.render()
		assert.LessOrEqualf(t, len(rendered), maxCommentLength-minReserve, "piece %d", i)

		reassembled.WriteString(fencedContent(t, rendered))

		assert.Containsf(t, rendered, fmt.Sprintf("part %d/%d", i+1, len(pieces)), "piece %d", i)
	}

	assert.Equal(t, output, reassembled.String())
}

func TestSplitDetailSection_FitsWithoutSplitting(t *testing.T) {
	r := ProjectResult{ProjectName: "vpc", Operation: "plan", Success: true, Output: "no changes"}

	pieces := splitDetailSection(r, minReserve, testMaxParts)
	require.Len(t, pieces, 1)
	assert.NotContains(t, pieces[0].render(), "output part")
}

func testSection(name, chunk string) section {
	return section{
		result: ProjectResult{ProjectName: name, Operation: "plan", Success: true},
		chunk:  chunk,
		part:   1,
		total:  1,
	}
}

func TestClampBody_NoOpUnderLimit(t *testing.T) {
	got := clampBody("**verdict**\n\n", []section{testSection("vpc", "ok")}, "")

	assert.LessOrEqual(t, len(got), maxCommentLength)
	assert.Contains(t, got, "**verdict**")
	assert.NotContains(t, got, "omitted", "nothing was dropped, so nothing is announced")
}

// Tier 1: whole sections come off the front, which keeps the body valid
// markdown by construction — each section opens and closes its own fence
// and <details>, so removing entire ones leaves nothing to repair.
func TestClampBody_DropsWholeLeadingSections(t *testing.T) {
	big := strings.Repeat("x", 30000)
	var sections []section
	for i := range 4 {
		sections = append(sections, testSection(fmt.Sprintf("proj%d", i), big))
	}

	got := clampBody("**verdict**\n\n", sections, "")

	assert.LessOrEqual(t, len(got), maxCommentLength)
	assert.Contains(t, got, "earlier section", "the omission is announced")
	assert.Contains(t, got, "proj3", "the last section survives")
	assert.NotContains(t, got, "proj0", "the earliest section is the first dropped")
	assert.Equal(t, strings.Count(got, "<details>"), strings.Count(got, "</details>"),
		"no partial <details> may survive the drop")
}

// Tier 2: one section remains and still does not fit, so bytes come off
// the front of its output and the section is rebuilt — not patched as
// text — so the fence and <details> are reopened rather than guessed at.
func TestClampBody_CutsWithinTheLastSectionKeepingItsEnd(t *testing.T) {
	output := numberedLines(20000)

	got := clampBody("**verdict**\n\n", []section{testSection("huge", output)}, "")

	assert.LessOrEqual(t, len(got), maxCommentLength)
	assert.Equal(t, strings.Count(got, "<details>"), strings.Count(got, "</details>"),
		"the rebuilt section must still be valid markdown")
	assert.Contains(t, got, chunkOmissionMarker, "the cut is announced inside the fence")
	assert.Contains(t, got, "line-19999", "the end of the output is what survives")
	assert.NotContains(t, got, "line-0\n", "the beginning is what is dropped")
}

// A section cut to fit a body keeps its output from the start of a whole
// line, not from wherever the byte count lands.
func TestClampBody_CutKeepsTheEndFromAWholeLine(t *testing.T) {
	output := numberedLines(20000)

	got := clampBody("**verdict**\n\n", []section{testSection("huge", output)}, "")

	_, after, ok := strings.Cut(got, chunkOmissionMarker)
	require.True(t, ok, "the cut is announced")
	assert.True(t, strings.HasPrefix(after, "line-"), "kept output starts at a whole line, got %.20q", after)
	assert.LessOrEqual(t, len(got), maxCommentLength)
}

// When the last line alone is longer than the room, the cut falls inside
// it, at a character boundary.
func TestClampBody_CutInsideAnOverlongLastLineIsAtACharacterBoundary(t *testing.T) {
	output := "first\n" + strings.Repeat("é💡", maxCommentLength)

	got := clampBody("**verdict**\n\n", []section{testSection("huge", output)}, "")

	assert.LessOrEqual(t, len(got), maxCommentLength)
	assert.True(t, utf8.ValidString(got), "no character is broken")
	assert.NotContains(t, got, "first")
	_, after, ok := strings.Cut(got, chunkOmissionMarker)
	require.True(t, ok)
	assert.True(t, utf8.RuneStart(after[0]))
}

func TestKeptStart(t *testing.T) {
	tests := []struct {
		name string
		s    string
		cut  int
		want int
	}{
		{"no cut", "ab\ncd\n", 0, 0},
		{"cut at a line start", "ab\ncd\n", 3, 3},
		{"cut inside a line moves to the next line", "ab\ncd\nef\n", 4, 6},
		{"cut inside the last line stays", "ab\ncdef", 5, 5},
		{"cut inside a character moves to a boundary", "ab\ncé", 5, 6},
		{"only a closing annotation after the cut stays", "ab\ncd\n@@ turnip: x @@\n", 4, 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, keptStart(tt.s, tt.cut))
		})
	}
}

func TestBuildConsolidatedComment_EmptyResults(t *testing.T) {
	bodies := BuildConsolidatedComment(nil, testMaxParts)
	assert.Nil(t, bodies)
}

// The collapsed summary line is what a reviewer sees before expanding
// anything, so it carries the whole per-Project verdict. This is the line
// that replaced the summary table.
func TestBuildConsolidatedComment_SummaryLineCarriesStatusAndCounts(t *testing.T) {
	bodies := buildForTest([]ProjectResult{
		{ProjectName: "infra", Operation: "diff", Success: true, Output: "x",
			Changes: ChangeCounts{Add: 1, Change: 4, Destroy: 2}},
	})
	require.Len(t, bodies, 1)

	assert.Contains(t, bodies[0], "<summary>✅ <code>infra</code>: diff, +1 ~4 -2</summary>")
}

// Requirement 2.3: a Project that reported no changes says so, rather than
// rendering three zeros that read like a measurement.
func TestBuildConsolidatedComment_NoChangesSaysSoRatherThanZeros(t *testing.T) {
	bodies := buildForTest([]ProjectResult{
		{ProjectName: "apps", Operation: "diff", Success: true, Output: "No changes."},
	})
	require.Len(t, bodies, 1)

	assert.Contains(t, bodies[0], "<summary>✅ <code>apps</code>: diff, no changes</summary>")
	assert.NotContains(t, bodies[0], "+0 ~0 -0")
}

// Requirement 5.3: a Plugin that declares its counts unreported renders a
// successful Mutating_Operation as "succeeded" in the summary line and the
// verdict line, never as "no changes".
func TestBuildConsolidatedComment_UnreportedRendersAsSucceeded(t *testing.T) {
	bodies := buildForTest([]ProjectResult{
		{ProjectName: "web", Operation: "apply", Success: true, Output: "applied",
			Changes: ChangeCounts{Unreported: true}},
	})
	require.Len(t, bodies, 1)

	assert.Contains(t, bodies[0], "<summary>✅ <code>web</code>: apply, succeeded</summary>")
	assert.Contains(t, bodies[0], "**Succeeded in `web`.**")
	assert.NotContains(t, bodies[0], "no changes")
}

// Requirement 5.3, multi-Project: a run mixing an unreported success with
// a counted one states the total from the counted Project alone and notes
// the unreported one separately, never folding it into "no changes".
func TestBuildConsolidatedComment_UnreportedMixedWithCounted(t *testing.T) {
	bodies := buildForTest([]ProjectResult{
		{ProjectName: "web", Operation: "apply", Success: true, Output: "applied",
			Changes: ChangeCounts{Change: 3}},
		{ProjectName: "api", Operation: "apply", Success: true, Output: "applied",
			Changes: ChangeCounts{Unreported: true}},
	})
	require.Len(t, bodies, 1)

	assert.Contains(t, bodies[0], "**3 changes across 2 projects**: 1 succeeded.")
	assert.Contains(t, bodies[0], "<summary>✅ <code>web</code>: apply, +0 ~3 -0</summary>")
	assert.Contains(t, bodies[0], "<summary>✅ <code>api</code>: apply, succeeded</summary>")
}

// Requirement 3.1: a failed Mutating_Operation still renders as failed,
// whatever its Plugin says about unreported counts.
func TestBuildConsolidatedComment_FailedRendersFailedEvenWhenUnreported(t *testing.T) {
	bodies := buildForTest([]ProjectResult{
		{ProjectName: "web", Operation: "apply", Success: false, Output: "boom",
			Changes: ChangeCounts{Unreported: true}},
	})
	require.Len(t, bodies, 1)

	assert.Contains(t, bodies[0], "<summary>❌ <code>web</code>: apply, failed</summary>")
	assert.Contains(t, bodies[0], "**`web` failed.**")
	assert.NotContains(t, bodies[0], "succeeded")
}

// Requirement 2.4: a failure reports no usable counts, so none are shown —
// presenting its zeroes would make a run that never completed look like
// one that planned nothing.
func TestBuildConsolidatedComment_FailureShowsNoCounts(t *testing.T) {
	bodies := buildForTest([]ProjectResult{
		{ProjectName: "edge", Operation: "diff", Success: false, Output: "boom",
			Changes: ChangeCounts{Add: 9, Change: 9, Destroy: 9}},
	})
	require.Len(t, bodies, 1)

	assert.Contains(t, bodies[0], "<summary>❌ <code>edge</code>: diff, failed</summary>")
	assert.NotContains(t, bodies[0], "+9 ~9 -9",
		"counts from an Operation that did not complete must not be shown")
}

// Requirement 3.4. The assertion whose absence would let turnip invite a
// reader to apply a plan that never finished.
func TestBuildConsolidatedComment_FailedProjectIsNeverInvitedToApply(t *testing.T) {
	bodies := buildForTest([]ProjectResult{
		{ProjectName: "edge", Operation: "diff", Success: false, Output: "boom",
			PlanOperation: "diff", ApplyOperation: "apply", Locked: true},
	})
	require.Len(t, bodies, 1)

	assert.NotContains(t, bodies[0], "/turnip apply edge")
	assert.Contains(t, bodies[0], "/turnip diff edge", "re-planning is still offered")
	assert.Contains(t, bodies[0], "/turnip unlock edge", "a held lock is still releasable")
}

// Requirement 3.2: steps are offered by state, not from a fixed list. This
// is what keeps the slice correct when Slice 18 changes which Projects
// hold Locks after a failed plan — the renderer reports Locked rather than
// inferring from Success.
func TestBuildConsolidatedComment_UnlockOnlyWhereALockIsHeld(t *testing.T) {
	bodies := buildForTest([]ProjectResult{
		{ProjectName: "held", Operation: "diff", Success: true, Output: "x",
			Changes: ChangeCounts{Change: 1}, PlanOperation: "diff", ApplyOperation: "apply", Locked: true},
		{ProjectName: "free", Operation: "diff", Success: true, Output: "x",
			Changes: ChangeCounts{Change: 1}, PlanOperation: "diff", ApplyOperation: "apply", Locked: false},
	})
	require.Len(t, bodies, 1)

	assert.Contains(t, bodies[0], "/turnip unlock held")
	assert.NotContains(t, bodies[0], "/turnip unlock free")
	assert.Contains(t, bodies[0], "holds locks on `held`")
	assert.NotContains(t, bodies[0], "`free`,", "the footer names only locked Projects")
}

// Requirement 5: a contended Lock names the blocking pull request and
// links to it, rather than leaving the reader to search.
func TestBuildConsolidatedComment_ContentionLinksTheBlockingPullRequest(t *testing.T) {
	bodies := buildForTest([]ProjectResult{
		{ProjectName: "infra", Operation: "diff", Success: false, Output: "locked by PR #42",
			BlockedBy: &BlockingPullRequest{Number: 42, URL: "https://example.invalid/pull/42"}},
	})
	require.Len(t, bodies, 1)

	assert.Contains(t, bodies[0], "[#42](https://example.invalid/pull/42)")
	assert.NotContains(t, bodies[0], "/turnip apply", "a blocked Operation offers no apply")
}

// Requirement 5.3: an unidentifiable holder is reported without inventing
// a reference.
func TestBuildConsolidatedComment_ContentionWithoutURLInventsNothing(t *testing.T) {
	bodies := buildForTest([]ProjectResult{
		{ProjectName: "infra", Operation: "diff", Success: false, Output: "locked",
			BlockedBy: &BlockingPullRequest{Number: 42}},
	})
	require.Len(t, bodies, 1)

	assert.Contains(t, bodies[0], "Locked by pull request #42.")
	assert.NotContains(t, bodies[0], "](")
}

// Requirement 1: the verdict line states the total and names only the
// exceptions. A single Project reads as a sentence about that Project
// rather than as arithmetic over a set of one.
func TestBuildVerdictLine(t *testing.T) {
	changed := func(name string, n int) ProjectResult {
		return ProjectResult{ProjectName: name, Operation: "diff", Success: true,
			Changes: ChangeCounts{Change: n}}
	}
	clean := func(name string) ProjectResult {
		return ProjectResult{ProjectName: name, Operation: "diff", Success: true}
	}
	failed := func(name string) ProjectResult {
		return ProjectResult{ProjectName: name, Operation: "diff", Success: false}
	}
	unreported := func(name string) ProjectResult {
		return ProjectResult{ProjectName: name, Operation: "apply", Success: true,
			Changes: ChangeCounts{Unreported: true}}
	}

	for _, tc := range []struct {
		name    string
		results []ProjectResult
		want    string
	}{
		{"one project with changes", []ProjectResult{changed("infra", 4)}, "**4 changes in `infra`.**"},
		{"one change reads singular", []ProjectResult{changed("infra", 1)}, "**1 change in `infra`.**"},
		{"one project unchanged", []ProjectResult{clean("infra")}, "**No changes in `infra`.**"},
		{"one project failed", []ProjectResult{failed("infra")}, "**`infra` failed.**"},
		{
			"mixed", []ProjectResult{changed("a", 4), clean("b"), failed("c")},
			"**4 changes across 3 projects**: 1 with no changes, 1 failed.",
		},
		{
			"all clean", []ProjectResult{clean("a"), clean("b")},
			"**No changes across 2 projects.**",
		},
		{
			"all failed", []ProjectResult{failed("a"), failed("b")},
			"**All 2 projects failed.**",
		},
		{
			"changes everywhere", []ProjectResult{changed("a", 2), changed("b", 3)},
			"**5 changes across 2 projects.**",
		},
		{"one project unreported", []ProjectResult{unreported("infra")}, "**Succeeded in `infra`.**"},
		{
			"all unreported", []ProjectResult{unreported("a"), unreported("b")},
			"**Succeeded across 2 projects.**",
		},
		{
			"unreported mixed with counted changes",
			[]ProjectResult{changed("a", 4), unreported("b")},
			"**4 changes across 2 projects**: 1 succeeded.",
		},
		{
			"unreported, counted and no changes",
			[]ProjectResult{changed("a", 4), unreported("b"), clean("c")},
			"**4 changes across 3 projects**: 1 with no changes, 1 succeeded.",
		},
		{
			"unreported, counted, no changes and failed",
			[]ProjectResult{changed("a", 4), unreported("b"), clean("c"), failed("d")},
			"**4 changes across 4 projects**: 1 with no changes, 1 succeeded, 1 failed.",
		},
		{
			"unreported mixed with no changes, no counted changes",
			[]ProjectResult{unreported("a"), clean("b")},
			"**Succeeded across 2 projects**: 1 with no changes.",
		},
		{
			"unreported mixed with failed, no counted changes",
			[]ProjectResult{unreported("a"), failed("b")},
			"**Succeeded across 2 projects**: 1 failed.",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, buildVerdictLine(tc.results))
		})
	}
}

// Requirement 10.5: output is fenced for syntax highlighting. A success is
// fenced `diff` so GitHub colors added and removed lines, which is most of
// what makes a plan readable. A failure is not: its body is an error
// message, and diff highlighting would color every line beginning with
// "-" red for no reason.
func TestBuildConsolidatedComment_DiffFenceOnSuccessPlainOnFailure(t *testing.T) {
	success := buildForTest([]ProjectResult{
		{ProjectName: "ok", Operation: "diff", Success: true, Output: "- old\n+ new"},
	})
	require.Len(t, success, 1)
	assert.Contains(t, success[0], "```diff\n- old\n+ new\n```")

	failure := buildForTest([]ProjectResult{
		{ProjectName: "bad", Operation: "diff", Success: false, Output: "- could not connect"},
	})
	require.Len(t, failure, 1)
	assert.Contains(t, failure[0], "```\n- could not connect\n```")
	assert.NotContains(t, failure[0], "```diff",
		"an error message must not be diff-highlighted")
}

// The note belongs outside the fence: inside it, turnip's own sentence is
// styled as tool output and is carried away by output splitting.
func TestBuildConsolidatedComment_LockNoteRendersOutsideTheFence(t *testing.T) {
	const note = "Lock released: the plan failed, so nothing was recorded."
	parts := buildForTest([]ProjectResult{{
		ProjectName: "web",
		Tool:        "helmfile",
		Operation:   "diff",
		Success:     false,
		Output:      "some tool failure",
		LockNote:    note,
	}})
	require.Len(t, parts, 1)
	body := parts[0]

	require.Contains(t, body, note)

	// Everything between the opening and closing fence is the tool's.
	openIdx := strings.Index(body, "```")
	closeIdx := strings.Index(body[openIdx+3:], "```") + openIdx + 3
	fenced := body[openIdx:closeIdx]
	assert.NotContains(t, fenced, note, "turnip's voice must not sit inside the tool's code block")
}

func TestBuildConsolidatedComment_NoLockNoteRendersNothing(t *testing.T) {
	parts := buildForTest([]ProjectResult{{
		ProjectName: "web", Tool: "helmfile", Operation: "diff", Success: true, Output: "no changes",
	}})
	require.Len(t, parts, 1)
	assert.NotContains(t, parts[0], "Lock released")
}

// Content carrying its own fence must not escape the block. A fence of N
// backticks is closed by a line of N or more, so the opening fence widens
// past the longest run in the content rather than the content being
// rewritten — the tool's output reaches the reader as the tool wrote it.
func TestBuildConsolidatedComment_OutputCannotEscapeItsFence(t *testing.T) {
	for _, tc := range []struct {
		name   string
		output string
	}{
		{"a bare fence terminator", "before\n```\n> [!WARNING]\nforged\nafter"},
		{"a fenced block of its own", "before\n```yaml\nkey: value\n```\nafter"},
		{"an indented terminator", "before\n   ```\nafter"},
		{"a longer run than the fence", "before\n`````\nafter"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parts := buildForTest([]ProjectResult{{
				ProjectName: "web", Tool: "helmfile", Operation: "diff",
				Success: true, Output: tc.output,
			}})
			require.Len(t, parts, 1)
			body := parts[0]

			// The opening fence must be strictly longer than anything the
			// content can close with.
			openIdx := strings.Index(body, "```")
			require.GreaterOrEqual(t, openIdx, 0)
			fence := 0
			for openIdx+fence < len(body) && body[openIdx+fence] == '`' {
				fence++
			}

			for _, line := range strings.Split(tc.output, "\n") {
				trimmed := strings.TrimLeft(line, " \t")
				run := 0
				for run < len(trimmed) && trimmed[run] == '`' {
					run++
				}
				assert.Less(t, run, fence,
					"a run of %d backticks would close a fence of %d", run, fence)
			}
		})
	}
}

// The split fence is deliberate and predates this: a failure's body is an
// error message, and diff highlighting would redden any line starting with
// "-" for no reason.
func TestBuildConsolidatedComment_FailureKeepsAPlainFence(t *testing.T) {
	parts := buildForTest([]ProjectResult{{
		ProjectName: "web", Tool: "helmfile", Operation: "diff",
		Success: false, Output: "Error: release not found\n-  some detail",
	}})
	require.Len(t, parts, 1)
	assert.NotContains(t, parts[0], "```diff",
		"an error message is not a diff, and coloring it as one is wrong in general")
}

// The marker is what a reader sees without expanding anything, so it has
// to survive the same hazards the fenced block does.
func TestBuildConsolidatedComment_ScopeMarker(t *testing.T) {
	t.Run("absent when the Operation took no arguments", func(t *testing.T) {
		parts := buildForTest([]ProjectResult{{
			ProjectName: "web", Tool: "helmfile", Operation: "diff", Success: true, Output: "no changes",
		}})
		require.Len(t, parts, 1)
		assert.Contains(t, parts[0], "<summary>✅ <code>web</code>: diff, no changes</summary>")
	})

	t.Run("verbatim, uninterpreted", func(t *testing.T) {
		parts := buildForTest([]ProjectResult{{
			ProjectName: "web", Tool: "helmfile", Operation: "diff", Success: true, Output: "x",
			ScopeArgs: []string{"-l", "name=api"},
		}})
		require.Len(t, parts, 1)
		assert.Contains(t, parts[0], "<summary>✅ <code>web</code>: diff, no changes, <code>-l name=api</code></summary>",
			"<code>, not backticks: GitHub does not parse markdown inside <summary>")
	})

	t.Run("truncated, without splitting a rune", func(t *testing.T) {
		long := strings.Repeat("é", 80)
		marker := scopeMarker([]string{long})
		assert.True(t, utf8.ValidString(marker), "truncation must not split a rune")
		assert.Less(t, len([]rune(marker)), 80)
		assert.Contains(t, marker, "…")
	})

	t.Run("an argument carrying markup is escaped, not rendered", func(t *testing.T) {
		parts := buildForTest([]ProjectResult{{
			ProjectName: "web", Tool: "helmfile", Operation: "diff", Success: true, Output: "x",
			ScopeArgs: []string{"-l", "name=</code></summary><a href=x>`&`"},
		}})
		require.Len(t, parts, 1)
		assert.Contains(t, parts[0],
			"<code>-l name=&lt;/code&gt;&lt;/summary&gt;&lt;a href=x&gt;`&amp;`</code></summary>")
		assert.Equal(t, 1, strings.Count(parts[0], "</summary>"), "the argument cannot close the summary")
		assert.NotContains(t, parts[0], "<a href")
	})

	t.Run("truncated before escaping, so no entity is cut", func(t *testing.T) {
		// The 39th and 40th runes are "&"; escaping first would put the cut
		// inside "&amp;".
		marker := scopeMarker([]string{strings.Repeat("a", 38) + "&&&"})
		assert.Equal(t, "<code>"+strings.Repeat("a", 38)+"&amp;…</code>", marker)
	})
}

// A Project name comes from turnip.yaml and lands in the same <summary>.
func TestBuildConsolidatedComment_ProjectNameIsEscapedInTheSummary(t *testing.T) {
	parts := buildForTest([]ProjectResult{{
		ProjectName: "a<b>&c", Tool: "helmfile", Operation: "diff", Success: true, Output: "x",
	}})
	require.Len(t, parts, 1)
	assert.Contains(t, parts[0], "<summary>✅ <code>a&lt;b&gt;&amp;c</code>: diff, no changes</summary>")
}

// Requirement 2: the footer says applying replays a recorded scope, and
// never offers a command carrying the arguments — a mutating Operation
// that supplies its own is refused.
func TestBuildConsolidatedComment_FooterIsScopeAware(t *testing.T) {
	scoped := []ProjectResult{{
		ProjectName: "web", Tool: "helmfile", Operation: "diff", Success: true, Output: "x",
		Locked: true, ScopeArgs: []string{"-l", "name=api"},
	}}
	unscoped := []ProjectResult{{
		ProjectName: "web", Tool: "helmfile", Operation: "diff", Success: true, Output: "x",
		Locked: true,
	}}

	withScope := BuildConsolidatedComment(scoped, testMaxParts)[0]
	withoutScope := BuildConsolidatedComment(unscoped, testMaxParts)[0]

	assert.Contains(t, withScope, "replays the scope")
	assert.NotContains(t, withoutScope, "replays the scope",
		"unchanged where no locked Project recorded arguments")

	assert.NotContains(t, withScope, "/turnip apply -l",
		"a command carrying the arguments would be refused if anyone pasted it")
	assert.Contains(t, withScope, "`/turnip apply` or `/turnip unlock`")
}

// Per Project, not once per comment. ParseTriggers stops collecting
// Project names at the first "-"-prefixed token, so a trigger carrying
// arguments and no names applies them to every selected Project — and one
// marker per comment would attribute a scope to Projects it may not be
// meaningful for.
func TestBuildConsolidatedComment_ScopeMarkerIsPerProject(t *testing.T) {
	parts := buildForTest([]ProjectResult{
		{ProjectName: "web", Tool: "helmfile", Operation: "diff", Success: true, Output: "x",
			ScopeArgs: []string{"-l", "name=api"}},
		{ProjectName: "api", Tool: "helmfile", Operation: "diff", Success: true, Output: "y",
			ScopeArgs: []string{"-l", "name=api"}},
	})
	require.Len(t, parts, 1)
	assert.Equal(t, 2, strings.Count(parts[0], "-l name=api"),
		"both Projects the trigger fanned out to carry the marker")
}

// testMaxParts is large enough that no test output is cut by the cap.
const testMaxParts = 1000

func buildForTest(results []ProjectResult, notices ...string) []string {
	return BuildConsolidatedComment(results, testMaxParts, notices...)
}

func partsOf(t *testing.T, pieces []section) string {
	t.Helper()
	var b strings.Builder
	for _, p := range pieces {
		b.WriteString(p.chunk)
	}
	return b.String()
}

func TestSplitDetailSection_CapsPartsAndKeepsTheEnd(t *testing.T) {
	output := numberedLines(500000) // roughly 80 parts
	r := ProjectResult{ProjectName: "huge", Operation: "plan", Success: true, Output: output}
	require.Greater(t, len(splitDetailSection(r, minReserve, testMaxParts)), 60, "precondition: many parts")

	for _, cap := range []int{3, 5} {
		pieces := splitDetailSection(r, minReserve, cap)
		require.Len(t, pieces, cap)
		for i, p := range pieces {
			assert.LessOrEqualf(t, len(p.render()), maxCommentLength-minReserve, "piece %d", i)
			assert.Equal(t, cap, p.total)
		}
		got := partsOf(t, pieces)
		assert.True(t, strings.HasPrefix(got, "…("), "no leading run, so the marker opens the output")
		assert.True(t, strings.HasSuffix(got, "line-499999\n"), "the end survives")
	}
}

// A last tool line longer than the whole cap keeps its own end, cut
// inside it, rather than leaving only the annotation lines and the
// marker: the reader must still see the end of the output.
func TestSplitDetailSection_OversizedLastLineKeepsItsEnd(t *testing.T) {
	head := "@@ turnip: web, helmfile v1.7.4 @@\n@@ turnip: helmfile diff @@\n"
	long := "data: " + strings.Repeat("é", 125000) + "END" // 250,009 bytes, multi-byte runes
	for name, tail := range map[string]string{
		"with a closing annotation":    "\n@@ turnip: exit 0 in 3s @@\n",
		"without a closing annotation": "\n",
	} {
		t.Run(name, func(t *testing.T) {
			output := head + numberedLines(100) + long + tail
			r := ProjectResult{ProjectName: "web", Operation: "plan", Success: true, Output: output}

			got := partsOf(t, splitDetailSection(r, minReserve, 3))

			assert.True(t, strings.HasPrefix(got, head), "the leading annotation lines survive")
			assert.True(t, strings.HasSuffix(got, "END"+tail), "the long line's own end survives")
			assert.True(t, utf8.ValidString(got), "the cut falls on a rune boundary")
			assert.Contains(t, got, "…(100 earlier lines omitted)\n", "only the whole lines before it are counted, not the long line it cut into")
			assert.Greater(t, len(got), 100000, "most of the cap is the long line's end, not just the marker")
		})
	}
}

func TestSplitDetailSection_MarkerCountsRemovedLinesExactly(t *testing.T) {
	output := numberedLines(500000)
	r := ProjectResult{ProjectName: "huge", Operation: "plan", Success: true, Output: output}

	got := partsOf(t, splitDetailSection(r, minReserve, 3))

	var k int
	_, err := fmt.Sscanf(got, "…(%d earlier lines omitted)\n", &k)
	require.NoError(t, err)
	require.Positive(t, k)
	marker := fmt.Sprintf("…(%d earlier lines omitted)\n", k)
	kept := strings.TrimPrefix(got, marker)
	assert.Equal(t, output[len(output)-len(kept):], kept, "the kept text is a suffix of the output")
	assert.Equal(t, 500000, k+strings.Count(kept, "\n"), "removed plus kept lines is every line")
	assert.True(t, strings.HasPrefix(kept, "line-"), "the cut falls on a whole line")
}

func TestSplitDetailSection_RunnerOmissionLineSurvivesTheCut(t *testing.T) {
	lead := "@@ turnip: 12 earlier lines omitted (3.0 MiB) @@\n@@ turnip: started @@\n"
	output := lead + numberedLines(500000)
	r := ProjectResult{ProjectName: "huge", Operation: "plan", Success: true, Output: output}

	got := partsOf(t, splitDetailSection(r, minReserve, 3))

	assert.True(t, strings.HasPrefix(got, lead+"…("), "marker follows the leading run")
	assert.Contains(t, got, " earlier lines omitted)\n")
	assert.True(t, strings.HasSuffix(got, "line-499999\n"))
}

func TestSplitDetailSection_OutputThatFitsIsUntouched(t *testing.T) {
	output := numberedLines(100)
	r := ProjectResult{ProjectName: "vpc", Operation: "plan", Success: true, Output: output}
	pieces := splitDetailSection(r, minReserve, 3)
	require.Len(t, pieces, 1)
	assert.Equal(t, output, pieces[0].chunk)

	// Several parts, but within the cap.
	big := numberedLines(20000)
	r.Output = big
	pieces = splitDetailSection(r, minReserve, 50)
	assert.Equal(t, big, partsOf(t, pieces))
}

func TestBuildConsolidatedComment_CapsOutputAtThreeComments(t *testing.T) {
	bodies := BuildConsolidatedComment([]ProjectResult{
		{ProjectName: "huge", Operation: "plan", Success: true, Output: numberedLines(500000)},
	}, 3)
	assert.Len(t, bodies, 3)
	assert.Contains(t, strings.Join(bodies, ""), " earlier lines omitted)")
}

// reserveForChunk returns the reserve that makes splitDetailSection pack
// parts of at most chunk output bytes for r.
func reserveForChunk(r ProjectResult, chunk int) int {
	return maxCommentLength - len(buildDetailSectionPart(r, "", 9999, 9999)) - chunk
}

// Feature: fix-comment-part-split, Property 1: Parts end at whole lines
func TestSplitDetailSection_PartsEndAtWholeLines(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		chunk := rapid.IntRange(1, 300).Draw(t, "chunk")
		lines := rapid.SliceOfN(
			rapid.OneOf(
				rapid.StringMatching(`[a-z0-9 :]{0,12}`),
				rapid.StringMatching(`[a-zé💡]{0,60}`),
				rapid.StringMatching(`[a-zé💡]{200,700}`),
			), 1, 40).Draw(t, "lines")
		output := strings.Join(lines, "\n")
		if rapid.Bool().Draw(t, "trailingNewline") {
			output += "\n"
		}
		r := ProjectResult{ProjectName: "web", Operation: "plan", Success: true, Output: output}

		pieces := splitDetailSection(r, reserveForChunk(r, chunk), 100000)

		var joined strings.Builder
		for i, p := range pieces {
			assert.Equal(t, i+1, p.part)
			assert.Len(t, pieces, p.total)
			assert.True(t, utf8.ValidString(p.chunk), "part %d is valid UTF-8", i)
			if len(pieces) > 1 { // an output that fits whole is not packed
				assert.LessOrEqual(t, len(p.chunk), max(chunk, utf8.UTFMax), "part %d size", i)
			}
			joined.WriteString(p.chunk)

			if i == len(pieces)-1 || strings.HasSuffix(p.chunk, "\n") {
				continue
			}
			// Not the last part and not ending a line: it must end inside a
			// line longer than a part.
			end := joined.Len()
			startOfLine := strings.LastIndexByte(output[:end], '\n') + 1
			lineLen := len(output[startOfLine:])
			if nl := strings.IndexByte(output[startOfLine:], '\n'); nl >= 0 {
				lineLen = nl + 1
			}
			assert.Greaterf(t, lineLen, chunk, "part %d ends inside a line that fits a part", i)
		}
		assert.Equal(t, output, joined.String())
	})
}

func splitChunks(t *testing.T, output string, chunk int) []string {
	t.Helper()
	r := ProjectResult{ProjectName: "web", Operation: "plan", Success: true, Output: output}
	var chunks []string
	for _, p := range splitDetailSection(r, reserveForChunk(r, chunk), 1000) {
		chunks = append(chunks, p.chunk)
	}
	return chunks
}

func TestSplitDetailSection_LineAtPartEndStartsNextPart(t *testing.T) {
	// "ports:\nport: 8888\n" with room for 12 bytes would cut "port: 8888"
	// after "port: 8"; the whole line moves to the next part instead.
	output := "ports:\n" + "port: 8888\n" + strings.Repeat("filler line\n", 3)
	got := splitChunks(t, output, 14)
	require.Greater(t, len(got), 1)
	assert.Equal(t, "ports:\n", got[0])
	assert.True(t, strings.HasPrefix(got[1], "port: 8888\n"))
	assert.Equal(t, output, strings.Join(got, ""))
}

func TestSplitDetailSection_MultiByteCharacterAtPartEnd(t *testing.T) {
	for _, ch := range []string{"é", "💡"} {
		output := strings.Repeat("a", 7) + "\n" + ch + ch + "\n" + strings.Repeat("b", 7) + "\n"
		got := splitChunks(t, output, 9)
		assert.Equal(t, output, strings.Join(got, ""))
		for i, c := range got {
			assert.Truef(t, utf8.ValidString(c), "%q part %d", ch, i)
		}
		assert.Equal(t, 2, strings.Count(strings.Join(got, ""), ch), "each character once")
	}
}

func TestSplitDetailSection_LineLongerThanAPartSplitsBetweenCharacters(t *testing.T) {
	long := strings.Repeat("é", 20) + "\n" // 41 bytes
	output := "before\n" + long + "after\n"
	got := splitChunks(t, output, 10)

	assert.Equal(t, output, strings.Join(got, ""))
	assert.Equal(t, "before\n", got[0])
	for i, c := range got {
		assert.Truef(t, utf8.ValidString(c), "part %d", i)
		assert.LessOrEqualf(t, len(c), 10, "part %d", i)
	}
	assert.True(t, strings.HasSuffix(got[len(got)-1], "after\n"))
}

// splitWithinTime runs splitDetailSection and fails, rather than hanging the
// whole test binary, when it does not return.
func splitWithinTime(t require.TestingT, r ProjectResult, reserve, maxParts int) []section {
	done := make(chan []section, 1)
	go func() { done <- splitDetailSection(r, reserve, maxParts) }()
	select {
	case pieces := <-done:
		return pieces
	case <-time.After(5 * time.Second):
		require.FailNow(t, "splitDetailSection did not return")
		return nil
	}
}

// Feature: fix-comment-part-split, Property 2: The cap holds for parts packed by lines
func TestSplitDetailSection_CapHoldsForPartsPackedByLines(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		chunk := rapid.IntRange(120, 400).Draw(t, "chunk")
		maxParts := rapid.IntRange(1, 6).Draw(t, "maxParts")
		annotations := rapid.SliceOfN(rapid.OneOf(
			rapid.StringMatching(`@@ turnip: [a-z ]{0,20} @@\n`),
			rapid.StringMatching(`@@ turnip: [a-z]{300,999}[a-z]{0,500} @@\n`), // longer than a part
		), 0, 3).Draw(t, "annotations")
		lines := rapid.SliceOfN(
			rapid.OneOf(
				rapid.StringMatching(`[a-z0-9 :]{0,12}`),
				rapid.StringMatching(`[a-zé💡]{0,90}`),
				rapid.StringMatching(`[a-zé💡]{200,900}`),
			), 1, 200).Draw(t, "lines")
		lead := strings.Join(annotations, "")
		output := lead + strings.Join(lines, "\n") + "\n"
		if lead != "" && rapid.Bool().Draw(t, "onlyAnnotations") {
			output = strings.TrimSuffix(lead, "\n") // nothing the cap can cut
		}
		r := ProjectResult{ProjectName: "web", Operation: "plan", Success: true, Output: output}

		pieces := splitWithinTime(t, r, reserveForChunk(r, chunk), maxParts)

		require.NotEmpty(t, pieces)
		var all strings.Builder
		for _, p := range pieces {
			all.WriteString(p.chunk)
		}
		joined := all.String()
		if len(pieces) > maxParts {
			// The cap cannot cut the leading annotation lines, so it is over
			// only when they, and the omission line, alone take more parts.
			if joined != output {
				require.True(t, strings.HasPrefix(joined, lead))
				assert.Regexp(t, `^…\(\d+ earlier lines omitted\)\n$`, joined[len(lead):], "cut to its annotation lines and the omission line")
			}
			return
		}
		if joined == output {
			return
		}
		prefix := lead + "…("
		assert.Truef(t, strings.HasPrefix(joined, prefix), "a cut output opens with its annotation lines and the omission line, got %.80q", joined)
		assert.Contains(t, joined[len(lead):], " earlier lines omitted)\n")
	})
}

// bigMixedOutput builds an output of about size bytes: lines of varied
// length with multi-byte characters throughout, a line of code fences now
// and then, a short "port: 8888" line often enough that some byte cut would
// land inside one, and a few lines longer than a whole comment.
func bigMixedOutput(size int) string {
	rng := rand.New(rand.NewSource(7))
	alphabet := []string{"a", "b", "é", "💡", "日", " ", ":"}
	var b strings.Builder
	b.WriteString("@@ turnip: web, example v1.0.0 @@\n@@ turnip: example plan @@\n")
	for i := 0; b.Len() < size; i++ {
		switch {
		case i%12000 == 11999:
			b.WriteString(strings.Repeat("é", 40000+rng.Intn(10000)) + "\n") // longer than a comment
		case i%997 == 0:
			b.WriteString("port: 8888\n")
		case i%1500 == 0:
			b.WriteString("  ```yaml\n")
		default:
			for range rng.Intn(120) {
				b.WriteString(alphabet[rng.Intn(len(alphabet))])
			}
			b.WriteString("\n")
		}
	}
	b.WriteString("@@ turnip: exit 0 in 3s @@\n")
	return b.String()
}

// partsByProject reads every comment body back into the outputs of the
// sections it carries, keyed by Project name and in order. It parses what
// the builder rendered, so it also checks that each section opens a fence
// and closes the same one before its </details>.
func partsByProject(t *testing.T, bodies []string, names []string) map[string][]string {
	t.Helper()
	got := map[string][]string{}
	for bi, body := range bodies {
		assert.Equalf(t, strings.Count(body, "<details>"), strings.Count(body, "</details>"), "body %d closes every <details>", bi)
		assert.NotContainsf(t, body, "omitted because this comment reached GitHub's size limit", "body %d dropped a section", bi)
		for _, seg := range strings.Split(body, "<details>\n<summary>")[1:] {
			summary, rest, ok := strings.Cut(seg, "</summary>\n\n")
			require.True(t, ok, "summary closes")
			openLine, rest, ok := strings.Cut(rest, "\n")
			require.True(t, ok, "fence opens")
			bar := strings.TrimSuffix(openLine, "diff")
			require.GreaterOrEqual(t, len(bar), 3)
			require.Equal(t, strings.Repeat("`", len(bar)), bar, "the opening fence is backticks")
			section, _, ok := strings.Cut(rest, "\n\n</details>")
			require.True(t, ok, "section closes")
			chunk, _, ok := strings.Cut(section, "\n"+bar)
			require.True(t, ok, "the fence the section opens is closed in it")

			name := ""
			for _, n := range names {
				if strings.Contains(summary, "<code>"+n+"</code>") {
					name = n
				}
			}
			require.NotEmptyf(t, name, "summary %q names a Project", summary)
			got[name] = append(got[name], chunk)
		}
	}
	return got
}

// assertPartsEndAtLines checks the parts cut nothing inside a character, and
// nothing inside a line unless the line is longer than every part.
func assertPartsEndAtLines(t *testing.T, name string, parts []string) string {
	t.Helper()
	longest := 0
	for _, p := range parts {
		longest = max(longest, len(p))
	}
	joined := strings.Join(parts, "")
	end := 0
	for i, p := range parts {
		assert.Truef(t, utf8.ValidString(p), "%s part %d is valid UTF-8", name, i)
		end += len(p)
		if i == len(parts)-1 || strings.HasSuffix(p, "\n") {
			continue
		}
		start := strings.LastIndexByte(joined[:end], '\n') + 1
		lineLen := len(joined) - start
		if nl := strings.IndexByte(joined[start:], '\n'); nl >= 0 {
			lineLen = nl + 1
		}
		assert.Greaterf(t, lineLen, longest, "%s part %d ends inside a line that fits a part", name, i)
	}
	return joined
}

// The checkpoint of fix-comment-part-split: each piece of the split can be
// right while a reader still sees a broken line, so this builds whole
// comments and reads them back.
func TestBuildConsolidatedComment_LargeOutputReadsBackWithoutBrokenLines(t *testing.T) {
	big := bigMixedOutput(2 << 20)
	medium := bigMixedOutput(150000)
	results := []ProjectResult{
		{ProjectName: "api", Operation: "plan", Success: true, Output: "no changes\n"},
		{ProjectName: "web", Operation: "plan", Success: true, Output: big},
		{ProjectName: "db", Operation: "plan", Success: false, Output: medium},
		{ProjectName: "cache", Operation: "plan", Success: true, Output: "port: 8888\n"},
	}
	names := []string{"api", "web", "db", "cache"}

	for _, tc := range []struct {
		name     string
		maxParts int
	}{{"default cap", testMaxParts}, {"raised cap", 60}} {
		t.Run(tc.name, func(t *testing.T) {
			bodies := BuildConsolidatedComment(results, tc.maxParts)
			require.NotEmpty(t, bodies)
			for i, body := range bodies {
				assert.LessOrEqualf(t, utf8.RuneCountInString(body), maxCommentLength, "body %d", i)
				assert.LessOrEqualf(t, len(body), maxCommentLength, "body %d in bytes", i)
				assert.Truef(t, utf8.ValidString(body), "body %d is valid UTF-8", i)
			}

			parts := partsByProject(t, bodies, names)
			for _, r := range results {
				joined := assertPartsEndAtLines(t, r.ProjectName, parts[r.ProjectName])
				if len(parts[r.ProjectName]) > tc.maxParts {
					t.Errorf("%s takes %d parts, over the cap %d", r.ProjectName, len(parts[r.ProjectName]), tc.maxParts)
				}
				if joined == r.Output {
					continue
				}
				// Cut by the cap: the leading annotation lines, the omission
				// line, then the end of the output from the start of a line.
				lead := r.Output[:leadingAnnotationLen(r.Output)]
				require.Truef(t, strings.HasPrefix(joined, lead+"…("), "%s opens with its annotation lines and the omission line", r.ProjectName)
				_, kept, ok := strings.Cut(joined[len(lead):], " earlier lines omitted)\n")
				require.True(t, ok)
				assert.Truef(t, strings.HasSuffix(r.Output, kept), "%s keeps the end of its output", r.ProjectName)
				require.Less(t, len(kept), len(r.Output))
				assert.Equalf(t, byte('\n'), r.Output[len(r.Output)-len(kept)-1], "%s keeps its end from the start of a line", r.ProjectName)
			}

			for _, i := range []int{0, 3} {
				assert.Equal(t, results[i].Output, strings.Join(parts[results[i].ProjectName], ""), "an output that fits is untouched")
			}
			if tc.maxParts > testMaxParts {
				assert.Equal(t, big, strings.Join(parts["web"], ""), "the raised cap keeps every line")
				assert.Equal(t, medium, strings.Join(parts["db"], ""))
			}
		})
	}
}

// Annotation lines the cap cannot cut, too long for the cap's parts: the
// split still returns, with the output cut to those lines and the omission
// line, and every body stays within the limit.
func TestBuildConsolidatedComment_LeadLongerThanTheCapStillReturns(t *testing.T) {
	lead := "@@ turnip: web, terraform 1.9 @@\n@@ turnip: terraform plan " +
		strings.Repeat("-var=x ", 10000) + "@@\n"
	r := ProjectResult{ProjectName: "web", Operation: "plan", Success: true, Output: lead + numberedLines(20000)}

	pieces := splitWithinTime(t, r, minReserve, 1)
	assert.Regexp(t, `^…\(20000 earlier lines omitted\)\n$`, strings.TrimPrefix(partsOf(t, pieces), lead))

	only := ProjectResult{ProjectName: "web", Operation: "plan", Success: true,
		Output: strings.TrimSuffix(strings.Repeat("@@ turnip: "+strings.Repeat("a", 300)+" @@\n", 400), "\n")}
	assert.Equal(t, only.Output, partsOf(t, splitWithinTime(t, only, minReserve, 1)))
}

// A tiny part followed by a full one: packing must count the separators and
// the footer, or the body they share overflows and loses the tiny part.
func TestBuildConsolidatedComment_TinyPartBeforeAFullOneIsNotDropped(t *testing.T) {
	r := ProjectResult{
		ProjectName: "web", Operation: "plan", Success: false, Locked: true, PlanOperation: "plan",
		Output: strings.Repeat("x", 500000) + "\n",
	}

	bodies := BuildConsolidatedComment([]ProjectResult{r}, 2)

	for i, body := range bodies {
		assert.LessOrEqualf(t, len(body), maxCommentLength, "body %d", i)
		assert.NotContainsf(t, body, "omitted because this comment reached GitHub's size limit", "body %d dropped a section", i)
		assert.NotContainsf(t, body, chunkOmissionMarker, "body %d cut into a section", i)
	}
	all := strings.Join(bodies, "")
	assert.Contains(t, all, "(output part 1/2)")
	assert.Contains(t, all, "(output part 2/2)")
}

// Output full of backtick runs widens each part's fence past the three
// backticks the scaffold was sized with; the bodies must still fit.
func TestBuildConsolidatedComment_WideFencesStillFit(t *testing.T) {
	output := strings.Repeat(strings.Repeat("`", 500)+"\n", 400)
	for _, maxParts := range []int{1, 5} {
		bodies := BuildConsolidatedComment([]ProjectResult{
			{ProjectName: "web", Operation: "plan", Success: true, Output: output},
		}, maxParts)
		for i, body := range bodies {
			assert.LessOrEqualf(t, len(body), maxCommentLength, "cap %d body %d", maxParts, i)
		}
	}

	// The last resort, cutting into one section, also sizes for the fence.
	got := clampBody("**verdict**\n\n", []section{testSection("huge", output)}, "")
	assert.LessOrEqual(t, len(got), maxCommentLength)
}

// Feature: fix-apply-suggested-with-lock, Decision 1: the apply line
// follows success, a plan operation, the Lock and a registered tool, and
// never the counts.
func TestNextSteps_ApplyOfferTable(t *testing.T) {
	counts := map[string]ChangeCounts{
		"none":       {},
		"some":       {Change: 1},
		"unreported": {Unreported: true},
	}
	for _, success := range []bool{true, false} {
		for _, op := range []string{"diff", "apply"} {
			for _, locked := range []bool{true, false} {
				for name, c := range counts {
					for _, registered := range []bool{true, false} {
						r := ProjectResult{ProjectName: "web", Operation: op, Success: success,
							Locked: locked, Changes: c}
						if registered {
							r.PlanOperation, r.ApplyOperation = "diff", "apply"
						}
						want := success && op == "diff" && locked && registered
						got := strings.Contains(nextSteps(r), "Apply just this project")
						assert.Equal(t, want, got,
							"success=%v op=%s locked=%v counts=%s registered=%v", success, op, locked, name, registered)
					}
				}
			}
		}
	}
}

// Delta scenario "Plan with changes keeps its Lock": the exact three lines,
// in order, so a reordered or partly dropped trailer fails here.
func TestNextSteps_PlanWithChangesKeepsItsLock(t *testing.T) {
	out := nextSteps(ProjectResult{ProjectName: "web", Operation: "diff", Success: true, Locked: true,
		Changes: ChangeCounts{Change: 1}, PlanOperation: "diff", ApplyOperation: "apply"})
	assert.Equal(t, "\n\n- Apply just this project: `/turnip apply web`\n"+
		"- Re-plan it: `/turnip diff web`\n- Release its lock: `/turnip unlock web`", out)
}

func TestNextSteps_CleanPlanWithKeptLock(t *testing.T) {
	out := nextSteps(ProjectResult{ProjectName: "web", Operation: "diff", Success: true, Locked: true,
		PlanOperation: "diff", ApplyOperation: "apply"})
	assert.Equal(t, "\n\n- Apply just this project: `/turnip apply web`\n"+
		"- Re-plan it: `/turnip diff web`\n- Release its lock: `/turnip unlock web`", out)
}

func TestNextSteps_PlanWithUnreportedCounts(t *testing.T) {
	out := nextSteps(ProjectResult{ProjectName: "web", Operation: "diff", Success: true, Locked: true,
		Changes: ChangeCounts{Unreported: true}, PlanOperation: "diff", ApplyOperation: "apply"})
	assert.Contains(t, out, "`/turnip apply web`")
}

func TestNextSteps_CleanPlanWithReleasedLock(t *testing.T) {
	out := nextSteps(ProjectResult{ProjectName: "web", Operation: "diff", Success: true, Locked: false,
		PlanOperation: "diff", ApplyOperation: "apply"})
	assert.Equal(t, "\n\n- Re-plan it: `/turnip diff web`", out)
}

func TestNextSteps_SuccessfulApplyOffersNoApply(t *testing.T) {
	out := nextSteps(ProjectResult{ProjectName: "web", Operation: "apply", Success: true, Locked: false,
		Changes: ChangeCounts{Change: 1}, PlanOperation: "diff", ApplyOperation: "apply"})
	assert.NotContains(t, out, "Apply just this project")
}

func TestNextSteps_FailedPlanOffersNoApply(t *testing.T) {
	out := nextSteps(ProjectResult{ProjectName: "web", Operation: "diff", Success: false, Locked: true,
		PlanOperation: "diff", ApplyOperation: "apply"})
	assert.NotContains(t, out, "Apply just this project")
}
