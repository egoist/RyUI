package update

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// FileList names the file, in an installed app on Windows and Linux, that
// lists what its version installed, one path a line, relative to the
// install's root with slashes: `asuka build` writes it into the app, and
// the updater rewrites it with the paths of each update it installs
// (Swap). An update removes the paths that the list of the version it
// replaces names and its own version lacks, and leaves those that no list
// names: a portable copy's data beside a Windows app, the other programs
// of a Linux prefix such as ~/.local. A Mac's app bundle has none: an
// update replaces the whole bundle.
//
// On Windows the list sits in the app's folder and names its entries (the
// .exe, assets); on Linux it is share/<exe>/.asuka-files in the prefix and
// names the app's files (bin/<exe>, share/applications/<id>.desktop...).
const FileList = ".asuka-files"

// WriteFileList writes the list at file (FileList, or a path ending in
// it), naming paths (sorted, each once; what ValidListPath refuses left
// out), through a temporary file renamed into place, so that the list is
// never half written.
func WriteFileList(file string, paths []string) error {
	if fi, err := os.Stat(file); err == nil && fi.IsDir() {
		file = filepath.Join(file, FileList)
	}
	var names []string
	for _, p := range paths {
		if ValidListPath(p) {
			names = append(names, p)
		}
	}
	slices.Sort(names)
	names = slices.Compact(names)
	var b strings.Builder
	for _, n := range names {
		b.WriteString(n)
		b.WriteByte('\n')
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(file), FileList+".*.tmp")
	if err != nil {
		return err
	}
	_, err = f.WriteString(b.String())
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(f.Name(), file)
	}
	if err != nil {
		os.Remove(f.Name())
	}
	return err
}

// ParseFileList returns the paths that the content of a FileList names,
// leaving out the lines ValidListPath refuses, which an update must never
// remove.
func ParseFileList(b []byte) []string {
	var names []string
	for line := range strings.Lines(string(b)) {
		if n := strings.TrimRight(line, "\r\n"); ValidListPath(n) {
			names = append(names, n)
		}
	}
	return names
}

// ValidListPath reports whether p can be in a FileList: a path inside the
// install's root, with slashes, that isn't a FileList itself.
func ValidListPath(p string) bool {
	return p != "" && p != "." && fs.ValidPath(p) && path.Base(p) != FileList &&
		!strings.ContainsAny(p, "\\\x00\r\n")
}
