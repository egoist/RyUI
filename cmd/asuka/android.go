package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/crc32"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/egoist/asuka/internal/androidsdk"
	androidHost "github.com/egoist/asuka/platform/android"
)

// The Android dev loop: the shell app is built with Gradle once per host
// version and app ID (and installed once); on each change the app's Go
// code is rebuilt as a shared library and pushed into the app's private
// files, where the shell app loads it from, and the app is restarted.

const androidMinAPI = "33"

type androidToolchain struct {
	// sys is the computer the tools are on (androidsdk: their places
	// and names, adb.exe and gradlew.bat on Windows).
	sys                     androidsdk.System
	sdk, ndk, ndkBin, clang string
	adb, emulator, javaHome string
	verbose                 bool
}

func newAndroidToolchain(verbose bool) (*androidToolchain, error) {
	return newAndroidToolchainOn(androidsdk.Local(), verbose)
}

// newAndroidToolchainOn finds the tools on sys.
func newAndroidToolchainOn(sys androidsdk.System, verbose bool) (*androidToolchain, error) {
	sdk := sys.SDK()
	if sdk == "" {
		return nil, errors.New("no Android SDK: install Android Studio or set ANDROID_HOME")
	}
	ndk, bin, err := sys.NDK(sdk)
	if err != nil {
		return nil, err
	}
	return &androidToolchain{
		sys:      sys,
		sdk:      sdk,
		ndk:      ndk,
		ndkBin:   bin,
		clang:    sys.Clang(bin),
		adb:      sys.ADB(sdk),
		emulator: sys.Emulator(sdk),
		javaHome: sys.Java(),
		verbose:  verbose,
	}, nil
}

