//go:build unix && !darwin

package document

import "os"

// replaceFile puts the new file at name in path's place, at once.
func replaceFile(path, name string) error { return os.Rename(name, path) }

// movedTo finds where the file of identity id went: in its folder, as
// another app renames it there (a move elsewhere has no trace to follow
// without watching the whole disk).
func movedTo(dir, id string) string { return movedInDir(dir, id) }
