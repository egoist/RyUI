package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// macOS: the app's Go code and its host (host/darwin, AppKit through
// purego) are one Go program, built for this Mac with CGO_ENABLED=0.
//
// ryui dev -platform mac runs it here, from an .app like a release's (its
// Info.plist, icon and languages, signed ad hoc, answering for its own
// privacy prompts), and on each change rebuilds it and runs it again (its
// signals' state kept, as with the player). ryui build -platform mac
// makes an .app: a universal binary (arm64 and x86_64), its Info.plist,
// icon and assets, signed (ad hoc unless -sign names an identity), with
// its provisioning profile (Contents/embedded.provisionprofile) and the
// entitlements the app's packages need of it (mac_profile.go); with
// -appstore, sandboxed, in a .pkg for the Mac App Store.

const macMin = "11.0"

// macLdflags is the -ldflags of a Mac build: ryui.json's, and the app's
// name (its menus and window title).
func macLdflags(app *goApp, release bool) string {
	f := strings.TrimPrefix(app.cfg.ldflags(release), "-ldflags=")
	name := app.displayName()
	return "-ldflags=" + strings.TrimSpace(f+" -X "+strconv.Quote("github.com/egoist/ryui/internal/boot.Name="+name))
}

func devMac(dir string, o *devOptions, stop <-chan os.Signal) error {
	app, err := loadApp(dir)
	if err != nil {
		return err
	}
	work, err := workDir(app, "mac")
	if err != nil {
		return err
	}
	fmt.Printf("ryui: %s on this Mac\n", app.name)
	start := time.Now()
	// The app runs from an .app, as built: its Info.plist (the bundle's
	// identifier, which notifications and the Dock need; its URL schemes,
	// purpose strings and languages) and its icon. Its assets stay where
	// they are, read from the app's directory, so changing one needs no
	// rebuild.
	run := macRunner(o.verbose)
	bundle := filepath.Join(work, app.displayName()+".app")
	exe, err := writeMacBundle(app, bundle, work, false, run)
	if err != nil {
		return err
	}
	proc := &hostProcess{app: app, work: work, flags: macLdflags(app, false), env: []string{
		"RYUI_STATE=" + filepath.Join(work, "state.json"),
		"RYUI_DATA=" + filepath.Join(work, "data"),
		"RYUI_ASSETS=" + filepath.Join(app.dir, "assets"),
	}}
	proc.bin = filepath.Join(bundle, "Contents", "MacOS", exe)
	// Signed as a whole, so that the bundle's identity is the app's.
	ents, err := writeMacEntitlements(app, work)
	if err != nil {
		return err
	}
	proc.afterBuild = func() error { return signMac(bundle, "-", ents, run) }
	// It answers for itself, as when it's opened from the Finder: its
	// privacy prompts (camera, location...) are its own, with its purpose
	// strings, not the terminal's.
	proc.own = true
	os.MkdirAll(filepath.Join(work, "data"), 0o755)
	if err := proc.build(); err != nil {
		return err
	}
	if err := proc.restart(); err != nil {
		return err
	}
	fmt.Printf("ryui: built and started in %.1fs\n", time.Since(start).Seconds())
	if o.once {
		<-proc.done
		return nil
	}
	return watchHost(proc, stop, func() {})
}

// macRunner runs a tool for a Mac build, its output shown only when it
// fails (or with -v).
func macRunner(verbose bool) func(dir string, env []string, cmd string, args ...string) error {
	return func(dir string, env []string, cmd string, args ...string) error {
		if verbose {
			fmt.Println("+", cmd, strings.Join(args, " "))
		}
		c := exec.Command(cmd, args...)
		c.Dir = dir
		c.Env = append(os.Environ(), env...)
		out, err := c.CombinedOutput()
		if err != nil {
			return fmt.Errorf("%s failed: %v\n%s", cmd, err, out)
		}
		return nil
	}
}

