//go:build unix

package document

import (
	"fmt"
	"os"
	"syscall"
)

// fileLinks returns how many hard links a file has.
func fileLinks(info os.FileInfo) int {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return int(st.Nlink)
	}
	return 1
}

// openShared opens the file at path to read it, which other apps may
// rename or replace meanwhile, as everywhere on Unix.
func openShared(path string) (*os.File, error) { return os.Open(path) }

// fileWritable reports whether the user may write the file at path.
func fileWritable(path string) bool { return syscall.Access(path, 2 /* W_OK */) == nil }

// lockFile takes an exclusive lock on f, which fails while another
// process holds it, until f is closed.
func lockFile(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
}

// fileID is the file's identity: its device and inode, which a rename
// keeps.
func fileID(info os.FileInfo) string {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return fmt.Sprintf("%d:%d", st.Dev, st.Ino)
	}
	return ""
}

// openFileID is the identity of an open file, where fileID has none.
func openFileID(*os.File) string { return "" }

// pathID is the identity of the file at path, "" if there's none.
func pathID(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return ""
	}
	return fileID(info)
}

// keepMode gives the new file at name the permissions, owner and group
// of the file it replaces, as GIO does (an owner it can't give is left).
func keepMode(name string, old os.FileInfo) {
	os.Chmod(name, old.Mode().Perm())
	if st, ok := old.Sys().(*syscall.Stat_t); ok {
		os.Chown(name, int(st.Uid), int(st.Gid))
	}
}
