package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	iosHost "github.com/egoist/ryui/platform/ios"
)

// The share extension (ryui.json's shareTarget): platform/ios/Share, a
// binary of its own in the app's PlugIns. It runs no Go: it opens the
// app with what was shared in a URL of the app's share scheme, which the
// host turns into the Shared event (ui.OnShare), as Android's
// ACTION_SEND opens the app. Files go through the App Group the two
// share (shareGroup): the extension copies them there, the app moves
// them to its caches.

const shareExe = "RyUIShare"

// shareID is the extension's bundle ID, which a device build needs a
// provisioning profile for, as the app's.
func (c *appConfig) shareID() string { return c.ID + ".share" }

// shareScheme is the URL scheme the extension opens the app with.
func (c *appConfig) shareScheme() string { return "ryuishare." + c.ID }

// shareEntitlements are the linker flags that give the extension its
// entitlements on the simulator, as the app's (simulatorEntitlements):
// the App Group it leaves files in, and with a share sheet, the app's
// keychain group (ryui/secure's items are the app's there).
func (t *iosToolchain) shareEntitlements(work string, cfg *appConfig) ([]string, string, error) {
	g := cfg.shareGroup()
	if g == "" || !strings.HasSuffix(t.target, "-simulator") {
		return nil, "", nil
	}
	m := map[string]any{
		"application-identifier":                "RYUISIMULA." + cfg.shareID(),
		"com.apple.security.application-groups": []string{g},
	}
	if cfg.shareSheet {
		m["keychain-access-groups"] = []string{"RYUISIMULA." + cfg.ID}
	}
	ent := plist(m)
	path := filepath.Join(work, "share.entitlements")
	if err := os.WriteFile(path, []byte(ent), 0o644); err != nil {
		return nil, "", err
	}
	return []string{"-Xlinker", "-sectcreate", "-Xlinker", "__TEXT", "-Xlinker", "__entitlements", "-Xlinker", path}, ent, nil
}

// buildShare makes the extension's executable for cfg's app. With a share
// sheet (app.ShareSheet), it links the app's framework (fw: the host and
// the app's Go code), whose RyUIShareSheetController is its principal
// class. Without, it's platform/ios/Share, which runs no Go, cached (by
// its sources, the SDK and the flags) beside the host's builds, shared by
// the apps that build the same.
func (t *iosToolchain) buildShare(work string, cfg *appConfig, fw string) (string, error) {
	ents, ent, err := t.shareEntitlements(work, cfg)
	if err != nil {
		return "", err
	}
	// An extension's entry point is NSExtensionMain, which runs the
	// principal class Info.plist names; it's linked as Xcode links one.
	entry := []string{"-Xlinker", "-e", "-Xlinker", "_NSExtensionMain", "-Xlinker", "-application_extension",
		"-framework", "UIKit", "-framework", "Foundation"}
	if cfg.sharedLayout() {
		return t.linkSheet(work, fw, ents)
	}
	opt := []string{"-Onone", "-D", "DEBUG"}
	if t.release {
		opt = []string{"-O", "-Xlinker", "-dead_strip"}
	}
	// With the extension-safe APIs only.
	args := append([]string{"swiftc", "-target", t.target, "-sdk", t.sdk, "-swift-version", "5",
		"-module-name", shareExe, "-parse-as-library", "-application-extension"}, opt...)
	args = append(append(args, entry...), ents...)
	h := sha256.New()
	h.Write([]byte(ent))
	fmt.Fprintln(h, args)
	// Its sources, and the collector it shares with the host's sheet.
	files := []string{"Sources/RyUIHost/ShareCollect.swift"}
	err = fs.WalkDir(iosHost.Files, "Share", func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(path, ".swift") {
			files = append(files, path)
		}
		return err
	})
	if err != nil {
		return "", err
	}
	sort.Strings(files)
	srcs := make([][]byte, len(files))
	for i, f := range files {
		srcs[i], _ = iosHost.Files.ReadFile(f)
		h.Write([]byte(f))
		h.Write(srcs[i])
	}
	dir := filepath.Join(filepath.Dir(filepath.Dir(work)), "ios-share", hex.EncodeToString(h.Sum(nil))[:16])
	exe := filepath.Join(dir, shareExe)
	if exists(exe) {
		return exe, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	args = append(args, "-o", exe+".tmp")
	for i, f := range files {
		p := filepath.Join(dir, filepath.Base(f))
		if err := os.WriteFile(p, srcs[i], 0o644); err != nil {
			return "", err
		}
		args = append(args, p)
	}
	if err := t.run(dir, nil, "xcrun", args...); err != nil {
		return "", err
	}
	return exe, os.Rename(exe+".tmp", exe)
}

// sharePrincipal is the extension's principal class: the host's sheet,
// running the app's Go, or the one that opens the app.
func (c *appConfig) sharePrincipal() string {
	if c.shareSheet {
		return "RyUIShareSheetController"
	}
	return "RyUIShareViewController"
}

// sheetSymbol is app.ShareSheet's: the app's Go calls it (its share
// target shows a sheet) if the archive has it.
const sheetSymbol = "github.com/egoist/ryui/app.ShareSheet"

// shareInfo is the extension's Info.plist. It takes the app's name,
// version, platform and build keys from app (the app's Info.plist),
// which must match; it shows in share sheets as the app, with its icon. It
// takes what shareTarget says (activationRule): by default text and web
// links, which is what apps share most (a page from a browser, a video
// from YouTube), as Android's text/plain filter.
func (c *appConfig) shareInfo(app map[string]any) map[string]any {
	m := map[string]any{
		"CFBundleIdentifier":            c.shareID(),
		"CFBundleExecutable":            shareExe,
		"CFBundlePackageType":           "XPC!",
		"CFBundleInfoDictionaryVersion": "6.0",
		"CFBundleDevelopmentRegion":     "en",
		"RyUIShareScheme":               c.shareScheme(),
		"NSExtension": map[string]any{
			"NSExtensionPointIdentifier": "com.apple.share-services",
			"NSExtensionPrincipalClass":  c.sharePrincipal(),
			"NSExtensionAttributes": map[string]any{
				"NSExtensionActivationRule": c.ShareTarget.activationRule(),
			},
		},
	}
	if g := c.shareGroup(); g != "" {
		m["RyUIShareGroup"] = g
	}
	if c.shareSheet {
		m["RyUIShareSheet"] = true
		m["RyUIAppID"] = c.ID
	}
	for k, v := range app {
		switch k {
		case "CFBundleName", "CFBundleDisplayName", "CFBundleShortVersionString", "CFBundleVersion", "MinimumOSVersion", "CFBundleSupportedPlatforms", "UIDeviceFamily":
			m[k] = v
		default:
			if strings.HasPrefix(k, "DT") {
				m[k] = v
			}
		}
	}
	return m
}

// embedShare puts the extension (its executable exe and Info.plist info)
// in bundle's PlugIns, returning its path. It's signed before the app,
// whose signature seals it.
func embedShare(bundle, exe string, info map[string]any) (string, error) {
	appex := filepath.Join(bundle, "PlugIns", shareExe+".appex")
	if err := os.MkdirAll(appex, 0o755); err != nil {
		return "", err
	}
	data, err := os.ReadFile(exe)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(appex, shareExe), data, 0o755); err != nil {
		return "", err
	}
	return appex, os.WriteFile(filepath.Join(appex, "Info.plist"), []byte(plist(info)), 0o644)
}
