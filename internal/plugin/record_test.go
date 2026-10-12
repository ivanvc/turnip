package plugin

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

// recordOf builds a closed Record from lines.
func recordOf(lines ...Line) *Record {
	r := NewRecord()
	for _, l := range lines {
		r.Append(l.Stream, l.Text)
	}
	r.Close()
	return r
}

func texts(lines []Line) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = l.Text
	}
	return out
}

func TestRecord_NextReturnsWhatIsThereWithoutWaiting(t *testing.T) {
	r := NewRecord()
	r.Append(Stdout, "a")
	r.Append(Stderr, "b")

	lines, done := r.Next(context.Background(), 0)
	assert.Equal(t, []string{"a", "b"}, texts(lines))
	assert.False(t, done, "the Record is still open")

	lines, done = r.Next(context.Background(), 1)
	assert.Equal(t, []string{"b"}, texts(lines))
	assert.False(t, done)
}

// A reader that fell behind gets every line it has not read, in order, and
// learns the Record is closed with the last of them.
func TestRecord_AReaderBehindGetsEveryLine(t *testing.T) {
	r := NewRecord()
	for i := range 1000 {
		r.Append(Stdout, fmt.Sprint(i))
	}
	r.Close()

	var got []string
	from := 0
	for {
		lines, done := r.Next(context.Background(), from)
		got = append(got, texts(lines)...)
		from += len(lines)
		if done {
			break
		}
	}
	require.Len(t, got, 1000)
	assert.Equal(t, "0", got[0])
	assert.Equal(t, "999", got[999])
}

func TestRecord_AWaitingReaderIsWokenByAnAppend(t *testing.T) {
	r := NewRecord()
	got := make(chan []Line, 1)
	go func() {
		lines, _ := r.Next(context.Background(), 0)
		got <- lines
	}()

	select {
	case <-got:
		require.Fail(t, "Next returned before anything was appended")
	case <-time.After(20 * time.Millisecond):
	}

	r.Append(Stderr, "late")
	select {
	case lines := <-got:
		assert.Equal(t, []string{"late"}, texts(lines))
	case <-time.After(5 * time.Second):
		require.Fail(t, "Next was not woken by the append")
	}
}

func TestRecord_AWaitingReaderIsWokenByTheClose(t *testing.T) {
	r := NewRecord()
	r.Append(Stdout, "only")

	type result struct {
		lines []Line
		done  bool
	}
	got := make(chan result, 1)
	go func() {
		lines, done := r.Next(context.Background(), 1)
		got <- result{lines, done}
	}()

	r.Close()
	select {
	case res := <-got:
		assert.Empty(t, res.lines)
		assert.True(t, res.done, "a closed Record with nothing left to read is done")
	case <-time.After(5 * time.Second):
		require.Fail(t, "Next was not woken by the close")
	}
}

func TestRecord_NextReturnsWhenItsContextEnds(t *testing.T) {
	r := NewRecord()
	ctx, cancel := context.WithCancel(context.Background())
	got := make(chan bool, 1)
	go func() {
		lines, done := r.Next(ctx, 0)
		got <- len(lines) == 0 && !done
	}()

	cancel()
	select {
	case ok := <-got:
		assert.True(t, ok, "no lines, and not done: the Record is still open")
	case <-time.After(5 * time.Second):
		require.Fail(t, "Next ignored its context")
	}
}

func TestRecord_ClosedTakesNoMoreLines(t *testing.T) {
	r := recordOf(Line{Stream: Stdout, Text: "a"})
	r.Append(Stdout, "after")
	r.Close()
	assert.Equal(t, "a", r.Text())
	assert.Equal(t, 1, r.Len())
}

func TestRecord_AppendStampsTheLine(t *testing.T) {
	before := time.Now()
	r := NewRecord()
	r.Append(Stdout, "a")
	lines, _ := r.Next(context.Background(), 0)
	require.Len(t, lines, 1)
	assert.False(t, lines[0].At.Before(before))
}

