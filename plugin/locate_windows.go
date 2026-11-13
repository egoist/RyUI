//go:build windows

package plugin

import "path/filepath"

// bundled is where asuka build puts a library on Windows: beside the
// .exe.
func bundled(exeDir string, l Library, f File) []string {
	return []string{filepath.Join(exeDir, f.Name)}
}

func systemName(File) string { return "" }