// writeMacBundle makes the .app at bundle, all but its executable: its
// Info.plist, icon and languages, and for a release its assets. It
// returns the executable's name, in Contents/MacOS.
func writeMacBundle(app *goApp, bundle, work string, release bool, run func(dir string, env []string, cmd string, args ...string) error) (string, error) {
	name := app.displayName()
	os.RemoveAll(bundle)
	contents := filepath.Join(bundle, "Contents")
	res := filepath.Join(contents, "Resources")
	for _, d := range []string{filepath.Join(contents, "MacOS"), res} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return "", err
		}
	}
	exe := sanitizeExe(name)
	info := map[string]any{
		"CFBundleExecutable":                   exe,
		"CFBundleIdentifier":                   app.bundleID,
		"CFBundleName":                         name,
		"CFBundleDisplayName":                  name,
		"CFBundlePackageType":                  "APPL",
		"CFBundleShortVersionString":           app.cfg.Version,
		"CFBundleVersion":                      strconv.Itoa(app.cfg.Build),
		"CFBundleInfoDictionaryVersion":        "6.0",
		"LSMinimumSystemVersion":               macMin,
		"NSHighResolutionCapable":              true,
		"NSPrincipalClass":                     "NSApplication",
		"NSSupportsAutomaticGraphicsSwitching": true,
		"LSApplicationCategoryType":            "public.app-category.utilities",
	}
	if t := app.cfg.urlTypes(false); len(t) > 0 {
		info["CFBundleURLTypes"] = t
	}
	// The purpose strings are the same keys on macOS (camera, contacts...);
	// location's is NSLocationUsageDescription there.
	for k, v := range app.cfg.IOS.Usage {
		info[k] = v
	}
	if v := app.cfg.IOS.Usage["NSLocationWhenInUseUsageDescription"]; v != "" && info["NSLocationUsageDescription"] == nil {
		info["NSLocationUsageDescription"] = v
	}
	if app.cfg.Icon != "" {
		if err := macIcon(app.cfg.Icon, work, filepath.Join(res, "AppIcon.icns"), run); err != nil {
			return "", err
		}
		info["CFBundleIconFile"] = "AppIcon"
	}
	app.cfg.plistLocalizations(info)
	if err := app.cfg.writeLprojs(res); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(contents, "Info.plist"), []byte(plist(info)), 0o644); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(contents, "PkgInfo"), []byte("APPL????"), 0o644); err != nil {
		return "", err
	}
	if release {
		if err := copyDir(filepath.Join(app.dir, "assets"), filepath.Join(res, "assets")); err != nil {
			return "", err
		}
	}
	return exe, nil
}

// macResourceEntitlements are the hardened runtime's entitlements an
// app needs for what its purpose strings ask for: without them, an app
// signed with a Developer ID (the hardened runtime) is refused the
// camera, the microphone or the user's data, prompt or not.
var macResourceEntitlements = map[string]string{
	"NSCameraUsageDescription":              "com.apple.security.device.camera",
	"NSMicrophoneUsageDescription":          "com.apple.security.device.audio-input",
	"NSLocationWhenInUseUsageDescription":   "com.apple.security.personal-information.location",
	"NSContactsUsageDescription":            "com.apple.security.personal-information.addressbook",
	"NSCalendarsUsageDescription":           "com.apple.security.personal-information.calendars",
	"NSCalendarsFullAccessUsageDescription": "com.apple.security.personal-information.calendars",
	"NSPhotoLibraryUsageDescription":        "com.apple.security.personal-information.photos-library",
	"NSPhotoLibraryAddUsageDescription":     "com.apple.security.personal-information.photos-library",
}

// writeMacEntitlements writes the app's entitlements for its signature
// without a provisioning profile into work, and returns the file ("" if
// it needs none). The ones only a profile grants (push's aps-environment,
// Sign in with Apple, App Attest) aren't among them: signed with those
// and no profile, macOS won't open the app.
func writeMacEntitlements(app *goApp, work string) (string, error) {
	ents, err := macEntitlements(app.cfg, nil, false)
	if err != nil {
		return "", err
	}
	return writeEntitlementsFile(ents, work)
}

// writeEntitlementsFile writes ents into work, and returns the file (""
// for none).
func writeEntitlementsFile(ents map[string]any, work string) (string, error) {
	if len(ents) == 0 {
		return "", nil
	}
	f := filepath.Join(work, "entitlements.plist")
	return f, os.WriteFile(f, []byte(plist(ents)), 0o644)
}

