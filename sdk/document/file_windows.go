//go:build windows

package document

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"syscall"
	"time"
	"unsafe"
)

func fileLinks(os.FileInfo) int { return 1 }

// openShared opens the file at path to read it, sharing its deletion as
// Notepad and VS Code do: os.Open shares reading and writing only, so
// while the app read a document, another app deleting it, or replacing it
// as editors save, was refused.
func openShared(path string) (*os.File, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	h, err := syscall.CreateFile(p, syscall.GENERIC_READ, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE, nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	return os.NewFile(uintptr(h), path), nil
}

// fileWritable reports whether the file at path lacks the read-only
// attribute, as Notepad checks it.
func fileWritable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().Perm()&0o200 != 0
}

var (
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	procLockFileEx   = kernel32.NewProc("LockFileEx")
	procReplaceFileW = kernel32.NewProc("ReplaceFileW")
)

// lockFile takes an exclusive lock on f, which fails while another
// process holds it, until f is closed.
func lockFile(f *os.File) error {
	const lockfileFailImmediately, lockfileExclusiveLock = 1, 2
	var ol syscall.Overlapped
	r, _, err := procLockFileEx.Call(f.Fd(), lockfileFailImmediately|lockfileExclusiveLock, 0, 1, 0, uintptr(unsafe.Pointer(&ol)))
	if r == 0 {
		return err
	}
	return nil
}

// fileID: a FileInfo of Windows' carries no file index (openFileID and
// pathID read it).
func fileID(os.FileInfo) string { return "" }

// openFileID is the open file's volume and file index, which a rename
// keeps.
func openFileID(f *os.File) string {
	var d syscall.ByHandleFileInformation
	if syscall.GetFileInformationByHandle(syscall.Handle(f.Fd()), &d) != nil {
		return ""
	}
	return fmt.Sprintf("%x:%x:%x", d.VolumeSerialNumber, d.FileIndexHigh, d.FileIndexLow)
}

// pathID is the identity of the file at path, "" if there's none.
func pathID(path string) string {
	f, err := openShared(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	return openFileID(f)
}

func keepMode(string, os.FileInfo) {}

// replaceFile puts the new file at name in path's place with ReplaceFileW,
// which keeps the old file's security descriptor, attributes, alternate
// data streams and creation time, as Office and Notepad save; a file
// that isn't there yet is the new one renamed.
func replaceFile(path, name string) error {
	if _, err := os.Lstat(path); errors.Is(err, fs.ErrNotExist) {
		return os.Rename(name, path)
	}
	to, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	from, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return err
	}
	const replacefileIgnoreMergeErrors = 2
	var last error
	for range 5 { // another program may hold it open a moment
		r, _, err := procReplaceFileW.Call(uintptr(unsafe.Pointer(to)), uintptr(unsafe.Pointer(from)), 0, replacefileIgnoreMergeErrors, 0, 0)
		if r != 0 {
			return nil
		}
		last = err
		if _, serr := os.Lstat(path); errors.Is(serr, fs.ErrNotExist) {
			// ERROR_UNABLE_TO_MOVE_REPLACEMENT: the old file is gone, the
			// new one still here.
			return os.Rename(name, path)
		}
		time.Sleep(50 * time.Millisecond)
	}
	return &os.LinkError{Op: "replace", Old: name, New: path, Err: last}
}

// movedTo finds where the file of identity id went: in its folder, as
// another app renames it there.
func movedTo(dir, id string) string { return movedInDir(dir, id) }
