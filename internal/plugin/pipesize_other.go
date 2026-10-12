//go:build unix && !linux

package plugin

import "os"

// shrinkPipe does nothing where a pipe's size cannot be set; the pipe
// keeps the system's size.
func shrinkPipe(*os.File) {}
