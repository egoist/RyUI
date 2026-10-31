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

	iosHost "github.com/egoist/ryui/hosts/ios"
)

// The share extension (ryui.json's shareTarget): hosts/ios/Share, a
// binary of its own in the app's PlugIns. It runs no Go: it opens the
// app with what was shared in a URL of the app's share scheme, which the
// host turns into the Shared event (ui.OnShare), as Android's
// ACTION_SEND opens the app.

const shareExe = "RyUIShare"

// shareID is the extension's bundle ID, which a device build needs a
// provisioning profile for, as the app's.
func (c *appConfig) shareID() string { return c.ID + ".share" }

// shareScheme is the URL scheme the extension opens the app with.
func (c *appConfig) shareScheme() string { return "ryuishare." + c.ID }

// buildShare compiles and links the extension's executable, cached (by
// its sources, the SDK and the flags) beside the host's builds, shared
// by all apps.
func (t *iosToolchain) buildShare(work string) (string, error) {
	opt := []string{"-Onone", "-D", "DEBUG"}
	if t.release {
		opt = []string{"-O", "-Xlinker", "-dead_strip"}
	}
	// An extension's entry point is NSExtensionMain, which runs the
	// principal class Info.plist names; it's linked as Xcode links one,
	// with the extension-safe APIs only.
	args := append([]string{"swiftc", "-target", t.target, "-sdk", t.sdk, "-swift-version", "5",
		"-module-name", shareExe, "-parse-as-library", "-application-extension"}, opt...)
	args = append(args, "-Xlinker", "-e", "-Xlinker", "_NSExtensionMain", "-Xlinker", "-application_extension",
		"-framework", "UIKit", "-framework", "Foundation")
	h := sha256.New()
	fmt.Fprintln(h, args)
	var files []string
	err := fs.WalkDir(iosHost.Files, "Share", func(path string, d fs.DirEntry, err error) error {
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

// shareInfo is the extension's Info.plist. It takes the app's name,
// version, platform and build keys from app (the app's Info.plist),
// which must match; it shows in share sheets as the app, with its icon. It
// takes text and web links, which is what apps share most (a page from
// a browser, a video from YouTube), as Android's text/plain filter.
func (c *appConfig) shareInfo(app map[string]any) map[string]any {
	m := map[string]any{
		"CFBundleIdentifier":            c.shareID(),
		"CFBundleExecutable":            shareExe,
		"CFBundlePackageType":           "XPC!",
		"CFBundleInfoDictionaryVersion": "6.0",
		"CFBundleDevelopmentRegion":     "en",
		"RyUIShareScheme":                c.shareScheme(),
		"NSExtension": map[string]any{
			"NSExtensionPointIdentifier": "com.apple.share-services",
			"NSExtensionPrincipalClass":  "RyUIShareViewController",
			"NSExtensionAttributes": map[string]any{
				"NSExtensionActivationRule": map[string]any{
					"NSExtensionActivationSupportsText":               true,
					"NSExtensionActivationSupportsWebURLWithMaxCount": 1,
				},
			},
		},
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
