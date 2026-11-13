package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// Plugins on iOS (plugins.go): each library a framework of its own in
// the app's Frameworks, <name>.framework, as the App Store takes a
// dynamic library only in a framework: the device's or the simulator's
// file by the build, its Info.plist, and its privacy manifest (the
// reasons for the APIs its binary uses, which App Store Connect checks
// each binary for), signed with the app before it (signFrameworks); the
// app doesn't link it. An extension (PlugIns/<name>.appex) that runs the
// app's Go code finds the app's. An app embedded in another
// (--xcframework) comes with an XCFramework of each, which the app
// around it embeds too.

// pluginTarget is the plugin target of the toolchain's SDK.
func (t *iosToolchain) pluginTarget() string {
	if t.device() {
		return "ios"
	}
	return "ios-simulator"
}

// pluginLibs are the libraries of the plugins app uses, for the
// toolchain's SDK.
func (t *iosToolchain) pluginLibs(app *goApp) ([]pluginLib, error) {
	goarch := t.goarch
	if goarch == "" {
		goarch = "arm64"
	}
	dir, pkg := app.goPackage()
	ps, err := pkgPlugins(dir, pkg, "ios", goarch)
	if err != nil {
		return nil, err
	}
	return pluginLibs(ps, t.pluginTarget())
}

// pluginFrameworkInfo is a plugin library's framework's Info.plist, for
// platform (iPhoneOS, iPhoneSimulator).
func pluginFrameworkInfo(l pluginLib, platform string) map[string]any {
	version := l.lib.Version
	if version == "" {
		version = "1.0"
	}
	return map[string]any{
		"CFBundleIdentifier":            "dev.asuka.plugin." + l.lib.Name,
		"CFBundleExecutable":            l.lib.Name,
		"CFBundleName":                  l.lib.Name,
		"CFBundlePackageType":           "FMWK",
		"CFBundleInfoDictionaryVersion": "6.0",
		"CFBundleDevelopmentRegion":     "en",
		"CFBundleShortVersionString":    version,
		"CFBundleVersion":               version,
		"CFBundleSupportedPlatforms":    []string{platform},
		"MinimumOSVersion":              iosMin, // the libraries' (as plugins build them)
	}
}

// writePluginFramework writes l's framework in dir: <name>.framework,
// with its binary for the toolchain's SDK, its Info.plist and its
// privacy manifest. It returns the framework's path.
func (t *iosToolchain) writePluginFramework(dir string, l pluginLib) (string, error) {
	if l.file.Name != l.lib.Name {
		return "", fmt.Errorf("plugin %s: library %s's iOS file is %s: it must be named for the library, its framework's binary", l.m.Name, l.lib.Name, l.file.Name)
	}
	fw := filepath.Join(dir, l.lib.Name+".framework")
	os.RemoveAll(fw)
	bin := filepath.Join(fw, l.lib.Name)
	if err := l.write(bin); err != nil {
		return "", err
	}
	platform := "iPhoneOS"
	if !t.device() {
		platform = "iPhoneSimulator"
	}
	if err := os.WriteFile(filepath.Join(fw, "Info.plist"), []byte(plist(pluginFrameworkInfo(l, platform))), 0o644); err != nil {
		return "", err
	}
	manifest, err := libraryPrivacy(bin)
	if err != nil {
		return "", err
	}
	return fw, os.WriteFile(filepath.Join(fw, "PrivacyInfo.xcprivacy"), manifest, 0o644)
}

// embedPlugins puts the frameworks of the plugins app uses in bundle's
// Frameworks, with those of its Swift packages, and their resource
// bundles in it; signFrameworks signs the frameworks.
func (t *iosToolchain) embedPlugins(bundle string, app *goApp) error {
	if err := t.packages.embed(filepath.Join(bundle, "Frameworks"), bundle); err != nil {
		return err
	}
	libs, err := t.pluginLibs(app)
	if err != nil {
		return err
	}
	for _, l := range libs {
		if _, err := t.writePluginFramework(filepath.Join(bundle, "Frameworks"), l); err != nil {
			return err
		}
	}
	return nil
}
