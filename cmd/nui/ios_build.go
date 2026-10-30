package main

import (
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// buildIOS makes a release build: an .ipa signed for distribution, or
// with -simulator a release .app for the simulator. No Xcode project:
// swiftc compiles the host (optimized), the Go archive is built for the
// device, actool compiles the icon, and the bundle is signed with the
// provisioning profile that fits.
func buildIOS(app *goApp, o *buildOptions) error {
	start := time.Now()
	cfg := app.cfg
	sdkName := "iphoneos"
	if o.simulator {
		sdkName = "iphonesimulator"
	}
	tc, err := newIOSToolchainFor(sdkName, o.verbose)
	if err != nil {
		return err
	}
	tc.release = true
	work, err := workDir(app, "ios-release-"+sdkName)
	if err != nil {
		return err
	}
	// Sign first: no point building for a profile that isn't there.
	var prof *profile
	if !o.simulator {
		if prof, err = pickProfile(cfg, o); err != nil {
			return err
		}
		fmt.Printf("nui: signing with %q (%s, team %s) as %s\n", prof.name, prof.kind, prof.team, prof.identityName)
	}
	fmt.Printf("nui: building %s %s (%d) for %s\n", cfg.Name, cfg.Version, cfg.Build, map[bool]string{true: "the simulator", false: "devices"}[o.simulator])
	hostObj, err := tc.buildHost(work, app)
	if err != nil {
		return err
	}
	lib, err := tc.buildGo(app, work)
	if err != nil {
		return err
	}
	bundle := filepath.Join(work, safeName(cfg.Name)+".app")
	os.RemoveAll(bundle)
	if err := os.MkdirAll(bundle, 0o755); err != nil {
		return err
	}
	args := []string{"swiftc", "-target", tc.target, "-sdk", tc.sdk, hostObj, lib,
		"-o", filepath.Join(bundle, "NuiApp"), "-framework", "UIKit", "-framework", "Foundation", "-lresolv",
		"-Xlinker", "-dead_strip"}
	if o.simulator {
		ents, err := simulatorEntitlements(work, cfg)
		if err != nil {
			return err
		}
		args = append(args, ents...)
	}
	if err := tc.run(work, nil, "xcrun", args...); err != nil {
		return err
	}
	if err := copyDir(filepath.Join(app.dir, "assets"), filepath.Join(bundle, "assets")); err != nil {
		return err
	}
	info, err := tc.infoPlist(cfg, o.simulator)
	if err != nil {
		return err
	}
	infoPath := filepath.Join(bundle, "Info.plist")
	if err := os.WriteFile(infoPath, []byte(plist(info)), 0o644); err != nil {
		return err
	}
	if cfg.Icon != "" {
		if err := tc.compileIcon(cfg, work, bundle, infoPath); err != nil {
			return err
		}
	} else {
		fmt.Println("nui: no icon in nui.json; the App Store needs one (1024×1024 PNG)")
	}
	if o.simulator {
		if err := tc.run(work, nil, "codesign", "--force", "--sign", "-", bundle); err != nil {
			return err
		}
		out := filepath.Join(o.out, filepath.Base(bundle))
		os.RemoveAll(out)
		if err := tc.run(work, nil, "ditto", bundle, out); err != nil {
			return err
		}
		fmt.Printf("nui: %s in %.0fs (install: xcrun simctl install booted %s)\n", out, time.Since(start).Seconds(), out)
		return nil
	}
	if err := prof.sign(tc, work, bundle, cfg); err != nil {
		return err
	}
	ipa := filepath.Join(o.out, safeName(cfg.Name)+".ipa")
	if err := packIPA(tc, work, bundle, ipa); err != nil {
		return err
	}
	fmt.Printf("nui: %s in %.0fs\n", ipa, time.Since(start).Seconds())
	if prof.kind == "app-store" {
		fmt.Println("nui: upload it with Transporter, or: xcrun altool --upload-app -f " + ipa + " -t ios --apiKey KEY --apiIssuer ISSUER")
	}
	return nil
}

func safeName(s string) string {
	s = regexp.MustCompile(`[/\\:*?"<>|]+`).ReplaceAllString(s, "-")
	if s == "" {
		return "App"
	}
	return s
}

func newIOSToolchainFor(sdkName string, verbose bool) (*iosToolchain, error) {
	if sdkName == "iphonesimulator" {
		return newIOSToolchain(verbose)
	}
	sdk, err := output("xcrun", "--sdk", sdkName, "--show-sdk-path")
	if err != nil {
		return nil, fmt.Errorf("no iOS SDK (install Xcode): %v", err)
	}
	clang, err := output("xcrun", "--sdk", sdkName, "--find", "clang")
	if err != nil {
		return nil, err
	}
	return &iosToolchain{sdk: sdk, sdkName: sdkName, clang: clang, target: "arm64-apple-ios" + iosMin, goarch: "arm64", verbose: verbose}, nil
}

// infoPlist is a release build's Info.plist, with the build keys App
// Store validation checks.
func (t *iosToolchain) infoPlist(cfg *appConfig, simulator bool) (map[string]any, error) {
	sdkVersion, err := output("xcrun", "--sdk", t.sdkName, "--show-sdk-version")
	if err != nil {
		return nil, err
	}
	sdkBuild, _ := output("xcrun", "--sdk", t.sdkName, "--show-sdk-build-version")
	xcode, xcodeBuild := xcodeVersion()
	platform := "iPhoneOS"
	if simulator {
		platform = "iPhoneSimulator"
	}
	var orient []string
	if cfg.allows("portrait") {
		orient = append(orient, "UIInterfaceOrientationPortrait")
	}
	if cfg.allows("landscape") {
		orient = append(orient, "UIInterfaceOrientationLandscapeLeft", "UIInterfaceOrientationLandscapeRight")
	}
	m := map[string]any{
		"CFBundleIdentifier":                   cfg.ID,
		"CFBundleExecutable":                   "NuiApp",
		"CFBundleName":                         cfg.Name,
		"CFBundleDisplayName":                  cfg.Name,
		"CFBundlePackageType":                  "APPL",
		"CFBundleShortVersionString":           cfg.Version,
		"CFBundleVersion":                      strconv.Itoa(cfg.Build),
		"CFBundleInfoDictionaryVersion":        "6.0",
		"CFBundleDevelopmentRegion":            "en",
		"CFBundleSupportedPlatforms":           []string{platform},
		"MinimumOSVersion":                     iosMin,
		"LSRequiresIPhoneOS":                   true,
		"UIDeviceFamily":                       []int{1},
		"UIRequiredDeviceCapabilities":         []string{"arm64"},
		"UILaunchScreen":                       map[string]any{},
		"CADisableMinimumFrameDurationOnPhone": true, // 120 Hz on ProMotion iPhones
		"UIApplicationSceneManifest":           map[string]any{"UIApplicationSupportsMultipleScenes": false},
		"UISupportedInterfaceOrientations":     orient,
		"ITSAppUsesNonExemptEncryption":        false,
		"DTPlatformName":                       t.sdkName,
		"DTPlatformVersion":                    sdkVersion,
		"DTSDKName":                            t.sdkName + sdkVersion,
		"DTSDKBuild":                           sdkBuild,
		"DTPlatformBuild":                      sdkBuild,
		"DTXcode":                              xcode,
		"DTXcodeBuild":                         xcodeBuild,
		"DTCompiler":                           "com.apple.compilers.llvm.clang.1_0",
	}
	if t := cfg.urlTypes(); t != nil {
		m["CFBundleURLTypes"] = t
	}
	for k, v := range cfg.IOS.Usage {
		m[k] = v
	}
	for k, v := range cfg.IOS.InfoPlist {
		m[k] = v
	}
	return m, nil
}

// xcodeVersion returns DTXcode (16.2 → "1620") and the build.
func xcodeVersion() (string, string) {
	out, err := output("xcodebuild", "-version")
	if err != nil {
		return "", ""
	}
	var ver, build string
	for _, l := range strings.Split(out, "\n") {
		if v, ok := strings.CutPrefix(l, "Xcode "); ok {
			ver = v
		}
		if b, ok := strings.CutPrefix(l, "Build version "); ok {
			build = b
		}
	}
	p := strings.Split(ver, ".")
	for len(p) < 3 {
		p = append(p, "0")
	}
	major, _ := strconv.Atoi(p[0])
	return fmt.Sprintf("%02d%s%s", major, p[1][:1], p[2][:1]), build
}

// compileIcon makes the app icon's asset catalog from nui.json's icon
// and compiles it into the bundle, merging the icon keys into Info.plist.
func (t *iosToolchain) compileIcon(cfg *appConfig, work, bundle, infoPath string) error {
	icon, err := loadIcon(cfg.Icon)
	if err != nil {
		return err
	}
	set := filepath.Join(work, "Assets.xcassets", "AppIcon.appiconset")
	os.RemoveAll(filepath.Dir(set))
	if err := writePNG(filepath.Join(set, "icon.png"), opaque(resize(icon, 1024))); err != nil {
		return err
	}
	contents := `{"images":[{"filename":"icon.png","idiom":"universal","platform":"ios","size":"1024x1024"}],"info":{"author":"nui","version":1}}`
	if err := os.WriteFile(filepath.Join(set, "Contents.json"), []byte(contents), 0o644); err != nil {
		return err
	}
	os.WriteFile(filepath.Join(filepath.Dir(set), "Contents.json"), []byte(`{"info":{"author":"nui","version":1}}`), 0o644)
	partial := filepath.Join(work, "icon-info.plist")
	if err := t.run(work, nil, "xcrun", "actool", "--compile", bundle, "--platform", t.sdkName,
		"--minimum-deployment-target", iosMin, "--app-icon", "AppIcon", "--target-device", "iphone",
		"--output-partial-info-plist", partial, "--output-format", "human-readable-text", filepath.Dir(set)); err != nil {
		return err
	}
	return t.run(work, nil, "/usr/libexec/PlistBuddy", "-c", "Merge "+partial, infoPath)
}

func packIPA(t *iosToolchain, work, bundle, ipa string) error {
	stage := filepath.Join(work, "ipa")
	os.RemoveAll(stage)
	payload := filepath.Join(stage, "Payload")
	if err := os.MkdirAll(payload, 0o755); err != nil {
		return err
	}
	if err := t.run(work, nil, "ditto", bundle, filepath.Join(payload, filepath.Base(bundle))); err != nil {
		return err
	}
	os.Remove(ipa)
	return t.run(stage, nil, "ditto", "-c", "-k", "--sequesterRsrc", "--keepParent", "Payload", ipa)
}

// Provisioning profiles

type profile struct {
	path, name, uuid, team, appID string
	kind                          string // app-store, ad-hoc, development, enterprise
	expires                       time.Time
	certs                         []string // SHA-1s of the certificates it allows
	identity                      string   // the SHA-1 of the keychain identity that signs with it
	identityName                  string
}

func profileDirs() []string {
	home, _ := os.UserHomeDir()
	return []string{
		filepath.Join(home, "Library", "Developer", "Xcode", "UserData", "Provisioning Profiles"),
		filepath.Join(home, "Library", "MobileDevice", "Provisioning Profiles"),
	}
}

// readProfile decodes a .mobileprovision (a signed plist).
func readProfile(path string) (*profile, error) {
	raw, err := exec.Command("security", "cms", "-D", "-i", path).Output()
	if err != nil {
		return nil, fmt.Errorf("%s: can't decode", path)
	}
	tmp, err := os.CreateTemp("", "nui-profile-*.plist")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmp.Name())
	tmp.Write(raw)
	tmp.Close()
	get := func(key string) string {
		out, _ := exec.Command("plutil", "-extract", key, "raw", "-o", "-", tmp.Name()).Output()
		return strings.TrimSpace(string(out))
	}
	p := &profile{path: path, name: get("Name"), uuid: get("UUID"), team: get("TeamIdentifier.0"), appID: get("Entitlements.application-identifier")}
	p.expires, _ = time.Parse(time.RFC3339, get("ExpirationDate"))
	switch {
	case get("ProvisionsAllDevices") == "true":
		p.kind = "enterprise"
	case get("Entitlements.get-task-allow") == "true":
		p.kind = "development"
	case get("ProvisionedDevices") != "":
		p.kind = "ad-hoc"
	default:
		p.kind = "app-store"
	}
	n, _ := strconv.Atoi(get("DeveloperCertificates"))
	for i := 0; i < n; i++ {
		der, err := base64.StdEncoding.DecodeString(get(fmt.Sprintf("DeveloperCertificates.%d", i)))
		if err == nil {
			sum := sha1.Sum(der)
			p.certs = append(p.certs, strings.ToUpper(hex.EncodeToString(sum[:])))
		}
	}
	return p, nil
}

