package plugin

import (
	"context"
	"strings"
	"sync"
	"time"
)

// Stream says where a line of a Record came from.
type Stream int

const (
	// Stdout and Stderr are the tool's own streams.
	Stdout Stream = iota
	Stderr
	// Turnip marks the annotation lines turnip writes around the tool's
	// output. They are a stream of their own so that selecting by stream
	// keeps them without reading their text, and so that nothing reading
	// the tool's standard output has to skip them.
	Turnip
)

// Line is one line of a Record.
type Line struct {
	Stream Stream
	Text   string
	// At is when the line was recorded, which is what a log line sent to
	// the Server later carries as its timestamp.
	At time.Time
}

// Record is everything one command wrote, in the order it was read:
// append-only while the command runs, closed once it has ended. It is safe
// to read while it grows, and a reader follows it with Next at its own
// pace: nothing a reader does can make an Append wait, which is what keeps
// the order of the two streams (see execCommand).
type Record struct {
	mu     sync.Mutex
	lines  []Line
	closed bool
	// wake is closed, and replaced, by the first Append or Close after a
	// reader started waiting on it.
	wake    chan struct{}
	waiting bool
}

// NewRecord returns an empty, open Record.
func NewRecord() *Record {
	return &Record{wake: make(chan struct{})}
}

// Append adds one line, recorded now. It holds the Record's lock for the
// append alone and never calls out. Appending to a closed Record does
// nothing.
func (r *Record) Append(stream Stream, text string) {
	r.appendAt(stream, text, time.Now())
}

func (r *Record) appendAt(stream Stream, text string, at time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	r.lines = append(r.lines, Line{Stream: stream, Text: text, At: at})
	r.wakeLocked()
}

// Close marks the Record complete: no line follows. Closing twice is the
// same as closing once.
func (r *Record) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	r.closed = true
	r.wakeLocked()
}

func (r *Record) wakeLocked() {
	if r.waiting {
		close(r.wake)
		r.wake = make(chan struct{})
		r.waiting = false
	}
}

// Next returns the lines after position from, waiting until there is at
// least one, the Record is closed, or ctx ends. done is true when the
// Record is closed and the lines returned are its last, so a reader that
// advances from by len(lines) each call has read everything once done is
// true. When ctx ends first it returns no lines and done false.
func (r *Record) Next(ctx context.Context, from int) (lines []Line, done bool) {
	for {
		r.mu.Lock()
		if n := len(r.lines); from < n || r.closed {
			if from < n {
				// Capped, so a caller's append cannot write into
				// the Record's own array.
				lines = r.lines[from:n:n]
			}
			done = r.closed
			r.mu.Unlock()
			return lines, done
		}
		wake := r.wake
		r.waiting = true
		r.mu.Unlock()

		select {
		case <-wake:
		case <-ctx.Done():
			return nil, false
		}
	}
}

// Len is the number of lines recorded so far.
func (r *Record) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.lines)
}

// snapshot returns the lines recorded so far. The Record never changes a
// line once appended, so the slice is safe to read.
func (r *Record) snapshot() []Line {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lines[:len(r.lines):len(r.lines)]
}

// Text is the whole Record as the text a reader sees: every line, in
// order.
func (r *Record) Text() string {
	lines := r.snapshot()
	texts := make([]string, len(lines))
	for i, l := range lines {
		texts[i] = l.Text
	}
	return strings.Join(texts, "\n")
}

// StreamText is the lines of one stream alone, in order. For a consumer
// that derives a value from the tool's result and must not be swayed by
// what the tool writes on its other stream, or by turnip's own lines.
func (r *Record) StreamText(stream Stream) string {
	var texts []string
	for _, l := range r.snapshot() {
		if l.Stream == stream {
			texts = append(texts, l.Text)
		}
	}
	return strings.Join(texts, "\n")
}

// Select is the Record with only the lines of keep, between turnip's own:
// the leading Turnip lines, then note(omitted) as an annotation line when
// it returns text, then the lines of keep in order, then the closing
// Turnip line. omitted is the number of the tool's lines left out. A nil
// note adds no line.
//
// Text and Select are the two ways a Plugin turns a Record into its
// Operation's output, and both keep turnip's lines, so no Plugin can
// leave them out. Where the note goes is fixed here, right after the
// leading annotation lines, because that is where the Runner's cut and the
// comment's cap both keep a line; what it says is the Plugin's.
func (r *Record) Select(keep Stream, note func(omitted int) string) string {
	lines := r.snapshot()

	lead := 0
	for lead < len(lines) && lines[lead].Stream == Turnip {
		lead++
	}

	var kept []string
	omitted := 0
	for _, l := range lines[lead:] {
		switch l.Stream {
		case keep, Turnip:
			kept = append(kept, l.Text)
		default:
			omitted++
		}
	}

	texts := make([]string, 0, lead+1+len(kept))
	for _, l := range lines[:lead] {
		texts = append(texts, l.Text)
	}
	if note != nil {
		if text := note(omitted); text != "" {
			texts = append(texts, annotation(text))
		}
	}
	texts = append(texts, kept...)
	return strings.Join(texts, "\n")
}
