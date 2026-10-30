//go:build e2e

// Package e2e drives the example apps on a simulator and an emulator,
// through the platform's widgets, not the core. The tests build each app
// with `nui dev -once` (its Go code embedded, a debug build), then tap
// and read the screen through the debug hosts' automation
// (hosts/ios/Sources/NuiHost/Automation.swift, a notification;
// hosts/android/nui/src/main/java/dev/nui/Automation.kt, a broadcast).
//
//	go test -tags e2e ./e2e -run IOS      # a booted simulator, else the newest iPhone
//	go test -tags e2e ./e2e -run Android  # a running emulator or a device
//
// They wait for what they expect (bounded, so a broken app fails rather
// than hangs), never for a fixed time.
package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// within is how long a test waits for the screen to show what it expects.
const within = 60 * time.Second

func root(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// launch builds and launches example on the platform with nui dev -once,
// and returns what it printed.
func launch(t *testing.T, platform, example string) string {
	t.Helper()
	c := exec.Command("go", "run", "./cmd/nui", "dev", "-"+platform, "-once", "examples/"+example)
	c.Dir = root(t)
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("nui dev -%s -once examples/%s: %v\n%s", platform, example, err, out)
	}
	return string(out)
}

func run(t *testing.T, name string, args ...string) string {
	t.Helper()
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
	}
	return string(out)
}

// screen is what an app's screen shows, and input to it.
type screen interface {
	texts() []string
	tap(text string)
	command(cmds string) // the automation's: type <text>, submit...
	relaunch()           // quits the app and starts it again
}