// cgoEnv is the environment of a cgo build of Go code for Android:
// goarch (and goarm), with the NDK's clang for target (a triple, the API
// level added) as CC, called directly with --target (no NDK wrapper,
// which on Windows is a .cmd through cmd.exe), and 16 KB pages.
func (t *androidToolchain) cgoEnv(goarch, goarm, triple string) []string {
	env := []string{"GOOS=android", "GOARCH=" + goarch, "CGO_ENABLED=1",
		"CC=" + androidsdk.CC(t.clang, triple+androidMinAPI),
		"CGO_LDFLAGS=-Wl,-z,max-page-size=16384"}
	if goarm != "" {
		env = append(env, "GOARM="+goarm)
	}
	return env
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func (t *androidToolchain) run(dir string, env []string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	if t.verbose {
		shown := make([]string, len(env))
		for i, e := range env {
			if k, _, _ := strings.Cut(e, "="); strings.Contains(strings.ToLower(k), "password") {
				e = k + "=***"
			}
			shown[i] = e
		}
		fmt.Fprintln(os.Stderr, "+", strings.Join(append(shown, append([]string{name}, args...)...), " "))
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s failed:\n%s", filepath.Base(name), strings.Join(args[:min(len(args), 2)], " "), strings.TrimSpace(string(out)))
	}
	return nil
}

func (t *androidToolchain) adbOut(serial string, args ...string) (string, error) {
	out, err := exec.Command(t.adb, append([]string{"-s", serial}, args...)...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// pickDevice returns a connected device or emulator (want: a serial or
// AVD name), starting an emulator if none is running.
func (t *androidToolchain) pickDevice(want string) (string, error) {
	// adb's server first, with none of this process's output: on Windows
	// a server started by a command whose output is read (adb devices)
	// can hold its pipe open after it, and the read never ends.
	exec.Command(t.adb, "start-server").Run()
	devices := func() []string {
		out, _ := exec.Command(t.adb, "devices").Output()
		var ds []string
		for _, l := range strings.Split(string(out), "\n")[1:] {
			if f := strings.Fields(l); len(f) == 2 && f[1] == "device" {
				ds = append(ds, f[0])
			}
		}
		return ds
	}
	for _, d := range devices() {
		if want == "" || d == want {
			return d, nil
		}
	}
	emulator := t.emulator
	out, err := exec.Command(emulator, "-list-avds").Output()
	if err != nil {
		return "", fmt.Errorf("no Android device or emulator: %v", err)
	}
	var avds []string
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "INFO") {
			avds = append(avds, l)
		}
	}
	avd := ""
	for _, a := range avds {
		if want == "" || a == want {
			avd = a
			break
		}
	}
	if avd == "" {
		return "", errors.New("no Android device or emulator: make an AVD in Android Studio's Device Manager")
	}
	fmt.Printf("asuka: starting emulator %s\n", avd)
	cmd := exec.Command(emulator, "-avd", avd, "-no-snapshot-save")
	if err := cmd.Start(); err != nil {
		return "", err
	}
	go cmd.Wait()
	for i := 0; i < 240; i++ {
		for _, d := range devices() {
			if strings.HasPrefix(d, "emulator-") {
				if v, _ := t.adbOut(d, "shell", "getprop", "sys.boot_completed"); v == "1" {
					return d, nil
				}
			}
		}
		time.Sleep(time.Second)
	}
	return "", errors.New("the emulator didn't boot")
}

// goArch is the device's GOARCH, and its target triple (the NDK's).
func (t *androidToolchain) goArch(serial string) (goarch, triple string, err error) {
	abi, err := t.adbOut(serial, "shell", "getprop", "ro.product.cpu.abi")
	if err != nil {
		return "", "", fmt.Errorf("adb: %v: %s", err, abi)
	}
	for _, a := range androidABIs {
		if a.abi == abi && a.goarm == "" {
			return a.goarch, a.clang, nil
		}
	}
	return "", "", fmt.Errorf("unsupported Android ABI %q", abi)
}

// buildGo builds the app's Go code for the device, work/libasuka.so, and
// writes its plugins' libraries beside it (plugins: their paths).
func (t *androidToolchain) buildGo(app *goApp, work, serial string) (lib string, plugins []string, err error) {
	goarch, triple, err := t.goArch(serial)
	if err != nil {
		return "", nil, err
	}
	ov, err := writeOverlay(app, work)
	if err != nil {
		return "", nil, err
	}
	lib = filepath.Join(work, "libasuka.so")
	env := t.cgoEnv(goarch, "", triple)
	if err := t.run(app.dir, env, "go", "build", "-buildmode=c-shared", app.cfg.ldflags(false), "-overlay", ov, "-o", lib, "."); err != nil {
		return "", nil, err
	}
	// An earlier build's plugins' libraries go.
	old, _ := filepath.Glob(filepath.Join(work, "*.so"))
	for _, p := range old {
		if p != lib {
			os.Remove(p)
		}
	}
	libs, err := appLibs(app.dir, "android", goarch, "android-"+goarch)
	if err != nil {
		return "", nil, err
	}
	for _, l := range libs {
		p := filepath.Join(work, l.file.Name)
		if err := l.write(p); err != nil {
			return "", nil, err
		}
		plugins = append(plugins, p)
	}
	return lib, plugins, nil
}

// buildAPK builds the shell app for app with Gradle, cached by the host
// sources and the app ID.
func (t *androidToolchain) buildAPK(app *goApp, work string) (apk, hash string, err error) {
	// The host's sources and Gradle build are shared: one build at a time.
	unlock, err := lockShared(androidHostDir(work))
	if err != nil {
		return "", "", err
	}
	defer unlock()
	src, hash, err := prepareAndroidHost(app, work)
	if err != nil {
		return "", "", err
	}
	man := app.cfg.androidManifest(true)
	manifest := filepath.Join(work, "AndroidManifest.xml")
	if err := os.WriteFile(manifest, []byte(man), 0o644); err != nil {
		return "", "", err
	}
	extra := app.cfg.gradleProps()
	mods := strings.Join(extra, " ")
	// The splash and Firebase's settings (androidRes).
	res := filepath.Join(work, "res")
	os.RemoveAll(res)
	if wrote, err := androidRes(app.cfg, res); err != nil {
		return "", "", err
	} else if wrote {
		extra = append(extra, "-PasukaRes="+res)
		mods += dirDigest(res)
	}
	hash = fmt.Sprintf("%s-%08x", hash, crc32.ChecksumIEEE([]byte(man+mods))) // a manifest, modules or resources change rebuilds
	apk = filepath.Join(work, "app-"+hash+".apk")
	if exists(apk) {
		return apk, hash, nil
	}
	fmt.Println("asuka: building the Android app with Gradle (once; the first time downloads Gradle and Android libraries)")
	args := append([]string{":app:assembleDebug", "-PasukaApplicationId=" + androidPackage(app.bundleID), "-PasukaAppName=" + app.cfg.androidLabel(),
		"-PasukaManifest=" + manifest}, extra...)
	if err := t.gradle(src, args...); err != nil {
		return "", "", err
	}
	data, err := os.ReadFile(filepath.Join(src, "app", "build", "outputs", "apk", "debug", "app-debug.apk"))
	if err != nil {
		return "", "", err
	}
	return apk, hash, os.WriteFile(apk, data, 0o644)
}

func (t *androidToolchain) gradle(src string, args ...string) error {
	return t.gradleEnv(src, nil, args...)
}

func (t *androidToolchain) gradleEnv(src string, extra []string, args ...string) error {
	name, args, env := t.gradleCommand(src, extra, args)
	return t.run(src, env, name, args...)
}

// gradleCommand is Gradle's command in src, with extra in its
// environment: the wrapper (gradlew; gradlew.bat on Windows), given the
// tasks and flags on its command line and the properties (-Pname=value)
// in its environment, as ORG_GRADLE_PROJECT_name, as a password already
// goes. There no value passes through a shell: gradlew.bat's cmd.exe
// would expand a % in an app's name or a path, end an argument at a
// quote, and refuse a command line over 8191 characters (one path per
// native package).
func (t *androidToolchain) gradleCommand(src string, extra, args []string) (name string, cmdArgs, env []string) {
	env = append([]string{"ANDROID_HOME=" + t.sdk}, extra...)
	if t.javaHome != "" {
		env = append(env, "JAVA_HOME="+t.javaHome)
	}
	cmdArgs = []string{"-q"}
	for _, a := range args {
		if k, v, ok := strings.Cut(strings.TrimPrefix(a, "-P"), "="); ok && strings.HasPrefix(a, "-P") {
			env = append(env, "ORG_GRADLE_PROJECT_"+k+"="+v)
			continue
		}
		cmdArgs = append(cmdArgs, a)
	}
	return filepath.Join(src, t.sys.Script("gradlew")), cmdArgs, env
}

// androidHostDir is where the Android host is built, shared by every app
// (lockShared it while building there).
func androidHostDir(work string) string {
	return filepath.Join(filepath.Dir(filepath.Dir(work)), "android-host")
}

// prepareAndroidHost writes the embedded host, with the app's own
// Kotlin, where Gradle builds it (shared by the apps), and returns its
// directory and a hash of what it holds.
func prepareAndroidHost(app *goApp, work string) (src, hash string, err error) {
	h := sha256.New()
	fmt.Fprintln(h, app.bundleID, app.name, app.displayName())
	var files []string
	fs.WalkDir(androidHost.Files, ".", func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			files = append(files, path)
		}
		return nil
	})
	sort.Strings(files)
	src = androidHostDir(work)
	for _, f := range files {
		data, _ := androidHost.Files.ReadFile(f)
		h.Write([]byte(f))
		h.Write(data)
		dst := filepath.Join(src, f)
		os.MkdirAll(filepath.Dir(dst), 0o755)
		if old, err := os.ReadFile(dst); err != nil || !bytes.Equal(old, data) {
			if err := os.WriteFile(dst, data, 0o755); err != nil {
				return "", "", err
			}
		}
	}
	removeStaleSources(src, files)
	// The app's native packages' Kotlin: their android/, each a Gradle
	// module of the shell app's (the build's asukaNativeModules), their
	// manifests merged into the app's by Gradle.
	if app.cfg != nil {
		if err := app.cfg.androidNatives(app); err != nil {
			return "", "", err
		}
		for _, m := range app.cfg.nativeModules {
			fmt.Fprintln(h, m[0], m[1], dirDigest(m[1]))
		}
	}
	// The widgets' providers (asuka.json's widgets).
	gen := filepath.Join(src, "app", "src", "main", "java", "dev", "asuka", "app", "widgets")
	os.RemoveAll(gen)
	if app.cfg != nil && len(app.cfg.androidWidgets()) > 0 {
		code := app.cfg.androidWidgetKotlin()
		h.Write([]byte(code))
		os.MkdirAll(gen, 0o755)
		if err := os.WriteFile(filepath.Join(gen, "Widgets.kt"), []byte(code), 0o644); err != nil {
			return "", "", err
		}
	}
	return src, hex.EncodeToString(h.Sum(nil))[:16], nil
}