// identities are the keychain's code signing identities, by SHA-1.
func identities() map[string]string {
	out, _ := exec.Command("security", "find-identity", "-v", "-p", "codesigning").Output()
	m := map[string]string{}
	re := regexp.MustCompile(`\d+\) ([0-9A-F]{40}) "(.+)"`)
	for _, l := range strings.Split(string(out), "\n") {
		if s := re.FindStringSubmatch(l); s != nil {
			m[s[1]] = s[2]
		}
	}
	return m
}

// matches reports how well appID (TEAM.id or TEAM.prefix.*) fits id:
// 0 not at all, more for a closer fit.
func (p *profile) matches(team, id string) int {
	pt, pid, ok := strings.Cut(p.appID, ".")
	if !ok || (team != "" && pt != team) {
		return 0
	}
	if pid == id {
		return 1000
	}
	if prefix, ok := strings.CutSuffix(pid, "*"); ok && strings.HasPrefix(id, prefix) {
		return 1 + len(prefix)
	}
	return 0
}

// pickProfile finds the provisioning profile to sign with: the one
// asked for, else the closest unexpired match for the app's ID and the
// distribution whose certificate is in the keychain.
func pickProfile(cfg *appConfig, o *buildOptions) (*profile, error) {
	ids := identities()
	want := o.profile
	if want == "" {
		want = cfg.IOS.Profile
	}
	var all []*profile
	for _, d := range profileDirs() {
		files, _ := filepath.Glob(filepath.Join(d, "*.mobileprovision"))
		for _, f := range files {
			if p, err := readProfile(f); err == nil {
				all = append(all, p)
			}
		}
	}
	if want != "" && strings.HasSuffix(want, ".mobileprovision") {
		p, err := readProfile(want)
		if err != nil {
			return nil, err
		}
		all = []*profile{p}
		want = ""
	}
	var best *profile
	bestScore := 0
	var near []string
	for _, p := range all {
		if want != "" && p.name != want && p.uuid != want {
			continue
		}
		score := p.matches(cfg.IOS.Team, cfg.ID)
		if score == 0 {
			continue
		}
		if p.kind != o.distribution && want == "" {
			near = append(near, fmt.Sprintf("%q (%s)", p.name, p.kind))
			continue
		}
		if p.expires.Before(time.Now()) {
			near = append(near, fmt.Sprintf("%q (expired %s)", p.name, p.expires.Format("2006-01-02")))
			continue
		}
		for _, c := range p.certs {
			if name, ok := ids[c]; ok {
				p.identity, p.identityName = c, name // the SHA-1: names can be ambiguous
				if score > bestScore {
					best, bestScore = p, score
				}
				break
			}
		}
		if p.identity == "" {
			near = append(near, fmt.Sprintf("%q (its certificate isn't in the keychain)", p.name))
		}
	}
	if best != nil {
		return best, nil
	}
	msg := fmt.Sprintf("no %s provisioning profile for %s", o.distribution, cfg.ID)
	if len(near) > 0 {
		sort.Strings(near)
		msg += "; close ones: " + strings.Join(near, ", ")
	}
	return nil, errors.New(msg + ". Make one for the app ID in Xcode (Settings › Accounts) or at developer.apple.com, or pass -profile")
}

