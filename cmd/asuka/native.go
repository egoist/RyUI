package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/egoist/asuka/internal/nativegen"
)

// Native packages: Go packages that declare typed native modules
// (//asuka:native) and views (//asuka:view), with their native code beside
// them, a project of each platform's own (internal/nativegen): a Swift
// package (Package.swift, its Swift in swift/) for iOS and macOS, a
// Gradle module (android/) for Android, JavaScript (web/) for the web. The
// CLI keeps their generated files current, and builds each platform's
// project into the apps that import them: the Swift with xcodebuild, as a
// package of the app's (native_swift.go), the Kotlin as Gradle modules of
// the shell app, the JavaScript among the page's files.

// nativePackage is one of an app's packages (its own, a dependency's)
// with native code.
type nativePackage struct {
	path, name, dir string
	main            bool // in the app's own module: asuka writes its generated files
}

var (
	nativesMu    sync.Mutex
	nativesCache = map[string][]nativePackage{}
)

// appNatives are the native packages of package pkg, as the go command
// in dir builds it for goos and goarch: its dependencies with generated
// native code.
func appNatives(dir, pkg, goos, goarch string) ([]nativePackage, error) {
	key := dir + "\x00" + pkg + "\x00" + goos + "\x00" + goarch
	nativesMu.Lock()
	ps, ok := nativesCache[key]
	nativesMu.Unlock()
	if ok {
		return ps, nil
	}
	cgo := "0"
	if goos == "ios" || goos == "android" {
		cgo = "1"
	}
	c := exec.Command("go", "-C", dir, "list", "-deps", "-f", "{{.ImportPath}}\t{{.Name}}\t{{.Dir}}\t{{with .Module}}{{.Main}}{{end}}", pkg)
	c.Env = append(os.Environ(), "GOOS="+goos, "GOARCH="+goarch, "CGO_ENABLED="+cgo)
	var stderr bytes.Buffer
	c.Stderr = &stderr
	out, err := c.Output()
	if err != nil {
		return nil, fmt.Errorf("go list: %v\n%s", err, stderr.Bytes())
	}
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		f := strings.Split(l, "\t")
		if len(f) != 4 || f[2] == "" || !exists(filepath.Join(f[2], nativegen.GoFile(f[1]))) {
			continue
		}
		ps = append(ps, nativePackage{path: f[0], name: f[1], dir: f[2], main: f[3] == "true"})
	}
	nativesMu.Lock()
	nativesCache[key] = ps
	nativesMu.Unlock()
	return ps, nil
}

// forgetNatives drops what appNatives found for dir (asuka dev rebuilds:
// its imports may have changed).
func forgetNatives(dir string) {
	nativesMu.Lock()
	for k := range nativesCache {
		if strings.HasPrefix(k, dir+"\x00") {
			delete(nativesCache, k)
		}
	}
	nativesMu.Unlock()
}

// nativeFor are the app's native packages with native code for platform
// (ios, macos, android, web), built for goarch.
func nativeFor(app *goApp, platform, goarch string) ([]nativePackage, error) {
	goos := map[string]string{"ios": "ios", "macos": "darwin", "android": "android", "web": "js"}[platform]
	if platform == "web" {
		goarch = "wasm"
	}
	dir, pkg := app.goPackage()
	ps, err := appNatives(dir, pkg, goos, goarch)
	if err != nil {
		return nil, err
	}
	var out []nativePackage
	for _, p := range ps {
		switch platform {
		case "ios", "macos":
			if !nativegen.HasPlatform(p.dir, "swift") {
				continue
			}
			info, err := swiftPackageInfo(p.dir)
			if err != nil {
				return nil, err
			}
			if !info.supports(platform) {
				continue
			}
		default:
			if !nativegen.HasPlatform(p.dir, platform) {
				continue
			}
		}
		out = append(out, p)
	}
	return out, nil
}

