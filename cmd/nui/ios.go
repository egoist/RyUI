package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	iosHost "github.com/egoist/nui/hosts/ios"
)

// The iOS dev loop: the Swift host is compiled once (and cached); on each
// change the app's Go code is rebuilt as a c-archive, linked with it into
// an .app, and reinstalled and relaunched on the simulator.

const iosMin = "16.0"

type goApp struct {
	dir       string // the main package's directory
	name      string // its base name
	moduleDir string
	bundleID  string
	cfg       *appConfig // nui.json
}

// displayName is the name under the app's icon.
func (a *goApp) displayName() string {
	if a.cfg != nil {
		return a.cfg.Name
	}
	return a.name
}

func loadApp(dir string) (*goApp, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	out, err := exec.Command("go", "-C", abs, "list", "-f", "{{.Name}}\t{{.Module.Dir}}\t{{.Dir}}", ".").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%s: %s", dir, bytes.TrimSpace(out))
	}
	f := strings.Split(strings.TrimSpace(string(out)), "\t")
	if len(f) != 3 || f[0] != "main" {
		return nil, fmt.Errorf("%s is not a main package (it should call app.Run in main)", dir)
	}
	cfg, err := loadConfig(f[2])
	if err != nil {
		return nil, err
	}
	return &goApp{dir: f[2], name: filepath.Base(abs), moduleDir: f[1], bundleID: cfg.ID, cfg: cfg}, nil
}

type iosToolchain struct {
	sdk, sdkName, clang, target, goarch string
	release                             bool // optimized, without the debug automation
	verbose                             bool
}

func newIOSToolchain(verbose bool) (*iosToolchain, error) {
	sdk, err := output("xcrun", "--sdk", "iphonesimulator", "--show-sdk-path")
	if err != nil {
		return nil, fmt.Errorf("no iOS simulator SDK (install Xcode): %v", err)
	}
	clang, err := output("xcrun", "--sdk", "iphonesimulator", "--find", "clang")
	if err != nil {
		return nil, err
	}
	arch, goarch := "arm64", "arm64"
	if runtime.GOARCH == "amd64" {
		arch, goarch = "x86_64", "amd64"
	}
	return &iosToolchain{sdk: sdk, sdkName: "iphonesimulator", clang: clang, target: arch + "-apple-ios" + iosMin + "-simulator", goarch: goarch, verbose: verbose}, nil
}