// signMac signs the bundle with identity ("-": ad hoc) and entitlements
// (a file, or ""), and checks it.
func signMac(bundle, identity, entitlements string, run func(dir string, env []string, cmd string, args ...string) error) error {
	args := []string{"--force", "--sign", identity}
	if identity != "-" {
		args = append(args, "--options", "runtime", "--timestamp")
	}
	if entitlements != "" {
		args = append(args, "--entitlements", entitlements)
	}
	dir := filepath.Dir(bundle)
	if err := run(dir, nil, "codesign", append(args, bundle)...); err != nil {
		return err
	}
	return run(dir, nil, "codesign", "--verify", "--strict", bundle)
}

func buildMac(app *goApp, o *buildOptions) error {
	if o.appStore && o.notarize != "" {
		return errors.New("ryui: -notarize is for an app distributed outside the Mac App Store, which reviews the app itself: leave it out with -appstore")
	}
	work, err := workDir(app, "mac-release")
	if err != nil {
		return err
	}
	// The profile first: no point building for one that isn't there.
	prof, identity, err := macSigning(app.cfg, o)
	if err != nil {
		return err
	}
	ents, err := macEntitlements(app.cfg, prof, o.appStore)
	if err != nil {
		return err
	}
	run := macRunner(o.verbose)
	bundle := filepath.Join(o.out, app.displayName()+".app")
	exe, err := writeMacBundle(app, bundle, work, true, run)
	if err != nil {
		return err
	}

	// A universal binary: one per architecture, joined by lipo.
	var parts []string
	for _, arch := range []string{"arm64", "amd64"} {
		out := filepath.Join(work, "app-"+arch)
		env := []string{"GOOS=darwin", "GOARCH=" + arch, "CGO_ENABLED=0", "MACOSX_DEPLOYMENT_TARGET=" + macMin}
		if err := run(app.dir, env, "go", "build", "-trimpath", macLdflags(app, true), "-o", out, "."); err != nil {
			return err
		}
		parts = append(parts, out)
	}
	bin := filepath.Join(bundle, "Contents", "MacOS", exe)
	if err := run(work, nil, "lipo", append([]string{"-create", "-output", bin}, parts...)...); err != nil {
		return err
	}

	if prof != nil {
		data, err := os.ReadFile(prof.path)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(bundle, "Contents", "embedded.provisionprofile"), data, 0o644); err != nil {
			return err
		}
	}
	entsFile, err := writeEntitlementsFile(ents, work)
	if err != nil {
		return err
	}
	if err := signMac(bundle, identity, entsFile, run); err != nil {
		return err
	}
	if o.notarize != "" {
		if identity == "-" {
			return errors.New("ryui: -notarize needs -sign \"Developer ID Application: ...\": Apple notarizes only apps signed with a Developer ID")
		}
		if err := notarize(work, bundle, o.notarize, o.verbose); err != nil {
			return err
		}
	}
	fmt.Printf("ryui: %s\n", bundle)
	if o.appStore {
		pkg, err := macPackage(app, bundle, prof, identity, o.out, run)
		if err != nil {
			return err
		}
		fmt.Printf("ryui: %s (upload it with Transporter, or xcrun altool --upload-package)\n", pkg)
		return nil
	}
	if identity == "-" {
		if prof != nil {
			fmt.Println("ryui: signed ad hoc, with the profile's entitlements: macOS opens it only signed with a certificate the profile allows (-sign)")
		} else {
			fmt.Println("ryui: signed ad hoc; to distribute it, sign with -sign \"Developer ID Application: ...\" and -notarize")
		}
	}
	return nil
}

