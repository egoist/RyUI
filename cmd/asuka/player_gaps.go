package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// What the player can't stand in for. The player is one app for every
// app, so what makes an app itself (its ID, its Info.plist and manifest,
// its extensions) isn't there; the Go code runs on the computer, so what
// is on the device (a file shared with it) isn't where the Go code reads.
// asuka dev builds an app that uses one of these gaps' build ones (as Expo
// moves from Expo Go to a development build), and says what the others
// leave out.

// playerGap is one of them.
type playerGap struct {
	targets []string // ios (the simulator), ios-device, android
	// build: an app that uses it runs built, not in the player. Otherwise
	// asuka dev only says what's missing: in the player alone, or, with
	// always, built too (what no build from asuka dev has).
	build, always bool
	why           string
	// uses says whether the app uses it, and what of it ("imports
	// asuka/sdk/purchase").
	uses func(u gapUse) (string, bool)
}

// gapUse is what the gaps look at: the app, where it runs, and its
// imports, all the way down.
type gapUse struct {
	app    *goApp
	target string
	deps   []string
}

func (u gapUse) imports(pkg string) bool { return slices.Contains(u.deps, pkg) }

const (
	purchasePkg = "github.com/egoist/asuka/sdk/purchase"
	pushPkg     = "github.com/egoist/asuka/sdk/push"
)

var playerGaps = []playerGap{
	{
		// StoreKit sells the app's products to the app: in the simulator
		// from asuka.json's storekit, on a device from the sandbox.
		targets: []string{"ios", "ios-device"}, build: true,
		why:  "the App Store sells its products only to the app",
		uses: importsGap(purchasePkg),
	},
	{
		targets: []string{"android"}, always: true,
		why:  "Play sells only to a build installed from one of its tracks, so purchases are unavailable in asuka dev (docs/purchases.md)",
		uses: importsGap(purchasePkg),
	},
	{
		// The extension runs the app's Go code (ios_notification.go).
		targets: []string{"ios", "ios-device"}, build: true,
		why: "the player has no notification service extension to rewrite pushes in",
		uses: func(u gapUse) (string, bool) {
			if !u.imports(pushPkg) {
				return "", false
			}
			dir, pkg := u.app.goPackage()
			if !calls(dir, pkg, "ios", pushPkg, "Rewrite") {
				return "", false
			}
			return "calls push.Rewrite", true
		},
	},
	{
		// Info.plist's select-identifiers and systemcodes (nfc.go).
		targets: []string{"ios-device"}, build: true,
		why: "iOS finds only the cards the app's own Info.plist names",
		uses: func(u gapUse) (string, bool) {
			c := u.app.cfg
			if c == nil || !u.imports(nfcPkg) || len(c.NFC.ISO7816) == 0 && len(c.NFC.FeliCa) == 0 {
				return "", false
			}
			return "has asuka.json's nfc.iso7816 or nfc.felica", true
		},
	},
	{
		// The manifest's NDEF_DISCOVERED and TECH_DISCOVERED filters.
		targets: []string{"android"},
		why:     "a tag opens only the app's own build (--no-hot); sessions work in the player",
		uses: func(u gapUse) (string, bool) {
			c := u.app.cfg
			if c == nil || !u.imports(nfcPkg) || len(c.NFC.NDEF) == 0 && len(c.NFC.Tech) == 0 {
				return "", false
			}
			return "has asuka.json's nfc.ndef or nfc.tech", true
		},
	},
	{
		// The simulator's files are the Mac's; a phone's aren't.
		targets: []string{"ios-device", "android"}, build: true,
		why: "the files shared with it are on the device, not on the computer the player's Go code runs on",
		uses: func(u gapUse) (string, bool) {
			if c := u.app.cfg; c != nil && c.ShareTarget.files() {
				return "takes shared files (asuka.json's shareTarget)", true
			}
			return "", false
		},
	},
	{
		targets: []string{"ios", "ios-device", "android"},
		why:     "only the app's own build opens them; try them with --no-hot",
		uses: func(u gapUse) (string, bool) {
			c := u.app.cfg
			if c == nil || c.Scheme == "" && len(c.Links) == 0 {
				return "", false
			}
			return "has asuka.json's scheme or links", true
		},
	},
	{
		targets: []string{"ios", "ios-device"},
		why:     "the player asks with its own purpose strings; try yours with --no-hot",
		uses: func(u gapUse) (string, bool) {
			if c := u.app.cfg; c != nil && len(c.IOS.Usage) > 0 {
				return "has asuka.json's ios.usage", true
			}
			return "", false
		},
	},
}

// importsGap is a gap of an app that imports pkg.
func importsGap(pkg string) func(gapUse) (string, bool) {
	return func(u gapUse) (string, bool) {
		return "imports " + strings.TrimPrefix(pkg, "github.com/egoist/"), u.imports(pkg)
	}
}

// firstGap is the first of the gaps that has the app built.
func firstGap(u gapUse) (playerGap, string, bool) {
	for _, g := range playerGaps {
		if !g.build || !slices.Contains(g.targets, u.target) {
			continue
		}
		if what, ok := g.uses(u); ok {
			return g, what, true
		}
	}
	return playerGap{}, "", false
}

// calls reports whether a package of pkg's (it, or its dependencies
// outside the standard library and fnPkg itself) refers to fnPkg's
// function fn: its Go files, as the go command in dir builds pkg for
// goos, parsed for a selector of fnPkg's name.
func calls(dir, pkg, goos, fnPkg, fn string) bool {
	c := exec.Command("go", "-C", dir, "list", "-deps", "-f", "{{if not .Standard}}{{.Dir}}\t{{join .GoFiles \",\"}}\t{{join .Imports \",\"}}{{end}}", pkg)
	c.Env = append(c.Environ(), "GOOS="+goos, "GOARCH=arm64", "CGO_ENABLED=1")
	out, err := c.Output()
	if err != nil {
		return true // the build says why; built, it has the rewrite if it calls it
	}
	fset := token.NewFileSet()
	for _, l := range strings.Split(string(out), "\n") {
		f := strings.Split(l, "\t")
		if len(f) != 3 || !slices.Contains(strings.Split(f[2], ","), fnPkg) {
			continue
		}
		for _, name := range strings.Split(f[1], ",") {
			file, err := parser.ParseFile(fset, filepath.Join(f[0], name), nil, parser.SkipObjectResolution)
			if err != nil {
				return true
			}
			if refers(file, fnPkg, fn) {
				return true
			}
		}
	}
	return false
}

// refers reports whether file refers to fnPkg's fn, by the name it
// imports fnPkg as.
func refers(file *ast.File, fnPkg, fn string) bool {
	local := ""
	for _, im := range file.Imports {
		if p, _ := strconv.Unquote(im.Path.Value); p == fnPkg {
			local = filepath.Base(fnPkg)
			if im.Name != nil {
				local = im.Name.Name
			}
		}
	}
	switch local {
	case "", "_":
		return false
	case ".":
		return true // its names unqualified: assume it does
	}
	found := false
	ast.Inspect(file, func(n ast.Node) bool {
		if s, ok := n.(*ast.SelectorExpr); ok && s.Sel.Name == fn {
			if id, ok := s.X.(*ast.Ident); ok && id.Name == local {
				found = true
			}
		}
		return !found
	})
	return found
}
