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
	assert.Contains(t, body, "| Project | Operation | Status |")
	for _, r := range results {
		assert.Contains(t, body, r.ProjectName)
		assert.Contains(t, body, r.Output)
	}
	assert.Contains(t, body, "<details>")
	assert.Contains(t, body, "</details>")
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
	assert.Contains(t, bodies[0], "| Project | Operation | Status |")
	for i, body := range bodies[1:] {
		assert.NotContainsf(t, body, "| Project | Operation | Status |", "continuation body %d", i+1)
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

		start := strings.Index(body, "```\n") + len("```\n")
		end := strings.LastIndex(body, "\n```")
		require.Greaterf(t, end, start, "body %d missing a well-formed code fence", i)
		reassembled.WriteString(body[start:end])

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
		assert.LessOrEqualf(t, len(piece), maxCommentLength-minReserve, "piece %d", i)

		start := strings.Index(piece, "```\n") + len("```\n")
		end := strings.LastIndex(piece, "\n```")
		require.Greaterf(t, end, start, "piece %d missing a well-formed code fence", i)
		reassembled.WriteString(piece[start:end])

		assert.Containsf(t, piece, fmt.Sprintf("part %d/%d", i+1, len(pieces)), "piece %d", i)
	}

	assert.Equal(t, output, reassembled.String())
}

func TestSplitDetailSection_FitsWithoutSplitting(t *testing.T) {
	r := ProjectResult{ProjectName: "vpc", Operation: "plan", Success: true, Output: "no changes"}

	pieces := splitDetailSection(r, minReserve)
	require.Len(t, pieces, 1)
	assert.NotContains(t, pieces[0], "output part")
}

func TestTruncateBody_ClosesOpenFenceAndDetails(t *testing.T) {
	body := "<details>\n<summary>huge (plan) — success</summary>\n\n```\n" + strings.Repeat("y", maxCommentLength*2)

	got := truncateBody(body)
	assert.LessOrEqual(t, len(got), maxCommentLength)
	assert.True(t, strings.HasSuffix(got, "```\n\n_(truncated)_\n</details>"), "truncated body must close its own code fence and <details> tag, got suffix %q", got[len(got)-min(60, len(got)):])
}

func TestTruncateBody_NoOpUnderLimit(t *testing.T) {
	body := "short body"
	assert.Equal(t, body, truncateBody(body))
}

func TestBuildConsolidatedComment_EmptyResults(t *testing.T) {
	bodies := BuildConsolidatedComment(nil)
	assert.Nil(t, bodies)
}