// generateNatives keeps the app's native packages' generated files
// current: those of its own module (its dependencies there with
// //asuka: directives) are written again where their declarations
// changed; a dependency's of another module, stale, is an error (its
// module is what's published).
func generateNatives(app *goApp) error {
	c := exec.Command("go", "-C", app.dir, "list", "-deps", "-f", "{{.Dir}}\t{{.Name}}\t{{with .Module}}{{.Main}}{{end}}", ".")
	var stderr bytes.Buffer
	c.Stderr = &stderr
	out, err := c.Output()
	if err != nil {
		return fmt.Errorf("go list: %v\n%s", err, stderr.Bytes())
	}
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		f := strings.Split(l, "\t")
		if len(f) != 3 || f[0] == "" || f[2] == "" {
			continue // the standard library's
		}
		dir, main := f[0], f[2] == "true"
		switch {
		case main && hasDirectives(dir):
			if err := generateNative(dir); err != nil {
				return err
			}
		case !main && exists(filepath.Join(dir, nativegen.GoFile(f[1]))):
			if err := checkNative(dir); err != nil {
				return err
			}
		}
	}
	return nil
}

// hasDirectives reports whether a package's Go has //asuka:native or
// //asuka:view declarations.
func hasDirectives(dir string) bool {
	es, _ := os.ReadDir(dir)
	for _, e := range es {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") || strings.HasSuffix(n, "_native.go") {
			continue
		}
		if b, err := os.ReadFile(filepath.Join(dir, n)); err == nil && (bytes.Contains(b, []byte("//asuka:native")) || bytes.Contains(b, []byte("//asuka:view"))) {
			return true
		}
	}
	return false
}

// nativeDirs are the packages of the module in moduleDir (as ./...
// lists them) with //asuka:native or //asuka:view declarations.
func nativeDirs(moduleDir string) ([]string, error) {
	var dirs []string
	err := filepath.WalkDir(moduleDir, func(p string, d os.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		name := d.Name()
		if p != moduleDir && (strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") || name == "testdata" || name == "node_modules" ||
			exists(filepath.Join(p, "go.mod"))) {
			return filepath.SkipDir
		}
		if hasDirectives(p) {
			dirs = append(dirs, p)
		}
		return nil
	})
	sort.Strings(dirs)
	return dirs, err
}

// nativeStamp is what a package's generation depends on: its Go files'
// (but the generated one's) contents, and its projects' presence.
func nativeStamp(dir string) string {
	h := sha256.New()
	es, _ := os.ReadDir(dir)
	for _, e := range es {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") || strings.HasSuffix(n, "_native.go") {
			continue
		}
		b, _ := os.ReadFile(filepath.Join(dir, n))
		fmt.Fprintf(h, "%s %d\n", n, len(b))
		h.Write(b)
	}
	for _, pl := range nativegen.Platforms {
		fmt.Fprintln(h, pl, nativegen.HasPlatform(dir, pl))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// nativeStampPath is where a package's last generation's stamp is kept.
func nativeStampPath(dir string) string {
	base, _ := os.UserCacheDir()
	sum := sha256.Sum256([]byte(dir))
	return filepath.Join(base, "asuka", "native", hex.EncodeToString(sum[:])[:16])
}

// generateNative writes a package's generated files, unless nothing it
// depends on changed since it last did.
func generateNative(dir string) error {
	stamp := nativeStamp(dir)
	sp := nativeStampPath(dir)
	if b, err := os.ReadFile(sp); err == nil && string(b) == stamp {
		if p, _ := nativeHashes(dir); p {
			return nil
		}
	}
	p, err := nativegen.Load(dir)
	if err != nil {
		return err
	}
	if p == nil {
		return nil
	}
	wrote, err := nativegen.Write(p)
	if err != nil {
		return err
	}
	for _, w := range wrote {
		fmt.Printf("asuka: wrote %s\n", filepath.Join(rel(dir), w))
	}
	os.MkdirAll(filepath.Dir(sp), 0o755)
	return os.WriteFile(sp, []byte(stamp), 0o644)
}

// nativeHashes reports whether a package's generated files are there,
// all recording one hash.
func nativeHashes(dir string) (bool, string) {
	var hash string
	ok := true
	filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.Contains(d.Name(), ".asuka.") && !strings.HasSuffix(d.Name(), "_native.go") {
			return nil
		}
		b, _ := os.ReadFile(p)
		h := nativegen.HashOf(b)
		if hash == "" {
			hash = h
		}
		ok = ok && h != "" && h == hash
		return nil
	})
	return ok && hash != "", hash
}

