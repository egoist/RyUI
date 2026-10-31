// Run builds the benchmark apps (bench/ryui, bench/swiftui,
// bench/compose) as release builds, runs each on a simulator or an
// emulator, and compares them: bench/README.md.
//
//	go run ./bench/run -ios               # RyUI and SwiftUI, on a simulator of its own
//	go run ./bench/run -android           # RyUI and Compose, on the running emulator or device
//	go run ./bench/run -ios -android -runs 7
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
)

// result is one scenario's report from the meter.
type result struct {
	Scenario string  `json:"scenario"`
	Frames   int     `json:"frames"`
	Mean     float64 `json:"mean"`
	P50      float64 `json:"p50"`
	P95      float64 `json:"p95"`
	Max      float64 `json:"max"`
	Total    float64 `json:"total"`
	Dropped  int     `json:"dropped"`
	Peak     float64 `json:"peak"` // memory: the peak since launch
	Wall     float64 `json:"wall"` // Android: a frame's whole time, with the render thread and GPU
}

// app is one framework's benchmark app on a platform.
type app struct {
	framework string
	build     func() (string, error) // the installable: an .app or an .apk
	id        string                 // bundle ID or package
	activity  string                 // Android: the launcher activity
}

var (
	root   string
	runs   = flag.Int("runs", 5, "runs of each app (the median is reported)")
	outDir = flag.String("out", "bench/results", "where the results go")
)

func main() {
	ios := flag.Bool("ios", false, "ryui and SwiftUI on the iOS simulator")
	android := flag.Bool("android", false, "ryui and Jetpack Compose on Android")
	flag.Parse()
	if !*ios && !*android {
		*ios = true
	}
	wd, _ := os.Getwd()
	root = wd
	for !exists(filepath.Join(root, "go.mod")) {
		root = filepath.Dir(root)
	}
	os.MkdirAll(filepath.Join(root, *outDir), 0o755)
	var failed bool
	if *ios {
		if err := runIOS(); err != nil {
			fmt.Fprintln(os.Stderr, "bench: iOS:", err)
			failed = true
		}
	}
	if *android {
		if err := runAndroid(); err != nil {
			fmt.Fprintln(os.Stderr, "bench: Android:", err)
			failed = true
		}
	}
	if failed {
		os.Exit(1)
	}
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func command(dir string, env []string, name string, args ...string) *exec.Cmd {
	c := exec.Command(name, args...)
	c.Dir = dir
	c.Env = append(os.Environ(), env...)
	return c
}

func output(dir string, env []string, name string, args ...string) (string, error) {
	out, err := command(dir, env, name, args...).CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
	}
	return string(out), nil
}

// collect reads the meter's lines from r until it reports done.
func collect(r io.Reader) ([]result, error) {
	var out []result
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 1<<20), 1<<20)
	for s.Scan() {
		// "RYUIBENCH {...}" (iOS's console), "I/RYUIBENCH: {...}" (logcat).
		_, rest, ok := strings.Cut(s.Text(), "RYUIBENCH")
		i := strings.IndexByte(rest, '{')
		if !ok || i < 0 {
			continue
		}
		js := rest[i:]
		var res result
		if err := json.Unmarshal([]byte(js), &res); err != nil {
			return out, err
		}
		if res.Scenario == "done" {
			return out, nil
		}
		out = append(out, res)
	}
	return out, errors.New("the app stopped before it was done")
}

// measure runs each app runs times, interleaved (so the device's state
// drifts over all of them alike), after a run of each to warm up.
func measure(apps []app, launch func(app) ([]result, error)) (map[string][][]result, error) {
	all := map[string][][]result{}
	for i := -1; i < *runs; i++ {
		for _, a := range apps {
			res, err := launch(a)
			if err != nil {
				return nil, fmt.Errorf("%s: %v", a.framework, err)
			}
			if i >= 0 {
				all[a.framework] = append(all[a.framework], res)
			}
			fmt.Printf("bench: %s run %d: %d scenarios\n", a.framework, i+1, len(res))
		}
	}
	return all, nil
}

func median(vs []float64) float64 {
	if len(vs) == 0 {
		return 0
	}
	s := slices.Clone(vs)
	sort.Float64s(s)
	if len(s)%2 == 1 {
		return s[len(s)/2]
	}
	return (s[len(s)/2-1] + s[len(s)/2]) / 2
}

