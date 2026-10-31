package main

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// macOS: the app's Go code and its host (host/darwin, AppKit through
// purego) are one Go program, built for this Mac with CGO_ENABLED=0.
//
// nui dev -mac runs it here and, on each change, rebuilds it and runs it
// again (its signals' state kept, as with the player). nui build -mac
// makes an .app: a universal binary (arm64 and x86_64), its Info.plist,
// icon and assets, signed (ad hoc unless -sign names an identity).

const macMin = "11.0"

// macLdflags is the -ldflags of a Mac build: nui.json's, and the app's
// name (its menus and window title).
func macLdflags(app *goApp, release bool) string {
	f := strings.TrimPrefix(app.cfg.ldflags(release), "-ldflags=")
	name := app.displayName()
	return "-ldflags=" + strings.TrimSpace(f+" -X "+strconv.Quote("github.com/egoist/nui/internal/boot.Name="+name))
}

func devMac(dir string, o *devOptions, stop <-chan os.Signal) error {
	app, err := loadApp(dir)
	if err != nil {
		return err
	}
	work, err := workDir(app, "mac")
	if err != nil {
		return err
	}
	fmt.Printf("nui: %s on this Mac\n", app.name)
	proc := &hostProcess{app: app, work: work, flags: macLdflags(app, false), env: []string{
		"NUI_STATE=" + filepath.Join(work, "state.json"),
		"NUI_DATA=" + filepath.Join(work, "data"),
	}}
	os.MkdirAll(filepath.Join(work, "data"), 0o755)
	start := time.Now()
	if err := proc.build(); err != nil {
		return err
	}
	if err := proc.restart(); err != nil {
		return err
	}
	fmt.Printf("nui: built and started in %.1fs\n", time.Since(start).Seconds())
	if o.once {
		<-proc.done
		return nil
	}
	return watchHost(proc, stop, func() {})
}

func buildMac(app *goApp, o *buildOptions) error {
	name := app.displayName()
	work, err := workDir(app, "mac-release")
	if err != nil {
		return err
	}
	bundle := filepath.Join(o.out, name+".app")
	os.RemoveAll(bundle)
	contents := filepath.Join(bundle, "Contents")
	macos := filepath.Join(contents, "MacOS")
	res := filepath.Join(contents, "Resources")
	for _, d := range []string{macos, res} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	run := func(dir string, env []string, cmd string, args ...string) error {
		if o.verbose {
			fmt.Println("+", cmd, strings.Join(args, " "))
		}
		c := exec.Command(cmd, args...)
		c.Dir = dir
		c.Env = append(os.Environ(), env...)
		out, err := c.CombinedOutput()
		if err != nil {
			return fmt.Errorf("%s failed: %v\n%s", cmd, err, out)
		}
		return nil
	}

	// A universal binary: one per architecture, joined by lipo.
	exe := sanitizeExe(name)
	var parts []string
	for _, arch := range []string{"arm64", "amd64"} {
		out := filepath.Join(work, "app-"+arch)
		env := []string{"GOOS=darwin", "GOARCH=" + arch, "CGO_ENABLED=0", "MACOSX_DEPLOYMENT_TARGET=" + macMin}
		if err := run(app.dir, env, "go", "build", "-trimpath", macLdflags(app, true), "-o", out, "."); err != nil {
			return err
		}
		parts = append(parts, out)
	}
	bin := filepath.Join(macos, exe)
	if err := run(work, nil, "lipo", append([]string{"-create", "-output", bin}, parts...)...); err != nil {
		return err
	}

	info := map[string]any{
		"CFBundleExecutable":                   exe,
		"CFBundleIdentifier":                   app.bundleID,
		"CFBundleName":                         name,
		"CFBundleDisplayName":                  name,
		"CFBundlePackageType":                  "APPL",
		"CFBundleShortVersionString":           app.cfg.Version,
		"CFBundleVersion":                      strconv.Itoa(app.cfg.Build),
		"CFBundleInfoDictionaryVersion":        "6.0",
		"LSMinimumSystemVersion":               macMin,
		"NSHighResolutionCapable":              true,
		"NSPrincipalClass":                     "NSApplication",
		"NSSupportsAutomaticGraphicsSwitching": true,
		"LSApplicationCategoryType":            "public.app-category.utilities",
	}
	if t := app.cfg.urlTypes(); len(t) > 0 {
		var a []any
		for _, m := range t {
			a = append(a, m)
		}
		info["CFBundleURLTypes"] = a
	}
	// The purpose strings are the same keys on macOS (camera, location...).
	for k, v := range app.cfg.IOS.Usage {
		info[k] = v
	}
	if app.cfg.Icon != "" {
		if err := macIcon(app.cfg.Icon, work, filepath.Join(res, "AppIcon.icns"), run); err != nil {
			return err
		}
		info["CFBundleIconFile"] = "AppIcon"
	}
	if err := os.WriteFile(filepath.Join(contents, "Info.plist"), []byte(plist(info)), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(contents, "PkgInfo"), []byte("APPL????"), 0o644); err != nil {
		return err
	}
	if err := copyDir(filepath.Join(app.dir, "assets"), filepath.Join(res, "assets")); err != nil {
		return err
	}

	identity := o.sign
	if identity == "" {
		identity = "-" // ad hoc: runs on this Mac; others need a Developer ID and notarization
	}
	args := []string{"--force", "--sign", identity}
	if identity != "-" {
		args = append(args, "--options", "runtime", "--timestamp")
	}
	if err := run(work, nil, "codesign", append(args, bundle)...); err != nil {
		return err
	}
	if err := run(work, nil, "codesign", "--verify", "--strict", bundle); err != nil {
		return err
	}
	fmt.Printf("nui: %s\n", bundle)
	if identity == "-" {
		fmt.Println("nui: signed ad hoc; to distribute it, sign with -sign \"Developer ID Application: ...\" and notarize it (xcrun notarytool)")
	}
	return nil
}

