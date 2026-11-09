//go:build !darwin

package main

import (
	"errors"
	"os"
)

// spawnOwn is macOS's (see spawn_darwin.go).
func spawnOwn(path, dir string, env []string) (*os.Process, error) {
	return nil, errors.New("ryui: a process of its own is macOS's")
}
