package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/egoist/asuka/plugin"
)

// Plugins (package plugin): the packages among an app's dependencies with
// a manifest (asuka-plugin.json) bring native libraries, which a build
// ships with the app for its platform, where package plugin finds them:
// in Contents/Frameworks on macOS, beside the .exe on Windows, in
// lib/<exe> of the prefix on Linux, as a framework each in an iOS app's
// Frameworks, beside libasuka.so in each of an APK's ABIs, and in
// lib/ of a web build's host files. A desktop build that ships any builds
// with pluginTag, which leaves out the plugins' fetching their own for go
// run and go test. The files are the plugins' checkout's builds, or the
// published ones, downloaded once into the user's cache.

// pluginTag is the build tag of an app whose plugins' libraries ship
// with it.
const pluginTag = "asuka_bundled"

// appPlugin is a plugin an app uses: its directory and manifest.
type appPlugin struct {
	dir string
	m   *plugin.Manifest
}

// pluginLib is a plugin's library's file for a target.
type pluginLib struct {
	appPlugin
	lib    plugin.Library
	file   plugin.File
	target string
}

var (
	pluginsMu    sync.Mutex
	pluginsCache = map[string][]appPlugin{}
)

// appPlugins are the plugins of the main package in dir, built for goos
// and goarch: its dependencies with a manifest.
func appPlugins(dir, goos, goarch string) ([]appPlugin, error) {
	return pkgPlugins(dir, ".", goos, goarch)
}

// pkgPlugins are the plugins of package pkg, as the go command in dir
// builds it (the player: mobile/player, in the app's module).
func pkgPlugins(dir, pkg, goos, goarch string) ([]appPlugin, error) {
	key := dir + "\x00" + pkg + "\x00" + goos + "\x00" + goarch
	pluginsMu.Lock()
	ps, ok := pluginsCache[key]
	pluginsMu.Unlock()
	if ok {
		return ps, nil
	}
	cgo := "0"
	if goos == "ios" || goos == "android" {
		cgo = "1" // their build modes'
	}
	c := exec.Command("go", "-C", dir, "list", "-deps", "-f", "{{.Dir}}", pkg)
	c.Env = append(os.Environ(), "GOOS="+goos, "GOARCH="+goarch, "CGO_ENABLED="+cgo)
	var stderr bytes.Buffer
	c.Stderr = &stderr
	out, err := c.Output()
	if err != nil {
		return nil, fmt.Errorf("go list: %v\n%s", err, stderr.Bytes())
	}
	for _, d := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		data, err := os.ReadFile(filepath.Join(d, plugin.ManifestFile))
		if err != nil {
			continue
		}
		m, err := plugin.Parse(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", d, err)
		}
		ps = append(ps, appPlugin{dir: d, m: m})
	}
	pluginsMu.Lock()
	pluginsCache[key] = ps
	pluginsMu.Unlock()
	return ps, nil
}

// forgetPlugins drops what appPlugins found for dir: its imports may
// have changed (asuka dev rebuilds).
func forgetPlugins(dir string) {
	pluginsMu.Lock()
	for k := range pluginsCache {
		if strings.HasPrefix(k, dir+"\x00") {
			delete(pluginsCache, k)
		}
	}
	pluginsMu.Unlock()
}

// appLibs are the libraries of the plugins the main package in dir uses,
// built for goos and goarch, for target (plugin.Targets): what its build
// ships.
func appLibs(dir, goos, goarch, target string) ([]pluginLib, error) {
	ps, err := appPlugins(dir, goos, goarch)
	if err != nil {
		return nil, err
	}
	return pluginLibs(ps, target)
}

// pluginLibs are the libraries of plugins ps for target. Every platform
// puts them in one place, by their files' names, so no two may share one.
func pluginLibs(ps []appPlugin, target string) ([]pluginLib, error) {
	var libs []pluginLib
	byName := map[string]pluginLib{}
	for _, p := range ps {
		for _, l := range p.m.Libraries {
			f, ok := l.Files[target]
			if !ok {
				return nil, fmt.Errorf("plugin %s: library %s has no file for %s", p.m.Name, l.Name, target)
			}
			if o, ok := byName[f.Name]; ok {
				return nil, fmt.Errorf("plugins %s and %s both have a file %s for %s", o.m.Name, p.m.Name, f.Name, target)
			}
			byName[f.Name] = pluginLib{appPlugin: p, lib: l, file: f, target: target}
			libs = append(libs, byName[f.Name])
		}
	}
	return libs, nil
}

// path is the library's file on this machine, downloaded first if need
// be (saying so).
func (l pluginLib) path() (string, error) {
	if !plugin.Cached(l.dir, l.lib, l.target) {
		fmt.Printf("asuka: downloading %s, plugin %s's, for %s\n", l.file.Name, l.m.Name, l.target)
	}
	return plugin.FilePath(l.dir, l.lib, l.target)
}

// read is the library's bytes.
func (l pluginLib) read() ([]byte, error) {
	p, err := l.path()
	if err != nil {
		return nil, err
	}
	return os.ReadFile(p)
}

// write writes the library to path.
func (l pluginLib) write(path string) error {
	data, err := l.read()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o755)
}

// writeLibs writes each library into dir under its file's name.
func writeLibs(libs []pluginLib, dir string) error {
	for _, l := range libs {
		if err := l.write(filepath.Join(dir, l.file.Name)); err != nil {
			return err
		}
	}
	return nil
}

// pluginBuildFlags are go build's flags for a desktop build that ships
// libs (none if it ships none): pluginTag, with GOFLAGS' tags.
func pluginBuildFlags(libs []pluginLib) []string {
	if len(libs) == 0 {
		return nil
	}
	return []string{goTags(os.Getenv("GOFLAGS"), pluginTag)}
}
