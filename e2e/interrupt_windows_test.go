//go:build e2e && windows

package e2e

import (
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// interruptible starts c's process in a process group of its own, which
// interrupt's console event reaches (and not this one).
func interruptible(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
}

// interrupt sends p's process group what Ctrl-Break does, which Go
// delivers as os.Interrupt: Windows has no signal to send a process.
func interrupt(p *os.Process) error {
	return windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, uint32(p.Pid))
}
