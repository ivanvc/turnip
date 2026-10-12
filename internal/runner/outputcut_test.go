package runner

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/ivanvc/turnip/internal/plugin"
)

const (
	testHeaderLines  = "@@ turnip: environments/web, helmfile v1.1.0 @@\n@@ turnip: helmfile diff @@\n"
	testClosingLine  = "@@ turnip: exit 0 in 12.3s @@\n"
	testLastToolLine = "Comparing release=web, chart=charts/web, namespace=web\n"
)

var omittedLine = regexp.MustCompile(`(?m)^@@ turnip: (\d+) earlier lines omitted \(([0-9.]+ [KM]iB)\) @@\n`)

// outputOfSize builds a transcript of exactly size bytes: the header
// lines, numbered tool lines, a recognizable last tool line, then the
// closing annotation line.
func outputOfSize(t *testing.T, size int) string {
	t.Helper()
	var b strings.Builder
	b.WriteString(testHeaderLines)
	fixed := len(testHeaderLines) + len(testLastToolLine) + len(testClosingLine)
	for i := 0; b.Len() < size-len(testLastToolLine)-len(testClosingLine)-100; i++ {
		fmt.Fprintf(&b, "+  resource line %07d with some padding to look like a manifest\n", i)
	}
	pad := size - b.Len() - len(testLastToolLine) - len(testClosingLine)
	require.GreaterOrEqual(t, pad, 1)
	require.Greater(t, size, fixed)
	b.WriteString(strings.Repeat("x", pad-1) + "\n")
	b.WriteString(testLastToolLine)
	b.WriteString(testClosingLine)
	out := b.String()
	require.Len(t, out, size)
	return out
}

// The spec's scenario: a first install's plan printed 5,069,133 bytes.
func TestCutOutput_FirstInstallPlan(t *testing.T) {
	output := outputOfSize(t, 5_069_133)

	got := cutOutput(output)

	assert.LessOrEqual(t, len(got), maxSentOutputBytes)
	require.True(t, strings.HasPrefix(got, testHeaderLines), "the leading annotation lines are kept")
	assert.True(t, strings.HasSuffix(got, testLastToolLine+testClosingLine), "the end of the tool's output and the closing line are kept")

	rest := strings.TrimPrefix(got, testHeaderLines)
	m := omittedLine.FindStringSubmatchIndex(rest)
	require.NotNil(t, m, "the omission line follows the leading annotation lines")
	assert.Equal(t, 0, m[0], "the omission line comes right after the leading annotation lines")
	assert.Equal(t, "3.8 MiB", rest[m[4]:m[5]])

	kept := rest[m[1]:]
	assert.True(t, strings.HasSuffix(output, kept), "what follows the omission line is the original output's end")
	removed := len(output) - len(testHeaderLines) - len(kept)
	var lines int
	_, err := fmt.Sscanf(rest[m[2]:m[3]], "%d", &lines)
	require.NoError(t, err)
	assert.Equal(t, strings.Count(output[len(testHeaderLines):len(testHeaderLines)+removed], "\n"), lines)
	assert.Equal(t, formatSize(removed), rest[m[4]:m[5]])
}

func TestCutOutput_WithinTheLimitIsUnchanged(t *testing.T) {
	output := outputOfSize(t, 200*1024)

	assert.Equal(t, output, cutOutput(output))
}

func TestCutOutput_ExactlyAtTheLimitIsUnchanged(t *testing.T) {
	output := outputOfSize(t, maxSentOutputBytes)

	assert.Equal(t, output, cutOutput(output))
}

// A single tool line longer than the whole budget keeps its own end, cut
// inside the line, rather than leaving nothing of the tool's output.
func TestCutOutput_SingleOversizedLineKeepsItsEnd(t *testing.T) {
	line := strings.Repeat("é", 1<<20) + "THE END\n" // two bytes a rune, so the cut must find a rune boundary
	output := testHeaderLines + line + testClosingLine

	got := cutOutput(output)

	assert.LessOrEqual(t, len(got), maxSentOutputBytes)
	assert.True(t, utf8.ValidString(got), "a cut inside a line never splits a rune")
	assert.True(t, strings.HasPrefix(got, testHeaderLines))
	assert.True(t, strings.HasSuffix(got, "THE END\n"+testClosingLine))
	assert.Regexp(t, `@@ turnip: 0 earlier lines omitted \([0-9.]+ MiB\) @@`, got)
}

