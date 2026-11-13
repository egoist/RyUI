//go:build darwin && !ios

package plugin

import "path/filepath"

// bundled is where asuka build puts a library on macOS: the app's
// Frameworks (Contents/Frameworks, beside Contents/MacOS), or beside the
// program.
func bundled(exeDir string, l Library, f File) []string {
	return []string{
		filepath.Join(exeDir, "..", "Frameworks", f.Name),
		filepath.Join(exeDir, f.Name),
	}
}

func systemName(File) string { return "" }