// removeStaleSources removes what the host's source trees (each
// module's src/) hold that the embedded host doesn't: a file an earlier
// version of the host had and this one hasn't would build with it. What
// Gradle makes (build/, .gradle/) isn't touched, and the app's own
// Kotlin and widgets are written again after.
func removeStaleSources(src string, files []string) {
	keep := make(map[string]bool, len(files))
	for _, f := range files {
		keep[filepath.FromSlash(f)] = true
	}
	filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(src, path)
		if d.IsDir() {
			switch d.Name() {
			case "build", ".gradle", ".kotlin", ".idea":
				return filepath.SkipDir
			}
			return nil
		}
		inSrc := false
		for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
			if part == "src" {
				inSrc = true
				break
			}
		}
		if inSrc && !keep[rel] {
			os.Remove(path)
		}
		return nil
	})
}

func devAndroid(dir string, o *devOptions, stop <-chan os.Signal) error {
	app, err := loadApp(dir)
	if err != nil {
		return err
	}
	tc, err := newAndroidToolchain(o.verbose)
	if err != nil {
		return err
	}
	work, err := workDir(app, "android")
	if err != nil {
		return err
	}
	serial, err := tc.pickDevice(o.device)
	if err != nil {
		return err
	}
	fmt.Printf("asuka: %s on %s\n", app.name, serial)
	if usePlayer(app, o, "android") {
		return devAndroidPlayer(app, tc, work, serial, stop)
	}
	pkg := androidPackage(app.bundleID)

	var logs *exec.Cmd
	build := func() error {
		start := time.Now()
		// The Go code first: whether it has a share sheet is the manifest's.
		lib, plugins, err := tc.buildGo(app, work, serial)
		if err != nil {
			return err
		}
		app.cfg.shareSheet = hasSheet(lib)
		app.cfg.pushRewrite = hasSymbol(lib, rewriteSymbol)
		apk, hash, err := tc.buildAPK(app, work)
		if err != nil {
			return err
		}
		if err := tc.installOnce(serial, pkg, apk, hash, filepath.Join(work, "installed-"+serial)); err != nil {
			return err
		}
		if logs != nil && logs.Process != nil {
			logs.Process.Kill()
			logs.Wait()
			logs = nil
		}
		// The Go library, and its plugins' beside it.
		tmp := "/data/local/tmp/asuka-" + pkg + ".so"
		if err := tc.run(work, nil, tc.adb, "-s", serial, "push", lib, tmp); err != nil {
			return err
		}
		var pushed []devLib
		for _, p := range plugins {
			l := devLib{tmp: "/data/local/tmp/asuka-" + pkg + "-" + filepath.Base(p), name: filepath.Base(p)}
			if err := tc.run(work, nil, tc.adb, "-s", serial, "push", p, l.tmp); err != nil {
				return err
			}
			pushed = append(pushed, l)
		}
		tc.run(work, nil, tc.adb, "-s", serial, "shell", "am", "force-stop", pkg)
		if err := tc.run(work, nil, tc.adb, "-s", serial, "shell", "run-as", pkg, "sh", "-c",
			"'"+devLibsScript(tmp, pushed)+"'"); err != nil {
			return err
		}
		if err := tc.pushAssets(app, work, serial, pkg); err != nil {
			return err
		}
		if err := tc.run(work, nil, tc.adb, "-s", serial, "shell", "am", "start", "-W", "-n", pkg+"/dev.asuka.AsukaActivity"); err != nil {
			return err
		}
		fmt.Printf("asuka: built and started in %.1fs\n", time.Since(start).Seconds())
		if o.once {
			return nil
		}
		pid, _ := tc.adbOut(serial, "shell", "pidof", pkg)
		args := []string{"-s", serial, "logcat", "-v", "brief", "-T", "1"}
		if pid != "" {
			args = append(args, "--pid="+strings.Fields(pid)[0])
		}
		logs = exec.Command(tc.adb, args...)
		logs.Stdout, logs.Stderr = os.Stdout, os.Stderr
		return logs.Start()
	}
	if err := build(); err != nil {
		if o.once {
			return err
		}
		fmt.Fprintln(os.Stderr, "asuka:", err)
	}
	if o.once {
		return nil
	}
	fmt.Println("asuka: watching for changes (Ctrl-C to stop)")
	natives, _ := nativeFor(app, "android", "arm64") // its native packages' Kotlin, wherever they are
	w := newWatcher(append([]string{app.moduleDir, app.dir}, nativeDirsOf(natives, "android")...)...)
	for {
		select {
		case <-stop:
			if logs != nil && logs.Process != nil {
				logs.Process.Kill()
			}
			return nil
		case <-w.wake:
			if !w.changed() {
				continue
			}
			fmt.Println("asuka: change detected, rebuilding")
			if err := build(); err != nil {
				fmt.Fprintln(os.Stderr, "asuka:", err)
			}
		}
	}
}

