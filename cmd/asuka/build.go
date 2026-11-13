package main

import (
	"errors"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	iupdate "github.com/egoist/asuka/internal/update"
)

type buildOptions struct {
	platform     string
	sign         string // macOS: a signing identity (default: ad hoc)
	notarize     string // macOS: notarytool's keychain profile ("env": ASUKA_APPLE_ID...)
	simulator    bool   // iOS: a release build for the simulator, unsigned
	distribution string // iOS: app-store, ad-hoc, development
	profile      string // iOS and macOS: a provisioning profile (path, name or UUID)
	appStore     bool   // macOS: a .pkg for the Mac App Store
	out          string
	xcframework  bool   // iOS: an .xcframework to embed in another app
	aar          bool   // Android: an .aar to embed in another app
	arch         string // Linux and Windows: amd64 or arm64
	verbose      bool
	noVet        bool
	version      string // asuka.json's version, for this build
	publishing   bool   // asuka publish: the update files are required
}

func buildFlags(o *buildOptions) *flagSet {
	fs := newFlagSet("build", "[dir]")
	fs.about = "Makes a release build of the app, in dist in its directory."
	platformFlag(fs, &o.platform, []string{platformIOS, platformAndroid, platformMac, platformWindows, platformLinux, platformWeb},
		"the `platform` to build for: ios, an .ipa for the App Store (or --distribution); android, an .aab for Google Play; mac, a universal .app (arm64 and x86_64); windows, a folder with the .exe (its icon and version built in) and its assets; linux, a folder in dist/linux laid out to install (the executable, its desktop file, icons, metainfo and assets), a .tar.gz of it and a Flatpak manifest; web, a static site (WebAssembly and the web host) in dist/web. Default on macOS, Windows and Linux: this computer's")
	fs.String(&o.sign, "sign", 0, "", "macOS: a signing `identity`, e.g. \"Developer ID Application: ...\" (default: ad hoc)")
	fs.String(&o.notarize, "notarize", 0, "", "macOS: notarize the signed app and staple the ticket: a notarytool keychain `profile` (xcrun notarytool store-credentials), or \"env\" for ASUKA_APPLE_ID, ASUKA_APPLE_PASSWORD (app-specific) and ASUKA_TEAM_ID")
	fs.Bool(&o.simulator, "simulator", 0, "iOS: a release .app for the simulator instead (not signed for devices)")
	fs.Bool(&o.xcframework, "xcframework", 0, "iOS: AsukaApp.xcframework, the app to embed in an app made in Xcode (import AsukaApp; AsukaViewController())")
	fs.String(&o.arch, "arch", 0, "", "Linux: amd64 or arm64 (default: this machine's, else amd64); Windows: amd64 or arm64 (default: amd64, which runs on Windows on Arm too)")
	fs.Bool(&o.aar, "aar", 0, "Android: an .aar, the app to embed in an app made in Android Studio (start dev.asuka.AsukaActivity)")
	fs.String(&o.distribution, "distribution", 0, "app-store", "iOS: app-store, ad-hoc or development")
	fs.String(&o.profile, "profile", 0, "", "iOS and macOS: a provisioning `profile`'s path, name or UUID (default: asuka.json's (macOS: or ASUKA_MAC_PROFILE), else the best match installed; macOS: only for an app whose packages need one: push, Sign in with Apple, App Attest)")
	fs.Bool(&o.appStore, "appstore", 0, "macOS: a .pkg for the Mac App Store: the app sandboxed, signed with a Mac App Store profile's certificate (Apple Distribution), the .pkg with Mac Installer Distribution")
	fs.String(&o.out, "out", 'o', "", "the output `directory` (default: dist in the app's directory)")
	fs.Bool(&o.noVet, "no-vet", 0, "don't run asuka vet first (by default it prints what it finds, as warnings: the build goes on)")
	fs.String(&o.version, "version", 0, "", "the `version` to build, in place of asuka.json's (as CI takes it from the tag: 1.2.0, 1.3.0-beta.1)")
	fs.Bool(&o.verbose, "verbose", 'v', "print the commands asuka runs")
	return fs
}

