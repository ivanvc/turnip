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
	stdout, _, exitCode, err := execCommand(context.Background(), "", "sh", []string{"-c", "echo hello"}, nil)
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)
	assert.Equal(t, "hello", strings.TrimSpace(string(stdout)))
}

func TestExecCommand_NonZeroExit(t *testing.T) {
	stdout, stderr, exitCode, err := execCommand(context.Background(), "", "sh", []string{"-c", "echo out; echo err >&2; exit 3"}, nil)
	require.NoError(t, err)
	assert.Equal(t, 3, exitCode)
	assert.Equal(t, "out", strings.TrimSpace(string(stdout)))
	assert.Equal(t, "err", strings.TrimSpace(string(stderr)))
}

func TestExecCommand_BinaryNotFound(t *testing.T) {
	_, _, exitCode, err := execCommand(context.Background(), "", "turnip-nonexistent-binary-xyz", nil, nil)
	require.Error(t, err)
	assert.Equal(t, -1, exitCode)
}

func TestExecCommand_RespectsWorkingDir(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "marker.txt"), []byte("x"), 0o644))

	stdout, _, exitCode, err := execCommand(context.Background(), dir, "ls", nil, nil)
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

	assert.Equal(t, []string{"out1", "out2"}, gotStdout)
	assert.Equal(t, []string{"err1", "err2"}, gotStderr)
	assert.Equal(t, strings.Join(gotStdout, "\n"), string(stdout))
	assert.Equal(t, strings.Join(gotStderr, "\n"), string(stderr))
}

func TestExecCommand_OnOutputNilIsNoOp(t *testing.T) {
	stdout, _, exitCode, err := execCommand(context.Background(), "", "sh", []string{"-c", "echo hello"}, nil)
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)
	assert.Equal(t, "hello", string(stdout))
}