// scenarios, in the order the apps run them, and what each reports.
var scenarios = []struct{ name, what string }{
	{"startup", "from the process's start to the first frame, ms"},
	{"memory-idle", "at the first frame, MB"},
	{"mount", "1,000 rows appear: the main thread's time, ms"},
	{"memory-mount", "with the 1,000 rows, MB"},
	{"update-one", "one row's text changes each frame: ms a frame"},
	{"update-all", "all 1,000 rows' texts change each frame: ms a frame"},
	{"scroll", "a lazy list of 10,000 rows scrolls 8 pt a frame: ms a frame"},
	{"memory-list", "after scrolling the list, MB"},
}

// report writes the comparison: the median of the runs, per scenario
// and framework.
func report(platform, device string, apps []app, all map[string][][]result) error {
	var b strings.Builder
	fmt.Fprintf(&b, "## %s\n\n%s, %s; release builds; the median of %d runs (after one to warm up).\n\n", platform, device, time.Now().Format("2006-01-02"), *runs)
	if platform == "iOS" {
		b.WriteString("Memory is the footprint iOS counts against the app (phys_footprint).\n\n")
	} else {
		b.WriteString("Memory is the proportional set size (PSS); its peak, the resident set's high-water mark (VmHWM).\n\n")
	}
	fmt.Fprintf(&b, "| scenario | |")
	for _, a := range apps {
		fmt.Fprintf(&b, " %s |", a.framework)
	}
	b.WriteString("\n|---|---|")
	for range apps {
		b.WriteString("---:|")
	}
	b.WriteString("\n")
	for _, sc := range scenarios {
		perFrame := sc.name == "update-one" || sc.name == "update-all" || sc.name == "scroll"
		rows := []struct {
			label string
			f     func(result) float64
		}{{sc.what, func(r result) float64 { return r.Total }}}
		if strings.HasPrefix(sc.name, "memory") {
			rows = append(rows, struct {
				label string
				f     func(result) float64
			}{"peak since launch, MB", func(r result) float64 { return r.Peak }})
		}
		if perFrame {
			rows = []struct {
				label string
				f     func(result) float64
			}{
				{"mean ms a frame", func(r result) float64 { return r.Mean }},
				{"95th percentile", func(r result) float64 { return r.P95 }},
				{"dropped frames", func(r result) float64 { return float64(r.Dropped) }},
			}
			if platform == "Android" {
				rows = append(rows, struct {
					label string
					f     func(result) float64
				}{"frame's whole time, ms (with the GPU)", func(r result) float64 { return r.Wall }})
			}
		}
		for i, row := range rows {
			name := ""
			if i == 0 {
				name = "**" + sc.name + "**"
			}
			fmt.Fprintf(&b, "| %s | %s |", name, row.label)
			for _, a := range apps {
				var vs []float64
				for _, run := range all[a.framework] {
					for _, r := range run {
						if r.Scenario == sc.name {
							vs = append(vs, row.f(r))
						}
					}
				}
				if len(vs) == 0 {
					b.WriteString(" — |")
					continue
				}
				fmt.Fprintf(&b, " %s |", format(median(vs)))
			}
			b.WriteString("\n")
		}
	}
	fmt.Print("\n" + b.String())
	raw, _ := json.MarshalIndent(all, "", "  ")
	base := filepath.Join(root, *outDir, strings.ToLower(platform))
	if err := os.WriteFile(base+".json", raw, 0o644); err != nil {
		return err
	}
	return os.WriteFile(base+".md", []byte(b.String()), 0o644)
}

func format(v float64) string {
	switch {
	case v >= 100:
		return fmt.Sprintf("%.0f", v)
	case v >= 10:
		return fmt.Sprintf("%.1f", v)
	}
	return fmt.Sprintf("%.2f", v)
}

// iOS

// simulator is a simulator of the benchmarks' own: one that UI
// automation (which turns accessibility on, and with it work UIKit does
// for every label it draws) has never touched.
func simulator() (string, error) {
	out, err := output(root, nil, "xcrun", "simctl", "list", "devices", "-j")
	if err != nil {
		return "", err
	}
	var list struct {
		Devices map[string][]struct {
			Name, UDID, State string
		} `json:"devices"`
	}
	json.Unmarshal([]byte(out), &list)
	udid, state := "", ""
	for _, ds := range list.Devices {
		for _, d := range ds {
			if d.Name == "ryui-bench" {
				udid, state = d.UDID, d.State
			}
		}
	}
	if udid == "" {
		out, err := output(root, nil, "xcrun", "simctl", "create", "ryui-bench", "iPhone 17 Pro")
		if err != nil {
			return "", err
		}
		udid = strings.TrimSpace(out)
	}
	if state != "Booted" {
		fmt.Println("bench: booting the ryui-bench simulator")
		if _, err := output(root, nil, "xcrun", "simctl", "boot", udid); err != nil {
			return "", err
		}
		if _, err := output(root, nil, "xcrun", "simctl", "bootstatus", udid, "-b"); err != nil {
			return "", err
		}
		time.Sleep(20 * time.Second) // what the system does after booting settles
	}
	return udid, nil
}

