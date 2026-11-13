//go:build darwin

package document

import (
	"bytes"
	"fmt"
	"os"
	"runtime"
	"strings"
	"unsafe"

	"golang.org/x/sys/unix"
)

// replaceFile puts the new file at name in path's place, at once, with
// the old file's extended attributes (Finder's tags and comments, the
// quarantine, an app's own), which NSFileManager's replaceItemAtURL:
// keeps too.
func replaceFile(path, name string) error {
	copyXattrs(path, name)
	return os.Rename(name, path)
}

// copyXattrs gives the file at to the extended attributes of the file at
// from, those it can.
func copyXattrs(from, to string) {
	n, err := unix.Listxattr(from, nil)
	if err != nil || n <= 0 {
		return
	}
	list := make([]byte, n)
	if n, err = unix.Listxattr(from, list); err != nil {
		return
	}
	for _, name := range bytes.Split(list[:n], []byte{0}) {
		if len(name) == 0 {
			continue
		}
		attr := string(name)
		size, err := unix.Getxattr(from, attr, nil)
		if err != nil || size < 0 {
			continue
		}
		v := make([]byte, size)
		if size, err = unix.Getxattr(from, attr, v); err != nil {
			continue
		}
		unix.Setxattr(to, attr, v[:size], 0)
	}
}

// movedTo finds where the file of identity id went, anywhere on its
// volume, as NSDocument follows a document the user moves in Finder: by
// its device and inode (/.vol/<dev>/<ino>), whose path F_GETPATH tells.
// A file moved to the Trash counts as deleted: "".
func movedTo(dir, id string) string {
	var dev, ino uint64
	if _, err := fmt.Sscanf(id, "%d:%d", &dev, &ino); err != nil {
		return movedInDir(dir, id)
	}
	fd, err := unix.Open(fmt.Sprintf("/.vol/%d/%d", dev, ino), unix.O_RDONLY|unix.O_EVTONLY, 0)
	if err != nil {
		return movedInDir(dir, id)
	}
	defer unix.Close(fd)
	buf := make([]byte, 1024) // MAXPATHLEN
	_, err = unix.FcntlInt(uintptr(fd), unix.F_GETPATH, int(uintptr(unsafe.Pointer(&buf[0]))))
	runtime.KeepAlive(buf)
	if err != nil {
		return movedInDir(dir, id)
	}
	p := string(buf[:bytes.IndexByte(buf, 0)])
	if p == "" || strings.Contains(p, "/.Trash/") || strings.Contains(p, "/.Trashes/") {
		return ""
	}
	return p
}
