//go:build unix

package app

import (
	"errors"
	"os/exec"
	"syscall"
	"time"
)

// waitExit waits for the process pid to exit, at most max.
func waitExit(pid int, max time.Duration) {
	for deadline := time.Now().Add(max); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if err := syscall.Kill(pid, 0); err != nil && !errors.Is(err, syscall.EPERM) {
			return
		}
	}
}

// detach starts cmd in a session of its own: the terminal's Ctrl+C, which
// signals the app's process group, doesn't reach it.
func detach(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }
