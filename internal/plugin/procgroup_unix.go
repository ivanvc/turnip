//go:build unix

package plugin

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// startProcessGroup makes cmd start in a process group of its own, whose
// id is the tool's process id, so a signal can reach the tool and every
// process it started.
func startProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// signalGroup sends sig to the process group process leads. A group that
// no longer exists is reported as os.ErrProcessDone, which exec.Cmd.Cancel
// takes to mean the tool had already exited.
func signalGroup(process *os.Process, sig syscall.Signal) error {
	if process == nil {
		return os.ErrProcessDone
	}
	err := syscall.Kill(-process.Pid, sig)
	if errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	}
	return err
}
