//go:build e2e && !windows

package e2e

import (
	"os"
	"os/exec"
)

// interruptible makes c's process one interrupt reaches (interrupt).
func interruptible(c *exec.Cmd) {}

// interrupt sends p what Ctrl-C does.
func interrupt(p *os.Process) error { return p.Signal(os.Interrupt) }
