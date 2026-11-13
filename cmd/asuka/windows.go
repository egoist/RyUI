package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/asuka/internal/filetype"
)

// Windows: the app's Go code and its host (host/windows, Win32 through
// syscall) are one Go program, built with CGO_ENABLED=0 for amd64 or
// arm64: asuka dev builds for this PC's, asuka build for amd64 unless
// --arch arm64 (an amd64 build runs on Windows on Arm too, emulated).
//
// asuka dev --platform windows runs it here and, on each change, rebuilds it and runs
// it again (its signals' state kept, as with the player: Windows can't
// send a process SIGINT, so Asuka closes its standard input, ASUKA_STOP=stdin,
// and it saves its state and quits). asuka build --platform windows makes a folder
// with the .exe, its icon, manifest and version information built in
// (a .syso written beside the app's Go code for the build), and its
// assets beside it; it builds from any system. With ASUKA_WINDOWS_CERT
// (a .pfx) and ASUKA_WINDOWS_CERT_PASSWORD set, it signs the .exe,
// timestamped: with signtool on Windows, osslsigncode elsewhere.

// windowsLdflags is the -ldflags of a Windows build: asuka.json's, the
// app's name (its window title), ID (its AppUserModelID, and what makes
// it run once), scheme (registered for the user as it starts) and
// version (its crash reports'), and for a release, the GUI subsystem (no
// console window).
func windowsLdflags(app *goApp, release bool) string {
	f := strings.TrimPrefix(app.cfg.ldflags(release), "-ldflags=")
	f += " -X " + strconv.Quote("github.com/egoist/asuka/internal/boot.Name="+app.displayName())
	if app.cfg != nil {
		if app.cfg.ID != "" {
			// The app's AppUserModelID, its toasts' (host/windows).
			f += " -X " + strconv.Quote("github.com/egoist/asuka/internal/boot.ID="+app.cfg.ID)
		}
		if app.cfg.Scheme != "" {
			f += " -X " + strconv.Quote("github.com/egoist/asuka/internal/boot.Scheme="+app.cfg.Scheme)
		}
		// The file types, registered as the app starts; a release's icons
		// are the executable's (winresSyso).
		if types := app.cfg.fileTypes(); len(types) > 0 {
			ids := map[int]int{}
			for i, t := range types {
				if release && t.icon != "" {
					ids[i] = fileTypeIconID(i)
				}
			}
			f += " -X " + strconv.Quote("github.com/egoist/asuka/internal/boot.FileTypes="+filetype.Encode(windowsFileTypes(types, ids)))
		}
		f += app.cfg.multipleInstances()
		f += " -X " + strconv.Quote("github.com/egoist/asuka/internal/boot.Version="+app.cfg.Version)
		f += " -X " + strconv.Quote("github.com/egoist/asuka/internal/boot.Build="+strconv.Itoa(app.cfg.Build))
	}
	if release {
		f += " -H windowsgui"
	}
	return "-ldflags=" + strings.TrimSpace(f)
}

func devWindows(dir string, o *devOptions, stop <-chan os.Signal) error {
	if runtime.GOOS != "windows" {
		return errors.New("asuka dev --platform windows runs the app on this PC: run it on Windows (asuka build --platform windows builds from anywhere)")
	}
	app, err := loadApp(dir)
	if err != nil {
		return err
	}
	work, err := workDir(app, "windows")
	if err != nil {
		return err
	}
	fmt.Printf("asuka: %s on this PC\n", app.name)
	proc := &hostProcess{app: app, work: work, flags: windowsLdflags(app, false), goarch: runtime.GOARCH, env: []string{
		"ASUKA_STATE=" + filepath.Join(work, "state.json"),
		"ASUKA_DATA=" + filepath.Join(work, "data"),
		"ASUKA_STOP=stdin",
	}}
	os.MkdirAll(filepath.Join(work, "data"), 0o755)
	start := time.Now()
	if err := proc.build(); err != nil {
		return err
	}
	if err := proc.restart(); err != nil {
		return err
	}
	fmt.Printf("asuka: built and started in %.1fs\n", time.Since(start).Seconds())
	if o.once {
		<-proc.done
		return nil
	}
	return watchHost(proc, stop, func() {})
}

// windowsArch is a Windows build's GOARCH: arch (amd64 or arm64), else
// amd64, which runs on every Windows PC.
func windowsArch(arch string) (string, error) {
	switch arch {
	case "":
		return "amd64", nil
	case "amd64", "arm64":
		return arch, nil
	}
	return "", fmt.Errorf("--arch %q: amd64 or arm64", arch)
}

