package app

import (
	"os/exec"
	"syscall"
	"time"
)

// waitExit waits for the process pid to exit, at most max.
func waitExit(pid int, max time.Duration) {
	const synchronize = 0x00100000
	h, err := syscall.OpenProcess(synchronize, false, uint32(pid))
	if err != nil {
		return // gone already
	}
	defer syscall.CloseHandle(h)
	syscall.WaitForSingleObject(h, uint32(max/time.Millisecond))
}

// detach starts cmd apart from this process's console, if it has one.
func detach(cmd *exec.Cmd) {
	const createNewProcessGroup = 0x00000200
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewProcessGroup}
}
