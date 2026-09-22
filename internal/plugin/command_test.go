package plugin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExecCommand_Success(t *testing.T) {
	stdout, _, exitCode, err := execCommand(context.Background(), "", "sh", []string{"-c", "echo hello"}, "", nil)
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, string(stdout), "hello", "the tool's output, alongside turnip's transcript")
}

func TestExecCommand_NonZeroExit(t *testing.T) {
	stdout, stderr, exitCode, err := execCommand(context.Background(), "", "sh", []string{"-c", "echo out; echo err >&2; exit 3"}, "", nil)
	require.NoError(t, err)
	assert.Equal(t, 3, exitCode)
	assert.Contains(t, string(stdout), "out")
	assert.Equal(t, "err", strings.TrimSpace(string(stderr)), "stderr carries no transcript")
}

func TestExecCommand_BinaryNotFound(t *testing.T) {
	_, _, exitCode, err := execCommand(context.Background(), "", "turnip-nonexistent-binary-xyz", nil, "", nil)
	require.Error(t, err)
	assert.Equal(t, -1, exitCode)
}

func TestExecCommand_RespectsWorkingDir(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "marker.txt"), []byte("x"), 0o644))

	stdout, _, exitCode, err := execCommand(context.Background(), dir, "ls", nil, "", nil)
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, string(stdout), "marker.txt")
}

type recordedLine struct {
	stream string
	line   string
}

func TestExecCommand_OnOutputCalledPerLineTaggedByStream(t *testing.T) {
	var mu sync.Mutex
	var got []recordedLine
	onOutput := func(stream, line string) {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, recordedLine{stream: stream, line: line})
	}

	stdout, stderr, exitCode, err := execCommand(
		context.Background(), "", "sh",
		[]string{"-c", "echo out1; echo out2; echo err1 >&2; echo err2 >&2"},
		"",
		onOutput,
	)
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)

	var gotStdout, gotStderr []string
	for _, rl := range got {
		switch rl.stream {
		case "stdout":
			gotStdout = append(gotStdout, rl.line)
		case "stderr":
			gotStderr = append(gotStderr, rl.line)
		default:
			t.Fatalf("unexpected stream tag: %q", rl.stream)
		}
	}

	// The tool's own lines, tagged by the stream they came from. stdout
	// also carries turnip's transcript, which is mirrored here by design —
	// so this asserts the tool's lines are present and correctly tagged,
	// not that they are the only ones.
	assert.Subset(t, gotStdout, []string{"out1", "out2"})
	assert.Equal(t, []string{"err1", "err2"}, gotStderr,
		"stderr carries no annotation, so it is exactly the tool's")

	// Every mirrored line reaches the buffer for its stream, which is what
	// keeps the live view and the final comment telling the same story.
	assert.Equal(t, strings.Join(gotStdout, "\n"), string(stdout))
	assert.Equal(t, strings.Join(gotStderr, "\n"), string(stderr))
}

func TestExecCommand_OnOutputNilIsNoOp(t *testing.T) {
	stdout, _, exitCode, err := execCommand(context.Background(), "", "sh", []string{"-c", "echo hello"}, "", nil)
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, string(stdout), "hello")
}

// The transcript has to reach the *captured buffer*, not only onOutput.
//
// These are two destinations, and only one becomes the pull request
// comment: the buffer is returned here and ends up in
// ExecuteResult.Output, while onOutput feeds the live log stream the
// Server currently drops. A transcript emitted through the callback alone
// would satisfy every test that asserts the callback fired and be absent
// from every comment.
//
// A fake commandRunner cannot prove this, which is why it runs a real
// process.
func TestExecCommand_TranscriptReachesTheCapturedOutput(t *testing.T) {
	var live []recordedLine
	onOutput := func(stream, line string) { live = append(live, recordedLine{stream, line}) }

	stdout, _, exitCode, err := execCommand(
		context.Background(), "/tmp", "sh", []string{"-c", "echo payload"}, "v1.2.3", onOutput)
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)

	captured := string(stdout)
	assert.Contains(t, captured, "# turnip · /tmp · sh v1.2.3", "the provenance line")
	assert.Contains(t, captured, "# turnip · sh -c echo payload", "the resolved command")
	assert.Contains(t, captured, "# turnip · exit 0 ·", "the trailer")
	assert.Contains(t, captured, "payload", "and the tool's own output")

	// The same lines are mirrored live, so a future viewer sees them too.
	var mirrored []string
	for _, l := range live {
		mirrored = append(mirrored, l.line)
	}
	assert.Contains(t, mirrored, "# turnip · /tmp · sh v1.2.3")
}

// Ordering is asserted rather than assumed: emitting after the scanners
// start would usually still look right, and would race.
func TestExecCommand_TranscriptPrecedesToolOutput(t *testing.T) {
	stdout, _, _, err := execCommand(
		context.Background(), "", "sh", []string{"-c", "echo payload"}, "", nil)
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(string(stdout)), "\n")
	require.GreaterOrEqual(t, len(lines), 4)
	assert.True(t, strings.HasPrefix(lines[0], "# turnip ·"))
	assert.True(t, strings.HasPrefix(lines[1], "# turnip · sh"))
	assert.Equal(t, "payload", lines[2], "the tool's first line follows the command that produced it")
	assert.True(t, strings.HasPrefix(lines[len(lines)-1], "# turnip · exit "))
}

// A command that never started must not report a transcript for something
// that never ran.
func TestExecCommand_NoTranscriptWhenTheCommandNeverStarts(t *testing.T) {
	stdout, _, _, err := execCommand(
		context.Background(), "", "turnip-nonexistent-binary-xyz", nil, "v1", nil)
	require.Error(t, err)
	assert.Empty(t, stdout)
}

// The annotation never begins with "+" or "-": those belong to the diff
// payload, and an annotation so prefixed is counted by eye as a change.
func TestTranscriptHeader_NeverUsesThePayloadsPrefixes(t *testing.T) {
	for _, line := range transcriptHeader("/w/dir", "helmfile", []string{"-l", "name=web"}, "v0.169.0") {
		assert.True(t, strings.HasPrefix(line, transcriptPrefix), "one prefix marks every annotation")
		assert.False(t, strings.HasPrefix(line, "+"))
		assert.False(t, strings.HasPrefix(line, "-"))
	}
}
