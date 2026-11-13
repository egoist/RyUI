package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"time"

	iosHost "github.com/egoist/asuka/platform/ios"
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
	cfg       *appConfig // asuka.json
}

// displayName is the name under the app's icon.
func (a *goApp) displayName() string {
	if a.cfg != nil && a.cfg.Name != "" {
		return a.cfg.Name
	}
	return a.name // the player's
}

func loadApp(dir string) (*goApp, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	// Its stdout only: with modules to download first, go list says so on
	// stderr ("go: downloading ..."), which isn't the answer.
	var stderr bytes.Buffer
	c := exec.Command("go", "-C", abs, "list", "-f", "{{.Name}}\t{{.Module.Dir}}\t{{.Dir}}", ".")
	c.Stderr = &stderr
	out, err := c.Output()
	if err != nil {
		return nil, fmt.Errorf("%s: %s", dir, bytes.TrimSpace(stderr.Bytes()))
	}
	f := strings.Split(strings.TrimSpace(string(out)), "\t")
	if len(f) != 3 || f[0] != "main" {
		return nil, fmt.Errorf("%s is not a main package (it should call app.Run in main)", dir)
	}
	cfg, err := loadConfig(f[2])
	if err != nil {
		return nil, err
	}
	if err := cfg.generateCatalogs(); err != nil {
		return nil, err
	}
	beforeRebuild = func() {
		if err := cfg.generateCatalogs(); err != nil {
			fmt.Fprintln(os.Stderr, "asuka:", err)
		}
	}
	deps, _ := exec.Command("go", "-C", abs, "list", "-deps", ".").Output()
	cfg.permissionsFor(strings.Fields(string(deps)))
	warnCatalogsUnused(cfg, strings.Fields(string(deps)))
	warnDevLang(cfg)
	warnFrozenMessages(cfg, f[1])
	startDevVet(f[2])
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
func (t *iosToolchain) buildHost(work string, app *goApp, features []string) (string, error) {
	opt := []string{"-Onone", "-D", "DEBUG"}
	if t.release {
		// With debug information, for the app's dSYM (iosSymbols).
		opt = []string{"-O", "-g"}
	}
	h := sha256.New()
	fmt.Fprintln(h, t.sdk, t.target, t.release, features, opt)
	var files []string
	err := fs.WalkDir(iosHost.Files, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if strings.HasPrefix(path, "Share/") || strings.HasPrefix(path, "Widget/") {
			return nil // the extensions, built on their own (buildShare, buildWidgets)
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(files)
	shared := filepath.Join(filepath.Dir(filepath.Dir(work)), "ios-host") // shared by all apps
	// Its sources are written again for each build: one at a time.
	unlock, err := lockShared(shared)
	if err != nil {
		return "", err
	}
	defer unlock()
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
	fmt.Println("asuka: compiling the iOS host (once)")
	args := append([]string{"swiftc", "-target", t.target, "-sdk", t.sdk, "-swift-version", "5",
		"-module-name", "AsukaApp", "-parse-as-library", "-wmo"}, opt...)
	for _, f := range features {
		args = append(args, "-D", f)
	}
	args = append(args, "-I", filepath.Join(src, "Sources", "CAsuka", "include"), "-c", "-o", obj)
	if err := t.run(shared, nil, "xcrun", append(args, swift...)...); err != nil {
		return "", err
	}
	return obj, nil
}

// iosFeatures are the parts of the host that link a framework of their
// own (WebKit, AVKit...), and the Go function an app calls to use each.
// Loading a framework costs at launch whether or not it's used (WebKit
// and AVKit's together, about a second on the simulator), so a host is
// built with only those its app's Go code calls: the Go linker drops
// what isn't called, which leaves its symbol out of the archive (each is
// go:noinline, so a call keeps it).
var iosFeatures = []struct{ flag, symbol string }{
	{"ASUKA_WEBVIEW", "github.com/egoist/asuka/ui.WebView"},
	{"ASUKA_VIDEO", "github.com/egoist/asuka/ui.Video"},
	{"ASUKA_SHADER", "github.com/egoist/asuka/ui.Shader"},
	{"ASUKA_PHOTOS", "github.com/egoist/asuka/device.PickPhotos"},
	{"ASUKA_BROWSER", "github.com/egoist/asuka/device.Browse"},
	{"ASUKA_WEBAUTH", "github.com/egoist/asuka/webauth.Start"},
	{"ASUKA_LOCATION", "github.com/egoist/asuka/location.Current"},
	{"ASUKA_LOCATION", "github.com/egoist/asuka/location.Watch"},
	{"ASUKA_LOCATION", "github.com/egoist/asuka/location.PlaceName"},
	{"ASUKA_NOTIFY", "github.com/egoist/asuka/notify.Request"},
	{"ASUKA_NOTIFY", "github.com/egoist/asuka/notify.Schedule"},
	{"ASUKA_NOTIFY", "github.com/egoist/asuka/notify.Show"},
	{"ASUKA_NOTIFY", "github.com/egoist/asuka/notify.SetCategories"},
	{"ASUKA_NOTIFY", "github.com/egoist/asuka/notify.OnAction"},
	{"ASUKA_NOTIFY", "github.com/egoist/asuka/notify.Present"},
	{"ASUKA_NOTIFY", "github.com/egoist/asuka/notify.Showing"},
	{"ASUKA_NOTIFY", "github.com/egoist/asuka/notify.SetBadge"},
	{"ASUKA_NOTIFY", "github.com/egoist/asuka/notify.Remove"},
	{"ASUKA_NOTIFY", "github.com/egoist/asuka/notify.RemoveThread"},
	{"ASUKA_NOTIFY", "github.com/egoist/asuka/notify.RemoveAll"},
	{"ASUKA_NOTIFY", "github.com/egoist/asuka/notify.Cancel"},
	// Messages in conversations: Intents' INSendMessageIntent.
	{"ASUKA_CONVERSATIONS", "github.com/egoist/asuka/notify.Notification.From"},
	{"ASUKA_CONVERSATIONS", "github.com/egoist/asuka/push.(*Message).From"},
	// The Notification Service Extension's principal class.
	{"ASUKA_NOTIFICATION_SERVICE", rewriteSymbol},
	{"ASUKA_AUDIO", "github.com/egoist/asuka/audio.Load"},
	{"ASUKA_NOWPLAYING", "github.com/egoist/asuka/nowplaying.Show"},
	{"ASUKA_CAMERAVIEW", "github.com/egoist/asuka/camera.View"},
	{"ASUKA_SCAN", "github.com/egoist/asuka/scan.File"},
	{"ASUKA_VISION", "github.com/egoist/asuka/vision.Analyze"},
	{"ASUKA_PHOTOLIBRARY", "github.com/egoist/asuka/photos.Current"},
	{"ASUKA_PHOTOLIBRARY", "github.com/egoist/asuka/photos.Request"},
	{"ASUKA_PHOTOLIBRARY", "github.com/egoist/asuka/photos.ManageLimited"},
	{"ASUKA_PHOTOLIBRARY", "github.com/egoist/asuka/photos.List"},
	{"ASUKA_PHOTOLIBRARY", "github.com/egoist/asuka/photos.Thumbnail"},
	{"ASUKA_PHOTOLIBRARY", "github.com/egoist/asuka/photos.Export"},
	{"ASUKA_WEBVIEW", "github.com/egoist/asuka/js.Eval"},
	{"ASUKA_LIBRARY", "github.com/egoist/asuka/library.Save"},
	{"ASUKA_NETWORK", "github.com/egoist/asuka/network.Current"},
	{"ASUKA_NETWORK", "github.com/egoist/asuka/network.Watch"},
	{"ASUKA_RECORD", "github.com/egoist/asuka/camera/record.Start"},
	{"ASUKA_FILES", "github.com/egoist/asuka/device.PickFiles"},
	{"ASUKA_REVIEW", "github.com/egoist/asuka/review.Request"},
	{"ASUKA_TRACKING", "github.com/egoist/asuka/tracking.Request"},
	{"ASUKA_TRACKING", "github.com/egoist/asuka/tracking.Current"},
	{"ASUKA_BIOMETRIC", "github.com/egoist/asuka/biometric.Check"},
	{"ASUKA_BIOMETRIC", "github.com/egoist/asuka/biometric.Authenticate"},
	{"ASUKA_PUSH", "github.com/egoist/asuka/push.Register"},
	{"ASUKA_AUDIORECORD", "github.com/egoist/asuka/audio/record.Start"},
	{"ASUKA_TRANSCRIBE", "github.com/egoist/asuka/transcribe.File"},
	{"ASUKA_TRANSCRIBE", "github.com/egoist/asuka/transcribe.Check"},
	{"ASUKA_INTEGRITY", "github.com/egoist/asuka/integrity.Supported"},
	{"ASUKA_INTEGRITY", "github.com/egoist/asuka/integrity.GenerateKey"},
	{"ASUKA_INTEGRITY", "github.com/egoist/asuka/integrity.AttestKey"},
	{"ASUKA_INTEGRITY", "github.com/egoist/asuka/integrity.Assert"},
	{"ASUKA_AGERANGE", "github.com/egoist/asuka/agerange.Request"},
	{"ASUKA_MAPS", "github.com/egoist/asuka/maps.View"},
	{"ASUKA_PERIODIC", "github.com/egoist/asuka/periodic.Schedule"},
	{"ASUKA_WIDGET", "github.com/egoist/asuka/widget.Set"},
	{"ASUKA_CONTACTS", "github.com/egoist/asuka/contacts.Request"},
	{"ASUKA_CONTACTS", "github.com/egoist/asuka/contacts.List"},
	{"ASUKA_CONTACTS", "github.com/egoist/asuka/contacts.Pick"},
	{"ASUKA_CONTACTS", "github.com/egoist/asuka/contacts.Add"},
	{"ASUKA_CALENDAR", "github.com/egoist/asuka/calendar.Request"},
	{"ASUKA_CALENDAR", "github.com/egoist/asuka/calendar.Calendars"},
	{"ASUKA_CALENDAR", "github.com/egoist/asuka/calendar.Events"},
	{"ASUKA_CALENDAR", "github.com/egoist/asuka/calendar.Add"},
	{"ASUKA_SENSORS", "github.com/egoist/asuka/sensors.setRate"},
	{"ASUKA_SENSORS", "github.com/egoist/asuka/sensors.Available"},
	{"ASUKA_SENSORS", "github.com/egoist/asuka/sensors.StepsBetween"},
	{"ASUKA_SPEECH", "github.com/egoist/asuka/speech.Speak"},
	{"ASUKA_SPEECH", "github.com/egoist/asuka/speech.Voices"},
	{"ASUKA_IMAGEEDIT", "github.com/egoist/asuka/imageedit.Edit"},
	{"ASUKA_IMAGEEDIT", "github.com/egoist/asuka/imageedit.Size"},
	{"ASUKA_IMAGEEDIT", "github.com/egoist/asuka/imageedit.VideoFrame"},
	{"ASUKA_APPLEAUTH", "github.com/egoist/asuka/appleauth.SignIn"},
	{"ASUKA_APPLEAUTH", "github.com/egoist/asuka/appleauth.Available"},
	{"ASUKA_APPLEAUTH", "github.com/egoist/asuka/appleauth.CredentialState"},
	{"ASUKA_CRASH", "github.com/egoist/asuka/crash.Reports"},
	{"ASUKA_CRASH", "github.com/egoist/asuka/crash.OnReport"},
	{"ASUKA_PREVIEW", "github.com/egoist/asuka/preview.Show"},
	{"ASUKA_PRINT", "github.com/egoist/asuka/print.File"},
	{"ASUKA_PRINT", "github.com/egoist/asuka/print.HTML"},
	{"ASUKA_PRINT", "github.com/egoist/asuka/print.WebView"},
	{"ASUKA_PRINT", "github.com/egoist/asuka/print.PDF"},
	{"ASUKA_PRINTHTML", "github.com/egoist/asuka/print.HTML"},
	{"ASUKA_PRINTHTML", "github.com/egoist/asuka/print.PDF"},
	{"ASUKA_COMPOSE", "github.com/egoist/asuka/compose.Mail"},
	{"ASUKA_COMPOSE", "github.com/egoist/asuka/compose.Message"},
	{"ASUKA_COMPOSE", "github.com/egoist/asuka/compose.Check"},
}

// featureExtras are what a feature needs of the app beyond its framework:
// the entitlements (with a simulator build's values; a release's profile
// must have them) and Info.plist's arrays, as featurePermissions' are a
// package's.
var featureExtras = map[string]struct {
	entitlements map[string]any
	plistArrays  map[string][]string
}{
	// Communication notifications: the capability, and the intent the
	// app donates for each message.
	"ASUKA_CONVERSATIONS": {
		entitlements: map[string]any{"com.apple.developer.usernotifications.communication": true},
		plistArrays:  map[string][]string{"NSUserActivityTypes": {"INSendMessageIntent"}},
	},
	// Now Playing: playing on with the screen locked and the app away,
	// which the lock screen's controls need.
	"ASUKA_NOWPLAYING": {
		plistArrays: map[string][]string{"UIBackgroundModes": {"audio"}},
	},
}

// addFeatureExtras adds what the features flags need (featureExtras).
func (c *appConfig) addFeatureExtras(flags []string) {
	for _, f := range flags {
		x, ok := featureExtras[f]
		if !ok {
			continue
		}
		for k, v := range x.entitlements {
			if c.entitlements == nil {
				c.entitlements = map[string]any{}
			}
			c.entitlements[k] = v
		}
		for k, vs := range x.plistArrays {
			if c.plistArrays == nil {
				c.plistArrays = map[string][]string{}
			}
			for _, v := range vs {
				if !slices.Contains(c.plistArrays[k], v) {
					c.plistArrays[k] = append(c.plistArrays[k], v)
				}
			}
		}
	}
}

// allFeatures is every feature: the player runs any app.
func allFeatures() []string {
	var out []string
	for _, f := range iosFeatures {
		if !slices.Contains(out, f.flag) {
			out = append(out, f.flag)
		}
	}
	return out
}

// definedSymbols are the functions the Go archive lib defines (nil if it
// can't be read).
func definedSymbols(lib string) map[string]bool {
	out, err := exec.Command("xcrun", "nm", lib).Output()
	if err != nil {
		return nil
	}
	defined := map[string]bool{}
	for _, l := range strings.Split(string(out), "\n") {
		if f := strings.Fields(l); len(f) >= 3 && (f[1] == "t" || f[1] == "T") {
			defined[strings.TrimPrefix(f[2], "_")] = true
		}
	}
	return defined
}

// usedFeatures are the features the Go archive lib calls (all of them if
// it can't be read), and notes in cfg whether it has a share sheet or a
// push rewrite, and what its features need (addFeatureExtras).
func usedFeatures(lib string, cfg *appConfig) []string {
	defined := definedSymbols(lib)
	if defined == nil {
		all := allFeatures()
		cfg.addFeatureExtras(all)
		return all
	}
	cfg.shareSheet = defined[sheetSymbol]
	cfg.pushRewrite = defined[rewriteSymbol]
	var used []string
	for _, f := range iosFeatures {
		if defined[f.symbol] && !slices.Contains(used, f.flag) {
			used = append(used, f.flag)
		}
	}
	cfg.addFeatureExtras(used)
	return used
}

// writeOverlay writes a go build overlay that adds a file to the app's
// main package: it links the mobile bridge and hands it main.
func writeOverlay(app *goApp, work string) (string, error) {
	stub := filepath.Join(work, "zz_asuka_main.go")
	code := "package main\n\nimport \"github.com/egoist/asuka/mobile\"\n\nfunc init() { mobile.Main = main }\n"
	if err := os.WriteFile(stub, []byte(code), 0o644); err != nil {
		return "", err
	}
	overlay, _ := json.Marshal(map[string]map[string]string{"Replace": {filepath.Join(app.dir, "zz_asuka_main.go"): stub}})
	ovPath := filepath.Join(work, "overlay.json")
	return ovPath, os.WriteFile(ovPath, overlay, 0o644)
}

// buildGo builds the app's Go code as a c-archive.
func (t *iosToolchain) buildGo(app *goApp, work string) (string, error) {
	ovPath, err := writeOverlay(app, work)
	if err != nil {
		return "", err
	}
	lib := filepath.Join(work, "libasuka.a")
	flags := "-isysroot " + t.sdk + " -target " + t.target
	env := []string{"GOOS=ios", "GOARCH=" + t.goarch, "CGO_ENABLED=1", "CC=" + t.clang,
		"CGO_CFLAGS=" + flags, "CGO_LDFLAGS=" + flags}
	// A release keeps Go's symbols and debug information, for the dSYM:
	// the executable is stripped of them once it's made (iosSymbols).
	args := []string{"build", "-tags", "ios", "-buildmode=c-archive", "-overlay", ovPath, "-o", lib, app.cfg.ldflagsStrip()}
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
	ents, err := simulatorEntitlements(work, app.cfg)
	if err != nil {
		return "", err
	}
	code, err := t.linkApp(work, bundle, app.cfg, hostObj, lib, ents)
	if err != nil {
		return "", err
	}
	if err := t.embedPlugins(bundle, app); err != nil {
		return "", err
	}
	if err := copyDir(filepath.Join(app.dir, "assets"), filepath.Join(bundle, "assets")); err != nil {
		return "", err
	}
	urls := ""
	if t := app.cfg.urlTypes(true); t != nil {
		schemes := t[0].(map[string]any)["CFBundleURLSchemes"].([]string)
		for i := range schemes {
			schemes[i] = html.EscapeString(schemes[i])
		}
		urls = fmt.Sprintf("<key>CFBundleURLTypes</key><array><dict><key>CFBundleURLName</key><string>%s</string>"+
			"<key>CFBundleURLSchemes</key><array><string>%s</string></array></dict></array>\n",
			html.EscapeString(app.bundleID), strings.Join(schemes, "</string><string>"))
	}
	// The purpose strings: without one, iOS ends an app that asks for what it protects.
	keys := make([]string, 0, len(app.cfg.IOS.Usage))
	for k := range app.cfg.IOS.Usage {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		urls += fmt.Sprintf("<key>%s</key><string>%s</string>\n", html.EscapeString(k), html.EscapeString(app.cfg.IOS.Usage[k]))
	}
	for _, k := range app.cfg.iosTrue {
		urls += fmt.Sprintf("<key>%s</key><true/>\n", html.EscapeString(k))
	}
	urls += plistKeys(app.cfg.plistExtras())
	if g := app.cfg.appGroup(); g != "" {
		urls += fmt.Sprintf("<key>AsukaAppGroup</key><string>%s</string>\n", html.EscapeString(g))
	}
	if app.cfg.ShareTarget.on && app.cfg.shareSheet {
		urls += "<key>AsukaShareSheet</key><true/>\n"
	}
	if app.cfg.pushRewrite {
		urls += "<key>AsukaNotificationService</key><true/>\n"
	}
	devices := ""
	for _, d := range app.cfg.iosDevices() {
		devices += fmt.Sprintf("<integer>%d</integer>", d)
	}
	if o := app.cfg.ipadOrientations(); o != nil {
		urls += "<key>UISupportedInterfaceOrientations~ipad</key><array><string>" + strings.Join(o, "</string><string>") + "</string></array>\n"
	}
	urls += app.cfg.plistLocalizationKeys()
	plist := fmt.Sprintf(infoPlist, app.bundleID, app.displayName(), app.displayName(), iosMin, devices, urls)
	if err := os.WriteFile(filepath.Join(bundle, "Info.plist"), []byte(plist), 0o644); err != nil {
		return "", err
	}
	if err := app.cfg.writeLprojs(bundle); err != nil {
		return "", err
	}
	if app.cfg.splashOn() {
		if err := t.compileAssets(app.cfg, work, bundle, filepath.Join(bundle, "Info.plist"), false); err != nil {
			return "", err
		}
	}
	// Signed as Xcode signs for the simulator (simulatorIdentity).
	sign := simulatorIdentity(app.cfg.IOS.Team)
	if sign == "-" && len(app.cfg.Shortcuts) > 0 {
		fmt.Println("asuka: no Apple Development identity in the keychain: the app is signed ad hoc, and its shortcuts' App Intents won't run (Xcode › Settings › Accounts makes one)")
	}
	if app.cfg.ShareTarget.on {
		exe, err := t.buildShare(work, app.cfg, code)
		if err != nil {
			return "", err
		}
		appex, err := embedShare(bundle, exe, app.cfg.shareInfo(map[string]any{
			"CFBundleName": app.displayName(), "CFBundleDisplayName": app.displayName(), "CFBundleShortVersionString": "1.0", "CFBundleVersion": "1", "MinimumOSVersion": iosMin,
			"CFBundleSupportedPlatforms": []string{"iPhoneSimulator"}, "UIDeviceFamily": app.cfg.iosDevices(),
		}))
		if err != nil {
			return "", err
		}
		if err := t.run(work, nil, "codesign", "--force", "--sign", sign, appex); err != nil {
			return "", err
		}
	}
	if app.cfg.pushRewrite {
		exe, err := t.buildNotification(work, app.cfg, code)
		if err != nil {
			return "", err
		}
		appex, err := embedNotification(bundle, exe, app.cfg.notificationInfo(map[string]any{
			"CFBundleName": app.displayName(), "CFBundleDisplayName": app.displayName(), "CFBundleShortVersionString": "1.0", "CFBundleVersion": "1", "MinimumOSVersion": iosMin,
			"CFBundleSupportedPlatforms": []string{"iPhoneSimulator"}, "UIDeviceFamily": app.cfg.iosDevices(),
		}))
		if err != nil {
			return "", err
		}
		if err := t.run(work, nil, "codesign", "--force", "--sign", sign, appex); err != nil {
			return "", err
		}
	}
	if len(app.cfg.Widgets) > 0 {
		exe, err := t.buildWidgets(work, app.cfg)
		if err != nil {
			return "", err
		}
		appex, err := embedWidgets(bundle, exe, app.cfg.widgetInfo(map[string]any{
			"CFBundleName": app.displayName(), "CFBundleDisplayName": app.displayName(), "CFBundleShortVersionString": "1.0", "CFBundleVersion": "1", "MinimumOSVersion": iosMin,
			"CFBundleSupportedPlatforms": []string{"iPhoneSimulator"}, "UIDeviceFamily": app.cfg.iosDevices(),
		}))
		if err != nil {
			return "", err
		}
		if err := app.cfg.writeLprojs(appex); err != nil {
			return "", err
		}
		if err := t.run(work, nil, "codesign", "--force", "--sign", sign, appex); err != nil {
			return "", err
		}
	}
	if err := t.signFrameworks(work, bundle, sign); err != nil {
		return "", err
	}
	return bundle, t.run(work, nil, "codesign", "--force", "--sign", sign, bundle)
}

// simulatorEntitlements are the linker flags that give a simulator build
// the entitlements the keychain wants (an app identifier and its access
// group; without them SecItemAdd fails with errSecMissingEntitlement, and
// asuka/secure with it). As Xcode does: in the binary's __entitlements
// section, for an ad hoc signature can't carry them.
func simulatorEntitlements(work string, cfg *appConfig) ([]string, error) {
	id := "ASUKASIMULA." + cfg.ID // a team ID's shape: ten characters
	ent := filepath.Join(work, "simulator.entitlements")
	m := map[string]any{
		"application-identifier": id,
		"keychain-access-groups": []string{id},
	}
	if ds := cfg.associatedDomains(true); len(ds) > 0 {
		m["com.apple.developer.associated-domains"] = ds
	}
	if g := cfg.appGroup(); g != "" {
		m["com.apple.security.application-groups"] = []string{g}
	}
	for k, v := range cfg.entitlements {
		m[k] = v
	}
	text := plist(m)
	if err := os.WriteFile(ent, []byte(text), 0o644); err != nil {
		return nil, err
	}
	return []string{"-Xlinker", "-sectcreate", "-Xlinker", "__TEXT", "-Xlinker", "__entitlements", "-Xlinker", ent}, nil
}

const infoPlist = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>CFBundleIdentifier</key><string>%s</string>
<key>CFBundleExecutable</key><string>AsukaApp</string>
<key>CFBundleName</key><string>%s</string>
<key>CFBundleDisplayName</key><string>%s</string>
<key>CFBundlePackageType</key><string>APPL</string>
<key>CFBundleShortVersionString</key><string>1.0</string>
<key>CFBundleVersion</key><string>1</string>
<key>MinimumOSVersion</key><string>%s</string>
<key>CFBundleSupportedPlatforms</key><array><string>iPhoneSimulator</string></array>
<key>UIDeviceFamily</key><array>%s</array>
<key>CADisableMinimumFrameDurationOnPhone</key><true/>
<key>NSAppTransportSecurity</key><dict><key>NSAllowsLocalNetworking</key><true/></dict>
<key>UIApplicationSceneManifest</key><dict><key>UIApplicationSupportsMultipleScenes</key><false/></dict>
<key>UISupportedInterfaceOrientations</key><array><string>UIInterfaceOrientationPortrait</string><string>UIInterfaceOrientationLandscapeLeft</string><string>UIInterfaceOrientationLandscapeRight</string></array>
%s</dict></plist>
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
		fmt.Printf("asuka: booting %s\n", pick.Name)
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
	dir := filepath.Join(base, "asuka", app.name+"-"+hex.EncodeToString(sum[:])[:8], platform)
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
	fmt.Printf("asuka: %s on %s (%s)\n", app.name, sim.Name, sim.UDID)
	if usePlayer(app, o, "ios") {
		return devIOSPlayer(app, tc, work, sim, stop)
	}

	var logs *exec.Cmd
	build := func() error {
		start := time.Now()
		lib, err := tc.buildGo(app, work)
		if err != nil {
			return err
		}
		hostObj, err := tc.buildHost(work, app, usedFeatures(lib, app.cfg))
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
		fmt.Printf("asuka: built and installed in %.1fs\n", time.Since(start).Seconds())
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
		fmt.Fprintln(os.Stderr, "asuka:", err)
	}
	if o.once {
		return nil
	}
	fmt.Println("asuka: watching for changes (Ctrl-C to stop)")
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
			fmt.Println("asuka: change detected, rebuilding")
			if err := build(); err != nil {
				fmt.Fprintln(os.Stderr, "asuka:", err)
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
