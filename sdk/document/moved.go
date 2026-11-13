package document

import (
	"os"
	"path/filepath"
)

// movedInDir returns the path of the file in dir whose identity is id, or
// "": where another app renamed a document's file in its folder.
func movedInDir(dir, id string) string {
	if id == "" {
		return ""
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue
		}
		p := filepath.Join(dir, e.Name())
		got := ""
		if info, err := e.Info(); err == nil {
			got = fileID(info)
		}
		if got == "" {
			got = pathID(p)
		}
		if got == id {
			return p
		}
	}
	return ""
}
