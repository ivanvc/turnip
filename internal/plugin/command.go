package plugin

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
)

// commandRunner abstracts subprocess execution so tests can substitute a
// fake without invoking a real tool binary.
type commandRunner func(ctx context.Context, dir, name string, args []string) (stdout, stderr []byte, exitCode int, err error)

// execCommand is the default commandRunner, backed by os/exec.
//
// When the subprocess starts and runs — even to a non-zero exit — its real
// stdout, stderr, and exit code are returned with a nil error: that's a
// normal tool failure, not an execution error. err is only non-nil when the
// subprocess could not be started at all (binary not found, invalid working
// directory), in which case exitCode is -1 and there is no tool output to
// report.
func execCommand(ctx context.Context, dir, name string, args []string) (stdout, stderr []byte, exitCode int, err error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	runErr := cmd.Run()
	if runErr == nil {
		return stdoutBuf.Bytes(), stderrBuf.Bytes(), 0, nil
	}

	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		return stdoutBuf.Bytes(), stderrBuf.Bytes(), exitErr.ExitCode(), nil
	}

	return nil, nil, -1, runErr
}