// macSigning is the profile (nil for none) and the identity ("-": ad
// hoc) a Mac build signs with: the profile -profile, RYUI_MAC_PROFILE or
// ryui.json names, else for an app whose packages need one (or for the
// Mac App Store) the best match installed; the identity -sign names, else
// the profile's, else ad hoc. Without a profile, it says what won't work.
func macSigning(cfg *appConfig, o *buildOptions) (*macProfile, string, error) {
	want := o.profile
	if want == "" {
		want = os.Getenv("RYUI_MAC_PROFILE")
	}
	if want == "" {
		want = cfg.Mac.Profile
		if o.appStore {
			want = cfg.Mac.AppStoreProfile
		}
	}
	needs := macProfileNeeds(cfg)
	if want == "" && !o.appStore && len(needs) == 0 {
		identity := o.sign
		if identity == "" {
			identity = "-" // ad hoc: runs on this Mac; others need a Developer ID and notarization
		}
		return nil, identity, nil
	}
	ids := identities()
	prof, err := pickMacProfile(cfg.IOS.Team, cfg.ID, want, o.appStore, ids)
	if err != nil {
		if want != "" || o.appStore {
			return nil, "", err
		}
		// None installed: the app builds, without what needs one.
		fmt.Printf("ryui: %s won't work on the Mac without a provisioning profile: %v (or set RYUI_MAC_PROFILE, or ryui.json's mac.profile)\n",
			strings.Join(needs, ", "), err)
		identity := o.sign
		if identity == "" {
			identity = "-"
		}
		return nil, identity, nil
	}
	identity, name, err := macIdentity(o.sign, prof, ids)
	if err != nil {
		return nil, "", err
	}
	note := ""
	if !prof.signed {
		note = ", not signed by Apple"
	}
	fmt.Printf("ryui: provisioning profile %q (%s, team %s%s), signing as %s\n", prof.name, prof.kind, prof.team, note, name)
	return prof, identity, nil
}

// macPackage makes the Mac App Store's .pkg of the signed app, signed
// with the team's Mac Installer Distribution identity (unsigned only for
// a profile that isn't Apple's, to look at).
func macPackage(app *goApp, bundle string, prof *macProfile, identity, out string, run func(dir string, env []string, cmd string, args ...string) error) (string, error) {
	pkg := filepath.Join(out, app.displayName()+".pkg")
	os.Remove(pkg)
	args := []string{"--component", bundle, "/Applications"}
	if prof.signed || identity != "-" {
		installer := installerIdentity(prof.team)
		if installer == "" {
			return "", fmt.Errorf("no Mac Installer Distribution identity for team %s in the keychain, which signs the .pkg: make one in Xcode (Settings › Accounts › Manage Certificates) or at developer.apple.com", prof.team)
		}
		args = append(args, "--sign", installer)
	} else {
		fmt.Println("ryui: the .pkg isn't signed (the profile isn't Apple's): App Store Connect won't take it")
	}
	if err := run(filepath.Dir(bundle), nil, "productbuild", append(args, pkg)...); err != nil {
		return "", err
	}
	return pkg, nil
}

// notarize sends the signed app to Apple's notary service, waits for its
// verdict, and staples the ticket to the app, so Gatekeeper opens it
// offline too. profile is notarytool's keychain profile, or "env" for an
// Apple ID's app-specific password from the environment (CI).
func notarize(work, bundle, profile string, verbose bool) error {
	zip := filepath.Join(work, "notarize.zip")
	os.Remove(zip)
	if out, err := exec.Command("ditto", "-c", "-k", "--keepParent", bundle, zip).CombinedOutput(); err != nil {
		return fmt.Errorf("ditto failed: %v\n%s", err, out)
	}
	defer os.Remove(zip)
	creds, err := notaryCredentials(profile, os.Getenv)
	if err != nil {
		return err
	}
	fmt.Println("ryui: notarizing (Apple's notary service; usually a few minutes)...")
	args := append([]string{"notarytool", "submit", zip, "--wait", "--output-format", "json"}, creds...)
	if verbose {
		fmt.Println("+ xcrun", strings.Join(redacted(args), " "))
	}
	out, err := exec.Command("xcrun", args...).Output()
	id, status := notaryResult(out)
	if err != nil && status == "" {
		var stderr []byte
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = ee.Stderr
		}
		return fmt.Errorf("xcrun notarytool submit failed: %v\n%s%s", err, out, stderr)
	}
	if status != "Accepted" {
		log, _ := exec.Command("xcrun", append([]string{"notarytool", "log", id}, creds...)...).CombinedOutput()
		return fmt.Errorf("ryui: notarization %s (submission %s):\n%s", strings.ToLower(status), id, log)
	}
	if out, err := exec.Command("xcrun", "stapler", "staple", bundle).CombinedOutput(); err != nil {
		return fmt.Errorf("xcrun stapler staple failed: %v\n%s", err, out)
	}
	fmt.Println("ryui: notarized, and the ticket stapled")
	return nil
}