func TestTooLargeResult_NeverReadsAsTheLimit(t *testing.T) {
	got := tooLargeResult(OperationResult{ExitCode: 0}, (4<<20)+1).ErrorMessage
	assert.Equal(t, "the result was too large to deliver (4194305 bytes; the Server accepts at most 4 MiB)", got)
	got = tooLargeResult(OperationResult{ExitCode: 0}, 5<<20).ErrorMessage
	assert.Equal(t, "the result was too large to deliver (5.0 MiB; the Server accepts at most 4 MiB)", got)
}

func TestFormatSize(t *testing.T) {
	cases := map[int]string{
		0:               "0.0 KiB",
		1536:            "1.5 KiB",
		200 * 1024:      "200.0 KiB",
		1 << 20:         "1.0 MiB",
		4_020_557:       "3.8 MiB",
		5 * (1 << 20):   "5.0 MiB",
		(1 << 20) + 1:   "1.0 MiB",
		(1 << 20) - 512: "1023.5 KiB",
		(1 << 20) - 1:   "1.0 MiB", // rounds to 1024.0 KiB, so it reads in MiB
		(1 << 20) - 51:  "1.0 MiB",
	}
	for n, want := range cases {
		assert.Equal(t, want, formatSize(n), "%d bytes", n)
	}
}

// The plugin reads its counts from the whole output, and plan data is
// never cut: only the output's copy in the result is.
func TestRunWith_CutLeavesCountsAndPlanDataAlone(t *testing.T) {
	output := outputOfSize(t, 5_069_133)
	planData := []byte(strings.Repeat("p", 2<<20))
	p := &fakePlugin{
		operations: []string{"diff"},
		result: &plugin.ExecuteResult{
			ExitCode:      0,
			Output:        output,
			ChangeSummary: plugin.ChangeSummary{Add: 7, Change: 3, Destroy: 1},
			PlanData:      planData,
		},
	}
	rep := &fakeReporter{}
	var stdout, stderr safeBuffer

	exitCode := runWith(context.Background(), testRunConfig(), rep, fakeSelector(p), &stdout, &stderr)

	require.Equal(t, 0, exitCode)
	assert.True(t, rep.lastResult.Success)
	assert.LessOrEqual(t, len(rep.lastResult.Output), maxSentOutputBytes)
	assert.Contains(t, rep.lastResult.Output, "(3.8 MiB) @@")
	assert.Equal(t, plugin.ChangeSummary{Add: 7, Change: 3, Destroy: 1}, rep.lastResult.Changes)
	assert.Equal(t, planData, rep.lastResult.PlanData)
}

// Feature: fix-large-output, Property 1: The cut output fits and keeps the end
func TestCutOutput_Property_FitsAndKeepsTheEnd(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		limit := rapid.IntRange(2*maxOmittedLen, 8192).Draw(t, "limit")
		lineGen := rapid.OneOf(
			rapid.StringMatching(`@@ turnip: [a-z ]{0,20} @@`),
			rapid.StringOfN(rapid.RuneFrom([]rune("abcé😀 +-@")), 0, 2000, -1),
		)
		lines := rapid.SliceOfN(lineGen, 1, 60).Draw(t, "lines")
		output := strings.Join(lines, "\n")
		if rapid.Bool().Draw(t, "trailingNewline") {
			output += "\n"
		}

		got := cutOutputTo(output, limit)

		assert.LessOrEqual(t, len(got), limit)
		assert.True(t, utf8.ValidString(got))
		if len(output) <= limit {
			assert.Equal(t, output, got)
			return
		}
		loc := omittedLine.FindStringIndex(got)
		require.NotNil(t, loc, "a cut output says what it omitted")
		tail := got[loc[1]:]
		assert.NotEmpty(t, tail)
		assert.True(t, strings.HasSuffix(output, tail), "the cut output ends with the original output's end")
		assert.True(t, strings.HasPrefix(output, got[:loc[0]]), "what precedes the omission line is the original's start")
	})
}
