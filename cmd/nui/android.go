package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	androidHost "github.com/egoist/nui/hosts/android"
)

// The Android dev loop: the shell app is built with Gradle once per host
// version and app ID (and installed once); on each change the app's Go
// code is rebuilt as a shared library and pushed into the app's private
// files, where the shell app loads it from, and the app is restarted.

const androidMinAPI = "26"

type androidToolchain struct {
	sdk, ndkBin, adb, javaHome string
	verbose                    bool
}

func findAndroidSDK() string {
	for _, k := range []string{"ANDROID_HOME", "ANDROID_SDK_ROOT"} {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	home, _ := os.UserHomeDir()
	for _, p := range []string{filepath.Join(home, "Library", "Android", "sdk"), filepath.Join(home, "Android", "Sdk")} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func newAndroidToolchain(verbose bool) (*androidToolchain, error) {
	sdk := findAndroidSDK()
	if sdk == "" {
		return nil, errors.New("no Android SDK: install Android Studio or set ANDROID_HOME")
	}
	ndks, _ := filepath.Glob(filepath.Join(sdk, "ndk", "*"))
	if len(ndks) == 0 {
		return nil, errors.New("no Android NDK: install one with Android Studio's SDK Manager")
	}
	sort.Strings(ndks)
	host := "darwin-x86_64"
	if runtime.GOOS == "linux" {
		host = "linux-x86_64"
	}
	t := &androidToolchain{
		sdk:     sdk,
		ndkBin:  filepath.Join(ndks[len(ndks)-1], "toolchains", "llvm", "prebuilt", host, "bin"),
		adb:     filepath.Join(sdk, "platform-tools", "adb"),
		verbose: verbose,
	}
	t.javaHome = os.Getenv("JAVA_HOME")
	if t.javaHome == "" {
		if jbr := "/Applications/Android Studio.app/Contents/jbr/Contents/Home"; exists(jbr) {
			t.javaHome = jbr
		}
	}
	return t, nil
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func (t *androidToolchain) run(dir string, env []string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	if t.verbose {
		fmt.Fprintln(os.Stderr, "+", strings.Join(append(env, append([]string{name}, args...)...), " "))
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
	emulator := filepath.Join(t.sdk, "emulator", "emulator")
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
	fmt.Printf("nui: starting emulator %s\n", avd)
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

func (t *androidToolchain) goArch(serial string) (goarch, clang string, err error) {
	abi, err := t.adbOut(serial, "shell", "getprop", "ro.product.cpu.abi")
	if err != nil {
		return "", "", fmt.Errorf("adb: %v: %s", err, abi)
	}
	switch abi {
	case "arm64-v8a":
		return "arm64", "aarch64-linux-android" + androidMinAPI + "-clang", nil
	case "x86_64":
		return "amd64", "x86_64-linux-android" + androidMinAPI + "-clang", nil
	}
	return "", "", fmt.Errorf("unsupported Android ABI %q", abi)
}

func (t *androidToolchain) buildGo(app *goApp, work, serial string) (string, error) {
	goarch, clang, err := t.goArch(serial)
	if err != nil {
		return "", err
	}
	ov, err := writeOverlay(app, work)
	if err != nil {
		return "", err
	}
	lib := filepath.Join(work, "libnui.so")
	env := []string{"GOOS=android", "GOARCH=" + goarch, "CGO_ENABLED=1", "CC=" + filepath.Join(t.ndkBin, clang),
		"CGO_LDFLAGS=-Wl,-z,max-page-size=16384"}
	return lib, t.run(app.dir, env, "go", "build", "-buildmode=c-shared", "-overlay", ov, "-o", lib, ".")
}

// buildAPK builds the shell app for app with Gradle, cached by the host
// sources and the app ID.
func (t *androidToolchain) buildAPK(app *goApp, work string) (apk, hash string, err error) {
	h := sha256.New()
	fmt.Fprintln(h, app.bundleID, app.name)
	var files []string
	fs.WalkDir(androidHost.Files, ".", func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			files = append(files, path)
		}
		return nil
	})
	sort.Strings(files)
	src := filepath.Join(filepath.Dir(filepath.Dir(work)), "android-host")
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
	hash = hex.EncodeToString(h.Sum(nil))[:16]
	apk = filepath.Join(work, "app-"+hash+".apk")
	if exists(apk) {
		return apk, hash, nil
	}
	fmt.Println("nui: building the Android app with Gradle (once; the first time downloads Gradle and Android libraries)")
	env := []string{"ANDROID_HOME=" + t.sdk}
	if t.javaHome != "" {
		env = append(env, "JAVA_HOME="+t.javaHome)
	}
	if err := t.run(src, env, filepath.Join(src, "gradlew"), "-q", ":app:assembleDebug",
		"-PnuiApplicationId="+androidPackage(app.bundleID), "-PnuiAppName="+app.name); err != nil {
		return "", "", err
	}
	data, err := os.ReadFile(filepath.Join(src, "app", "build", "outputs", "apk", "debug", "app-debug.apk"))
	if err != nil {
		return "", "", err
	}
	return apk, hash, os.WriteFile(apk, data, 0o644)
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
	fmt.Printf("nui: %s on %s\n", app.name, serial)
	pkg := androidPackage(app.bundleID)

	var logs *exec.Cmd
	build := func() error {
		start := time.Now()
		apk, hash, err := tc.buildAPK(app, work)
		if err != nil {
			return err
		}
		marker := filepath.Join(work, "installed-"+serial)
		if prev, _ := os.ReadFile(marker); string(prev) != hash {
			if err := tc.run(work, nil, tc.adb, "-s", serial, "install", "-r", "-d", apk); err != nil {
				return err
			}
			os.WriteFile(marker, []byte(hash), 0o644)
		}
		lib, err := tc.buildGo(app, work, serial)
		if err != nil {
			return err
		}
		if logs != nil && logs.Process != nil {
			logs.Process.Kill()
			logs.Wait()
			logs = nil
		}
		tmp := "/data/local/tmp/nui-" + pkg + ".so"
		if err := tc.run(work, nil, tc.adb, "-s", serial, "push", lib, tmp); err != nil {
			return err
		}
		tc.run(work, nil, tc.adb, "-s", serial, "shell", "am", "force-stop", pkg)
		if err := tc.run(work, nil, tc.adb, "-s", serial, "shell", "run-as", pkg, "sh", "-c",
			// Read-only: Android refuses to load writable libraries.
			"'mkdir -p files/nui && rm -f files/nui/libnui.so && cp "+tmp+" files/nui/libnui.so && chmod 444 files/nui/libnui.so'"); err != nil {
			return err
		}
		if err := tc.run(work, nil, tc.adb, "-s", serial, "shell", "am", "start", "-W", "-n", pkg+"/dev.nui.NuiActivity"); err != nil {
			return err
		}
		fmt.Printf("nui: built and started in %.1fs\n", time.Since(start).Seconds())
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
		fmt.Fprintln(os.Stderr, "nui:", err)
	}
	if o.once {
		return nil
	}
	fmt.Println("nui: watching for changes (Ctrl-C to stop)")
	w := newWatcher(app.moduleDir, app.dir)
	tick := time.NewTicker(400 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-stop:
			if logs != nil && logs.Process != nil {
				logs.Process.Kill()
			}
			return nil
		case <-tick.C:
			if !w.changed() {
				continue
			}
			fmt.Println("nui: change detected, rebuilding")
			if err := build(); err != nil {
				fmt.Fprintln(os.Stderr, "nui:", err)
			}
		}
	}
}

// androidPackage makes an Android package name from a bundle ID: no
// hyphens, and no segment starting with a digit.
func androidPackage(id string) string {
	parts := strings.Split(strings.ReplaceAll(id, "-", "_"), ".")
	for i, p := range parts {
		if p != "" && p[0] >= '0' && p[0] <= '9' {
			parts[i] = "app" + p
		}
	}
	return strings.Join(parts, ".")
}
