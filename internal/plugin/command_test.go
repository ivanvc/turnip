package plugin

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

// run executes a real command through execCommand and returns its record.
func run(t *testing.T, dir, name string, args []string, version string) (rec *Record, exitCode int, err error) {
	t.Helper()
	rec = NewRecord()
	exitCode, err = execCommand(context.Background(), dir, name, args, version, rec)
	return rec, exitCode, err
}

// toolLines drops turnip's own lines and the times, leaving what the tool
// itself wrote and on which stream.
func toolLines(rec *Record) []Line {
	var out []Line
	for _, l := range rec.snapshot() {
		if l.Stream != Turnip {
			out = append(out, Line{Stream: l.Stream, Text: l.Text})
		}
	}
	return out
}

func TestExecCommand_Success(t *testing.T) {
	rec, exitCode, err := run(t, "", "sh", []string{"-c", "echo hello"}, "")
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)
	assert.Equal(t, []Line{stdoutLine("hello")}, toolLines(rec))
}

func TestExecCommand_NonZeroExit(t *testing.T) {
	rec, exitCode, err := run(t, "", "sh", []string{"-c", "echo out; echo err >&2; exit 3"}, "")
	require.NoError(t, err)
	assert.Equal(t, 3, exitCode)
	assert.ElementsMatch(t, []Line{stdoutLine("out"), stderrLine("err")}, toolLines(rec))
	assert.Equal(t, "err", rec.StreamText(Stderr), "stderr carries no transcript")
	assert.Equal(t, "out", rec.StreamText(Stdout), "nor does stdout")
}

func TestExecCommand_BinaryNotFound(t *testing.T) {
	rec, exitCode, err := run(t, "", "turnip-nonexistent-binary-xyz", nil, "")
	require.Error(t, err)
	assert.Equal(t, -1, exitCode)
	assert.Zero(t, rec.Len())
}

func TestExecCommand_RespectsWorkingDir(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "marker.txt"), []byte("x"), 0o644))

	rec, exitCode, err := run(t, dir, "ls", nil, "")
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, rec.Text(), "marker.txt")
}

// The record is in the order the tool wrote, across both streams, when it
// finishes writing to one before it writes to the other: here 20,000 lines
// of standard output and then 5 of standard error, with nothing to pace
// them. The capture this replaced recorded thousands of the standard
// output lines after the first standard error line, because whatever the
// stdout pipe still held when stderr was written was merged in behind it.
//
// It ends on stderr on purpose: the trailer must still come after it.
func TestExecCommand_RecordsInWriteOrderAcrossStreams(t *testing.T) {
	rec, exitCode, err := run(t, "", "sh", []string{"-c",
		`i=0; while [ $i -lt 20000 ]; do echo "out $i"; i=$((i+1)); done; ` +
			`i=0; while [ $i -lt 5 ]; do echo "err $i" >&2; i=$((i+1)); done`,
	}, "")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)

	lines := toolLines(rec)
	require.Len(t, lines, 20005)
	for i, l := range lines[:20000] {
		require.Equal(t, stdoutLine(fmt.Sprintf("out %d", i)), l,
			"every standard output line precedes the first standard error line")
	}
	for i, l := range lines[20000:] {
		require.Equal(t, stderrLine(fmt.Sprintf("err %d", i)), l)
	}

	all := rec.snapshot()
	require.Len(t, all, 20008, "two header lines, the tool's lines, one trailer")
	assert.Equal(t, Turnip, all[0].Stream, "the provenance line opens the record")
	assert.True(t, strings.HasPrefix(all[1].Text, transcriptPrefix+"sh "), "then the command")
	assert.Equal(t, Turnip, all[len(all)-1].Stream)
	assert.True(t, strings.HasPrefix(all[len(all)-1].Text, transcriptPrefix+"exit 0 in "),
		"the trailer closes the record, after the last stderr line")
}