// sign embeds the profile, signs the bundle with its entitlements and
// checks the signature.
func (p *profile) sign(t *iosToolchain, work, bundle string, cfg *appConfig) error {
	id := cfg.ID
	data, err := os.ReadFile(p.path)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(bundle, "embedded.mobileprovision"), data, 0o644); err != nil {
		return err
	}
	decoded := filepath.Join(work, "profile.plist")
	raw, err := exec.Command("security", "cms", "-D", "-i", p.path).Output()
	if err != nil {
		return err
	}
	os.WriteFile(decoded, raw, 0o644)
	ent := filepath.Join(work, "entitlements.plist")
	if err := t.run(work, nil, "plutil", "-extract", "Entitlements", "xml1", "-o", ent, decoded); err != nil {
		return err
	}
	// A wildcard profile's app ID becomes the app's.
	if err := t.run(work, nil, "plutil", "-replace", "application-identifier", "-string", p.team+"."+id, ent); err != nil {
		return err
	}
	// Universal links: the domains, which the profile must allow (the
	// Associated Domains capability of the app ID).
	if ds := cfg.associatedDomains(false); len(ds) > 0 {
		js, _ := json.Marshal(ds)
		if err := t.run(work, nil, "plutil", "-replace", "com.apple.developer.associated-domains", "-json", string(js), ent); err != nil {
			return err
		}
	}
	if err := t.run(work, nil, "codesign", "--force", "--sign", p.identity, "--entitlements", ent,
		"--generate-entitlement-der", bundle); err != nil {
		return err
	}
	return t.run(work, nil, "codesign", "--verify", "--deep", "--strict", bundle)
}