// waitFor waits until the screen shows every one of want and none of
// not; it fails the test if it doesn't within the bound.
func waitFor(t *testing.T, s screen, want []string, not ...string) {
	t.Helper()
	deadline := time.Now().Add(within)
	var got []string
	for time.Now().Before(deadline) {
		got = s.texts()
		if hasAll(got, want) && !hasAny(got, not) {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("the screen doesn't show %q (without %q); it shows %q", want, not, got)
}

func hasAll(got, want []string) bool {
	for _, w := range want {
		if !hasAny(got, []string{w}) {
			return false
		}
	}
	return true
}

func hasAny(got, any []string) bool {
	for _, g := range got {
		for _, a := range any {
			if g == a {
				return true
			}
		}
	}
	return false
}

// counter is the same test on each platform: the counter counts, shows
// a note past ten, and resets.
func counter(t *testing.T, s screen) {
	waitFor(t, s, []string{"Count: 0", "Add", "Reset"}, "That's a lot")
	for i := 1; i <= 10; i++ {
		s.tap("Add")
		waitFor(t, s, []string{fmt.Sprintf("Count: %d", i)})
	}
	waitFor(t, s, []string{"That's a lot"})
	s.tap("Reset")
	waitFor(t, s, []string{"Count: 0"}, "That's a lot")
}

// notes is the same test on each platform: a note typed and submitted
// is written to SQLite and listed, and is still there when the app
// starts again.
func notes(t *testing.T, s screen) {
	waitFor(t, s, []string{"Add"})
	note := fmt.Sprintf("e2e note %d", time.Now().UnixNano())
	s.command("type " + note)
	s.command("submit")
	waitFor(t, s, []string{note})
	s.relaunch()
	waitFor(t, s, []string{"Add", note})
}

// navigation is the same test on each platform: a row pushes its
// screen; a button there presents a sheet, whose field and Save button
// write back to the screen; back pops it; a tab shows another stack.
func navigation(t *testing.T, s screen) {
	waitFor(t, s, []string{"Message 20", "Message 19"})
	s.tap("Message 20")
	waitFor(t, s, []string{"Add a note", "No note yet."})
	s.tap("Add a note")
	waitFor(t, s, []string{"Save", "Note"})
	s.command("type hello")
	s.tap("Save")
	waitFor(t, s, []string{"Note: hello"}, "Save")
	s.command("back")
	waitFor(t, s, []string{"Message 19"}, "Add a note", "Note: hello")
	s.command("tab Settings")
	waitFor(t, s, []string{"Notifications", "About"}, "Message 19")
	s.tap("About")
	waitFor(t, s, []string{"Native widgets, driven by Go and fine-grained signals."})
	s.command("back")
	waitFor(t, s, []string{"About"}, "Native widgets, driven by Go and fine-grained signals.")
	s.command("tab Inbox")
	waitFor(t, s, []string{"Message 19"}, "Notifications")
}

// iOS

type iosScreen struct {
	t      *testing.T
	udid   string
	bundle string
	tmp    string // the app's data container's tmp, where the automation reads and writes
}

func (s *iosScreen) command(cmds string) {
	if err := os.WriteFile(filepath.Join(s.tmp, "nui-commands.txt"), []byte(cmds+"\n"), 0o644); err != nil {
		s.t.Fatal(err)
	}
	run(s.t, "xcrun", "simctl", "spawn", s.udid, "notifyutil", "-p", "dev.nui.automation")
}

func (s *iosScreen) texts() []string {
	dump := filepath.Join(s.tmp, "nui-dump.txt")
	os.Remove(dump)
	s.command("dump")
	for range 50 {
		if b, err := os.ReadFile(dump); err == nil {
			return strings.Split(strings.TrimSpace(string(b)), "\n")
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil
}

func (s *iosScreen) tap(text string) { s.command("tap " + text) }

func (s *iosScreen) relaunch() {
	run(s.t, "xcrun", "simctl", "launch", "--terminate-running-process", s.udid, s.bundle)
}

func iosApp(t *testing.T, example, bundle string) *iosScreen {
	if _, err := exec.LookPath("xcrun"); err != nil {
		t.Skip("no Xcode")
	}
	out := launch(t, "ios", example)
	m := regexp.MustCompile(`\(([0-9A-F-]{36})\)`).FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("no simulator in:\n%s", out)
	}
	data := strings.TrimSpace(run(t, "xcrun", "simctl", "get_app_container", m[1], bundle, "data"))
	return &iosScreen{t: t, udid: m[1], bundle: bundle, tmp: filepath.Join(data, "tmp")}
}

func TestCounterIOS(t *testing.T) { counter(t, iosApp(t, "counter", "dev.nui.counter")) }
func TestNotesIOS(t *testing.T)   { notes(t, iosApp(t, "notes", "dev.nui.notes")) }
func TestNavigationIOS(t *testing.T) {
	navigation(t, iosApp(t, "navigation", "dev.nui.navigation"))
}

// Android

type androidScreen struct {
	t      *testing.T
	serial string
	pkg    string
}

func (s *androidScreen) adb(args ...string) (string, error) {
	out, err := exec.Command("adb", append([]string{"-s", s.serial}, args...)...).CombinedOutput()
	return string(out), err
}

func (s *androidScreen) command(cmds string) {
	// adb shell joins its arguments into a command line: quoted once more.
	q := "'" + strings.ReplaceAll(cmds, "'", `'\''`) + "'"
	// adb fails now and then just after an install: tried again.
	var out string
	var err error
	for range 5 {
		if out, err = s.adb("shell", "am", "broadcast", "-a", "dev.nui.automation", "-p", s.pkg, "--es", "commands", q); err == nil {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	s.t.Fatalf("broadcast %q: %v\n%s", cmds, err, out)
}

func (s *androidScreen) texts() []string {
	s.adb("shell", "run-as", s.pkg, "rm", "-f", "files/nui-dump.txt")
	s.command("dump")
	for range 50 {
		if out, err := s.adb("exec-out", "run-as", s.pkg, "cat", "files/nui-dump.txt"); err == nil && strings.HasSuffix(out, "\n") {
			return strings.Split(strings.TrimSpace(out), "\n")
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil
}

func (s *androidScreen) tap(text string) { s.command("tap " + text) }

func (s *androidScreen) relaunch() {
	s.adb("shell", "am", "force-stop", s.pkg)
	if out, err := s.adb("shell", "monkey", "-p", s.pkg, "-c", "android.intent.category.LAUNCHER", "1"); err != nil {
		s.t.Fatalf("launch: %v\n%s", err, out)
	}
}

func androidApp(t *testing.T, example, pkg string) *androidScreen {
	if _, err := exec.LookPath("adb"); err != nil {
		t.Skip("no adb")
	}
	out := launch(t, "android", example)
	m := regexp.MustCompile(`(?m)^nui: \S+ on (\S+)$`).FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("no device in:\n%s", out)
	}
	return &androidScreen{t: t, serial: m[1], pkg: pkg}
}

func TestCounterAndroid(t *testing.T) { counter(t, androidApp(t, "counter", "dev.nui.counter")) }
func TestNotesAndroid(t *testing.T)   { notes(t, androidApp(t, "notes", "dev.nui.notes")) }
func TestNavigationAndroid(t *testing.T) {
	navigation(t, androidApp(t, "navigation", "dev.nui.navigation"))
}
