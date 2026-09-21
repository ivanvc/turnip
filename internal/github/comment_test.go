package github

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildConsolidatedComment_SingleBody(t *testing.T) {
	results := []ProjectResult{
		{ProjectName: "vpc", Operation: "plan", Success: true, Output: "no changes"},
		{ProjectName: "k8s-apps", Operation: "diff", Success: false, Output: "1 changed release"},
	}

	bodies := BuildConsolidatedComment(results)
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

	bodies := BuildConsolidatedComment(results)
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

	bodies := BuildConsolidatedComment(results)
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

	pieces := splitDetailSection(r, minReserve)
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

	pieces := splitDetailSection(r, minReserve)
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

func TestBuildConsolidatedComment_EmptyResults(t *testing.T) {
	bodies := BuildConsolidatedComment(nil)
	assert.Nil(t, bodies)
}

// The collapsed summary line is what a reviewer sees before expanding
// anything, so it carries the whole per-Project verdict. This is the line
// that replaced the summary table.
func TestBuildConsolidatedComment_SummaryLineCarriesStatusAndCounts(t *testing.T) {
	bodies := BuildConsolidatedComment([]ProjectResult{
		{ProjectName: "infra", Operation: "diff", Success: true, Output: "x",
			Changes: ChangeCounts{Add: 1, Change: 4, Destroy: 2}},
	})
	require.Len(t, bodies, 1)

	assert.Contains(t, bodies[0], "<summary>✅ infra · diff · +1 ~4 -2</summary>")
}

// Requirement 2.3: a Project that reported no changes says so, rather than
// rendering three zeros that read like a measurement.
func TestBuildConsolidatedComment_NoChangesSaysSoRatherThanZeros(t *testing.T) {
	bodies := BuildConsolidatedComment([]ProjectResult{
		{ProjectName: "apps", Operation: "diff", Success: true, Output: "No changes."},
	})
	require.Len(t, bodies, 1)

	assert.Contains(t, bodies[0], "<summary>✅ apps · diff · no changes</summary>")
	assert.NotContains(t, bodies[0], "+0 ~0 -0")
}

// Requirement 2.4: a failure reports no usable counts, so none are shown —
// presenting its zeroes would make a run that never completed look like
// one that planned nothing.
func TestBuildConsolidatedComment_FailureShowsNoCounts(t *testing.T) {
	bodies := BuildConsolidatedComment([]ProjectResult{
		{ProjectName: "edge", Operation: "diff", Success: false, Output: "boom",
			Changes: ChangeCounts{Add: 9, Change: 9, Destroy: 9}},
	})
	require.Len(t, bodies, 1)

	assert.Contains(t, bodies[0], "<summary>❌ edge · diff · failed</summary>")
	assert.NotContains(t, bodies[0], "+9 ~9 -9",
		"counts from an Operation that did not complete must not be shown")
}

// Requirement 3.4. The assertion whose absence would let turnip invite a
// reader to apply a plan that never finished.
func TestBuildConsolidatedComment_FailedProjectIsNeverInvitedToApply(t *testing.T) {
	bodies := BuildConsolidatedComment([]ProjectResult{
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
	bodies := BuildConsolidatedComment([]ProjectResult{
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
	bodies := BuildConsolidatedComment([]ProjectResult{
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
	bodies := BuildConsolidatedComment([]ProjectResult{
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
			"**4 changes across 3 projects** — 1 with no changes, 1 failed.",
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
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, buildVerdictLine(tc.results))
		})
	}
}

// Requirement 10.5: output is fenced for syntax highlighting. A success is
// fenced `diff` so GitHub colours added and removed lines, which is most of
// what makes a plan readable. A failure is not: its body is an error
// message, and diff highlighting would colour every line beginning with
// "-" red for no reason.
func TestBuildConsolidatedComment_DiffFenceOnSuccessPlainOnFailure(t *testing.T) {
	success := BuildConsolidatedComment([]ProjectResult{
		{ProjectName: "ok", Operation: "diff", Success: true, Output: "- old\n+ new"},
	})
	require.Len(t, success, 1)
	assert.Contains(t, success[0], "```diff\n- old\n+ new\n```")

	failure := BuildConsolidatedComment([]ProjectResult{
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
	const note = "Lock released — the plan failed, so nothing was recorded."
	parts := BuildConsolidatedComment([]ProjectResult{{
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
	parts := BuildConsolidatedComment([]ProjectResult{{
		ProjectName: "web", Tool: "helmfile", Operation: "diff", Success: true, Output: "no changes",
	}})
	require.Len(t, parts, 1)
	assert.NotContains(t, parts[0], "Lock released")
}
