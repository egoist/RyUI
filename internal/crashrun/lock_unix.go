//go:build unix

package crashrun

import (
	"os"
	"syscall"
)

// lock holds an exclusive lock on f while the process lives: the run's
// mark that it hasn't ended.
func lock(f *os.File) { syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) }

// Alive reports whether the run whose crash output is path still runs:
// its process holds the file's lock.
func Alive(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return err == syscall.EWOULDBLOCK
	}
	syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return false
}

// redirectStderr: on Unix the runtime's standard error is the process's,
// and its crash output (SetCrashOutput) has all it prints of a crash.
func redirectStderr(path string) {}

// Lock holds an exclusive lock on path (created if need be) until unlock:
// one process at a time reads the directory into reports.
func Lock(path string) (unlock func()) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return func() {}
	}
	syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
	return func() {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}
}