// devLib is a plugin's library asuka dev pushed: where, and its name.
type devLib struct{ tmp, name string }

// devLibsScript is the shell script (run as the app) that puts the
// libraries asuka dev pushed to the device's tmp in the app's
// files/asuka: the Go code's, and its plugins' (an earlier build's go).
// Read-only: Android refuses to load writable libraries.
func devLibsScript(goTmp string, plugins []devLib) string {
	s := "mkdir -p files/asuka && rm -f files/asuka/*.so" +
		" && cp " + goTmp + " files/asuka/libasuka.so && chmod 444 files/asuka/libasuka.so"
	for _, l := range plugins {
		s += " && cp " + l.tmp + " files/asuka/" + l.name + " && chmod 444 files/asuka/" + l.name
	}
	return s
}

// androidPackage makes an Android package name from a bundle ID: no
// hyphens, and no segment starting with a digit.
// hasSheet reports whether the Go library at path calls app.ShareSheet:
// its name is in the functions' table, which stripping a release's
// symbols leaves (stack traces need it).
func hasSheet(path string) bool { return hasSymbol(path, sheetSymbol) }

// hasSymbol reports whether the Go library at path has the function
// symbol (its symbol table names it).
func hasSymbol(path, symbol string) bool {
	data, err := os.ReadFile(path)
	return err == nil && bytes.Contains(data, []byte(symbol+"\x00"))
}