// Errors written right after a burst are recorded after it. This is how
// Helmfile writes (every release's diff in one burst, then its errors), and
// what a reader per pipe got wrong in every run: the error was recorded
// while a pipe's worth of the burst was still unread, and landed inside it.
//
// Each error follows its burst with no pause: that order is the reader's
// rule, not timing, and is what this test is strict about. A burst that
// follows an error is given a moment, because an error and the start of a
// burst written before either is read can land either way round.
//
// Run repeatedly because the fault is a race; one pass proves little.
func TestExecCommand_ErrorsAfterABurstFollowIt(t *testing.T) {
	for range 50 {
		rec, exitCode, err := run(t, "", "sh", []string{"-c",
			"echo before >&2; sleep 0.02; seq 1 20000; echo after >&2; sleep 0.02; seq 20001 40000; echo last >&2",
		}, "")
		require.NoError(t, err)
		require.Equal(t, 0, exitCode)

		lines := toolLines(rec)
		require.Len(t, lines, 40003)
		require.Equal(t, stderrLine("before"), lines[0])
		require.Equal(t, stdoutLine("1"), lines[1])
		require.Equal(t, stdoutLine("20000"), lines[20000])
		require.Equal(t, stderrLine("after"), lines[20001], "after every line of the first burst, before any of the second")
		require.Equal(t, stdoutLine("20001"), lines[20002])
		require.Equal(t, stdoutLine("40000"), lines[40001])
		require.Equal(t, stderrLine("last"), lines[40002])
	}
}

// A warning written between two lines of standard output is recorded
// between them, not at the end.
//
// The writes are a moment apart. With no pause the tool has written to both
// pipes before either is read, nothing says which line was written first,
// and they can land either way round in any capture that keeps the streams
// apart.
func TestExecCommand_AWarningLandsWhereItWasWritten(t *testing.T) {
	rec, exitCode, err := run(t, "", "sh", []string{"-c",
		"echo a; sleep 0.05; echo warning >&2; sleep 0.05; echo c; sleep 0.05; echo again >&2",
	}, "")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)

	assert.Equal(t, []Line{
		stdoutLine("a"), stderrLine("warning"), stdoutLine("c"), stderrLine("again"),
	}, toolLines(rec))
}

// Written with no pause, the lines of each stream still keep their own
// order, whichever way the two streams end up interleaved.
func TestExecCommand_EachStreamKeepsItsOrderWithoutAPause(t *testing.T) {
	rec, _, err := run(t, "", "sh", []string{"-c",
		"i=0; while [ $i -lt 200 ]; do echo out$i; echo err$i >&2; i=$((i+1)); done",
	}, "")
	require.NoError(t, err)

	var wantOut, wantErr []string
	for i := range 200 {
		wantOut = append(wantOut, fmt.Sprintf("out%d", i))
		wantErr = append(wantErr, fmt.Sprintf("err%d", i))
	}
	assert.Equal(t, strings.Join(wantOut, "\n"), rec.StreamText(Stdout))
	assert.Equal(t, strings.Join(wantErr, "\n"), rec.StreamText(Stderr))
}

// Whoever follows the record sees each line while the command is still
// running, not once it has ended.
func TestExecCommand_TheRecordCanBeFollowedWhileTheCommandRuns(t *testing.T) {
	dir := t.TempDir()
	gate := filepath.Join(dir, "gate")

	rec := NewRecord()
	ended := make(chan error, 1)
	go func() {
		// Prints, then waits for the test to create the gate file.
		_, err := execCommand(context.Background(), dir, "sh", []string{"-c",
			"echo first; while [ ! -e gate ]; do sleep 0.01; done; echo second",
		}, "", rec)
		ended <- err
	}()

	var seen []string
	from := 0
	for !strings.Contains(strings.Join(seen, "\n"), "first") {
		lines, done := rec.Next(context.Background(), from)
		require.False(t, done, "the command cannot have ended: it is waiting for the gate")
		from += len(lines)
		seen = append(seen, texts(lines)...)
	}

	require.NoError(t, os.WriteFile(gate, nil, 0o600))
	require.NoError(t, <-ended)

	rest, done := rec.Next(context.Background(), from)
	assert.True(t, done, "the capture closes the record when the command ends")
	assert.Contains(t, texts(rest), "second")
}

