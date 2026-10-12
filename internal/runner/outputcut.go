package runner

import (
	"fmt"
	"math"
	"strings"
	"unicode/utf8"
)

// maxSentOutputBytes is the most output a result carries: 1 MiB, well
// inside the Server's rpc.MaxResultMessageBytes with room for plan data
// and the rest of the message. What the live log stream carries is never
// cut; only the result's copy is.
const maxSentOutputBytes = 1 << 20

const (
	annotationPrefix = "@@ turnip: "
	annotationSuffix = " @@"

	// omittedFormat is the line cutOutput puts after the leading
	// annotation lines: the count of tool lines removed, then their size.
	omittedFormat = annotationPrefix + "%d earlier lines omitted (%s)" + annotationSuffix + "\n"

	// maxOmittedLen bounds the omission line's length whatever its
	// numbers turn out to be: 20 digits for the count, 24 bytes for the
	// size. cutOutput reserves it before it knows either.
	maxOmittedLen = len(annotationPrefix+" earlier lines omitted ()"+annotationSuffix+"\n") + 20 + 24
)

// isAnnotation reports whether line is one of turnip's own transcript
// lines rather than the tool's.
func isAnnotation(line string) bool {
	return strings.HasPrefix(line, annotationPrefix) && strings.HasSuffix(line, annotationSuffix)
}

// leadingAnnotationEnd returns the offset just past the run of whole
// annotation lines that opens output.
func leadingAnnotationEnd(output string) int {
	n := 0
	for n < len(output) {
		line, next := output[n:], len(output)
		if i := strings.IndexByte(output[n:], '\n'); i >= 0 {
			line, next = output[n:n+i], n+i+1
		}
		if !isAnnotation(line) {
			break
		}
		n = next
	}
	return n
}

// closingAnnotationStart returns the offset of output's last line when
// that line is an annotation starting at or after from, and len(output)
// otherwise. A single trailing newline belongs to the last line.
func closingAnnotationStart(output string, from int) int {
	body := strings.TrimSuffix(output, "\n")
	start := strings.LastIndexByte(body, '\n') + 1
	if start < from || !isAnnotation(body[start:]) {
		return len(output)
	}
	return start
}

// cutOutput returns output unchanged when it is at most maxSentOutputBytes,
// and otherwise a version of it that is: the leading annotation lines and
// the closing one kept, tool lines removed from the start until the rest
// fits, and an omission line put right after the leading annotation lines
// saying how many lines were removed and how large they were.
//
// A tool line longer than everything left for the tool's output keeps its
// own end, cut inside the line at a rune boundary, so the result is still
// valid UTF-8 for the protobuf string it travels in.
func cutOutput(output string) string {
	return cutOutputTo(output, maxSentOutputBytes)
}

// cutOutputTo is cutOutput with the limit as a parameter, so the
// property test can explore limits that small inputs reach. limit must be
// at least twice maxOmittedLen.
func cutOutputTo(output string, limit int) string {
	if len(output) <= limit {
		return output
	}

	lead := leadingAnnotationEnd(output)
	closing := closingAnnotationStart(output, lead)

	// Annotation lines are kept while they take at most half the limit,
	// which a transcript's few lines never come near. Past that they are
	// cut like the tool's lines, so the end of the output still arrives.
	if lead+(len(output)-closing)+maxOmittedLen > limit/2 {
		lead = 0
		closing = closingAnnotationStart(output, 0)
	}
	if (len(output)-closing)+maxOmittedLen > limit/2 {
		closing = len(output)
	}

	middle := output[lead:closing]
	keep := max(limit-lead-(len(output)-closing)-maxOmittedLen, 0)

	cut := len(middle) - keep
	if cut > 0 && middle[cut-1] != '\n' {
		// Advance to the start of the next whole line, unless that would
		// leave none of the tool's output: then the last line is longer
		// than the budget and keeps its own end.
		if i := strings.IndexByte(middle[cut:], '\n'); i >= 0 && cut+i+1 < len(middle) {
			cut += i + 1
		}
	}
	for cut < len(middle) && !utf8.RuneStart(middle[cut]) {
		cut++
	}

	omitted := fmt.Sprintf(omittedFormat, strings.Count(middle[:cut], "\n"), formatSize(cut))
	return output[:lead] + omitted + middle[cut:] + output[closing:]
}

// formatSize renders n bytes with one decimal, in KiB below 1 MiB and in
// MiB from 1 MiB. The unit follows the rounded value, so a size a few
// bytes short of 1 MiB reads "1.0 MiB", never "1024.0 KiB".
func formatSize(n int) string {
	if kib := math.Round(float64(n)/(1<<10)*10) / 10; kib < 1024 {
		return fmt.Sprintf("%.1f KiB", kib)
	}
	return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
}
