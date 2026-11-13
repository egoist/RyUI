package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	iosHost "github.com/egoist/asuka/platform/ios"
)

// An app embedded in another (asuka build --platform ios --xcframework): its Go code
// and the UIKit host in AsukaApp.xcframework, a framework for devices and
// one for the simulator, with a Swift module interface, so an app made in
// Xcode adds it and shows the Asuka app where it likes:
//
//	import AsukaApp
//	present(AsukaViewController(), animated: true)
//
// The host's own app delegate isn't in it: the app around it is the
// other one. Assets go in the framework, where the host finds them. An
// app's plugins' libraries come beside it, an XCFramework each
// (ios_plugins.go), which the app around it embeds too: the Go code loads
// them from that app's Frameworks.

const embedModule = "AsukaApp"

func buildIOSFramework(app *goApp, o *buildOptions) error {
	start := time.Now()
	cfg := app.cfg
	var slices []string
	pluginSlices := map[string][]string{} // by library
	for _, sdk := range []string{"iphoneos", "iphonesimulator"} {
		tc, err := newIOSToolchainFor(sdk, o.verbose)
		if err != nil {
			return err
		}
		tc.release = true
		work, err := workDir(app, "ios-framework-"+sdk)
		if err != nil {
			return err
		}
		fmt.Printf("asuka: building %s %s as a framework for %s\n", cfg.Name, cfg.Version, map[bool]string{true: "devices", false: "the simulator"}[sdk == "iphoneos"])
		lib, err := tc.buildGo(app, work)
		if err != nil {
			return err
		}
		fw, err := tc.buildEmbedded(work, app, lib, usedFeatures(lib, cfg))
		if err != nil {
			return err
		}
		if err := copyDir(filepath.Join(app.dir, "assets"), filepath.Join(fw, "assets")); err != nil {
			return err
		}
		slices = append(slices, fw)
		// The plugins' libraries, which the Go code loads from the app's
		// Frameworks: a framework each, for the app to embed too.
		libs, err := tc.pluginLibs(app)
		if err != nil {
			return err
		}
		for _, l := range libs {
			pfw, err := tc.writePluginFramework(filepath.Join(work, "plugins"), l)
			if err != nil {
				return err
			}
			pluginSlices[l.lib.Name] = append(pluginSlices[l.lib.Name], pfw)
		}
	}
	tc := &iosToolchain{verbose: o.verbose}
	out := filepath.Join(o.out, embedModule+".xcframework")
	if err := tc.createXCFramework(o.out, out, slices); err != nil {
		return err
	}
	var extra []string
	var names []string
	for name := range pluginSlices {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		pout := filepath.Join(o.out, name+".xcframework")
		if err := tc.createXCFramework(o.out, pout, pluginSlices[name]); err != nil {
			return err
		}
		extra = append(extra, pout)
	}
	fmt.Printf("asuka: %s in %s\n", out, time.Since(start).Round(time.Second))
	if len(extra) > 0 {
		fmt.Printf("asuka: beside it, the libraries of its plugins, which the app's Go code loads: %s\n", strings.Join(extra, ", "))
		fmt.Println("asuka: add them all to your Xcode target (Embed & Sign), then: import AsukaApp; present(AsukaViewController(), animated: true)")
		return nil
	}
	fmt.Println("asuka: add it to your Xcode target (Embed & Sign), then: import AsukaApp; present(AsukaViewController(), animated: true)")
	return nil
}

// createXCFramework makes the XCFramework out of the frameworks.
func (t *iosToolchain) createXCFramework(dir, out string, frameworks []string) error {
	os.RemoveAll(out)
	args := []string{"xcodebuild", "-create-xcframework"}
	for _, fw := range frameworks {
		args = append(args, "-framework", fw)
	}
	return t.run(dir, nil, "xcrun", append(args, "-output", out)...)
}

// buildEmbedded compiles the host (without its app delegate) and links it
// with the app's Go archive into work/AsukaApp.framework, its module and
// module interface in Modules/.
func (t *iosToolchain) buildEmbedded(work string, app *goApp, lib string, features []string) (string, error) {
	src := filepath.Join(work, "host")
	os.RemoveAll(src)
	h := sha256.New()
	var swift []string
	err := fs.WalkDir(iosHost.Files, "Sources", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, _ := iosHost.Files.ReadFile(path)
		h.Write(data)
		dst := filepath.Join(src, path)
		os.MkdirAll(filepath.Dir(dst), 0o755)
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return err
		}
		if strings.HasSuffix(path, ".swift") {
			swift = append(swift, dst)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	own, _ := filepath.Glob(filepath.Join(app.dir, "ios", "*.swift"))
	sort.Strings(own)
	swift = append(swift, own...)
	fw := filepath.Join(work, embedModule+".framework")
	os.RemoveAll(fw)
	mods := filepath.Join(fw, "Modules", embedModule+".swiftmodule")
	if err := os.MkdirAll(mods, 0o755); err != nil {
		return "", err
	}
	triple := strings.Replace(t.target, "16.0", "", 1) // arm64-apple-ios, arm64-apple-ios-simulator
	bin := filepath.Join(fw, embedModule)
	args := []string{"swiftc", "-target", t.target, "-sdk", t.sdk, "-swift-version", "5",
		"-module-name", embedModule, "-parse-as-library", "-wmo", "-O", "-g",
		"-emit-library", "-emit-module", "-enable-library-evolution",
		"-emit-module-path", filepath.Join(mods, triple+".swiftmodule"),
		"-emit-module-interface-path", filepath.Join(mods, triple+".swiftinterface"),
		"-I", filepath.Join(src, "Sources", "CAsuka", "include"),
		"-o", bin, lib, "-framework", "UIKit", "-framework", "Foundation", "-lresolv",
		"-Xlinker", "-install_name", "-Xlinker", "@rpath/" + embedModule + ".framework/" + embedModule}
	for _, f := range features {
		args = append(args, "-D", f)
	}
	fmt.Println("asuka: compiling the iOS host as a framework")
	if err := t.run(work, nil, "xcrun", append(args, swift...)...); err != nil {
		return "", err
	}
	platform := "iPhoneOS"
	if !t.device() {
		platform = "iPhoneSimulator"
	}
	cfg := app.cfg
	info := plist(map[string]any{
		"CFBundleIdentifier":            cfg.ID + "." + embedModule,
		"CFBundleExecutable":            embedModule,
		"CFBundleName":                  embedModule,
		"CFBundlePackageType":           "FMWK",
		"CFBundleInfoDictionaryVersion": "6.0",
		"CFBundleDevelopmentRegion":     "en",
		"CFBundleShortVersionString":    firstOf(cfg.Version, "1.0"),
		"CFBundleVersion":               strconv.Itoa(max(cfg.Build, 1)),
		"CFBundleSupportedPlatforms":    []string{platform},
		"MinimumOSVersion":              iosMin,
		"AsukaHostHash":                 hex.EncodeToString(h.Sum(nil))[:16],
	})
	return fw, os.WriteFile(filepath.Join(fw, "Info.plist"), []byte(info), 0o644)
}