// The transcript is part of the record, as the Turnip stream, so it is in
// every output a Plugin composes and in everything that follows the record.
//
// A fake commandRunner cannot prove this, which is why it runs a real
// process.
func TestExecCommand_TranscriptIsTheRecordsTurnipStream(t *testing.T) {
	rec, exitCode, err := run(t, "/tmp", "sh", []string{"-c", "echo payload"}, "v1.2.3")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)

	lines := rec.snapshot()
	require.Len(t, lines, 4)
	assert.Equal(t, Turnip, lines[0].Stream)
	assert.Equal(t, "@@ turnip: /tmp, sh v1.2.3 @@", lines[0].Text, "the provenance line")
	assert.Equal(t, Turnip, lines[1].Stream)
	assert.Equal(t, "@@ turnip: sh -c echo payload @@", lines[1].Text, "the resolved command")
	assert.Equal(t, Stdout, lines[2].Stream)
	assert.Equal(t, "payload", lines[2].Text, "the tool's own output")
	assert.Equal(t, Turnip, lines[3].Stream)
	assert.Regexp(t, `^@@ turnip: exit 0 in \S+ @@$`, lines[3].Text, "the trailer")
}

// A command that never started must not report a transcript for something
// that never ran.
func TestExecCommand_NoTranscriptWhenTheCommandNeverStarts(t *testing.T) {
	rec, _, err := run(t, "", "turnip-nonexistent-binary-xyz", nil, "v1")
	require.Error(t, err)
	assert.Zero(t, rec.Len())
}

func TestExecCommand_ALastLineWithoutANewlineIsRecorded(t *testing.T) {
	rec, _, err := run(t, "", "sh", []string{"-c", "printf 'whole\\npartial'"}, "")
	require.NoError(t, err)
	assert.Equal(t, []Line{stdoutLine("whole"), stdoutLine("partial")}, toolLines(rec))
}

// A line too long to record fails the capture, and the tool is still read
// to its end rather than left blocked on a pipe nobody empties.
func TestExecCommand_ALineOverTheLimitFailsTheCapture(t *testing.T) {
	_, exitCode, err := run(t, "", "sh", []string{"-c",
		fmt.Sprintf("head -c %d /dev/zero | tr '\\0' x; echo; echo after", maxLineBytes+1),
	}, "")
	require.ErrorIs(t, err, bufio.ErrTooLong)
	assert.Equal(t, -1, exitCode)
}

func TestLineSplitter_DropsACarriageReturn(t *testing.T) {
	rec := NewRecord()
	s := &lineSplitter{stream: Stdout, rec: rec}
	s.feed([]byte("a\r\nb\n"))
	s.flush()
	assert.Equal(t, "a\nb", rec.Text())
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

// wantLines is what a stream's content holds, by the rule a line ends at
// its newline and a last line needs none.
func wantLines(content string) []string {
	if content == "" {
		return nil
	}
	lines := strings.Split(content, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// Feature: fix-output-order, Property 2: Each stream is recorded in order whatever the chunking
func TestProperty_EachStreamIsRecordedInOrderWhateverTheChunking(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		content := func(label string) string {
			lines := rapid.SliceOfN(rapid.StringMatching(`[a-z ]{0,30}`), 0, 30).Draw(t, label+" lines")
			text := strings.Join(lines, "\n")
			if len(lines) > 0 && rapid.Bool().Draw(t, label+" ends with a newline") {
				text += "\n"
			}
			return text
		}
		stdout, stderr := content("stdout"), content("stderr")

		// What a reader is handed is whatever a pipe held: pieces of any
		// size, of either stream, in any order.
		rec := NewRecord()
		splitters := [2]*lineSplitter{{stream: Stdout, rec: rec}, {stream: Stderr, rec: rec}}
		rest := [2][]byte{[]byte(stdout), []byte(stderr)}
		for len(rest[0]) > 0 || len(rest[1]) > 0 {
			i := rapid.IntRange(0, 1).Draw(t, "stream")
			if len(rest[i]) == 0 {
				i = 1 - i
			}
			n := min(rapid.IntRange(1, 40).Draw(t, "chunk size"), len(rest[i]))
			splitters[i].feed(rest[i][:n])
			rest[i] = rest[i][n:]
		}
		splitters[0].flush()
		splitters[1].flush()
		rec.Close()

		var gotOut, gotErr []string
		for _, l := range rec.snapshot() {
			if l.Stream == Stdout {
				gotOut = append(gotOut, l.Text)
			} else {
				gotErr = append(gotErr, l.Text)
			}
		}
		require.Equal(t, wantLines(stdout), gotOut, "standard output's lines, whole and in order")
		require.Equal(t, wantLines(stderr), gotErr, "standard error's lines, whole and in order")
	})
}