// sanitizeExe is the executable's name: the app's, without slashes.
func sanitizeExe(name string) string {
	s := strings.Map(func(r rune) rune {
		if r == '/' || r == ':' {
			return '-'
		}
		return r
	}, name)
	if s == "" {
		return "app"
	}
	return s
}

// macIcon makes the .icns from a square icon: macOS's shape, a rounded
// square inset in the canvas (824 of 1024 points), at each size iconutil
// wants.
func macIcon(src, work, out string, run func(dir string, env []string, cmd string, args ...string) error) error {
	img, err := loadIcon(src)
	if err != nil {
		return err
	}
	shaped := macShape(resize(img, 824))
	canvas := newNRGBA(1024)
	for y := range 824 {
		for x := range 824 {
			canvas.SetNRGBA(x+100, y+100, shaped.NRGBAAt(x, y))
		}
	}
	set := filepath.Join(work, "AppIcon.iconset")
	os.RemoveAll(set)
	for _, s := range []int{16, 32, 128, 256, 512} {
		if err := writePNG(filepath.Join(set, fmt.Sprintf("icon_%dx%d.png", s, s)), resize(canvas, s)); err != nil {
			return err
		}
		if err := writePNG(filepath.Join(set, fmt.Sprintf("icon_%dx%d@2x.png", s, s)), resize(canvas, s*2)); err != nil {
			return err
		}
	}
	return run(work, nil, "iconutil", "-c", "icns", "-o", out, set)
}

// macShape masks an icon to a rounded square (radius 22.5% of its side),
// antialiased.
func macShape(src *image.NRGBA) *image.NRGBA {
	n := src.Bounds().Dx()
	r := float64(n) * 0.225
	dst := image.NewNRGBA(src.Bounds())
	for y := range n {
		for x := range n {
			// Coverage of the pixel's center by the rounded square.
			cx, cy := float64(x)+0.5, float64(y)+0.5
			dx := math.Max(math.Max(r-cx, cx-(float64(n)-r)), 0)
			dy := math.Max(math.Max(r-cy, cy-(float64(n)-r)), 0)
			d := math.Hypot(dx, dy) - r
			cov := math.Min(math.Max(0.5-d, 0), 1)
			if dx == 0 || dy == 0 {
				cov = 1
			}
			c := src.NRGBAAt(x, y)
			c.A = uint8(float64(c.A) * cov)
			dst.SetNRGBA(x, y, color.NRGBA{c.R, c.G, c.B, c.A})
		}
	}
	return dst
}
