package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Windows: the app's Go code and its host (host/windows, Win32 through
// syscall) are one Go program, built with CGO_ENABLED=0 for amd64 (it
// runs on arm64 Windows too, emulated: the host's GDI+ calls pass floats
// in the registers amd64 has).
//
// ryui dev -windows runs it here and, on each change, rebuilds it and runs
// it again (its signals' state kept, as with the player: Windows can't
// send a process SIGINT, so RyUI closes its standard input, RYUI_STOP=stdin,
// and it saves its state and quits). ryui build -windows makes a folder
// with the .exe, its icon, manifest and version information built in
// (a .syso written beside the app's Go code for the build), and its
// assets beside it; it builds from any system.

// windowsLdflags is the -ldflags of a Windows build: ryui.json's, the
// app's name (its window title), and for a release, the GUI subsystem
// (no console window).
func windowsLdflags(app *goApp, release bool) string {
	f := strings.TrimPrefix(app.cfg.ldflags(release), "-ldflags=")
	f += " -X " + strconv.Quote("github.com/egoist/ryui/internal/boot.Name="+app.displayName())
	if release {
		f += " -H windowsgui"
	}
	return "-ldflags=" + strings.TrimSpace(f)
}

func devWindows(dir string, o *devOptions, stop <-chan os.Signal) error {
	if runtime.GOOS != "windows" {
		return errors.New("ryui dev -windows runs the app on this PC: run it on Windows (ryui build -windows builds from anywhere)")
	}
	app, err := loadApp(dir)
	if err != nil {
		return err
	}
	work, err := workDir(app, "windows")
	if err != nil {
		return err
	}
	fmt.Printf("ryui: %s on this PC\n", app.name)
	proc := &hostProcess{app: app, work: work, flags: windowsLdflags(app, false), goarch: "amd64", env: []string{
		"RYUI_STATE=" + filepath.Join(work, "state.json"),
		"RYUI_DATA=" + filepath.Join(work, "data"),
		"RYUI_STOP=stdin",
	}}
	os.MkdirAll(filepath.Join(work, "data"), 0o755)
	start := time.Now()
	if err := proc.build(); err != nil {
		return err
	}
	if err := proc.restart(); err != nil {
		return err
	}
	fmt.Printf("ryui: built and started in %.1fs\n", time.Since(start).Seconds())
	if o.once {
		<-proc.done
		return nil
	}
	return watchHost(proc, stop, func() {})
}

func buildWindows(app *goApp, o *buildOptions) error {
	name := app.displayName()
	exe := sanitizeExe(name) + ".exe"
	out := filepath.Join(o.out, sanitizeExe(name))
	if err := os.RemoveAll(out); err != nil {
		return err
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	// The resources: an icon, the manifest, the version. go build links the
	// main package's .syso files; -overlay doesn't reach them, so it's
	// written there for the build, unless the app has its own.
	syso := filepath.Join(app.dir, "ryui_windows_amd64.syso")
	own := false
	if m, _ := filepath.Glob(filepath.Join(app.dir, "*.syso")); len(m) > 0 {
		for _, f := range m {
			if f != syso {
				own = true
			}
		}
	}
	if !own {
		res, err := winresSyso(app)
		if err != nil {
			return err
		}
		if err := os.WriteFile(syso, res, 0o644); err != nil {
			return err
		}
		defer os.Remove(syso)
	} else {
		fmt.Println("ryui: using the app's own .syso resources")
	}
	args := []string{"build", "-trimpath", windowsLdflags(app, true), "-o", filepath.Join(out, exe), "."}
	if o.verbose {
		fmt.Println("+ go", strings.Join(args, " "))
	}
	c := exec.Command("go", args...)
	c.Dir = app.dir
	c.Env = append(os.Environ(), "GOOS=windows", "GOARCH=amd64", "CGO_ENABLED=0")
	if b, err := c.CombinedOutput(); err != nil {
		return fmt.Errorf("go build failed: %v\n%s", err, b)
	}
	if err := copyDir(filepath.Join(app.dir, "assets"), filepath.Join(out, "assets")); err != nil {
		return err
	}
	fmt.Printf("ryui: %s\n", filepath.Join(out, exe))
	fmt.Println("ryui: the folder is the app: zip it, or package it (an installer, MSIX); sign the .exe with signtool to keep SmartScreen quiet")
	return nil
}