// checkNative fails when a dependency's generated files are stale.
func checkNative(dir string) error {
	stamp := nativeStamp(dir)
	sp := nativeStampPath(dir)
	if b, err := os.ReadFile(sp); err == nil && string(b) == "checked "+stamp {
		return nil
	}
	p, err := nativegen.Load(dir)
	if err != nil {
		return err
	}
	if p == nil {
		return nil
	}
	stale, err := nativegen.Stale(p)
	if err != nil {
		return err
	}
	if len(stale) > 0 {
		return fmt.Errorf("%s: its generated native code is stale (%s): run asuka native generate in its module, and publish it", p.Path, strings.Join(stale, ", "))
	}
	os.MkdirAll(filepath.Dir(sp), 0o755)
	return os.WriteFile(sp, []byte("checked "+stamp), 0o644)
}

// rel is p relative to the working directory, when it's under it.
func rel(p string) string {
	if wd, err := os.Getwd(); err == nil {
		if r, err := filepath.Rel(wd, p); err == nil && !strings.HasPrefix(r, "..") {
			return r
		}
	}
	return p
}

// swiftInfo is what a package's Package.swift says that the CLI needs:
// the platforms it's for (none: all), and its library products.
type swiftInfo struct {
	Platforms []string `json:"platforms"`
	Products  []string `json:"products"`
}

func (s swiftInfo) supports(platform string) bool {
	return len(s.Platforms) == 0 || slices.Contains(s.Platforms, platform)
}

// swiftPackageInfo reads a package's Package.swift (swift package
// dump-package), once for each of its contents.
func swiftPackageInfo(dir string) (swiftInfo, error) {
	manifest, err := os.ReadFile(filepath.Join(dir, "Package.swift"))
	if err != nil {
		return swiftInfo{}, err
	}
	sum := sha256.Sum256(manifest)
	base, _ := os.UserCacheDir()
	cache := filepath.Join(base, "asuka", "swiftpm", hex.EncodeToString(sum[:])[:16]+".json")
	var info swiftInfo
	if b, err := os.ReadFile(cache); err == nil && json.Unmarshal(b, &info) == nil {
		return info, nil
	}
	if _, err := exec.LookPath("swift"); err != nil {
		return info, fmt.Errorf("%s has Swift (Package.swift): install Xcode", dir)
	}
	c := exec.Command("swift", "package", "dump-package", "--package-path", dir)
	var stderr bytes.Buffer
	c.Stderr = &stderr
	out, err := c.Output()
	if err != nil {
		return info, fmt.Errorf("%s/Package.swift: %v\n%s", dir, err, stderr.Bytes())
	}
	var dump struct {
		Platforms []struct {
			PlatformName string `json:"platformName"`
		} `json:"platforms"`
		Products []struct {
			Name string                     `json:"name"`
			Type map[string]json.RawMessage `json:"type"`
		} `json:"products"`
	}
	if err := json.Unmarshal(out, &dump); err != nil {
		return info, fmt.Errorf("%s/Package.swift: %v", dir, err)
	}
	for _, p := range dump.Platforms {
		if p.PlatformName == "ios" || p.PlatformName == "macos" {
			info.Platforms = append(info.Platforms, p.PlatformName)
		}
	}
	for _, p := range dump.Products {
		if _, ok := p.Type["library"]; ok {
			info.Products = append(info.Products, p.Name)
		}
	}
	if len(info.Products) == 0 {
		return info, fmt.Errorf("%s/Package.swift has no library product: the app links one", dir)
	}
	os.MkdirAll(filepath.Dir(cache), 0o755)
	b, _ := json.Marshal(info)
	os.WriteFile(cache, b, 0o644)
	return info, nil
}