func androidPackage(id string) string {
	parts := strings.Split(strings.ReplaceAll(id, "-", "_"), ".")
	for i, p := range parts {
		if p != "" && p[0] >= '0' && p[0] <= '9' {
			parts[i] = "app" + p
		}
	}
	return strings.Join(parts, ".")
}

// pushAssets copies the app's assets directory into its private files
// (files/asuka/assets), where the debug shell app reads them, when they
// changed.
func (t *androidToolchain) pushAssets(app *goApp, work, serial, pkg string) error {
	dir := filepath.Join(app.dir, "assets")
	if !exists(dir) {
		return nil
	}
	h := sha256.New()
	filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			data, _ := os.ReadFile(path)
			h.Write([]byte(path))
			h.Write(data)
		}
		return nil
	})
	sum := hex.EncodeToString(h.Sum(nil))
	marker := filepath.Join(work, "assets-"+serial)
	if prev, _ := os.ReadFile(marker); string(prev) == sum {
		return nil
	}
	tmp := "/data/local/tmp/asuka-" + pkg + "-assets"
	// adb doesn't make a directory with nothing in it, which an empty assets is.
	t.run(work, nil, t.adb, "-s", serial, "shell", "rm", "-rf", tmp, "&&", "mkdir", "-p", tmp)
	// dir's "." pushes what's in it into tmp, not tmp/assets.
	if err := t.run(work, nil, t.adb, "-s", serial, "push", dir+string(filepath.Separator)+".", tmp); err != nil {
		return err
	}
	if err := t.run(work, nil, t.adb, "-s", serial, "shell", "run-as", pkg, "sh", "-c",
		"'mkdir -p files/asuka && rm -rf files/asuka/assets && cp -r "+tmp+" files/asuka/assets'"); err != nil {
		return err
	}
	return os.WriteFile(marker, []byte(sum), 0o644)
}