func output(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("%s: %v: %s", name, err, bytes.TrimSpace(ee.Stderr))
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func (t *iosToolchain) run(dir string, env []string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	if t.verbose {
		fmt.Fprintln(os.Stderr, "+", strings.Join(append(env, append([]string{name}, args...)...), " "))
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s failed:\n%s", name, filterLinker(out))
	}
	return nil
}

// filterLinker drops the linker's noise about SDK versions.
func filterLinker(out []byte) string {
	var keep []string
	for _, l := range strings.Split(string(out), "\n") {
		if strings.Contains(l, "Wincompatible-sysroot") || strings.HasPrefix(l, "ld: warning:") {
			continue
		}
		keep = append(keep, l)
	}
	return strings.TrimSpace(strings.Join(keep, "\n"))
}

// buildHost compiles the embedded Swift host, with the app's own Swift
// (its ios/*.swift: native views for ui.Native), into one object file,
// cached by the hash of its sources and the SDK.
func (t *iosToolchain) buildHost(work string, app *goApp) (string, error) {
	h := sha256.New()
	fmt.Fprintln(h, t.sdk, t.target, t.release)
	var files []string
	err := fs.WalkDir(iosHost.Files, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(files)
	shared := filepath.Join(filepath.Dir(filepath.Dir(work)), "ios-host") // shared by all apps
	src := filepath.Join(shared, "src")
	os.RemoveAll(src)
	var swift []string
	for _, f := range files {
		data, _ := iosHost.Files.ReadFile(f)
		h.Write([]byte(f))
		h.Write(data)
		dst := filepath.Join(src, f)
		os.MkdirAll(filepath.Dir(dst), 0o755)
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return "", err
		}
		if strings.HasSuffix(f, ".swift") {
			swift = append(swift, dst)
		}
	}
	if app != nil {
		own, _ := filepath.Glob(filepath.Join(app.dir, "ios", "*.swift"))
		sort.Strings(own)
		for _, f := range own {
			data, err := os.ReadFile(f)
			if err != nil {
				return "", err
			}
			h.Write([]byte(f))
			h.Write(data)
			swift = append(swift, f)
		}
	}
	obj := filepath.Join(shared, hex.EncodeToString(h.Sum(nil))[:16]+".o")
	if _, err := os.Stat(obj); err == nil {
		return obj, nil
	}
	fmt.Println("nui: compiling the iOS host (once)")
	opt := []string{"-Onone", "-D", "DEBUG"}
	if t.release {
		opt = []string{"-O"}
	}
	args := append([]string{"swiftc", "-target", t.target, "-sdk", t.sdk, "-swift-version", "5",
		"-module-name", "NuiApp", "-parse-as-library", "-wmo"}, opt...)
	args = append(args, "-I", filepath.Join(src, "Sources", "CNui", "include"), "-c", "-o", obj)
	if err := t.run(shared, nil, "xcrun", append(args, swift...)...); err != nil {
		return "", err
	}
	return obj, nil
}

// writeOverlay writes a go build overlay that adds a file to the app's
// main package: it links the mobile bridge and hands it main.
func writeOverlay(app *goApp, work string) (string, error) {
	stub := filepath.Join(work, "zz_nui_main.go")
	code := "package main\n\nimport \"github.com/egoist/nui/mobile\"\n\nfunc init() { mobile.Main = main }\n"
	if err := os.WriteFile(stub, []byte(code), 0o644); err != nil {
		return "", err
	}
	overlay, _ := json.Marshal(map[string]map[string]string{"Replace": {filepath.Join(app.dir, "zz_nui_main.go"): stub}})
	ovPath := filepath.Join(work, "overlay.json")
	return ovPath, os.WriteFile(ovPath, overlay, 0o644)
}

// buildGo builds the app's Go code as a c-archive.
func (t *iosToolchain) buildGo(app *goApp, work string) (string, error) {
	ovPath, err := writeOverlay(app, work)
	if err != nil {
		return "", err
	}
	lib := filepath.Join(work, "libnui.a")
	flags := "-isysroot " + t.sdk + " -target " + t.target
	env := []string{"GOOS=ios", "GOARCH=" + t.goarch, "CGO_ENABLED=1", "CC=" + t.clang,
		"CGO_CFLAGS=" + flags, "CGO_LDFLAGS=" + flags}
	args := []string{"build", "-tags", "ios", "-buildmode=c-archive", "-overlay", ovPath, "-o", lib, app.cfg.ldflags(t.release)}
	if t.release {
		args = append(args, "-trimpath")
	}
	err = t.run(app.dir, env, "go", append(args, ".")...)
	return lib, err
}

func (t *iosToolchain) link(app *goApp, work, hostObj, lib string) (string, error) {
	bundle := filepath.Join(work, app.name+".app")
	os.RemoveAll(bundle)
	if err := os.MkdirAll(bundle, 0o755); err != nil {
		return "", err
	}
	if err := t.run(work, nil, "xcrun", "swiftc", "-target", t.target, "-sdk", t.sdk, hostObj, lib,
		"-o", filepath.Join(bundle, "NuiApp"), "-framework", "UIKit", "-framework", "Foundation", "-lresolv"); err != nil {
		return "", err
	}
	if err := copyDir(filepath.Join(app.dir, "assets"), filepath.Join(bundle, "assets")); err != nil {
		return "", err
	}
	plist := fmt.Sprintf(infoPlist, app.bundleID, app.displayName(), app.displayName(), iosMin)
	if err := os.WriteFile(filepath.Join(bundle, "Info.plist"), []byte(plist), 0o644); err != nil {
		return "", err
	}
	return bundle, t.run(work, nil, "codesign", "--force", "--sign", "-", bundle)
}

const infoPlist = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>CFBundleIdentifier</key><string>%s</string>
<key>CFBundleExecutable</key><string>NuiApp</string>
<key>CFBundleName</key><string>%s</string>
<key>CFBundleDisplayName</key><string>%s</string>
<key>CFBundlePackageType</key><string>APPL</string>
<key>CFBundleShortVersionString</key><string>1.0</string>
<key>CFBundleVersion</key><string>1</string>
<key>MinimumOSVersion</key><string>%s</string>
<key>CFBundleSupportedPlatforms</key><array><string>iPhoneSimulator</string></array>
<key>UIDeviceFamily</key><array><integer>1</integer></array>
<key>UILaunchScreen</key><dict/>
<key>NSAppTransportSecurity</key><dict><key>NSAllowsLocalNetworking</key><true/></dict>
<key>UIApplicationSceneManifest</key><dict><key>UIApplicationSupportsMultipleScenes</key><false/></dict>
<key>UISupportedInterfaceOrientations</key><array><string>UIInterfaceOrientationPortrait</string><string>UIInterfaceOrientationLandscapeLeft</string><string>UIInterfaceOrientationLandscapeRight</string></array>
</dict></plist>
`

type simDevice struct {
	Name        string `json:"name"`
	UDID        string `json:"udid"`
	State       string `json:"state"`
	IsAvailable bool   `json:"isAvailable"`
	runtime     string
}

// pickSimulator returns the device named want, or the booted iPhone, or
// the newest available iPhone, booting it if needed.
func pickSimulator(want string) (*simDevice, error) {
	out, err := exec.Command("xcrun", "simctl", "list", "devices", "available", "-j").Output()
	if err != nil {
		return nil, fmt.Errorf("simctl: %v (install Xcode and an iOS simulator runtime)", err)
	}
	var list struct {
		Devices map[string][]simDevice `json:"devices"`
	}
	if err := json.Unmarshal(out, &list); err != nil {
		return nil, err
	}
	var all []*simDevice
	runtimes := make([]string, 0, len(list.Devices))
	for rt := range list.Devices {
		if strings.Contains(rt, "iOS") {
			runtimes = append(runtimes, rt)
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(runtimes)))
	for _, rt := range runtimes {
		for i := range list.Devices[rt] {
			d := list.Devices[rt][i]
			d.runtime = rt
			if d.IsAvailable {
				all = append(all, &d)
			}
		}
	}
	var pick *simDevice
	for _, d := range all {
		switch {
		case want != "" && (d.UDID == want || d.Name == want):
			pick = d
		case want == "" && d.State == "Booted" && strings.HasPrefix(d.Name, "iPhone") && pick == nil:
			pick = d
		}
	}
	if pick == nil && want == "" {
		for _, d := range all {
			if strings.HasPrefix(d.Name, "iPhone") && strings.Contains(d.Name, "Pro") && !strings.Contains(d.Name, "Max") {
				pick = d
				break
			}
		}
		if pick == nil {
			for _, d := range all {
				if strings.HasPrefix(d.Name, "iPhone") {
					pick = d
					break
				}
			}
		}
	}
	if pick == nil {
		if want != "" {
			return nil, fmt.Errorf("no simulator %q (see xcrun simctl list devices)", want)
		}
		return nil, errors.New("no iPhone simulator; add one in Xcode")
	}
	if pick.State != "Booted" {
		fmt.Printf("nui: booting %s\n", pick.Name)
		if out, err := exec.Command("xcrun", "simctl", "boot", pick.UDID).CombinedOutput(); err != nil && !bytes.Contains(out, []byte("current state: Booted")) {
			return nil, fmt.Errorf("simctl boot: %s", bytes.TrimSpace(out))
		}
		exec.Command("xcrun", "simctl", "bootstatus", pick.UDID, "-b").Run()
		exec.Command("open", "-a", "Simulator").Run() // shows it, when installed
	}
	return pick, nil
}

func workDir(app *goApp, platform string) (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(app.dir))
	dir := filepath.Join(base, "nui", app.name+"-"+hex.EncodeToString(sum[:])[:8], platform)
	return dir, os.MkdirAll(dir, 0o755)
}

func devIOS(dir string, o *devOptions, stop <-chan os.Signal) error {
	app, err := loadApp(dir)
	if err != nil {
		return err
	}
	tc, err := newIOSToolchain(o.verbose)
	if err != nil {
		return err
	}
	work, err := workDir(app, "ios")
	if err != nil {
		return err
	}
	sim, err := pickSimulator(o.device)
	if err != nil {
		return err
	}
	fmt.Printf("nui: %s on %s (%s)\n", app.name, sim.Name, sim.UDID)
	if usePlayer(app, o, "ios") {
		return devIOSPlayer(app, tc, work, sim, stop)
	}

	var logs *exec.Cmd
	build := func() error {
		start := time.Now()
		hostObj, err := tc.buildHost(work, app)
		if err != nil {
			return err
		}
		lib, err := tc.buildGo(app, work)
		if err != nil {
			return err
		}
		bundle, err := tc.link(app, work, hostObj, lib)
		if err != nil {
			return err
		}
		if logs != nil && logs.Process != nil {
			logs.Process.Kill()
			logs.Wait()
			logs = nil
		}
		if err := tc.run(work, nil, "xcrun", "simctl", "install", sim.UDID, bundle); err != nil {
			return err
		}
		fmt.Printf("nui: built and installed in %.1fs\n", time.Since(start).Seconds())
		if o.once {
			return tc.run(work, nil, "xcrun", "simctl", "launch", "--terminate-running-process", sim.UDID, app.bundleID)
		}
		logs = exec.Command("xcrun", "simctl", "launch", "--console-pty", "--terminate-running-process", sim.UDID, app.bundleID)
		logs.Stdout, logs.Stderr = os.Stdout, os.Stderr
		return logs.Start()
	}
	if err := build(); err != nil {
		if o.once {
			return err
		}
		fmt.Fprintln(os.Stderr, "nui:", err)
	}
	if o.once {
		return nil
	}
	fmt.Println("nui: watching for changes (Ctrl-C to stop)")
	w := newWatcher(app.moduleDir, app.dir)
	tick := time.NewTicker(400 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-stop:
			if logs != nil && logs.Process != nil {
				logs.Process.Kill()
			}
			return nil
		case <-tick.C:
			if !w.changed() {
				continue
			}
			fmt.Println("nui: change detected, rebuilding")
			if err := build(); err != nil {
				fmt.Fprintln(os.Stderr, "nui:", err)
			}
		}
	}
}

// copyDir copies the files under src (if it exists) to dst.
func copyDir(src, dst string) error {
	if _, err := os.Stat(src); err != nil {
		return nil
	}
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		out := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(out, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(out, data, 0o644)
	})
}
