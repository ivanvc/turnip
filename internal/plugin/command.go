package plugin

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"
	"sync"
)

// commandRunner abstracts subprocess execution so tests can substitute a
// fake without invoking a real tool binary. onOutput, when non-nil, is
// called once per line of output as it's produced, tagged with which pipe
// ("stdout" or "stderr") it came from.
type commandRunner func(ctx context.Context, dir, name string, args []string, onOutput func(stream, line string)) (stdout, stderr []byte, exitCode int, err error)

// syncBuffer accumulates lines from one output stream, joined with "\n" as
// they arrive, safe for concurrent use by the goroutine scanning that
// stream's pipe and the goroutine that eventually reads the result.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
	err error
}

func (s *syncBuffer) writeLine(line string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.buf.Len() > 0 {
		s.buf.WriteByte('\n')
	}
	s.buf.WriteString(line)
}

func (s *syncBuffer) setErr(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.err = err
}

func (s *syncBuffer) Bytes() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Bytes()
}

func (s *syncBuffer) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

// execCommand is the default commandRunner, backed by os/exec.
//
// When the subprocess starts and runs — even to a non-zero exit — its real
// stdout, stderr, and exit code are returned with a nil error: that's a
// normal tool failure, not an execution error. err is only non-nil when the
// subprocess could not be started at all (binary not found, invalid working
// directory), in which case exitCode is -1 and there is no tool output to
// report.
//
// Output is captured by scanning the subprocess's stdout and stderr pipes
// line-by-line, concurrently, one goroutine per pipe — rather than waiting
// for the whole process to exit before any output is observable — so
// onOutput can be invoked as each line is produced.
func execCommand(ctx context.Context, dir, name string, args []string, onOutput func(stream, line string)) (stdout, stderr []byte, exitCode int, err error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, -1, err
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, nil, -1, err
	}

	if err := cmd.Start(); err != nil {
		return nil, nil, -1, err
	}

	var stdoutBuf, stderrBuf syncBuffer
	var wg sync.WaitGroup
	wg.Add(2)
	go scanLines(stdoutPipe, &stdoutBuf, "stdout", onOutput, &wg)
	go scanLines(stderrPipe, &stderrBuf, "stderr", onOutput, &wg)
	wg.Wait()

	runErr := cmd.Wait()
	if runErr == nil {
		if scanErr := stdoutBuf.Err(); scanErr != nil {
			return nil, nil, -1, scanErr
		}
		if scanErr := stderrBuf.Err(); scanErr != nil {
			return nil, nil, -1, scanErr
		}
		return stdoutBuf.Bytes(), stderrBuf.Bytes(), 0, nil
	}

	if exitErr, ok := errors.AsType[*exec.ExitError](runErr); ok {
		return stdoutBuf.Bytes(), stderrBuf.Bytes(), exitErr.ExitCode(), nil
	}

	return nil, nil, -1, runErr
}

// scanLines reads r line-by-line, recording each line in dst and, when
// onOutput is non-nil, reporting it tagged with stream.
func scanLines(r io.Reader, dst *syncBuffer, stream string, onOutput func(stream, line string), wg *sync.WaitGroup) {
	defer wg.Done()

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		dst.writeLine(line)
		if onOutput != nil {
			onOutput(stream, line)
		}
	}
	if err := scanner.Err(); err != nil {
		dst.setErr(err)
	}
}
