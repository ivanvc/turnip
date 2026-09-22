package plugin

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// commandRunner abstracts subprocess execution so tests can substitute a
// fake without invoking a real tool binary. onOutput, when non-nil, is
// called once per line of output as it's produced, tagged with which pipe
// ("stdout" or "stderr") it came from.
type commandRunner func(ctx context.Context, dir, name string, args []string, version string, onOutput func(stream, line string)) (stdout, stderr []byte, exitCode int, err error)

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
func execCommand(ctx context.Context, dir, name string, args []string, version string, onOutput func(stream, line string)) (stdout, stderr []byte, exitCode int, err error) {
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

	// The execution transcript, written here rather than by each Plugin so
	// that a Plugin cannot forget it and one added later is covered by
	// existing code.
	//
	// It goes into the captured buffer *and* through onOutput. Those are
	// two different destinations: the buffer becomes ExecuteResult.Output
	// and reaches the pull request comment, while onOutput feeds the live
	// log stream. Writing to only the callback would put the transcript in
	// a future live view and leave it out of every comment.
	//
	// After cmd.Start() returned nil, so a command that never started
	// reports nothing; and before the scanners run, so the transcript is
	// in the buffer ahead of anything the tool writes, without anything
	// having to sort them afterwards.
	started := time.Now()
	for _, line := range transcriptHeader(dir, name, args, version) {
		stdoutBuf.writeLine(line)
		if onOutput != nil {
			onOutput("stdout", line)
		}
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go scanLines(stdoutPipe, &stdoutBuf, "stdout", onOutput, &wg)
	go scanLines(stderrPipe, &stderrBuf, "stderr", onOutput, &wg)
	wg.Wait()

	runErr := cmd.Wait()

	// The trailer closes the record. It is written to stdout's buffer, and
	// a Plugin that appends stderr after stdout therefore places it above
	// that stderr — cosmetic, and the price of the seam writing to one
	// stream. Interleaving the two is deliberately out of this slice's
	// scope; execCommand already tags every line with its stream, so
	// whoever takes that on has what they need.
	trailer := transcriptTrailer(started, cmd.ProcessState)
	stdoutBuf.writeLine(trailer)
	if onOutput != nil {
		onOutput("stdout", trailer)
	}
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

// transcriptPrefix opens every line turnip writes into a tool's output.
//
// One prefix for all of them, rather than the tool's own name on the
// command line, because a consumer of the output has to be able to tell
// turnip's lines from the tool's without parsing them. parseChangedReleases
// is the consumer that forced this: it counts a release as changed when any
// non-empty line follows its "Comparing release=" line, so the trailer
// alone would have made the *last* release of every diff look changed.
//
// Tool output does not produce this prefix. A bare "#" would not do: helm
// renders "# Source: ..." comments into the manifests a diff prints.
const transcriptPrefix = "# turnip · "

// transcriptHeader renders the two annotation lines that open a command's
// record: where it ran and with what, then the argv itself.
//
// "#" rather than "$": in a diff-fenced block GitHub renders it as a muted
// comment, which is what an annotation subordinate to the payload should
// look like, and "$" would imply a shell line that argv does not round
// trip to. Never "+" or "-" — those belong to the payload, and an
// annotation so prefixed is counted by eye as part of the change.
//
// dir is the absolute working directory; the Runner's existing
// workspace-path stripping turns it repository-relative on the way to the
// Server, so nothing pod-internal reaches the comment.
func transcriptHeader(dir, name string, args []string, version string) []string {
	tool := name
	if version != "" {
		tool += " " + version
	}

	command := name
	if len(args) > 0 {
		command += " " + strings.Join(args, " ")
	}

	return []string{
		transcriptPrefix + dir + " · " + tool,
		transcriptPrefix + command,
	}
}

// transcriptTrailer records how the command ended. The duration answers
// "was it slow?" without opening the check run, and the exit code puts the
// reason at the bottom of the output rather than only in a mark beside it.
func transcriptTrailer(started time.Time, state *os.ProcessState) string {
	code := -1
	if state != nil {
		code = state.ExitCode()
	}
	return fmt.Sprintf("%sexit %d · %s", transcriptPrefix, code, time.Since(started).Round(time.Millisecond))
}