// nativeNeeds gives the app what its native packages' Swift needs of the
// platform (ios, macos): their Info.plist's keys (beside Package.swift),
// arrays joining what others add, the rest where nothing else set the
// key; and their entitlements (iOS.entitlements, macOS.entitlements).
func (c *appConfig) nativeNeeds(ps []nativePackage, platform string) error {
	ent := map[string]string{"ios": "iOS.entitlements", "macos": "macOS.entitlements"}[platform]
	for _, p := range ps {
		if m, err := readPlistDict(filepath.Join(p.dir, "Info.plist")); err != nil {
			return err
		} else if m != nil {
			for k, v := range m {
				if c.nativePlist == nil {
					c.nativePlist = map[string]any{}
				}
				switch x := v.(type) {
				case []any:
					have, _ := c.nativePlist[k].([]string)
					for _, e := range x {
						if s, ok := e.(string); ok && !slices.Contains(have, s) {
							have = append(have, s)
						}
					}
					sort.Strings(have)
					c.nativePlist[k] = have
				case string:
					// A purpose string is a usage: the app's asuka.json says better.
					if strings.HasSuffix(k, "UsageDescription") {
						if c.IOS.Usage == nil {
							c.IOS.Usage = map[string]string{}
						}
						if c.IOS.Usage[k] == "" {
							c.IOS.Usage[k] = x
						}
					} else if _, ok := c.nativePlist[k]; !ok {
						c.nativePlist[k] = x
					}
				default:
					if _, ok := c.nativePlist[k]; !ok {
						c.nativePlist[k] = plistEntitlement(v)
					}
				}
			}
		}
		m, err := readPlistDict(filepath.Join(p.dir, ent))
		if err != nil {
			return err
		}
		for k, v := range m {
			if c.entitlements == nil {
				c.entitlements = map[string]any{}
			}
			if _, ok := c.entitlements[k]; !ok {
				c.entitlements[k] = plistEntitlement(v)
			}
			if c.entitlementsFor == nil {
				c.entitlementsFor = map[string]string{}
			}
			if c.entitlementsFor[k] == "" {
				c.entitlementsFor[k] = k + " (" + p.path + ")"
			}
		}
	}
	return nil
}

// readPlistDict reads a property list's dictionary (nil, no error: no
// file).
func readPlistDict(path string) (map[string]any, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	v, err := parsePlist(b)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: not a dictionary", path)
	}
	return m, nil
}

// nativeDirsOf are the directories asuka dev watches for a platform's
// native code (the Swift's, the Kotlin's, the JavaScript's).
func nativeDirsOf(ps []nativePackage, platform string) []string {
	var out []string
	for _, p := range ps {
		switch platform {
		case "ios", "macos":
			out = append(out, filepath.Join(p.dir, nativegen.SwiftDir), filepath.Join(p.dir, "Package.swift"))
		default:
			out = append(out, filepath.Join(p.dir, platform))
		}
	}
	return out
}

// androidNatives sets the app's native packages' Gradle modules (their
// android/), named by their packages.
func (c *appConfig) androidNatives(app *goApp) error {
	natives, err := nativeFor(app, "android", "arm64")
	if err != nil {
		return err
	}
	c.nativeModules = nil
	for _, p := range natives {
		c.nativeModules = append(c.nativeModules, [2]string{"native-" + strings.ToLower(nativegen.Sanitize(p.path)), filepath.Join(p.dir, "android")})
	}
	return nil
}
