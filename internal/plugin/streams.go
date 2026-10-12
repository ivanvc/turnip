//go:build unix

package plugin

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// readBufferBytes is at least what a pipe holds, so one read takes
// everything in it.
const readBufferBytes = 64 * 1024

// readStreams reads the tool's two pipes to their end, recording their
// lines in rec, and closes both. It records nothing until header is closed,
// though it reads, and so settles the order of what it read, before that. It returns bufio.ErrTooLong when a line
// of either stream was too long to record.
//
// One loop reads both pipes, rather than a reader each. Two readers cannot
// agree on an order: when the tool writes a burst to one stream and then a
// line to the other, the second reader records its line at once while the
// first still has a pipe's worth to work through, and the line lands in
// the middle of the burst. That is how Helmfile writes: every release's
// diff in one burst, then its errors.
//
// Each round waits until either pipe has data and takes what a pipe holds
// in one read. When both have data, the stream taken last round goes
// first: that is the one this loop is behind on, so what its pipe still
// holds was written before the other stream's line. When neither was being
// read, or the tool wrote to both within the time one read takes, the
// order of those lines is not known and standard output goes first.
func readStreams(rec *Record, stdout, stderr *os.File, header <-chan struct{}) error {
	files := [2]*os.File{stdout, stderr}
	// Fd puts the descriptors in blocking mode, out of the runtime's
	// poller, which is what lets this loop poll them itself.
	fds := [2]int{int(stdout.Fd()), int(stderr.Fd())}
	splitters := [2]*lineSplitter{{stream: Stdout, rec: rec}, {stream: Stderr, rec: rec}}
	open := [2]bool{true, true}
	defer func() {
		for i, f := range files {
			if open[i] {
				_ = f.Close()
			}
		}
	}()

	buf := make([]byte, readBufferBytes)
	last := 0
	for open[0] || open[1] {
		var ready [2]bool
		pollFds := make([]unix.PollFd, 0, 2)
		which := make([]int, 0, 2)
		for i := range fds {
			if open[i] {
				pollFds = append(pollFds, unix.PollFd{Fd: int32(fds[i]), Events: unix.POLLIN})
				which = append(which, i)
			}
		}
		if _, err := unix.Poll(pollFds, -1); err != nil {
			if errors.Is(err, unix.EINTR) {
				continue
			}
			return err
		}
		for k, p := range pollFds {
			ready[which[k]] = p.Revents != 0
		}

		for _, i := range [2]int{last, 1 - last} {
			if !ready[i] {
				continue
			}
			n, err := unix.Read(fds[i], buf)
			if errors.Is(err, unix.EINTR) || errors.Is(err, unix.EAGAIN) {
				continue
			}
			if n > 0 {
				if header != nil {
					<-header
					header = nil
				}
				splitters[i].feed(buf[:n])
				last = i
				continue
			}
			// The end of the stream, or a read error, which ends it too.
			splitters[i].flush()
			_ = files[i].Close()
			open[i] = false
		}
	}
	return errors.Join(splitters[0].err, splitters[1].err)
}