func buildWindows(app *goApp, o *buildOptions) error {
	goarch, err := windowsArch(o.arch)
	if err != nil {
		return err
	}
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
	syso := filepath.Join(app.dir, "asuka_windows_"+goarch+".syso")
	own := false
	if m, _ := filepath.Glob(filepath.Join(app.dir, "*.syso")); len(m) > 0 {
		for _, f := range m {
			if f != syso {
				own = true
			}
		}
	}
	if !own {
		res, err := winresSyso(app, goarch)
		if err != nil {
			return err
		}
		if err := os.WriteFile(syso, res, 0o644); err != nil {
			return err
		}
		defer os.Remove(syso)
	} else {
		fmt.Println("asuka: using the app's own .syso resources")
	}
	args := []string{"build", "-trimpath", windowsLdflags(app, true), "-o", filepath.Join(out, exe), "."}
	if o.verbose {
		fmt.Println("+ go", strings.Join(args, " "))
	}
	c := exec.Command("go", args...)
	c.Dir = app.dir
	c.Env = append(os.Environ(), "GOOS=windows", "GOARCH="+goarch, "CGO_ENABLED=0")
	if b, err := c.CombinedOutput(); err != nil {
		return fmt.Errorf("go build failed: %v\n%s", err, b)
	}
	signed, err := signWindows(filepath.Join(out, exe), name, o.verbose)
	if err != nil {
		return err
	}
	if err := copyDir(filepath.Join(app.dir, "assets"), filepath.Join(out, "assets")); err != nil {
		return err
	}
	fmt.Printf("asuka: %s (windows/%s)\n", filepath.Join(out, exe), goarch)
	if signed {
		fmt.Println("asuka: the folder is the app, its .exe signed: zip it, or package it (an installer, MSIX)")
	} else {
		fmt.Println("asuka: the folder is the app: zip it, or package it (an installer, MSIX); set ASUKA_WINDOWS_CERT and ASUKA_WINDOWS_CERT_PASSWORD to sign the .exe and keep SmartScreen quiet")
	}
	return nil
}

// defaultTimestamps are the RFC 3161 timestamp servers a signature is
// timestamped by, so it stays valid after the certificate expires: the
// first that answers.
var defaultTimestamps = []string{
	"http://timestamp.digicert.com",
	"http://timestamp.sectigo.com",
	"http://timestamp.globalsign.com/tsa/r6advanced1",
}

