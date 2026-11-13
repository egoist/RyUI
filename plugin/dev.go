//go:build (darwin || linux || windows) && !ios && !android && !asuka_bundled

package plugin

// devPath is a desktop's library in go run, go test and asuka dev: the
// plugin's checkout's build, or the published file (FilePath).
func devPath(dir string, m *Manifest, l Library, target string) (string, error) {
	return FilePath(dir, l, target)
}