func TestRecord_StreamTextLeavesOutTurnipsLines(t *testing.T) {
	r := recordOf(
		Line{Stream: Turnip, Text: "@@ turnip: header @@"},
		Line{Stream: Stdout, Text: "out1"},
		Line{Stream: Stderr, Text: "err1"},
		Line{Stream: Stdout, Text: "out2"},
		Line{Stream: Turnip, Text: "@@ turnip: exit 0 in 1s @@"},
	)
	assert.Equal(t, "out1\nout2", r.StreamText(Stdout))
	assert.Equal(t, "err1", r.StreamText(Stderr))
}

func TestRecord_SelectPutsTheNoteAfterTheLeadingAnnotations(t *testing.T) {
	r := recordOf(
		Line{Stream: Turnip, Text: "@@ turnip: dir, tool v1 @@"},
		Line{Stream: Turnip, Text: "@@ turnip: tool diff @@"},
		Line{Stream: Stdout, Text: "out1"},
		Line{Stream: Stderr, Text: "err1"},
		Line{Stream: Stdout, Text: "out2"},
		Line{Stream: Stderr, Text: "err2"},
		Line{Stream: Turnip, Text: "@@ turnip: exit 1 in 1s @@"},
	)

	got := r.Select(Stderr, func(omitted int) string { return fmt.Sprintf("%d omitted", omitted) })
	assert.Equal(t, strings.Join([]string{
		"@@ turnip: dir, tool v1 @@",
		"@@ turnip: tool diff @@",
		"@@ turnip: 2 omitted @@",
		"err1",
		"err2",
		"@@ turnip: exit 1 in 1s @@",
	}, "\n"), got)
}

func TestRecord_SelectAddsNoLineForANoteWithoutText(t *testing.T) {
	r := recordOf(
		Line{Stream: Turnip, Text: "@@ turnip: tool diff @@"},
		Line{Stream: Stderr, Text: "err1"},
		Line{Stream: Turnip, Text: "@@ turnip: exit 1 in 1s @@"},
	)
	want := "@@ turnip: tool diff @@\nerr1\n@@ turnip: exit 1 in 1s @@"
	assert.Equal(t, want, r.Select(Stderr, func(int) string { return "" }))
	assert.Equal(t, want, r.Select(Stderr, nil))
}

// Feature: fix-output-order, Property 1: Select keeps turnip's lines and counts what it omits
func TestProperty_SelectKeepsTurnipsLinesAndCountsWhatItOmits(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		lineText := rapid.StringMatching(`[a-z @:+\-]{0,12}`)

		var all []Line
		var lead, closing, stdout, stderr []string
		for i := range rapid.IntRange(0, 3).Draw(t, "leading") {
			text := annotation(fmt.Sprintf("lead %d", i))
			all = append(all, Line{Stream: Turnip, Text: text})
			lead = append(lead, text)
		}
		for range rapid.IntRange(0, 40).Draw(t, "tool lines") {
			text := lineText.Draw(t, "text")
			if rapid.Bool().Draw(t, "stderr") {
				all = append(all, Line{Stream: Stderr, Text: text})
				stderr = append(stderr, text)
			} else {
				all = append(all, Line{Stream: Stdout, Text: text})
				stdout = append(stdout, text)
			}
		}
		// A closing line only where there is something before it: a lone
		// Turnip line is the leading run.
		if len(all) > len(lead) && rapid.Bool().Draw(t, "closed by turnip") {
			text := annotation("exit 0 in 1s")
			all = append(all, Line{Stream: Turnip, Text: text})
			closing = append(closing, text)
		}
		r := recordOf(all...)

		keep := rapid.SampledFrom([]Stream{Stdout, Stderr}).Draw(t, "keep")
		kept, left := stdout, stderr
		if keep == Stderr {
			kept, left = stderr, stdout
		}

		noted := -1
		got := r.Select(keep, func(omitted int) string {
			noted = omitted
			return "note"
		})

		require.Equal(t, len(left), noted, "the note is told how many of the tool's lines were left out")

		var want []string
		want = append(want, lead...)
		want = append(want, annotation("note"))
		want = append(want, kept...)
		want = append(want, closing...)
		require.Equal(t, strings.Join(want, "\n"), got,
			"turnip's lines, the note after the leading ones, and the kept stream in order")

		require.Equal(t, strings.Join(texts(all), "\n"), r.Text(), "Text is every line")
	})
}
