package document

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// What documents do with their files, off the main thread: read them,
// write them whole or not at all, and tell whether another app changed
// them since.

// fileStamp tells whether a file changed: its modification time and size
// as the document last read or wrote it, as NSDocument and VS Code
// compare them, and the file's identity, by which a document finds its
// file where another app moved it.
type fileStamp struct {
	ModTime int64 `json:"modTime"` // Unix nanoseconds
	Size    int64 `json:"size"`
	// ID is the file's identity (device and inode; Windows' volume and
	// file index); "" where the system gives none. It says nothing of the
	// content: same ignores it.
	ID string `json:"id,omitzero"`
}

func stampOf(info os.FileInfo) fileStamp {
	return fileStamp{ModTime: info.ModTime().UnixNano(), Size: info.Size(), ID: fileID(info)}
}

// same reports whether s and t stamp the same content.
func (s fileStamp) same(t fileStamp) bool { return s.ModTime == t.ModTime && s.Size == t.Size }

// statStamp returns the stamp of the file at path; ok is false when it
// doesn't exist.
func statStamp(path string) (s fileStamp, ok bool, err error) {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return fileStamp{}, false, nil
	}
	if err != nil {
		return fileStamp{}, false, err
	}
	return stampOf(info), true, nil
}

// readFile reads the file at path with read, and stamps it as it was
// read. Off the main thread.
func readFile[T any](path string, read func(io.Reader) (T, error)) (v T, stamp fileStamp, err error) {
	f, err := openShared(path)
	if err != nil {
		return v, stamp, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return v, stamp, err
	}
	if info.IsDir() {
		return v, stamp, fmt.Errorf("document: %s is a folder", path)
	}
	stamp = stampOf(info)
	if stamp.ID == "" {
		stamp.ID = openFileID(f)
	}
	v, err = read(f)
	return v, stamp, err
}

// writeFileSafely writes a file as NSDocument and GIO do: into a new file
// next to it, flushed to the disk, which then replaces it, so that a
// crash or a full disk never leaves half a file. The new file takes the
// old one's permissions. A symbolic link's target is replaced, and a file
// with other hard links, or in a folder the user can't create files in,
// is written in place, as GIO does.
func writeFileSafely(path string, write func(io.Writer) error) (fileStamp, error) {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	info, err := os.Stat(path)
	exists := err == nil
	if exists && fileLinks(info) > 1 {
		return writeFileInPlace(path, write)
	}
	tmp, err := createSibling(path)
	if err != nil {
		if exists && errors.Is(err, fs.ErrPermission) {
			return writeFileInPlace(path, write)
		}
		return fileStamp{}, err
	}
	name := tmp.Name()
	err = writeAndSync(tmp, write)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil && exists {
		keepMode(name, info)
	}
	if err == nil {
		err = replaceFile(path, name)
	}
	if err != nil {
		if _, serr := os.Lstat(path); exists && errors.Is(serr, fs.ErrNotExist) {
			// The system removed the file and could not put the new one in
			// its place: the sibling is the only copy left, so it stays.
			return fileStamp{}, fmt.Errorf("%w; the new contents are in %s", err, name)
		}
		os.Remove(name)
		return fileStamp{}, err
	}
	s, _, err := statStamp(path)
	return s, err
}

// createSibling creates a hidden file next to path, with the permissions
// new files get (the umask applies).
func createSibling(path string) (*os.File, error) {
	dir, base := filepath.Split(path)
	for range 10 {
		f, err := os.OpenFile(filepath.Join(dir, "."+base+"."+randomID()[:8]+".tmp"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o666)
		if !errors.Is(err, fs.ErrExist) {
			return f, err
		}
	}
	return nil, errors.New("document: cannot create a temporary file next to " + path)
}

func writeFileInPlace(path string, write func(io.Writer) error) (fileStamp, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0)
	if err != nil {
		return fileStamp{}, err
	}
	err = writeAndSync(f, write)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fileStamp{}, err
	}
	s, _, err := statStamp(path)
	return s, err
}

func writeAndSync(f *os.File, write func(io.Writer) error) error {
	if err := write(f); err != nil {
		return err
	}
	return f.Sync()
}

// writeAppFile writes a file of the package's own (a recovery copy, its
// index): a new file renamed over it, so that it is whole or not there.
func writeAppFile(path string, write func(io.Writer) error) error {
	f, err := createSibling(path)
	if err != nil {
		return err
	}
	err = writeAndSync(f, write)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(f.Name(), path)
	}
	if err != nil {
		os.Remove(f.Name())
	}
	return err
}

// copyFile copies the file at from to to, safely.
func copyFile(from, to string) error {
	src, err := openShared(from)
	if err != nil {
		return err
	}
	defer src.Close()
	_, err = writeFileSafely(to, func(w io.Writer) error {
		_, err := io.Copy(w, src)
		return err
	})
	return err
}

// randomID is 32 random hex digits: a recovery copy's name, a session's.
func randomID() string {
	var b [16]byte
	rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
