package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// lockShared holds dir (a host's sources and build, shared by every app
// and every checkout of ryui on the machine) for one build at a time:
// another ryui building there (an app in another terminal, another
// worktree) waits, rather than writing its sources under this one's
// Gradle or swiftc. unlock lets the next one in; a process that dies
// lets go of it too.
func lockShared(dir string) (unlock func(), err error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, ".ryui-lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if ok, err := tryLockFile(f); err != nil {
		f.Close()
		return nil, err
	} else if !ok {
		fmt.Printf("ryui: waiting for another build in %s\n", dir)
		start := time.Now()
		if err := lockFile(f); err != nil {
			f.Close()
			return nil, err
		}
		fmt.Printf("ryui: waited %s\n", time.Since(start).Round(time.Second))
	}
	return func() {
		unlockFile(f)
		f.Close()
	}, nil
}
