package main

import (
	"errors"
	"flag"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type buildOptions struct {
	platform     string
	sign         string // macOS: a signing identity (default: ad hoc)
	notarize     string // macOS: notarytool's keychain profile ("env": RYUI_APPLE_ID...)
	simulator    bool   // iOS: a release build for the simulator, unsigned
	distribution string // iOS: app-store, ad-hoc, development
	profile      string // iOS and macOS: a provisioning profile (path, name or UUID)
	appStore     bool   // macOS: a .pkg for the Mac App Store
	out          string
	xcframework  bool   // iOS: an .xcframework to embed in another app
	aar          bool   // Android: an .aar to embed in another app
	arch         string // Linux and Windows: amd64 or arm64
	verbose      bool
	vet          bool
}

func buildFlags(o *buildOptions) *flag.FlagSet {
	fs := flag.NewFlagSet("build", flag.ContinueOnError)
	platformFlag(fs, &o.platform, []string{platformIOS, platformAndroid, platformMac, platformWindows, platformLinux, platformWeb},
		"the `platform` to build for: ios, an .ipa for the App Store (or -distribution); android, an .aab for Google Play; mac, a universal .app (arm64 and x86_64); windows, a folder with the .exe (its icon and version built in) and its assets; linux, a folder in dist/linux laid out to install (the executable, its desktop file, icons, metainfo and assets), a .tar.gz of it and a Flatpak manifest; web, a static site (WebAssembly and the web host) in dist/web. Default on macOS, Windows and Linux: this computer's")
	fs.StringVar(&o.sign, "sign", "", "macOS: a signing identity, e.g. \"Developer ID Application: ...\" (default: ad hoc)")
	fs.StringVar(&o.notarize, "notarize", "", "macOS: notarize the signed app and staple the ticket: a notarytool keychain profile (xcrun notarytool store-credentials), or \"env\" for RYUI_APPLE_ID, RYUI_APPLE_PASSWORD (app-specific) and RYUI_TEAM_ID")
	fs.BoolVar(&o.simulator, "simulator", false, "iOS: a release .app for the simulator instead (not signed for devices)")
	fs.BoolVar(&o.xcframework, "xcframework", false, "iOS: RyUIApp.xcframework, the app to embed in an app made in Xcode (import RyUIApp; RyUIViewController())")
	fs.StringVar(&o.arch, "arch", "", "Linux: amd64 or arm64 (default: this machine's, else amd64); Windows: amd64 or arm64 (default: amd64, which runs on Windows on Arm too)")
	fs.BoolVar(&o.aar, "aar", false, "Android: an .aar, the app to embed in an app made in Android Studio (start dev.ryui.RyUIActivity)")
	fs.StringVar(&o.distribution, "distribution", "app-store", "iOS: app-store, ad-hoc or development")
	fs.StringVar(&o.profile, "profile", "", "iOS and macOS: a provisioning profile's path, name or UUID (default: ryui.json's (macOS: or RYUI_MAC_PROFILE), else the best match installed; macOS: only for an app whose packages need one: push, Sign in with Apple, App Attest)")
	fs.BoolVar(&o.appStore, "appstore", false, "macOS: a .pkg for the Mac App Store: the app sandboxed, signed with a Mac App Store profile's certificate (Apple Distribution), the .pkg with Mac Installer Distribution")
	fs.StringVar(&o.out, "o", "", "the output directory (default: dist in the app's directory)")
	fs.BoolVar(&o.verbose, "v", false, "print the commands ryui runs")
	fs.BoolVar(&o.vet, "vet", true, "run ryui vet first and print what it finds (warnings: the build goes on)")
	return fs
}

func cmdBuild(args []string) error {
	var o buildOptions
	fs := buildFlags(&o)
	if err := fs.Parse(args); err != nil {
		return err
	}
	dir := "."
	if fs.NArg() > 0 {
		dir = fs.Arg(0)
	}
	if o.platform == "" {
		if o.platform = thisComputer(); o.platform == "" {
			return errors.New("usage: ryui build -platform ios|android|mac|windows|linux|web [flags] [dir]")
		}
	}
	app, err := loadApp(dir)
	if err != nil {
		return err
	}
	vetBuild(app.dir, o.vet)
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
