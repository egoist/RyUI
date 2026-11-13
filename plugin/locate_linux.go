//go:build linux && !android

package plugin

import "path/filepath"

// bundled is where asuka build puts a library on Linux: beside the
// program, or in its install prefix's lib, in the program's own
// directory there (bin/<app>, lib/<app>/<file>).
func bundled(exeDir string, l Library, f File) []string {
	return []string{
		filepath.Join(exeDir, f.Name),
		filepath.Join(exeDir, "..", "lib", filepath.Base(exePath()), f.Name),
	}
}

func systemName(File) string { return "" }
