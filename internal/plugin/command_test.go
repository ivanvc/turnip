package plugin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// toolLines drops the transcript's annotation lines, leaving what the tool
// itself wrote.
func toolLines(lines []OutputLine) []OutputLine {
	var out []OutputLine
	for _, l := range lines {
		if !strings.HasPrefix(l.Text, transcriptPrefix) {
			out = append(out, l)
		}
	}
	return out
}

func TestExecCommand_Success(t *testing.T) {
	lines, exitCode, err := execCommand(context.Background(), "", "sh", []string{"-c", "echo hello"}, "", nil)
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)
	assert.Equal(t, []OutputLine{stdoutLine("hello")}, toolLines(lines))
}

func TestExecCommand_NonZeroExit(t *testing.T) {
	lines, exitCode, err := execCommand(context.Background(), "", "sh", []string{"-c", "echo out; echo err >&2; exit 3"}, "", nil)
	require.NoError(t, err)
	assert.Equal(t, 3, exitCode)
	assert.ElementsMatch(t, []OutputLine{stdoutLine("out"), stderrLine("err")}, toolLines(lines))
	assert.Equal(t, "err", streamText(lines, "stderr"), "stderr carries no transcript")
}

func TestExecCommand_BinaryNotFound(t *testing.T) {
	lines, exitCode, err := execCommand(context.Background(), "", "turnip-nonexistent-binary-xyz", nil, "", nil)
	require.Error(t, err)
	assert.Equal(t, -1, exitCode)
	assert.Empty(t, lines)
}

func TestExecCommand_RespectsWorkingDir(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "marker.txt"), []byte("x"), 0o644))

	lines, exitCode, err := execCommand(context.Background(), dir, "ls", nil, "", nil)
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, outputText(lines), "marker.txt")
}

// The record is in the order the tool wrote, across both streams — not
// stdout then stderr. The sleeps make that order deterministic; without
// them two lines on different pipes may legitimately arrive either way.
//
// It ends on stderr on purpose: the trailer must still come after it,
// which is exactly what grouping by stream got wrong.
func TestExecCommand_RecordInterleavesStreamsInArrivalOrder(t *testing.T) {
	lines, exitCode, err := execCommand(context.Background(), "", "sh", []string{"-c",
		"echo a; sleep 0.1; echo b >&2; sleep 0.1; echo c; sleep 0.1; echo d >&2",
	}, "", nil)
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)

	assert.Equal(t, []OutputLine{
		stdoutLine("a"), stderrLine("b"), stdoutLine("c"), stderrLine("d"),
	}, toolLines(lines))

	require.Len(t, lines, 7, "two header lines, four tool lines, one trailer")
	assert.True(t, strings.HasPrefix(lines[0].Text, transcriptPrefix), "the provenance line opens the record")
	assert.True(t, strings.HasPrefix(lines[1].Text, transcriptPrefix+"sh "), "then the command")
	assert.True(t, strings.HasPrefix(lines[6].Text, transcriptPrefix+"exit 0 in "),
		"the trailer closes the record, after the last stderr line")
}

// onOutput sees exactly the record, in the same order, so the live view and
// the final comment cannot tell different stories.
func TestExecCommand_OnOutputSeesTheRecordInOrder(t *testing.T) {
	var live []OutputLine
	onOutput := func(stream, line string) { live = append(live, OutputLine{Stream: stream, Text: line}) }

	lines, exitCode, err := execCommand(context.Background(), "", "sh", []string{"-c",
		"echo out1; echo err1 >&2; echo out2; echo err2 >&2",
	}, "", onOutput)
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)

	assert.Equal(t, lines, live)
	for _, l := range live {
		assert.Contains(t, []string{"stdout", "stderr"}, l.Stream)
	}
}

func TestExecCommand_OnOutputNilIsNoOp(t *testing.T) {
	lines, exitCode, err := execCommand(context.Background(), "", "sh", []string{"-c", "echo hello"}, "", nil)
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, outputText(lines), "hello")
}

// The transcript has to reach the *record*, not only onOutput.
//
// These are two destinations, and only one becomes the pull request
// comment: the record is returned here and ends up in ExecuteResult.Output,
// while onOutput feeds the live log stream the Server currently drops. A
// transcript emitted through the callback alone would satisfy every test
// that asserts the callback fired and be absent from every comment.
//
// A fake commandRunner cannot prove this, which is why it runs a real
// process.
func TestExecCommand_TranscriptReachesTheCapturedOutput(t *testing.T) {
	var live []string
	onOutput := func(stream, line string) { live = append(live, line) }

	lines, exitCode, err := execCommand(
		context.Background(), "/tmp", "sh", []string{"-c", "echo payload"}, "v1.2.3", onOutput)
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)

	require.Len(t, lines, 4)
	assert.Equal(t, stdoutLine("@@ turnip: /tmp, sh v1.2.3 @@"), lines[0], "the provenance line")
	assert.Equal(t, stdoutLine("@@ turnip: sh -c echo payload @@"), lines[1], "the resolved command")
	assert.Equal(t, stdoutLine("payload"), lines[2], "the tool's own output")
	assert.Regexp(t, `^@@ turnip: exit 0 in \S+ @@$`, lines[3].Text, "the trailer")

	// The same lines are mirrored live, so a future viewer sees them too.
	assert.Contains(t, live, "@@ turnip: /tmp, sh v1.2.3 @@")
}

// A command that never started must not report a transcript for something
// that never ran.
func TestExecCommand_NoTranscriptWhenTheCommandNeverStarts(t *testing.T) {
	lines, _, err := execCommand(
		context.Background(), "", "turnip-nonexistent-binary-xyz", nil, "v1", nil)
	require.Error(t, err)
	assert.Empty(t, lines)
}

// Every annotation is `@@ turnip: … @@` — highlighted as a hunk header in a
// diff fence — and never begins with "+" or "-": those belong to the diff
// payload, and an annotation so prefixed is counted by eye as a change.
func TestTranscriptHeader_UsesTheAnnotationMarkers(t *testing.T) {
	for _, line := range transcriptHeader("/w/dir", "helmfile", []string{"-l", "name=web"}, "v0.169.0") {
		assert.True(t, strings.HasPrefix(line, transcriptPrefix), "one prefix marks every annotation")
		assert.True(t, strings.HasSuffix(line, transcriptSuffix))
		assert.False(t, strings.HasPrefix(line, "+"))
		assert.False(t, strings.HasPrefix(line, "-"))
	}
}
