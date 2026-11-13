//go:build !unix && !windows

package app

import (
	"os/exec"
	"time"
)

// A browser's page doesn't relaunch itself (boot.Quit is nil there).
func waitExit(pid int, max time.Duration) {}
func detach(cmd *exec.Cmd)                {}
