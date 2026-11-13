package main

import (
	"os"
	"path/filepath"
	"strconv"
)

// An app with a share sheet (app.ShareSheet) or a push rewrite
// (push.Rewrite) runs its Go code and the host in more than one process:
// the app's, and its share extension's or its Notification Service
// Extension's. All are in one dynamic framework, Frameworks/AsukaApp.framework, as Xcode
// shares code between an app and its extensions: the app's executable is
// a main that starts the host's app delegate, the extension's links the
// framework and starts at NSExtensionMain. Go's runtime starts as the
// framework loads, in each. An app without either is one executable.

const (
	fwName = "AsukaApp" // the framework, and its binary
	appExe = "AsukaApp" // the app's executable (Info.plist's CFBundleExecutable)
)

// sharedLayout reports whether cfg's app is built with the framework.
func (c *appConfig) sharedLayout() bool { return c.ShareTarget.on && c.shareSheet || c.pushRewrite }

// appMain is the app's executable with the framework: UIApplicationMain
// with the host's delegate (App/AppDelegate.swift, in the framework).
const appMain = `import UIKit

// The app's code is in Frameworks/AsukaApp.framework, its share sheet's too.
_ = UIApplicationMain(CommandLine.argc, CommandLine.unsafeArgv, nil, "AsukaAppDelegate")
`

// linkApp links the app's code into bundle: the executable, or with a
// share sheet the framework and a main. It returns the binary that has
// the code (for the privacy manifest; the share extension links it).
// ents are the executable's linker flags for its entitlements (the
// simulator's).
func (t *iosToolchain) linkApp(work, bundle string, cfg *appConfig, hostObj, lib string, ents []string) (code string, err error) {
	strip := []string{}
	if t.release {
		strip = []string{"-Xlinker", "-dead_strip"}
	}
	exe := filepath.Join(bundle, appExe)
	// The shortcuts' App Intents: in the executable, their metadata in the bundle.
	intents, src, consts, err := t.buildShortcuts(work, cfg)
	if err != nil {
		return "", err
	}
	var extra []string
	if intents != "" {
		extra = append(extra, intents)
		defer func() {
			if err == nil {
				err = t.shortcutsMetadata(work, bundle, src, consts)
			}
		}()
	}
	if !cfg.sharedLayout() {
		args := append([]string{"swiftc", "-target", t.target, "-sdk", t.sdk, hostObj, lib,
			"-o", exe, "-framework", "UIKit", "-framework", "Foundation", "-lresolv"}, strip...)
		args = append(args, extra...)
		return exe, t.run(work, nil, "xcrun", append(args, ents...)...)
	}
	fw := filepath.Join(bundle, "Frameworks", fwName+".framework")
	if err := os.MkdirAll(fw, 0o755); err != nil {
		return "", err
	}
	bin := filepath.Join(fw, fwName)
	// Extension-safe, as a framework an extension links must be (it
	// links only frameworks that are).
	args := append([]string{"swiftc", "-emit-library", "-target", t.target, "-sdk", t.sdk, hostObj, lib,
		"-o", bin, "-framework", "UIKit", "-framework", "Foundation", "-lresolv",
		"-Xlinker", "-install_name", "-Xlinker", "@rpath/" + fwName + ".framework/" + fwName,
		"-Xlinker", "-application_extension"}, strip...)
	if err := t.run(work, nil, "xcrun", args...); err != nil {
		return "", err
	}
	if err := os.Chmod(bin, 0o755); err != nil {
		return "", err
	}
	platform := "iPhoneOS"
	if !t.device() {
		platform = "iPhoneSimulator"
	}
	info := plist(map[string]any{
		"CFBundleIdentifier":            cfg.ID + "." + fwName,
		"CFBundleExecutable":            fwName,
		"CFBundleName":                  fwName,
		"CFBundlePackageType":           "FMWK",
		"CFBundleInfoDictionaryVersion": "6.0",
		"CFBundleDevelopmentRegion":     "en",
		"CFBundleShortVersionString":    firstOf(cfg.Version, "1.0"),
		"CFBundleVersion":               strconv.Itoa(max(cfg.Build, 1)),
		"CFBundleSupportedPlatforms":    []string{platform},
		"MinimumOSVersion":              iosMin,
	})
	if err := os.WriteFile(filepath.Join(fw, "Info.plist"), []byte(info), 0o644); err != nil {
		return "", err
	}
	main := filepath.Join(work, "main.swift")
	if err := os.WriteFile(main, []byte(appMain), 0o644); err != nil {
		return "", err
	}
	args = append([]string{"swiftc", "-target", t.target, "-sdk", t.sdk, main, bin, "-o", exe,
		"-Xlinker", "-rpath", "-Xlinker", "@executable_path/Frameworks"}, strip...)
	args = append(args, extra...)
	return bin, t.run(work, nil, "xcrun", append(args, ents...)...)
}

// extensionStub is the only source of an extension's executable that
// links the framework: its code is the framework's.
const extensionStub = "// The extension's code is the app's framework (Frameworks/AsukaApp.framework).\n"

// linkSheet links the share extension's executable with the framework
// (bin), returning its path.
func (t *iosToolchain) linkSheet(work, bin string, ents []string) (string, error) {
	return t.linkExtension(work, bin, shareExe, ents)
}

// linkExtension links an extension's executable, name, with the
// framework (bin), returning its path.
func (t *iosToolchain) linkExtension(work, bin, name string, ents []string) (string, error) {
	stub := filepath.Join(work, name+"Main.swift")
	if err := os.WriteFile(stub, []byte(extensionStub), 0o644); err != nil {
		return "", err
	}
	exe := filepath.Join(work, name)
	args := []string{"swiftc", "-target", t.target, "-sdk", t.sdk, "-parse-as-library", "-application-extension", stub, bin, "-o", exe,
		"-Xlinker", "-e", "-Xlinker", "_NSExtensionMain", "-Xlinker", "-application_extension",
		"-Xlinker", "-rpath", "-Xlinker", "@executable_path/../../Frameworks",
		"-framework", "UIKit", "-framework", "Foundation"}
	return exe, t.run(work, nil, "xcrun", append(args, ents...)...)
}

// signFrameworks signs the app's frameworks (its code's, its plugins'), if
// it has any, before the app (whose signature seals them): identity "-"
// ad hoc.
func (t *iosToolchain) signFrameworks(work, bundle, identity string) error {
	fws, _ := filepath.Glob(filepath.Join(bundle, "Frameworks", "*.framework"))
	for _, fw := range fws {
		if err := t.run(work, nil, "codesign", "--force", "--sign", identity, fw); err != nil {
			return err
		}
	}
	return nil
}

func (t *iosToolchain) device() bool { return t.sdkName == "iphoneos" }
