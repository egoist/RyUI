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
	ios, android bool
	mac          bool
	windows      bool
	sign         string // macOS: a signing identity (default: ad hoc)
	simulator    bool   // iOS: a release build for the simulator, unsigned
	distribution string // iOS: app-store, ad-hoc, development
	profile      string // iOS: a provisioning profile (path, name or UUID)
	out          string
	verbose      bool
}

func buildFlags(o *buildOptions) *flag.FlagSet {
	fs := flag.NewFlagSet("build", flag.ContinueOnError)
	fs.BoolVar(&o.ios, "ios", false, "build an .ipa for the App Store (or -distribution)")
	fs.BoolVar(&o.android, "android", false, "build an .aab for Google Play, and an .apk")
	fs.BoolVar(&o.mac, "mac", false, "build a macOS .app (universal: arm64 and x86_64)")
	fs.BoolVar(&o.windows, "windows", false, "build a Windows app: a folder with the .exe (its icon and version built in) and its assets")
	fs.StringVar(&o.sign, "sign", "", "macOS: a signing identity, e.g. \"Developer ID Application: ...\" (default: ad hoc)")
	fs.BoolVar(&o.simulator, "simulator", false, "iOS: a release .app for the simulator instead (not signed for devices)")
	fs.StringVar(&o.distribution, "distribution", "app-store", "iOS: app-store, ad-hoc or development")
	fs.StringVar(&o.profile, "profile", "", "iOS: a provisioning profile's path, name or UUID (default: ryui.json's, else the best match installed)")
	fs.StringVar(&o.out, "o", "", "the output directory (default: dist in the app's directory)")
	fs.BoolVar(&o.verbose, "v", false, "print the commands ryui runs")
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
	if n := btoi(o.ios) + btoi(o.android) + btoi(o.mac) + btoi(o.windows); n != 1 {
		return errors.New("usage: ryui build -ios|-android|-mac|-windows [flags] [dir]")
	}
	app, err := loadApp(dir)
	if err != nil {
		return err
	}
	if o.out == "" {
		o.out = filepath.Join(app.dir, "dist")
	}
	if err := os.MkdirAll(o.out, 0o755); err != nil {
		return err
	}
	if o.ios {
		return buildIOS(app, &o)
	}
	if o.mac {
		return buildMac(app, &o)
	}
	if o.windows {
		return buildWindows(app, &o)
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

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}
