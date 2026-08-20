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

func TestExecCommand_Success(t *testing.T) {
	stdout, _, exitCode, err := execCommand(context.Background(), "", "sh", []string{"-c", "echo hello"})
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)
	assert.Equal(t, "hello", strings.TrimSpace(string(stdout)))
}

func TestExecCommand_NonZeroExit(t *testing.T) {
	stdout, stderr, exitCode, err := execCommand(context.Background(), "", "sh", []string{"-c", "echo out; echo err >&2; exit 3"})
	require.NoError(t, err)
	assert.Equal(t, 3, exitCode)
	assert.Equal(t, "out", strings.TrimSpace(string(stdout)))
	assert.Equal(t, "err", strings.TrimSpace(string(stderr)))
}

func TestExecCommand_BinaryNotFound(t *testing.T) {
	_, _, exitCode, err := execCommand(context.Background(), "", "turnip-nonexistent-binary-xyz", nil)
	require.Error(t, err)
	assert.Equal(t, -1, exitCode)
}

func TestExecCommand_RespectsWorkingDir(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "marker.txt"), []byte("x"), 0o644))

	stdout, _, exitCode, err := execCommand(context.Background(), dir, "ls", nil)
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, string(stdout), "marker.txt")
}
