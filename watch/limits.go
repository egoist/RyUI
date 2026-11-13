package watch

import "runtime"

// defaultMaxEntries is how many files and directories a watch keeps track
// of when Options.MaxEntries is zero. macOS watches a whole tree with one
// FSEvents stream, so there the limit bounds the memory of what the watch
// keeps, not the descriptors the system holds, and it can be high.
// Elsewhere the system takes a descriptor per directory (inotify) or per
// file (kqueue).
func defaultMaxEntries(backend string) int {
	if runtime.GOOS == "darwin" && backend == "" {
		return 65536
	}
	return 8192
}