// install installs a debug build, replacing a build signed with another
// key (a release build) on the way, which adb can't update.
// installOnce installs apk (whose sources hash is hash) unless it's what's
// installed: marker notes the hash and the device's install time, which
// a reinstall, a wiped emulator or one restored from an older snapshot
// changes (then the device doesn't have what the marker says).
func (t *androidToolchain) installOnce(serial, pkg, apk, hash, marker string) error {
	if prev, _ := os.ReadFile(marker); string(prev) == hash+"\n"+t.installStamp(serial, pkg) {
		return nil
	}
	if err := t.install(serial, pkg, apk); err != nil {
		return err
	}
	return os.WriteFile(marker, []byte(hash+"\n"+t.installStamp(serial, pkg)), 0o644)
}

// installStamp is what changes each time pkg is installed on serial: its
// last update time; "" if it isn't installed.
func (t *androidToolchain) installStamp(serial, pkg string) string {
	out, err := exec.Command(t.adb, "-s", serial, "shell", "dumpsys", "package", pkg).Output()
	if err != nil {
		return ""
	}
	for _, l := range strings.Split(string(out), "\n") {
		if l = strings.TrimSpace(l); strings.HasPrefix(l, "lastUpdateTime=") {
			return l
		}
	}
	return ""
}

// deviceHash is the file's hash on serial, read as pkg (as fileHash
// makes it); "" if it isn't there.
func (t *androidToolchain) deviceHash(serial, pkg, path string) string {
	out, err := exec.Command(t.adb, "-s", serial, "shell", "run-as", pkg, "sha256sum", path).Output()
	if err != nil || len(out) < 16 {
		return ""
	}
	return string(out[:16])
}

func (t *androidToolchain) install(serial, pkg, apk string) error {
	err := t.run("", nil, t.adb, "-s", serial, "install", "-r", "-d", apk)
	if err != nil && strings.Contains(err.Error(), "INSTALL_FAILED_UPDATE_INCOMPATIBLE") {
		fmt.Printf("asuka: %s is installed with another signing key (a release build?): replacing it, and its data\n", pkg)
		t.run("", nil, t.adb, "-s", serial, "uninstall", pkg)
		err = t.run("", nil, t.adb, "-s", serial, "install", "-r", "-d", apk)
	}
	return err
}