func buildRyUIIOS() (string, error) {
	out, err := output(root, nil, "go", "run", "./cmd/ryui", "build", "-ios", "-simulator", "bench/ryui")
	if err != nil {
		return "", err
	}
	m := regexp.MustCompile(`ryui: (.+\.app) in`).FindStringSubmatch(out)
	if m == nil {
		return "", fmt.Errorf("no .app in:\n%s", out)
	}
	return m[1], nil
}

func buildSwiftUI() (string, error) {
	sdk, err := output(root, nil, "xcrun", "--sdk", "iphonesimulator", "--show-sdk-path")
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, "bench", "swiftui", "dist", "SwiftUI Bench.app")
	os.RemoveAll(dir)
	os.MkdirAll(dir, 0o755)
	if _, err := output(root, nil, "xcrun", "swiftc", "-O", "-parse-as-library", "-target", "arm64-apple-ios17.0-simulator",
		"-sdk", strings.TrimSpace(sdk), "bench/swiftui/App.swift", "bench/ryui/ios/Meter.swift", "-o", filepath.Join(dir, "Bench")); err != nil {
		return "", err
	}
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>CFBundleIdentifier</key><string>dev.ryui.bench.swiftui</string>
<key>CFBundleExecutable</key><string>Bench</string>
<key>CFBundleName</key><string>SwiftUI Bench</string>
<key>CFBundlePackageType</key><string>APPL</string>
<key>CFBundleShortVersionString</key><string>1.0</string>
<key>CFBundleVersion</key><string>1</string>
<key>MinimumOSVersion</key><string>17.0</string>
<key>CFBundleSupportedPlatforms</key><array><string>iPhoneSimulator</string></array>
<key>UIDeviceFamily</key><array><integer>1</integer></array>
<key>UILaunchScreen</key><dict/>
<key>CADisableMinimumFrameDurationOnPhone</key><true/>
</dict></plist>
`
	if err := os.WriteFile(filepath.Join(dir, "Info.plist"), []byte(plist), 0o644); err != nil {
		return "", err
	}
	if _, err := output(root, nil, "codesign", "--force", "--sign", "-", dir); err != nil {
		return "", err
	}
	return dir, nil
}

func runIOS() error {
	udid, err := simulator()
	if err != nil {
		return err
	}
	apps := []app{
		{framework: "ryui", build: buildRyUIIOS, id: "dev.ryui.bench"},
		{framework: "SwiftUI", build: buildSwiftUI, id: "dev.ryui.bench.swiftui"},
	}
	for _, a := range apps {
		fmt.Printf("bench: building %s for iOS\n", a.framework)
		path, err := a.build()
		if err != nil {
			return err
		}
		if _, err := output(root, nil, "xcrun", "simctl", "install", udid, path); err != nil {
			return err
		}
	}
	all, err := measure(apps, func(a app) ([]result, error) {
		output(root, nil, "xcrun", "simctl", "terminate", udid, a.id)
		time.Sleep(time.Second)
		c := command(root, nil, "xcrun", "simctl", "launch", "--console-pty", "--terminate-running-process", udid, a.id)
		out, err := c.StdoutPipe()
		if err != nil {
			return nil, err
		}
		if err := c.Start(); err != nil {
			return nil, err
		}
		defer func() { c.Process.Kill(); c.Wait() }()
		return within(2*time.Minute, func() ([]result, error) { return collect(out) })
	})
	if err != nil {
		return err
	}
	return report("iOS", "iPhone 17 Pro simulator", apps, all)
}

// within runs f, failing after d.
func within(d time.Duration, f func() ([]result, error)) ([]result, error) {
	type ret struct {
		r   []result
		err error
	}
	ch := make(chan ret, 1)
	go func() { r, err := f(); ch <- ret{r, err} }()
	select {
	case r := <-ch:
		return r.r, r.err
	case <-time.After(d):
		return nil, errors.New("timed out")
	}
}

// Android

func adb(serial string, args ...string) (string, error) {
	return output(root, nil, "adb", append([]string{"-s", serial}, args...)...)
}

func device() (string, error) {
	out, err := output(root, nil, "adb", "devices")
	if err != nil {
		return "", err
	}
	for _, l := range strings.Split(out, "\n")[1:] {
		if f := strings.Fields(l); len(f) == 2 && f[1] == "device" {
			return f[0], nil
		}
	}
	return "", errors.New("no Android device or emulator running")
}

// keystore makes (once) a key to sign the release builds with.
func keystore() ([]string, error) {
	dir, _ := os.UserCacheDir()
	ks := filepath.Join(dir, "ryui", "bench.keystore")
	env := []string{"RYUI_KEYSTORE=" + ks, "RYUI_KEY_ALIAS=bench", "RYUI_KEYSTORE_PASSWORD=ryuibench"}
	if exists(ks) {
		return env, nil
	}
	os.MkdirAll(filepath.Dir(ks), 0o755)
	_, err := output(root, nil, "keytool", "-genkeypair", "-keystore", ks, "-alias", "bench", "-keyalg", "RSA", "-keysize", "2048",
		"-validity", "10000", "-storepass", "ryuibench", "-keypass", "ryuibench", "-dname", "CN=ryui bench")
	return env, err
}

func buildRyUIAndroid() (string, error) {
	env, err := keystore()
	if err != nil {
		return "", err
	}
	out, err := output(root, env, "go", "run", "./cmd/ryui", "build", "-android", "bench/ryui")
	if err != nil {
		return "", err
	}
	m := regexp.MustCompile(`ryui: (\S.*\.apk)`).FindStringSubmatch(out)
	if m == nil {
		return "", fmt.Errorf("no .apk in:\n%s", out)
	}
	return m[1], nil
}

func buildCompose() (string, error) {
	dir := filepath.Join(root, "bench", "compose")
	var env []string
	if os.Getenv("ANDROID_HOME") == "" {
		sdk := os.Getenv("ANDROID_SDK_ROOT")
		if home, _ := os.UserHomeDir(); sdk == "" {
			sdk = filepath.Join(home, "Library", "Android", "sdk") // Android Studio's, on macOS
			if !exists(sdk) {
				sdk = filepath.Join(home, "Android", "Sdk") // on Linux
			}
		}
		env = append(env, "ANDROID_HOME="+sdk)
	}
	if _, err := output(dir, env, filepath.Join(dir, "gradlew"), "-p", dir, ":app:assembleRelease", "-q"); err != nil {
		return "", err
	}
	return filepath.Join(dir, "app", "build", "outputs", "apk", "release", "app-release.apk"), nil
}

func runAndroid() error {
	serial, err := device()
	if err != nil {
		return err
	}
	if s, _ := adb(serial, "shell", "settings", "get", "secure", "enabled_accessibility_services"); strings.TrimSpace(s) != "null" && strings.TrimSpace(s) != "" {
		fmt.Printf("bench: warning: accessibility services are on (%s): every view change costs more\n", strings.TrimSpace(s))
	}
	model, _ := adb(serial, "shell", "getprop", "ro.product.model")
	apps := []app{
		{framework: "ryui", build: buildRyUIAndroid, id: "dev.ryui.bench", activity: "dev.ryui.RyUIActivity"},
		{framework: "Compose", build: buildCompose, id: "dev.ryui.bench.compose", activity: "dev.ryui.bench.compose.MainActivity"},
	}
	for _, a := range apps {
		fmt.Printf("bench: building %s for Android\n", a.framework)
		path, err := a.build()
		if err != nil {
			return err
		}
		if _, err := adb(serial, "install", "-r", path); err != nil {
			return err
		}
		// Ahead of time, all of it: steady-state code for each, rather than
		// whatever the JIT or a profile (Compose ships baseline profiles,
		// RyUI's host doesn't yet) has compiled by then.
		if _, err := adb(serial, "shell", "cmd", "package", "compile", "-m", "speed", "-f", a.id); err != nil {
			return err
		}
	}
	all, err := measure(apps, func(a app) ([]result, error) {
		adb(serial, "shell", "am", "force-stop", a.id)
		adb(serial, "logcat", "-c")
		time.Sleep(time.Second)
		logs := command(root, nil, "adb", "-s", serial, "logcat", "-v", "tag", "-s", "RYUIBENCH:I")
		out, err := logs.StdoutPipe()
		if err != nil {
			return nil, err
		}
		if err := logs.Start(); err != nil {
			return nil, err
		}
		defer func() { logs.Process.Kill(); logs.Wait() }()
		if _, err := adb(serial, "shell", "am", "start", "-n", a.id+"/"+a.activity); err != nil {
			return nil, err
		}
		return within(2*time.Minute, func() ([]result, error) { return collect(out) })
	})
	if err != nil {
		return err
	}
	return report("Android", strings.TrimSpace(model)+" emulator", apps, all)
}
