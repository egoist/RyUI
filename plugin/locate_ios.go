//go:build ios

package plugin

import "path/filepath"

// bundled is where asuka build puts a library on iOS: a framework named
// for it in the app's Frameworks, signed with the app; an extension (the
// share sheet, the notification service: PlugIns/<name>.appex) finds the
// app's.
func bundled(exeDir string, l Library, f File) []string {
	fw := filepath.Join("Frameworks", l.Name+".framework", l.Name)
	return []string{filepath.Join(exeDir, fw), filepath.Join(exeDir, "..", "..", fw)}
}

func systemName(File) string { return "" }
