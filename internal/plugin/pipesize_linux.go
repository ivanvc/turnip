package plugin

import (
	"os"

	"golang.org/x/sys/unix"
)

// shrinkPipe sets the pipe w writes to to the smallest size the kernel
// allows, one page. Best effort: a pipe that keeps its size only loosens
// how close to its place a line lands (see readStreams).
func shrinkPipe(w *os.File) {
	_, _ = unix.FcntlInt(w.Fd(), unix.F_SETPIPE_SZ, os.Getpagesize())
}
