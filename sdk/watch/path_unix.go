//go:build !windows

package watch

import (
	"errors"
	"io/fs"
	"os"
)

func openRoot(path string) (*os.Root, error) { return os.OpenRoot(path) }

// inaccessible reports whether err says a file can't be looked
// at.
func inaccessible(err error) bool { return errors.Is(err, fs.ErrPermission) }

// entryInfo returns what entry is, read from a directory opened in
// root: reading a directory opened in an os.Root stats every entry as it
// reads it, relative to the directory (fstatat, without following a
// symlink), so that what it says can't come from outside the root
// (TestEntryInfo).
func entryInfo(root *os.Root, entry fs.DirEntry) (os.FileInfo, error) {
	return entry.Info()
}
