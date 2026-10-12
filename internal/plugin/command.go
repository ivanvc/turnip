package plugin

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// commandRunner abstracts subprocess execution so tests can substitute a
// fake without invoking a real tool binary.
//
// It appends the command's whole record to rec, in the order the lines were
// read: stdout and stderr interleaved as the tool wrote them, opened by the
// transcript header and closed by its trailer, after which it closes rec.
// Whoever follows rec while the command runs sees each line as it is
// recorded.
type commandRunner func(ctx context.Context, dir, name string, args []string, version string, rec *Record) (exitCode int, err error)

// maxLineBytes is the longest line the capture records; a longer one fails
// the command's capture with bufio.ErrTooLong.
const maxLineBytes = 1024 * 1024

// execCommand is the default commandRunner, backed by os/exec.
//
// When the subprocess starts and runs — even to a non-zero exit — its
// exit code is returned with a nil error: that's a normal tool failure,
// not an execution error. err is only non-nil when the subprocess could
// not be started at all (binary not found, invalid working directory), in
// which case exitCode is -1 and rec is left empty.
//
// The tool writes to two pipes this function creates, one per stream, and
// one reader takes from both (see readStreams). The only thing that
// reader does with a line is append it to rec, which takes rec's lock for
// the append and calls nothing: it never waits on whoever consumes the
// output (the Runner's log, the Server), so a slow consumer cannot change
// the order lines are recorded in.
//
// The order is exact within a stream. Across the two it is the order the
// tool wrote in when it writes to one stream and then to the other,
// however much it wrote to the first; lines written to both at the same
// moment, or within the time it takes to read one, can land either way
// round. A single shared pipe would be exact there too, and would lose
// which stream each line came from, which a Plugin's output and its
// change count may both need. Which of the two suits a tool is its
// Plugin's choice; this is the capture that keeps the streams apart.
func execCommand(ctx context.Context, dir, name string, args []string, version string, rec *Record) (exitCode int, err error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir

	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		return -1, err
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		_ = stdoutR.Close()
		_ = stdoutW.Close()
		return -1, err
	}
	// Small pipes: the tool can then never be more than a page ahead of
	// the reader on either stream, which bounds how far a line can land
	// from where it was written when both streams have data at once.
	shrinkPipe(stdoutW)
	shrinkPipe(stderrW)
	cmd.Stdout, cmd.Stderr = stdoutW, stderrW

	// The reader is already waiting on the pipes when the tool starts. It
	// decides the order of what it reads the moment it reads it, so
	// starting it only after Start returned would leave the tool's first
	// lines, on both streams, sitting in the pipes together with nothing
	// to say which came first. It holds the first of them until header
	// is closed, so the transcript's header still opens the record.
	header := make(chan struct{})
	read := make(chan error, 1)
	go func() { read <- readStreams(rec, stdoutR, stderrR, header) }()

	err = cmd.Start()
	// The child holds its own copies of the write ends; ours must go, or
	// the pipes never report their end.
	_ = stdoutW.Close()
	_ = stderrW.Close()
	if err != nil {
		// Nothing was written, so the reader finds both pipes at their end.
		close(header)
		<-read
		return -1, err
	}
	defer rec.Close()

	// The execution transcript, written here rather than by each Plugin so
	// that a Plugin cannot forget it and one added later is covered by
	// existing code. Its lines are the Turnip stream of the record, so
	// they reach everything that follows the record (the Runner's log, the
	// live log stream) and every output a Plugin composes from it.
	//
	// After cmd.Start() returned nil, so a command that never started
	// reports nothing; and before the reader records anything, so the
	// header is in the record ahead of anything the tool writes.
	started := time.Now()
	for _, line := range transcriptHeader(dir, name, args, version) {
		rec.Append(Turnip, line)
	}
	close(header)

	readErr := <-read

	// Both pipes are read to their end before Wait.
	runErr := cmd.Wait()

	// The trailer closes the record: appended only once both streams are
	// exhausted, so no line of either can follow it.
	rec.Append(Turnip, transcriptTrailer(started, cmd.ProcessState))

	if runErr == nil {
		if readErr != nil {
			return -1, readErr
		}
		return 0, nil
	}

	if exitErr, ok := errors.AsType[*exec.ExitError](runErr); ok {
		return exitErr.ExitCode(), nil
	}

	return -1, runErr
}

// lineSplitter turns the bytes read from one stream into that stream's
// lines in rec. A line is recorded when its newline arrives, without the
// newline or a carriage return before it; flush records a last line that
// had none.
type lineSplitter struct {
	stream  Stream
	rec     *Record
	partial []byte
	// err is set once a line exceeds maxLineBytes. Nothing more of the
	// stream is recorded after that, though it is still read, so the tool
	// cannot block on a pipe nobody empties.
	err error
}

func (s *lineSplitter) feed(data []byte) {
	if s.err != nil {
		return
	}
	for len(data) > 0 {
		i := bytes.IndexByte(data, '\n')
		if i < 0 {
			s.partial = append(s.partial, data...)
			break
		}
		line := data[:i]
		if len(s.partial) > 0 {
			s.partial = append(s.partial, line...)
			line = s.partial
		}
		if len(line) >= maxLineBytes {
			break
		}
		s.rec.Append(s.stream, string(dropCR(line)))
		s.partial = s.partial[:0]
		data = data[i+1:]
	}
	if len(s.partial) >= maxLineBytes {
		s.err = bufio.ErrTooLong
		s.partial = nil
	}
}

func (s *lineSplitter) flush() {
	if s.err == nil && len(s.partial) > 0 {
		s.rec.Append(s.stream, string(dropCR(s.partial)))
	}
	s.partial = nil
}

// dropCR removes one trailing carriage return, as bufio.ScanLines does.
func dropCR(line []byte) []byte {
	if n := len(line); n > 0 && line[n-1] == '\r' {
		return line[:n-1]
	}
	return line
}

// transcriptPrefix and transcriptSuffix enclose every line turnip writes
// into a tool's output: `@@ turnip: <text> @@`.
//
// In a diff fence GitHub renders a line of that shape as a hunk header —
// highlighted, unlike anything the tool prints. "#" was used before and
// was the wrong choice: it is the tools' own annotation syntax
// (Terraform's "# aws_instance.web will be created", Helm's "# Source:"),
// so turnip's lines looked like the tool's. Never "+" or "-" — those belong
// to the payload, and an annotation so prefixed is counted by eye as part
// of the change. Not "$" either: it would imply a shell line that argv does
// not round-trip to.
//
// One prefix for all of them, because a consumer of the output has to be
// able to tell turnip's lines from the tool's without parsing them.
// parseChangedReleases is the consumer that forced this: it counts a
// release as changed when any non-empty line follows its
// "Comparing release=" line, so the trailer alone would have made the
// *last* release of every diff look changed. "turnip:" inside the markers
// keeps a real hunk header from some future tool from matching.
const (
	transcriptPrefix = "@@ turnip: "
	transcriptSuffix = " @@"
)

func annotation(text string) string {
	return transcriptPrefix + text + transcriptSuffix
}

// transcriptHeader renders the two annotation lines that open a command's
// record: where it ran and with what, then the argv itself.
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
		annotation(dir + ", " + tool),
		annotation(command),
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
	return annotation(fmt.Sprintf("exit %d in %s", code, time.Since(started).Round(time.Millisecond)))
}
