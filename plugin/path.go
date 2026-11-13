//go:build !js

package plugin

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// LibraryPath is the path of the plugin's library named library, for its
// code to load: where asuka build put it, with the app; else, in go run,
// go test and asuka dev on a desktop, the plugin's checkout's build
// (build/<target> beside the manifest) or the published file in the
// user's cache, downloaded there first. Call it from a file of the
// plugin's package's directory, the manifest's: that's where its build
// is. A shipped app (asuka_bundled), a phone's, never downloads.
func LibraryPath(manifest []byte, library string) (string, error) {
	m, err := Parse(manifest)
	if err != nil {
		return "", err
	}
	l, ok := m.Library(library)
	if !ok {
		return "", fmt.Errorf("plugin %s: no library %s", m.Name, library)
	}
	f, ok := l.Files[hostTarget()]
	if !ok && runtime.GOOS == "ios" {
		f, ok = l.Files["ios-simulator"]
	}
	if !ok {
		return "", fmt.Errorf("plugin %s: no %s library for %s", m.Name, library, hostTarget())
	}
	var tried []string
	if exe := exePath(); exe != "" {
		for _, p := range bundled(filepath.Dir(exe), l, f) {
			if _, err := os.Stat(p); err == nil {
				return p, nil
			}
			tried = append(tried, p)
		}
	}
	if p := systemName(f); p != "" {
		return p, nil
	}
	dir := ""
	if _, file, _, ok := runtime.Caller(1); ok && filepath.IsAbs(file) {
		dir = filepath.Dir(file)
	}
	if p, err := devPath(dir, m, l, hostTarget()); err != nil {
		return "", err
	} else if p != "" {
		return p, nil
	}
	return "", fmt.Errorf("plugin %s: library %s isn't with the app (looked for %s): build the app with asuka build", m.Name, library, strings.Join(tried, ", "))
}

// exePath is the program's path, its links resolved.
func exePath() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	return exe
}
