package plugin

import (
	"bufio"
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

// OutputLine is one line of a command's output, tagged with the stream
// ("stdout" or "stderr") it arrived on. The execution transcript's own
// lines are tagged "stdout", as they are when mirrored through onOutput.
type OutputLine struct {
	Stream string
	Text   string
}

// commandRunner abstracts subprocess execution so tests can substitute a
// fake without invoking a real tool binary.
//
// lines is the command's whole record, in the order the lines arrived:
// stdout and stderr interleaved as the tool wrote them, opened by the
// transcript header and closed by its trailer. onOutput, when non-nil, is
// called once per line in that same order, as each is produced.
type commandRunner func(ctx context.Context, dir, name string, args []string, version string, onOutput func(stream, line string)) (lines []OutputLine, exitCode int, err error)

// outputText joins every line of a record, in order, as the text a reader
// sees.
func outputText(lines []OutputLine) string {
	texts := make([]string, len(lines))
	for i, l := range lines {
		texts[i] = l.Text
	}
	return strings.Join(texts, "\n")
}

// streamText joins only the lines that arrived on stream, in order. For a
// consumer that derives a value from the tool's result and must not be
// swayed by what the tool writes on its other stream.
func streamText(lines []OutputLine, stream string) string {
	var texts []string
	for _, l := range lines {
		if l.Stream == stream {
			texts = append(texts, l.Text)
		}
	}
	return strings.Join(texts, "\n")
}

// execCommand is the default commandRunner, backed by os/exec.
//
// When the subprocess starts and runs — even to a non-zero exit — its
// record and exit code are returned with a nil error: that's a normal tool
// failure, not an execution error. err is only non-nil when the subprocess
// could not be started at all (binary not found, invalid working
// directory), in which case exitCode is -1 and there is no record.
//
// The two pipes are scanned concurrently, one goroutine each, and both feed
// a single consumer — this goroutine — which appends each line to the
// record and then hands it to onOutput. One consumer is what gives the
// comment and the live stream the same order by construction; it holds no
// lock across onOutput, whose Runner implementation may block on the
// Server.
//
// Arrival order is exact within a stream and near-exact across them: two
// lines written on different streams within the same instant can land
// either way round. A single shared pipe would be exact, and would lose
// which stream each line came from — which the change count needs.
func execCommand(ctx context.Context, dir, name string, args []string, version string, onOutput func(stream, line string)) (lines []OutputLine, exitCode int, err error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, -1, err
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, -1, err
	}

	if err := cmd.Start(); err != nil {
		return nil, -1, err
	}

	emit := func(stream, text string) {
		lines = append(lines, OutputLine{Stream: stream, Text: text})
		if onOutput != nil {
			onOutput(stream, text)
		}
	}

	// The execution transcript, written here rather than by each Plugin so
	// that a Plugin cannot forget it and one added later is covered by
	// existing code.
	//
	// It goes into the record *and* through onOutput. Those are two
	// different destinations: the record becomes ExecuteResult.Output and
	// reaches the pull request comment, while onOutput feeds the live log
	// stream. Writing to only the callback would put the transcript in a
	// future live view and leave it out of every comment.
	//
	// After cmd.Start() returned nil, so a command that never started
	// reports nothing; and before the scanners run, so the header is in the
	// record ahead of anything the tool writes.
	started := time.Now()
	for _, line := range transcriptHeader(dir, name, args, version) {
		emit("stdout", line)
	}

	scanned := make(chan OutputLine)
	var (
		wg                   sync.WaitGroup
		stdoutErr, stderrErr error
	)
	wg.Add(2)
	go func() { defer wg.Done(); stdoutErr = scanLines(stdoutPipe, "stdout", scanned) }()
	go func() { defer wg.Done(); stderrErr = scanLines(stderrPipe, "stderr", scanned) }()
	go func() { wg.Wait(); close(scanned) }()
	for line := range scanned {
		emit(line.Stream, line.Text)
	}

	// Both pipes are drained before Wait, as os/exec requires, and the
	// channel's close orders both scanners' errors before these reads.
	runErr := cmd.Wait()

	// The trailer closes the record: appended only once both streams are
	// exhausted, so no line of either can follow it.
	emit("stdout", transcriptTrailer(started, cmd.ProcessState))

	if runErr == nil {
		if scanErr := errors.Join(stdoutErr, stderrErr); scanErr != nil {
			return nil, -1, scanErr
		}
		return lines, 0, nil
	}

	if exitErr, ok := errors.AsType[*exec.ExitError](runErr); ok {
		return lines, exitErr.ExitCode(), nil
	}

	return nil, -1, runErr
}

// scanLines reads r line-by-line, sending each line to out tagged with
// stream.
func scanLines(r io.Reader, stream string, out chan<- OutputLine) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		out <- OutputLine{Stream: stream, Text: scanner.Text()}
	}
	return scanner.Err()
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
