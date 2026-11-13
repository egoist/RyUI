//go:build !unix && !windows

package document

import "os"

// A browser's files (js/wasm) are the app's own: no other app changes
// them, and nothing else holds them.

func fileLinks(os.FileInfo) int                 { return 1 }
func openShared(path string) (*os.File, error)  { return os.Open(path) }
func lockFile(*os.File) error                    { return nil }
func fileID(os.FileInfo) string                  { return "" }
func openFileID(*os.File) string                 { return "" }
func pathID(string) string                       { return "" }
func keepMode(string, os.FileInfo)               {}
func replaceFile(path, name string) error        { return os.Rename(name, path) }
func movedTo(dir, id string) string              { return "" }

func fileWritable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().Perm()&0o200 != 0
}