// notaryCredentials are notarytool's credential flags for -notarize.
func notaryCredentials(profile string, getenv func(string) string) ([]string, error) {
	if profile != "env" {
		return []string{"--keychain-profile", profile}, nil
	}
	id, pw, team := getenv("RYUI_APPLE_ID"), getenv("RYUI_APPLE_PASSWORD"), getenv("RYUI_TEAM_ID")
	if id == "" || pw == "" || team == "" {
		return nil, errors.New("ryui: -notarize env needs RYUI_APPLE_ID, RYUI_APPLE_PASSWORD (an app-specific password) and RYUI_TEAM_ID")
	}
	return []string{"--apple-id", id, "--password", pw, "--team-id", team}, nil
}

// redacted is args with the password after --password hidden.
func redacted(args []string) []string {
	out := append([]string(nil), args...)
	for i := range out {
		if out[i] == "--password" && i+1 < len(out) {
			out[i+1] = "***"
		}
	}
	return out
}

// notaryResult reads notarytool's JSON: the submission's id and status
// (Accepted, Invalid, Rejected...).
func notaryResult(out []byte) (id, status string) {
	var r struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if i := strings.IndexByte(string(out), '{'); i >= 0 {
		json.Unmarshal(out[i:], &r)
	}
	return r.ID, r.Status
}

// sanitizeExe is the executable's name: the app's, without slashes.
func sanitizeExe(name string) string {
	s := strings.Map(func(r rune) rune {
		if r == '/' || r == ':' {
			return '-'
		}
		return r
	}, name)
	if s == "" {
		return "app"
	}
	return s
}

// macIcon makes the .icns from a square icon: macOS's shape, a rounded
// square inset in the canvas (824 of 1024 points), at each size iconutil
// wants.
func macIcon(src, work, out string, run func(dir string, env []string, cmd string, args ...string) error) error {
	img, err := loadIcon(src)
	if err != nil {
		return err
	}
	shaped := macShape(resize(img, 824))
	canvas := newNRGBA(1024)
	for y := range 824 {
		for x := range 824 {
			canvas.SetNRGBA(x+100, y+100, shaped.NRGBAAt(x, y))
		}
	}
	set := filepath.Join(work, "AppIcon.iconset")
	os.RemoveAll(set)
	for _, s := range []int{16, 32, 128, 256, 512} {
		if err := writePNG(filepath.Join(set, fmt.Sprintf("icon_%dx%d.png", s, s)), resize(canvas, s)); err != nil {
			return err
		}
		if err := writePNG(filepath.Join(set, fmt.Sprintf("icon_%dx%d@2x.png", s, s)), resize(canvas, s*2)); err != nil {
			return err
		}
	}
	return run(work, nil, "iconutil", "-c", "icns", "-o", out, set)
}

// macShape masks an icon to a rounded square (radius 22.5% of its side),
// antialiased.
func macShape(src *image.NRGBA) *image.NRGBA {
	n := src.Bounds().Dx()
	r := float64(n) * 0.225
	dst := image.NewNRGBA(src.Bounds())
	for y := range n {
		for x := range n {
			// Coverage of the pixel's center by the rounded square.
			cx, cy := float64(x)+0.5, float64(y)+0.5
			dx := math.Max(math.Max(r-cx, cx-(float64(n)-r)), 0)
			dy := math.Max(math.Max(r-cy, cy-(float64(n)-r)), 0)
			d := math.Hypot(dx, dy) - r
			cov := math.Min(math.Max(0.5-d, 0), 1)
			if dx == 0 || dy == 0 {
				cov = 1
			}
			c := src.NRGBAAt(x, y)
			c.A = uint8(float64(c.A) * cov)
			dst.SetNRGBA(x, y, color.NRGBA{c.R, c.G, c.B, c.A})
		}
	}
	return dst
}