// setVersion makes --version the app's version for the build.
func (o *buildOptions) setVersion(app *goApp) error {
	if o.version == "" {
		return nil
	}
	v := strings.TrimPrefix(o.version, "v")
	if !iupdate.ValidVersion(v) {
		return fmt.Errorf("--version %q isn't a version (1.2.0, 1.3.0-beta.1)", o.version)
	}
	app.cfg.Version = v
	return nil
}

func cmdBuild(args []string) error {
	var o buildOptions
	fs := buildFlags(&o)
	if err := fs.parse(args); err != nil {
		return err
	}
	dir := "."
	if fs.Arg(0) != "" {
		dir = fs.Arg(0)
	}
	if o.platform == "" {
		if o.platform = thisComputer(); o.platform == "" {
			return errors.New("usage: asuka build --platform ios|android|mac|windows|linux|web [flags] [dir]")
		}
	}
	app, err := loadApp(dir)
	if err != nil {
		return err
	}
	if err := o.setVersion(app); err != nil {
		return err
	}
	vetBuild(app.dir, !o.noVet)
	if o.out == "" {
		o.out = filepath.Join(app.dir, "dist")
	}
	if err := os.MkdirAll(o.out, 0o755); err != nil {
		return err
	}
	switch o.platform {
	case platformIOS:
		if o.xcframework {
			return buildIOSFramework(app, &o)
		}
		return buildIOS(app, &o)
	case platformMac:
		return buildMac(app, &o)
	case platformWindows:
		return buildWindows(app, &o)
	case platformLinux:
		return buildLinux(app, &o)
	case platformWeb:
		return buildWeb(app, &o)
	}
	if o.aar {
		return buildAndroidAAR(app, &o)
	}
	return buildAndroid(app, &o)
}

// plist writes an XML property list of strings, bools, ints, string
// lists and nested dicts.
func plist(m map[string]any) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
`)
	writePlist(&b, m, 0)
	b.WriteString("</plist>\n")
	return b.String()
}

func writePlist(b *strings.Builder, v any, depth int) {
	ind := strings.Repeat("\t", depth)
	switch x := v.(type) {
	case map[string]any:
		b.WriteString(ind + "<dict>\n")
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(b, "%s\t<key>%s</key>\n", ind, html.EscapeString(k))
			writePlist(b, x[k], depth+1)
		}
		b.WriteString(ind + "</dict>\n")
	case []string:
		b.WriteString(ind + "<array>\n")
		for _, s := range x {
			fmt.Fprintf(b, "%s\t<string>%s</string>\n", ind, html.EscapeString(s))
		}
		b.WriteString(ind + "</array>\n")
	case []any:
		b.WriteString(ind + "<array>\n")
		for _, e := range x {
			writePlist(b, e, depth+1)
		}
		b.WriteString(ind + "</array>\n")
	case []int:
		b.WriteString(ind + "<array>\n")
		for _, n := range x {
			fmt.Fprintf(b, "%s\t<integer>%d</integer>\n", ind, n)
		}
		b.WriteString(ind + "</array>\n")
	case bool:
		if x {
			b.WriteString(ind + "<true/>\n")
		} else {
			b.WriteString(ind + "<false/>\n")
		}
	case int:
		fmt.Fprintf(b, "%s<integer>%d</integer>\n", ind, x)
	default:
		fmt.Fprintf(b, "%s<string>%s</string>\n", ind, html.EscapeString(fmt.Sprint(x)))
	}
}

// goTags is go build's -tags flag for tags, with those GOFLAGS sets: a
// -tags on the command line replaces GOFLAGS' own.
func goTags(goflags string, tags ...string) string {
	var all []string
	for _, f := range strings.Fields(goflags) {
		f = strings.TrimPrefix(f, "-")
		if v, ok := strings.CutPrefix(f, "-tags="); ok {
			f = "tags=" + v
		}
		if v, ok := strings.CutPrefix(f, "tags="); ok {
			all = append(all, strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ' ' })...)
		}
	}
	for _, t := range tags {
		if !slices.Contains(all, t) {
			all = append(all, t)
		}
	}
	return "-tags=" + strings.Join(all, ",")
}