// timestampServers are the servers ASUKA_WINDOWS_TIMESTAMP names (a
// comma-separated list; "none": no timestamp), else defaultTimestamps.
func timestampServers(env string) []string {
	env = strings.TrimSpace(env)
	if env == "" {
		return defaultTimestamps
	}
	if strings.EqualFold(env, "none") {
		return nil
	}
	var out []string
	for _, s := range strings.Split(env, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// signWindows signs exe with the certificate in ASUKA_WINDOWS_CERT (a
// .pfx, its password ASUKA_WINDOWS_CERT_PASSWORD), SHA-256 and timestamped
// (by ASUKA_WINDOWS_TIMESTAMP's servers, else defaultTimestamps', the
// first that answers; "none": not timestamped): with signtool (the
// Windows SDK's) on Windows, osslsigncode elsewhere. Without a
// certificate it leaves exe unsigned.
func signWindows(exe, name string, verbose bool) (bool, error) {
	cert := os.Getenv("ASUKA_WINDOWS_CERT")
	if cert == "" {
		return false, nil
	}
	if _, err := os.Stat(cert); err != nil {
		return false, fmt.Errorf("ASUKA_WINDOWS_CERT: %w", err)
	}
	password, ok := os.LookupEnv("ASUKA_WINDOWS_CERT_PASSWORD")
	if !ok {
		return false, errors.New("ASUKA_WINDOWS_CERT is set but not ASUKA_WINDOWS_CERT_PASSWORD (set it empty for a .pfx without one)")
	}
	servers := timestampServers(os.Getenv("ASUKA_WINDOWS_TIMESTAMP"))
	tries := servers
	if len(tries) == 0 {
		tries = []string{""} // not timestamped
	}
	var failed []string
	for _, ts := range tries {
		tool, args, err := windowsSigner(exe, name, cert, password, ts)
		if err != nil {
			return false, err
		}
		if verbose {
			fmt.Println("+", tool, strings.Join(redact(args, password), " "))
		}
		b, err := exec.Command(tool, args...).CombinedOutput()
		if err != nil {
			os.Remove(exe + ".signed")
			out := strings.ReplaceAll(string(b), password, "***")
			if ts != "" && timestampFailed(out) {
				// The server didn't answer: the next one.
				failed = append(failed, ts)
				if verbose {
					fmt.Printf("asuka: %s didn't timestamp the signature:\n%s\n", ts, out)
				}
				continue
			}
			return false, fmt.Errorf("signing %s failed: %v\n%s", filepath.Base(exe), err, out)
		}
		if !strings.EqualFold(filepath.Base(tool), "signtool.exe") {
			// osslsigncode writes a new file.
			if err := os.Rename(exe+".signed", exe); err != nil {
				return false, err
			}
		}
		if ts == "" {
			fmt.Printf("asuka: signed %s, not timestamped (ASUKA_WINDOWS_TIMESTAMP=none): its signature ends with the certificate\n", filepath.Base(exe))
		} else {
			fmt.Printf("asuka: signed %s (timestamped by %s)\n", filepath.Base(exe), ts)
		}
		return true, nil
	}
	return false, fmt.Errorf("signing %s: no timestamp server answered (%s): check the network, or set ASUKA_WINDOWS_TIMESTAMP to other RFC 3161 servers (comma-separated), or to none to sign without a timestamp (the signature then ends when the certificate does)",
		filepath.Base(exe), strings.Join(failed, ", "))
}

// timestampFailed reports whether a signer's output says the timestamp
// server failed (osslsigncode: "RFC 3161 timestamping failed"; signtool:
// "...the specified timestamp server either could not be reached...").
func timestampFailed(out string) bool {
	o := strings.ToLower(out)
	return strings.Contains(o, "timestamping failed") || strings.Contains(o, "timestamp server") ||
		strings.Contains(o, "while attempting to timestamp")
}

// windowsSigner is the command that signs exe, timestamped by ts (if not
// ""): signtool on Windows (on the PATH, else the newest Windows SDK's),
// osslsigncode elsewhere.
func windowsSigner(exe, name, cert, password, ts string) (string, []string, error) {
	if runtime.GOOS == "windows" {
		tool, err := exec.LookPath("signtool.exe")
		if err != nil {
			tool = sdkSigntool()
		}
		if tool == "" {
			return "", nil, errors.New("ASUKA_WINDOWS_CERT is set, but signtool isn't found: install the Windows SDK, or put signtool.exe on the PATH")
		}
		args := []string{"sign", "/f", cert, "/p", password, "/fd", "SHA256"}
		if ts != "" {
			args = append(args, "/tr", ts, "/td", "SHA256")
		}
		return tool, append(args, "/d", name, exe), nil
	}
	tool, err := exec.LookPath("osslsigncode")
	if err != nil {
		return "", nil, errors.New("ASUKA_WINDOWS_CERT is set, but osslsigncode isn't found (it signs Windows executables off Windows: brew install osslsigncode, apt install osslsigncode)")
	}
	args := []string{"sign", "-pkcs12", cert, "-pass", password, "-h", "sha256", "-n", name}
	if ts != "" {
		args = append(args, "-ts", ts)
	}
	return tool, append(args, "-in", exe, "-out", exe+".signed"), nil
}

// sdkSigntool is the newest Windows SDK's signtool for this PC, "" for
// none.
func sdkSigntool() string {
	arch := "x64"
	if runtime.GOARCH == "arm64" {
		arch = "arm64"
	}
	base := filepath.Join(os.Getenv("ProgramFiles(x86)"), "Windows Kits", "10", "bin")
	m, _ := filepath.Glob(filepath.Join(base, "10.*", arch, "signtool.exe"))
	if len(m) == 0 {
		m, _ = filepath.Glob(filepath.Join(base, "10.*", "x64", "signtool.exe"))
	}
	if len(m) == 0 {
		return ""
	}
	sort.Slice(m, func(i, j int) bool { return sdkVersionLess(m[i], m[j]) })
	return m[len(m)-1]
}

// sdkVersionLess orders paths by the SDK version in them (10.0.22621.0).
func sdkVersionLess(a, b string) bool {
	va := strings.Split(filepath.Base(filepath.Dir(filepath.Dir(a))), ".")
	vb := strings.Split(filepath.Base(filepath.Dir(filepath.Dir(b))), ".")
	for i := 0; i < len(va) && i < len(vb); i++ {
		x, _ := strconv.Atoi(va[i])
		y, _ := strconv.Atoi(vb[i])
		if x != y {
			return x < y
		}
	}
	return len(va) < len(vb)
}

// redact is args with the password hidden, to print.
func redact(args []string, password string) []string {
	out := slices.Clone(args)
	for i, a := range out {
		if password != "" && a == password {
			out[i] = "***"
		}
	}
	return out
}
